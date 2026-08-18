// Package watch follows a live session transcript and judges thinking blocks
// the moment they are written, instead of waiting for the next hook to fire.
//
// The hook only runs before a tool call and at turn end. A model that keeps
// reasoning without calling anything never reaches either point, so a run of
// English blocks can accumulate unseen. The MCP server, unlike the hook, stays
// alive for the whole session, so a goroutine there can tail the transcript
// and settle each block as it lands.
//
// The watcher cannot stop the model — only a hook can deny a call. What it
// does is make the verdict already true by the time the hook runs, and record
// the full count rather than whatever a late single pass happens to catch.
package watch

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/session"
)

// Verdict is one judged thinking block.
type Verdict struct {
	SessionID string
	Model     string
	Verdict   lang.Verdict
	Excerpt   string
	At        time.Time
}

// Notifier carries a message from the watcher to the connected client. The
// server owns the stdio connection for the life of the session, so it is the
// one path out of this process that does not wait for a hook to be invoked.
type Notifier interface {
	Notify(verdict, excerpt string)
}

// Watcher tails every transcript under a projects directory.
type Watcher struct {
	projectsDir string
	store       *session.Store
	notifier    Notifier
	interval    time.Duration

	mu      sync.Mutex
	offsets map[string]int64
	recent  []Verdict
	stop    chan struct{}
	done    sync.WaitGroup

	// pending is the violation this server has not yet acted on itself. The
	// hook clears its own flag in the session file; this one is separate,
	// because the server enforces on its own calls without waiting for a hook.
	pending *Verdict
}

const (
	pollInterval  = 300 * time.Millisecond
	recentKeep    = 64
	excerptChars  = 160
	maxLineBuffer = 64 << 20
)

// New builds a watcher. notifier may be nil, in which case a violation is
// recorded and left for the next hook to act on.
func New(projectsDir string, store *session.Store, notifier Notifier) *Watcher {
	return &Watcher{
		projectsDir: projectsDir,
		store:       store,
		notifier:    notifier,
		interval:    pollInterval,
		offsets:     make(map[string]int64),
		stop:        make(chan struct{}),
	}
}

// Start runs the polling loop until Stop is called.
func (w *Watcher) Start() {
	w.done.Add(1)
	go func() {
		defer w.done.Done()
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-t.C:
				w.scan()
			}
		}
	}()
}

// Stop waits for the polling goroutine to finish. The caller flushes and
// closes the connection this watcher writes notifications on, so returning
// while a scan is still in flight would leave two writers on one stream.
func (w *Watcher) Stop() {
	close(w.stop)
	w.done.Wait()
}

// Recent returns the most recent verdicts, newest last.
func (w *Watcher) Recent(n int) []Verdict {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n <= 0 || n > len(w.recent) {
		n = len(w.recent)
	}
	out := make([]Verdict, n)
	copy(out, w.recent[len(w.recent)-n:])
	return out
}

// scan walks every transcript once, reading only what was appended since the
// previous pass. A transcript that shrank was rotated, so its offset resets.
func (w *Watcher) scan() {
	for _, path := range w.transcripts() {
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		w.mu.Lock()
		off, known := w.offsets[path]
		if !known {
			// Everything already on disk was written before this server
			// existed; judging it now would charge a past session's reasoning
			// against the call being made right now. Start at the end and
			// watch what arrives after.
			w.offsets[path] = st.Size()
		}
		w.mu.Unlock()
		if !known || st.Size() == off {
			continue
		}
		if st.Size() < off {
			off = 0
		}
		newOff := w.consume(path, off)
		w.mu.Lock()
		w.offsets[path] = newOff
		w.mu.Unlock()
	}
}

func (w *Watcher) transcripts() []string {
	entries, err := os.ReadDir(w.projectsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(w.projectsDir, e.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".jsonl") {
				out = append(out, filepath.Join(w.projectsDir, e.Name(), f.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

// consume reads from off to the end, judging each complete line, and returns
// the offset just past the last complete line. A trailing partial line is left
// unread so the next pass sees it whole.
func (w *Watcher) consume(path string, off int64) int64 {
	f, err := os.Open(path)
	if err != nil {
		return off
	}
	defer f.Close()
	if _, err := f.Seek(off, 0); err != nil {
		return off
	}

	r := bufio.NewReaderSize(f, 1<<20)
	consumed := off
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// No newline yet: this line is still being written.
			break
		}
		consumed += int64(len(line))
		if len(line) > maxLineBuffer {
			continue
		}
		w.judge(line)
	}
	return consumed
}

type entry struct {
	Type    string `json:"type"`
	Message struct {
		Model   string `json:"model"`
		Content []struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		} `json:"content"`
	} `json:"message"`
	SessionID string `json:"sessionId"`
}

// judge classifies every thinking block on one transcript line and folds the
// result into that session's running state.
func (w *Watcher) judge(line []byte) {
	var e entry
	if err := json.Unmarshal(line, &e); err != nil {
		return
	}
	if e.Type != "assistant" || e.Message.Model == "<synthetic>" {
		return
	}
	for _, b := range e.Message.Content {
		if b.Type != "thinking" || strings.TrimSpace(b.Thinking) == "" {
			continue
		}
		v := lang.Classify(b.Thinking)
		if v == lang.VerdictEnglish && strings.HasPrefix(e.Message.Model, "claude-sonnet") {
			continue // documented Sonnet5 defect; JA still counts
		}
		if v != lang.VerdictEnglish && v != lang.VerdictJapanse {
			continue
		}
		w.record(Verdict{
			SessionID: e.SessionID,
			Model:     e.Message.Model,
			Verdict:   v,
			Excerpt:   excerpt(b.Thinking, v),
			At:        time.Now(),
		})
	}
}

// TakePending returns the violation the server has not yet acted on, clearing
// it as it hands it over. A flag left standing would refuse every later call.
func (w *Watcher) TakePending() *Verdict {
	w.mu.Lock()
	defer w.mu.Unlock()
	v := w.pending
	w.pending = nil
	return v
}

func (w *Watcher) record(v Verdict) {
	w.mu.Lock()
	w.recent = append(w.recent, v)
	if len(w.recent) > recentKeep {
		w.recent = w.recent[len(w.recent)-recentKeep:]
	}
	pv := v
	w.pending = &pv
	w.mu.Unlock()

	if v.SessionID == "" || w.store == nil {
		return
	}
	// Streak and LastVerdict belong to the hook: it resets them when a turn
	// comes back clean, and a second writer would make its repeat count wrong.
	// The watcher owns only its own tally.
	st := w.store.Load(v.SessionID)
	if v.Verdict == lang.VerdictJapanse {
		st.WatchJA++
	} else {
		st.WatchEN++
	}
	st.WatchSeen++
	st.WatchBlock = true
	st.WatchVerdict = string(v.Verdict)
	st.WatchQuote = v.Excerpt
	if st.Model == "" {
		st.Model = v.Model
	}
	_ = w.store.Save(st)

	// Sent after the state is on disk, so a client that ignores the message
	// still leaves the tally and the flag for the next hook to act on.
	if w.notifier != nil {
		w.notifier.Notify(string(v.Verdict), v.Excerpt)
	}
}

func excerpt(text string, v lang.Verdict) string {
	if v == lang.VerdictEnglish {
		if spans := lang.EnglishSpans(text, 1); len(spans) > 0 {
			return truncate(spans[0], excerptChars)
		}
	}
	return truncate(strings.Join(strings.Fields(text), " "), excerptChars)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
