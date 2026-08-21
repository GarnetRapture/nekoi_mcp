package watch

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/session"
	"nekoi_mcp/internal/sig"
)

type Verdict struct {
	SessionID string
	Model     string
	Verdict   lang.Verdict
	Excerpt   string
	At        time.Time
}

type Notifier interface {
	Notify(method string, params any) error
}

type Watcher struct {
	projectsDir string
	offsetDir   string
	store       *session.Store
	interval    time.Duration

	mu       sync.Mutex
	notifier Notifier
	offsets  map[string]int64
	recent   []Verdict
	stop     chan struct{}
	done     sync.WaitGroup

	pending *Verdict
}

const (
	pollInterval  = 300 * time.Millisecond
	recentKeep    = 64
	excerptChars  = 160
	maxLineBuffer = 64 << 20
	offsetChars   = 32
)

func New(projectsDir, offsetDir string, store *session.Store) *Watcher {
	return &Watcher{
		projectsDir: projectsDir,
		offsetDir:   offsetDir,
		store:       store,
		interval:    pollInterval,
		offsets:     make(map[string]int64),
		stop:        make(chan struct{}),
	}
}

func (w *Watcher) SetNotifier(n Notifier) {
	w.mu.Lock()
	w.notifier = n
	w.mu.Unlock()
}

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

func (w *Watcher) Stop() {
	close(w.stop)
	w.done.Wait()
}

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

func (w *Watcher) scan() {
	for _, path := range w.transcripts() {
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		w.mu.Lock()
		off, known := w.offsets[path]
		w.mu.Unlock()
		if !known {
			off = w.startOffset(path, st.Size())
			w.mu.Lock()
			w.offsets[path] = off
			w.mu.Unlock()
			w.saveOffset(path, off)
		}
		if st.Size() == off {
			continue
		}
		if st.Size() < off {
			off = 0
		}
		newOff := w.consume(path, off)
		w.mu.Lock()
		w.offsets[path] = newOff
		w.mu.Unlock()
		w.saveOffset(path, newOff)
	}
}

func (w *Watcher) startOffset(path string, size int64) int64 {
	if off, ok := w.loadOffset(path); ok {
		if off > size {
			return size
		}
		return off
	}
	return size
}

func (w *Watcher) offsetPath(transcript string) string {
	sum := sha256.Sum256([]byte(sig.NormalizePath(transcript)))
	return filepath.Join(w.offsetDir, hex.EncodeToString(sum[:])[:offsetChars])
}

func (w *Watcher) loadOffset(transcript string) (int64, bool) {
	b, err := os.ReadFile(w.offsetPath(transcript))
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (w *Watcher) saveOffset(transcript string, off int64) {
	if w.offsetDir == "" {
		return
	}
	if err := os.MkdirAll(w.offsetDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(w.offsetPath(transcript), []byte(strconv.FormatInt(off, 10)), 0o644)
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
			v = lang.VerdictKorean
		}
		if v != lang.VerdictEnglish && v != lang.VerdictJapanse && v != lang.VerdictKorean {
			continue
		}
		w.record(Verdict{
			SessionID: e.SessionID,
			Model:     e.Message.Model,
			Verdict:   v,
			Excerpt:   excerpt(b.Thinking, v),
			At:        time.Now(),
		}, sig.Text(b.Thinking))
	}
}

func (w *Watcher) TakePending() *Verdict {
	w.mu.Lock()
	defer w.mu.Unlock()
	v := w.pending
	w.pending = nil
	return v
}

func (w *Watcher) record(v Verdict, blockSig string) {
	if v.Verdict != lang.VerdictKorean {
		w.mu.Lock()
		w.recent = append(w.recent, v)
		if len(w.recent) > recentKeep {
			w.recent = w.recent[len(w.recent)-recentKeep:]
		}
		pv := v
		w.pending = &pv
		w.mu.Unlock()
	}

	if v.SessionID == "" || w.store == nil {
		return
	}
	st := w.store.Load(v.SessionID)
	if st.Model == "" {
		st.Model = v.Model
	}
	if !st.MarkJudged(blockSig) {
		return
	}
	st.LastVerdict = string(v.Verdict)

	if v.Verdict == lang.VerdictKorean {
		st.Streak = 0
		st.PendingBlock = false
		st.PendingReason = ""
		_ = w.store.Save(st)
		return
	}

	if v.Verdict == lang.VerdictJapanse {
		st.WatchJA++
		st.JACount++
	} else {
		st.WatchEN++
		st.ENCount++
	}
	st.WatchSeen++
	st.Streak++
	st.PendingBlock = true
	st.PendingReason = fmt.Sprintf(
		"[LIVE_WATCH: THINKING_NOT_KOREAN/%s — block #%d this session]\n> %s\n"+
			"That line is from a thinking block of this session, read from the transcript as it was written rather than at a tool call. Reasoning is covered by the check whether or not a tool follows it.\n"+
			"The rule for this project is that reasoning is written in Korean. This block stays flagged until a Korean thinking block appears in the transcript.",
		verdictLabel(v.Verdict), flaggedCount(st, v.Verdict), v.Excerpt)
	_ = w.store.Save(st)

	w.alert(st.PendingReason)
}

func (w *Watcher) alert(reason string) {
	w.mu.Lock()
	n := w.notifier
	w.mu.Unlock()
	if n == nil {
		return
	}
	_ = n.Notify("notifications/message", map[string]any{
		"level":  "error",
		"logger": "nekoi_mcp",
		"data":   reason,
	})
}

func verdictLabel(v lang.Verdict) string {
	if v == lang.VerdictJapanse {
		return "JA"
	}
	return "EN"
}

func flaggedCount(st *session.State, v lang.Verdict) int {
	if v == lang.VerdictJapanse {
		return st.JACount
	}
	return st.ENCount
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
