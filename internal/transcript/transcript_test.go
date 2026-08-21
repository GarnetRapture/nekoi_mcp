package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTranscript(t *testing.T, records []map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b strings.Builder
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func userPrompt(text string) map[string]any {
	return map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": []map[string]any{{"type": "text", "text": text}}},
	}
}

func assistant(blocks ...map[string]any) map[string]any {
	return map[string]any{
		"type":    "assistant",
		"message": map[string]any{"model": "claude-opus-5", "content": blocks},
	}
}

func toolResult(id string, isErr bool, content string) map[string]any {
	return map[string]any{
		"type": "user",
		"message": map[string]any{"role": "user", "content": []map[string]any{{
			"type": "tool_result", "tool_use_id": id, "is_error": isErr, "content": content,
		}}},
	}
}

func TestLoadAssignsThoughtSignatures(t *testing.T) {
	path := writeTranscript(t, []map[string]any{
		userPrompt("고쳐라"),
		assistant(map[string]any{"type": "thinking", "thinking": "원인부터 확인한다."}),
		assistant(map[string]any{"type": "thinking", "thinking": "다음 단계로 넘어간다."}),
	})
	turn, err := Load(path, 100)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(turn.Thoughts) != 2 {
		t.Fatalf("parsed %d thoughts, want 2", len(turn.Thoughts))
	}
	for i, th := range turn.Thoughts {
		if th.Sig == "" {
			t.Fatalf("thought %d carries no signature", i)
		}
	}
	if turn.Thoughts[0].Sig == turn.Thoughts[1].Sig {
		t.Fatal("two different blocks hashed to the same signature")
	}
}

func TestLoadSplitsAssistantTextIntoSignedBlocks(t *testing.T) {
	path := writeTranscript(t, []map[string]any{
		userPrompt("보고해라"),
		assistant(map[string]any{"type": "text", "text": "첫 블록"}),
		assistant(map[string]any{"type": "text", "text": "둘째 블록"}),
	})
	turn, err := Load(path, 100)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(turn.AssistantBlocks) != 2 {
		t.Fatalf("parsed %d text blocks, want 2", len(turn.AssistantBlocks))
	}
	if turn.AssistantTxt != "첫 블록\n둘째 블록" {
		t.Fatalf("AssistantTxt is %q", turn.AssistantTxt)
	}
}

func TestLoadSkipsFailedToolCalls(t *testing.T) {
	path := writeTranscript(t, []map[string]any{
		userPrompt("실행해라"),
		assistant(map[string]any{
			"type": "tool_use", "id": "t1", "name": "Bash",
			"input": map[string]any{"command": "ls"},
		}),
		toolResult("t1", true, "denied"),
		assistant(map[string]any{
			"type": "tool_use", "id": "t2", "name": "Bash",
			"input": map[string]any{"command": "ls"},
		}),
		toolResult("t2", false, "ok"),
	})
	turn, err := Load(path, 100)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if turn.ToolCalls != 1 {
		t.Fatalf("counted %d calls, want 1 (the denied one must not count)", turn.ToolCalls)
	}
	if len(turn.CallSigs) != 1 {
		t.Fatalf("recorded %d signatures, want 1", len(turn.CallSigs))
	}
}

func TestLoadCollectsEditedPathsIntoEvidence(t *testing.T) {
	path := writeTranscript(t, []map[string]any{
		userPrompt("수정해라"),
		assistant(map[string]any{
			"type": "tool_use", "id": "e1", "name": "Edit",
			"input": map[string]any{"file_path": `N:\nekoi_mcp\internal\rules\rules.go`},
		}),
		toolResult("e1", false, "updated"),
	})
	turn, err := Load(path, 100)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if turn.Edited != 1 {
		t.Fatalf("Edited is %d, want 1", turn.Edited)
	}
	if len(turn.EditFiles) != 1 {
		t.Fatalf("EditFiles holds %d entries, want 1", len(turn.EditFiles))
	}
	if !strings.Contains(turn.Evidence, "internal/rules/rules.go") {
		t.Fatalf("the edited path did not reach Evidence in normalized form: %q", turn.Evidence)
	}
}

func TestLoadCountsThoughtsBeforeTheLastPrompt(t *testing.T) {
	path := writeTranscript(t, []map[string]any{
		userPrompt("첫 지시"),
		assistant(map[string]any{"type": "thinking", "thinking": "이전 턴의 사고."}),
		userPrompt("둘째 지시"),
		assistant(map[string]any{"type": "thinking", "thinking": "이번 턴의 사고."}),
	})
	turn, err := Load(path, 100)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(turn.Thoughts) != 1 {
		t.Fatalf("this turn holds %d thoughts, want 1", len(turn.Thoughts))
	}
	if turn.TotalThought != 2 {
		t.Fatalf("TotalThought is %d, want 2", turn.TotalThought)
	}
	if turn.UserPrompt != "둘째 지시" {
		t.Fatalf("UserPrompt is %q", turn.UserPrompt)
	}
}

func TestLoadIgnoresSyntheticMessages(t *testing.T) {
	path := writeTranscript(t, []map[string]any{
		userPrompt("지시"),
		{
			"type":    "assistant",
			"message": map[string]any{"model": "<synthetic>", "content": []map[string]any{{"type": "thinking", "thinking": "synthetic noise"}}},
		},
	})
	turn, err := Load(path, 100)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(turn.Thoughts) != 0 || turn.TotalThought != 0 {
		t.Fatalf("a synthetic message was parsed: %d/%d", len(turn.Thoughts), turn.TotalThought)
	}
}
