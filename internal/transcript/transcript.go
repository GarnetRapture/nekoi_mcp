package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"nekoi_mcp/internal/sig"
)

// Entry is one JSONL record of a Claude Code session transcript.
type Entry struct {
	Type      string  `json:"type"`
	IsMeta    bool    `json:"isMeta"`
	Timestamp string  `json:"timestamp"`
	Message   Message `json:"message"`
}

type Message struct {
	Model   string          `json:"model"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   *Usage          `json:"usage"`
}

// Usage is the billed API accounting Claude Code records for each assistant
// message. These are the real network-side numbers, not an estimate derived
// from transcript text.
type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// Billed is the total charged for one request: fresh input, cache writes,
// cache reads and output.
func (u Usage) Billed() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// Context approximates how large the prompt has grown, which is what a notice
// injected now will be charged against on every later request.
func (u Usage) Context() int64 {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

type Block struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`

	// ID identifies a tool_use block; ToolUseID is the reference back to it
	// carried by the tool_result that answers it, and IsError marks that
	// answer as a failure. A call a hook denied never executed, so its result
	// is an error and its arguments produced no observation.
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

// Thought is one assistant thinking block with the model that produced it.
type Thought struct {
	Model string
	Text  string
}

// Turn holds everything the rules need from the transcript: the last real
// user prompt and every assistant block that followed it.
type Turn struct {
	UserPrompt   string
	Thoughts     []Thought
	AssistantTxt string
	HasToolUse   bool
	TotalThought int
	Model        string

	// LastUsage is the billed accounting of the most recent request, and
	// ContextTokens is how large the prompt had grown by then. Both come
	// from the API response, not from measuring transcript text.
	LastUsage     Usage
	ContextTokens int64
	OutputTokens  int64

	// Evidence is everything this turn actually observed: tool inputs, tool
	// results and the user's own prompt. A claim in AssistantTxt that cannot
	// be traced to this text was not observed but asserted.
	Evidence string
	Edited   int // Edit / Write / NotebookEdit calls
	Probed   int // Read / Grep / Glob calls
	Bashed   int // Bash / PowerShell calls

	ToolCalls int      // every tool_use this turn
	CallSigs  []string // canonical signature of each call this turn
	ProbeKeys []string // environment probes, session-wide
	EditFiles []string // paths edited this turn
}

func (m Message) blocks() []Block {
	if len(m.Content) == 0 {
		return nil
	}
	var arr []Block
	if err := json.Unmarshal(m.Content, &arr); err == nil {
		return arr
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil && s != "" {
		return []Block{{Type: "text", Text: s}}
	}
	return nil
}

// isUserPrompt reports whether the entry is a real user message rather than a
// tool result or a meta record. Tool results carry no text block.
func (e Entry) isUserPrompt() bool {
	if e.Type != "user" || e.IsMeta {
		return false
	}
	for _, b := range e.Message.blocks() {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

// failedCalls returns the ids of the tool_use blocks whose result came back an
// error. A result always follows the call that produced it, so the whole range
// is read before any call is tallied.
func failedCalls(ring []Entry) map[string]bool {
	failed := make(map[string]bool)
	for _, e := range ring {
		if e.Type != "user" {
			continue
		}
		for _, b := range e.Message.blocks() {
			if b.Type == "tool_result" && b.IsError && b.ToolUseID != "" {
				failed[b.ToolUseID] = true
			}
		}
	}
	return failed
}

func (e Entry) userText() string {
	var parts []string
	for _, b := range e.Message.blocks() {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Load reads the tail of a transcript and extracts the current turn. maxLines
// bounds the work: only the last maxLines records are parsed.
func Load(path string, maxLines int) (*Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ring := make([]Entry, 0, maxLines)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if len(ring) == maxLines {
			copy(ring, ring[1:])
			ring = ring[:maxLines-1]
		}
		ring = append(ring, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	t := &Turn{}
	start := 0
	for i := len(ring) - 1; i >= 0; i-- {
		if ring[i].isUserPrompt() {
			t.UserPrompt = ring[i].userText()
			start = i + 1
			break
		}
	}

	// A call that was refused still leaves its tool_use block behind, answered
	// by an error result. It never ran, so it observed nothing and consumed
	// nothing; counting it would let one refusal mark the arguments as already
	// tried and turn every reissue into a duplicate.
	failed := failedCalls(ring)

	var txt, evidence []string
	if t.UserPrompt != "" {
		evidence = append(evidence, t.UserPrompt)
	}
	for i, e := range ring {
		if e.Type == "user" && i >= start {
			for _, b := range e.Message.blocks() {
				if b.Type == "tool_result" && len(b.Content) > 0 {
					evidence = append(evidence, string(b.Content))
				}
			}
		}
		if e.Type != "assistant" || e.Message.Model == "<synthetic>" {
			continue
		}
		if u := e.Message.Usage; u != nil {
			t.LastUsage = *u
			if c := u.Context(); c > t.ContextTokens {
				t.ContextTokens = c
			}
			t.OutputTokens += u.OutputTokens
		}
		for _, b := range e.Message.blocks() {
			switch b.Type {
			case "thinking":
				if strings.TrimSpace(b.Thinking) == "" {
					continue
				}
				t.TotalThought++
				t.Model = e.Message.Model
				if i >= start {
					t.Thoughts = append(t.Thoughts, Thought{Model: e.Message.Model, Text: b.Thinking})
				}
			case "text":
				if i >= start {
					txt = append(txt, b.Text)
				}
			case "tool_use":
				if failed[b.ID] {
					continue
				}
				if k := sig.Probe(b.Name, b.Input); k != "" {
					t.ProbeKeys = append(t.ProbeKeys, k) // session-wide
				}
				if i < start {
					continue
				}
				t.HasToolUse = true
				t.ToolCalls++
				t.CallSigs = append(t.CallSigs, sig.Call(b.Name, b.Input))
				if b.Name == "Edit" || b.Name == "Write" || b.Name == "NotebookEdit" {
					var fp struct {
						FilePath string `json:"file_path"`
					}
					_ = json.Unmarshal(b.Input, &fp)
					if fp.FilePath != "" {
						t.EditFiles = append(t.EditFiles, fp.FilePath)
					}
				}
				if len(b.Input) > 0 {
					evidence = append(evidence, string(b.Input))
				}
				switch b.Name {
				case "Edit", "Write", "NotebookEdit":
					t.Edited++
				case "Read", "Grep", "Glob":
					t.Probed++
				case "Bash", "PowerShell":
					t.Bashed++
				}
			}
		}
	}
	t.AssistantTxt = strings.Join(txt, "\n")
	t.Evidence = sig.NormalizePath(strings.Join(evidence, "\n"))
	return t, nil
}
