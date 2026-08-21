package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/session"
)

func assistantLine(t *testing.T, sessionID, model, thinking string) string {
	t.Helper()
	rec := map[string]any{
		"type":      "assistant",
		"sessionId": sessionID,
		"message": map[string]any{
			"model":   model,
			"content": []map[string]any{{"type": "thinking", "thinking": thinking}},
		},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(b) + "\n"
}

func newFixture(t *testing.T) (projects, offsets, transcript string, store *session.Store) {
	t.Helper()
	root := t.TempDir()
	projects = filepath.Join(root, "projects")
	offsets = filepath.Join(root, "offsets")
	dir := filepath.Join(projects, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	transcript = filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(transcript, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return projects, offsets, transcript, session.NewStore(filepath.Join(root, "state"))
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
}

func TestScanPersistsOffsetOnFirstSight(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	appendLine(t, transcript, assistantLine(t, "s1", "claude-opus-5", "I will check the file first because it matters."))

	w := New(projects, offsets, store)
	w.scan()

	entries, err := os.ReadDir(offsets)
	if err != nil {
		t.Fatalf("the offset directory was never created: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("offset directory holds %d files, want 1", len(entries))
	}
	if st := store.Load("s1"); st.ENCount != 0 {
		t.Fatalf("pre-existing content was judged: ENCount=%d", st.ENCount)
	}
}

func TestRestartDoesNotRejudgeOldLines(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	first := New(projects, offsets, store)
	first.scan()

	appendLine(t, transcript, assistantLine(t, "s1", "claude-opus-5", "I need to read the file before deciding what to do."))
	first.scan()

	after := store.Load("s1")
	if after.ENCount != 1 {
		t.Fatalf("ENCount is %d after one English block, want 1", after.ENCount)
	}

	restarted := New(projects, offsets, store)
	restarted.scan()

	again := store.Load("s1")
	if again.ENCount != 1 {
		t.Fatalf("a restart re-counted old lines: ENCount=%d, want 1", again.ENCount)
	}
}

func TestJudgeCountsEachBlockOnce(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	w := New(projects, offsets, store)
	w.scan()

	line := assistantLine(t, "s1", "claude-opus-5", "The cause is not clear so I will read the file again.")
	appendLine(t, transcript, line)
	appendLine(t, transcript, line)
	w.scan()

	st := store.Load("s1")
	if st.ENCount != 1 {
		t.Fatalf("an identical block was counted %d times, want 1", st.ENCount)
	}
}

func TestKoreanBlockClearsPending(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	w := New(projects, offsets, store)
	w.scan()

	appendLine(t, transcript, assistantLine(t, "s1", "claude-opus-5", "I will read the target file before changing it."))
	w.scan()
	if st := store.Load("s1"); !st.PendingBlock {
		t.Fatal("an English block did not raise the pending flag")
	}

	appendLine(t, transcript, assistantLine(t, "s1", "claude-opus-5", "대상 파일을 먼저 읽고 원인을 확인한 뒤 수정한다."))
	w.scan()

	st := store.Load("s1")
	if st.PendingBlock || st.PendingReason != "" {
		t.Fatalf("a Korean block left the flag raised: %+v", st.PendingBlock)
	}
	if st.Streak != 0 {
		t.Fatalf("streak is %d after a Korean block, want 0", st.Streak)
	}
}

func TestSonnetEnglishIsExempt(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	w := New(projects, offsets, store)
	w.scan()

	appendLine(t, transcript, assistantLine(t, "s1", "claude-sonnet-5", "I will read the target file before changing it."))
	w.scan()

	st := store.Load("s1")
	if st.ENCount != 0 || st.PendingBlock {
		t.Fatalf("a Sonnet English block was charged: EN=%d pending=%v", st.ENCount, st.PendingBlock)
	}
}

func TestPartialLineIsLeftForTheNextPass(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	w := New(projects, offsets, store)
	w.scan()

	full := assistantLine(t, "s1", "claude-opus-5", "I will inspect the failing case before editing anything.")
	appendLine(t, transcript, full[:len(full)/2])
	w.scan()
	if st := store.Load("s1"); st.ENCount != 0 {
		t.Fatalf("a half-written line was judged: ENCount=%d", st.ENCount)
	}

	appendLine(t, transcript, full[len(full)/2:])
	w.scan()
	if st := store.Load("s1"); st.ENCount != 1 {
		t.Fatalf("the completed line was not judged: ENCount=%d", st.ENCount)
	}
}

func TestRecentHoldsFlaggedVerdictsOnly(t *testing.T) {
	projects, offsets, transcript, store := newFixture(t)
	w := New(projects, offsets, store)
	w.scan()

	appendLine(t, transcript, assistantLine(t, "s1", "claude-opus-5", "한국어로 먼저 원인을 확인한다."))
	appendLine(t, transcript, assistantLine(t, "s1", "claude-opus-5", "I will check the caller before editing the type."))
	w.scan()

	rows := w.Recent(10)
	if len(rows) != 1 {
		t.Fatalf("Recent holds %d rows, want 1", len(rows))
	}
	if rows[0].Verdict != lang.VerdictEnglish {
		t.Fatalf("Recent holds verdict %q, want EN", rows[0].Verdict)
	}
	if v := w.TakePending(); v == nil {
		t.Fatal("TakePending returned nothing after a flagged block")
	}
	if v := w.TakePending(); v != nil {
		t.Fatal("TakePending handed the same verdict out twice")
	}
}
