package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const JudgedKeep = 512

type State struct {
	SessionID   string    `json:"session_id"`
	Model       string    `json:"model"`
	CWD         string    `json:"cwd"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ENCount     int       `json:"en_count"`
	JACount     int       `json:"ja_count"`
	DenyCount   int       `json:"deny_count"`
	ToolCalls   int       `json:"tool_calls"`
	Streak      int       `json:"streak"`
	LastVerdict string    `json:"last_verdict"`
	RepeatSig   string    `json:"repeat_sig"`
	RepeatCount int       `json:"repeat_count"`

	AuditedEdits int `json:"audited_edits"`

	JudgedThoughts []string `json:"judged_thoughts"`
	ReportedTexts  []string `json:"reported_texts"`

	WatchEN   int `json:"watch_en"`
	WatchJA   int `json:"watch_ja"`
	WatchSeen int `json:"watch_seen"`

	PendingBlock  bool   `json:"pending_block"`
	PendingReason string `json:"pending_reason"`

	ContextTokens int64 `json:"context_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	InjectedChars int64 `json:"injected_chars"`
	Notices       int   `json:"notices"`
}

func (s *State) Judged(sig string) bool {
	return contains(s.JudgedThoughts, sig)
}

func (s *State) MarkJudged(sig string) bool {
	if sig == "" || contains(s.JudgedThoughts, sig) {
		return false
	}
	s.JudgedThoughts = push(s.JudgedThoughts, sig)
	return true
}

func (s *State) MarkReported(sig string) bool {
	if sig == "" || contains(s.ReportedTexts, sig) {
		return false
	}
	s.ReportedTexts = push(s.ReportedTexts, sig)
	return true
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func push(list []string, sig string) []string {
	list = append(list, sig)
	if len(list) > JudgedKeep {
		list = append([]string(nil), list[len(list)-JudgedKeep:]...)
	}
	return list
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
