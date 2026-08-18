package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State is the per-session record the censor keeps between hook invocations.
// One file per session; every hook run reads it, mutates it, writes it back.
type State struct {
	SessionID   string    `json:"session_id"`
	Model       string    `json:"model"`
	CWD         string    `json:"cwd"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ENCount     int       `json:"en_count"`   // English thinking blocks flagged
	JACount     int       `json:"ja_count"`   // Japanese thinking blocks flagged
	DenyCount   int       `json:"deny_count"` // tool calls actually denied
	ToolCalls   int       `json:"tool_calls"` // tool calls seen
	Cursor      int       `json:"cursor"`     // thinking blocks already judged
	Streak      int       `json:"streak"`     // consecutive EN verdicts
	LastVerdict string    `json:"last_verdict"`
	RepeatSig   string    `json:"repeat_sig"`   // signature of the previous call
	RepeatCount int       `json:"repeat_count"` // identical consecutive calls

	// The watcher tails the transcript continuously and sees blocks the hook
	// never reaches, because the hook only runs before a tool call and at turn
	// end. Its tally is kept apart from ENCount/JACount so the two never add
	// the same block twice; WatchSeen is how far it has read.
	WatchEN   int `json:"watch_en"`
	WatchJA   int `json:"watch_ja"`
	WatchSeen int `json:"watch_seen"`

	// WatchBlock is raised the moment the watcher reads a violating block and
	// is cleared by the first hook that acts on it. It carries the verdict and
	// the offending sentence so the denial quotes what was actually written
	// rather than asserting a violation the model cannot see.
	WatchBlock   bool   `json:"watch_block"`
	WatchVerdict string `json:"watch_verdict"`
	WatchQuote   string `json:"watch_quote"`

	// Billed API traffic, read from the usage accounting Claude Code stores
	// for each assistant message. ContextTokens is the prompt size the last
	// request was charged for, which is what an injected notice adds to on
	// every subsequent request.
	ContextTokens int64 `json:"context_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	InjectedChars int64 `json:"injected_chars"`
	Notices       int   `json:"notices"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) path(id string) string {
	if id == "" {
		id = "nosession"
	}
	safe := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			safe = append(safe, r)
		default:
			safe = append(safe, '_')
		}
	}
	if len(safe) > 80 {
		safe = safe[:80]
	}
	return filepath.Join(s.dir, string(safe)+".json")
}

func (s *Store) Load(id string) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := &State{SessionID: id, StartedAt: time.Now()}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return st
	}
	if err := json.Unmarshal(b, st); err != nil {
		return &State{SessionID: id, StartedAt: time.Now()}
	}
	return st
}

func (s *Store) Save(st *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	st.UpdatedAt = time.Now()
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := s.path(st.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(st.SessionID))
}

// List returns every session record currently on disk, newest first.
func (s *Store) List() []*State {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []*State
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var st State
		if err := json.Unmarshal(b, &st); err != nil {
			continue
		}
		out = append(out, &st)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt.After(out[i].UpdatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
