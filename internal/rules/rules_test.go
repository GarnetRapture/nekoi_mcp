package rules

import (
	"strings"
	"testing"

	"nekoi_mcp/internal/session"
	"nekoi_mcp/internal/sig"
	"nekoi_mcp/internal/transcript"
)

func thought(model, text string) transcript.Thought {
	return transcript.Thought{Model: model, Text: text, Sig: sig.Text(text)}
}

const (
	koreanBlock  = "사용자 지시대로 파일을 먼저 읽고 원인을 확인한다."
	englishBlock = "I need to check the file first because the cause is not clear yet."
)

func TestFreshThoughtsSkipsJudgedBlocks(t *testing.T) {
	st := &session.State{}
	a := thought("claude-opus-5", koreanBlock)
	b := thought("claude-opus-5", englishBlock)
	turn := &transcript.Turn{Thoughts: []transcript.Thought{a, b}}

	first := FreshThoughts(st, turn)
	if len(first) != 2 {
		t.Fatalf("first pass saw %d blocks, want 2", len(first))
	}
	EvaluateThinking(st, first)

	if again := FreshThoughts(st, turn); len(again) != 0 {
		t.Fatalf("second pass re-offered %d already judged blocks", len(again))
	}
}

func TestFreshThoughtsSurvivesWindowReset(t *testing.T) {
	st := &session.State{}
	old := thought("claude-opus-5", koreanBlock)
	EvaluateThinking(st, []transcript.Thought{old})

	fresh := thought("claude-opus-5", "다음 단계는 테스트를 추가하는 것이다.")
	turn := &transcript.Turn{Thoughts: []transcript.Thought{fresh}}
	got := FreshThoughts(st, turn)
	if len(got) != 1 || got[0].Sig != fresh.Sig {
		t.Fatalf("a new block after a window reset was not offered: %+v", got)
	}
}

func TestEvaluateThinkingFlagsEnglishAndRaisesPending(t *testing.T) {
	st := &session.State{}
	r := EvaluateThinking(st, []transcript.Thought{thought("claude-opus-5", englishBlock)})
	if r == nil || !r.Deny {
		t.Fatalf("an English block produced %+v", r)
	}
	if !st.PendingBlock || st.PendingReason == "" {
		t.Fatal("the pending flag was not raised")
	}
	if st.ENCount != 1 {
		t.Fatalf("ENCount is %d, want 1", st.ENCount)
	}
}

func TestEvaluateThinkingClearsPendingOnKoreanBlock(t *testing.T) {
	st := &session.State{PendingBlock: true, PendingReason: "raised by the watcher", Streak: 3}
	if r := EvaluateThinking(st, []transcript.Thought{thought("claude-opus-5", koreanBlock)}); r != nil {
		t.Fatalf("a Korean block produced %+v", r)
	}
	if st.PendingBlock || st.PendingReason != "" {
		t.Fatal("a Korean block left the pending flag raised")
	}
	if st.Streak != 0 {
		t.Fatalf("streak is %d, want 0", st.Streak)
	}
}

func TestEvaluateThinkingLeavesPendingWhenNoFreshBlocks(t *testing.T) {
	st := &session.State{PendingBlock: true, PendingReason: "raised by the watcher"}
	if r := EvaluateThinking(st, nil); r != nil {
		t.Fatalf("an empty block set produced %+v", r)
	}
	if !st.PendingBlock {
		t.Fatal("the pending flag was cleared without any Korean block")
	}
	p := EvaluatePending(st)
	if p == nil || !p.Deny || p.Message != "raised by the watcher" {
		t.Fatalf("EvaluatePending returned %+v", p)
	}
}

func TestEvaluateThinkingExemptsSonnetEnglish(t *testing.T) {
	st := &session.State{PendingBlock: true, PendingReason: "raised"}
	if r := EvaluateThinking(st, []transcript.Thought{thought("claude-sonnet-5", englishBlock)}); r != nil {
		t.Fatalf("a Sonnet English block produced %+v", r)
	}
	if st.ENCount != 0 {
		t.Fatalf("ENCount is %d, want 0", st.ENCount)
	}
	if st.PendingBlock {
		t.Fatal("the Sonnet exemption did not clear the pending flag")
	}
}

func TestEvaluateNoThinkingUsesBlockCountNotFreshness(t *testing.T) {
	if r := EvaluateNoThinking(0, true); r == nil || !r.Deny {
		t.Fatalf("an acted turn with zero blocks produced %+v", r)
	}
	if r := EvaluateNoThinking(3, true); r != nil {
		t.Fatalf("a turn that holds three blocks produced %+v", r)
	}
	if r := EvaluateNoThinking(0, false); r != nil {
		t.Fatalf("the first call of a turn produced %+v", r)
	}
}

func TestEvaluateRepeatSkipsCallsDeniedForAnotherReason(t *testing.T) {
	st := &session.State{}
	for i := 0; i < 5; i++ {
		if r := EvaluateRepeat(st, "sig-a", true); r != nil {
			t.Fatalf("an already denied call was counted as a repeat: %+v", r)
		}
	}
	if st.RepeatCount != 0 {
		t.Fatalf("RepeatCount is %d, want 0", st.RepeatCount)
	}
}

func TestEvaluateRepeatFiresOnThirdIdenticalCall(t *testing.T) {
	st := &session.State{}
	if r := EvaluateRepeat(st, "sig-a", false); r != nil {
		t.Fatalf("first call produced %+v", r)
	}
	if r := EvaluateRepeat(st, "sig-a", false); r != nil {
		t.Fatalf("second call produced %+v", r)
	}
	r := EvaluateRepeat(st, "sig-a", false)
	if r == nil || !r.Deny {
		t.Fatalf("third identical call produced %+v", r)
	}
	if EvaluateRepeat(st, "sig-b", false) != nil {
		t.Fatal("a different signature was still treated as a repeat")
	}
}

func TestTerseKeepsTheProcedure(t *testing.T) {
	out := Terse("THINKING_NOT_KOREAN/EN", 7, "reasoning is written in Korean")
	if !strings.Contains(out, "reasoning is written in Korean") {
		t.Fatalf("the procedure was dropped from the terse form: %q", out)
	}
	if !strings.Contains(out, "#7") {
		t.Fatalf("the occurrence count is missing: %q", out)
	}
}

func TestBuildMessageKeepsProcedureAfterSuppressionThreshold(t *testing.T) {
	st := &session.State{ENCount: RepeatSuppressAfter + 5}
	msg := buildMessage(st, "EN", thought("claude-opus-5", englishBlock))
	if !strings.Contains(msg, "Korean") {
		t.Fatalf("the suppressed form dropped the rule: %q", msg)
	}
	if !strings.Contains(msg, ">") {
		t.Fatalf("the suppressed form dropped the quote: %q", msg)
	}
}

func TestCountsInEvidenceRequiresALabel(t *testing.T) {
	if got := countsInEvidence("modified 2026-08-21 14:32:11 size 184320"); got != "" {
		t.Fatalf("bare numbers were treated as counts: %q", got)
	}
	if got := countsInEvidence("total 2669 lines"); got == "" {
		t.Fatal("a labelled count was not recognised")
	}
	if got := countsInEvidence("파일 1250개 확인"); got != "1250" {
		t.Fatalf("Korean unit form yielded %q, want 1250", got)
	}
}

func TestSettledValueFiresOnlyWithEvidence(t *testing.T) {
	empty := &transcript.Turn{}
	if r := settledValue(empty, "wc -l internal/rules/rules.go"); r != nil {
		t.Fatalf("a turn with no evidence produced %+v", r)
	}
	turn := &transcript.Turn{Evidence: "the file lists total 2669 lines"}
	if r := settledValue(turn, "find . -name '*.go' | wc -l"); r == nil || !r.Deny {
		t.Fatalf("a settled count produced %+v", r)
	}
}

func TestEvaluateTurnEndCatchesHandOffEvenWithToolUse(t *testing.T) {
	out := EvaluateTurnEnd("남은 파일은 탐색기에서 직접 지우시면 됩니다.", true)
	if len(out) != 1 || !strings.Contains(out[0].Message, "TASK_HANDED_TO_USER") {
		t.Fatalf("hand-off went undetected: %+v", out)
	}
}

func TestEvaluateTurnEndCatchesBlockedDeclaration(t *testing.T) {
	out := EvaluateTurnEnd("blocker: 권한이 없어 진행할 수 없습니다", true)
	if len(out) != 1 || !strings.Contains(out[0].Message, "TASK_HANDED_TO_USER") {
		t.Fatalf("blocked declaration went undetected: %+v", out)
	}
}

func TestEvaluateTurnEndStaysQuietForRealWork(t *testing.T) {
	if out := EvaluateTurnEnd("rules.go 의 판정 경로를 해시 기준으로 교체했다.", true); len(out) != 0 {
		t.Fatalf("a work report was flagged: %+v", out)
	}
}

func TestEvaluateReportingJudgesEachBlockOnce(t *testing.T) {
	st := &session.State{}
	body := "missing/nowhere.go 를 확인했다"
	turn := &transcript.Turn{
		AssistantBlocks: []transcript.TextBlock{{Text: body, Sig: sig.Text(body)}},
		Evidence:        "",
	}
	if out := EvaluateReporting(st, turn); len(out) == 0 {
		t.Fatal("an unbacked citation was not reported")
	}
	if out := EvaluateReporting(st, turn); len(out) != 0 {
		t.Fatalf("the same reply block was judged twice: %+v", out)
	}
}

func TestEvaluateReportingAcceptsBackedCitation(t *testing.T) {
	st := &session.State{}
	body := "internal/rules/rules.go 를 수정했다"
	turn := &transcript.Turn{
		AssistantBlocks: []transcript.TextBlock{{Text: body, Sig: sig.Text(body)}},
		Evidence:        sig.NormalizePath(`{"file_path":"N:\\nekoi_mcp\\internal\\rules\\rules.go"}`),
		Edited:          1,
	}
	if out := EvaluateReporting(st, turn); len(out) != 0 {
		t.Fatalf("a backed citation was flagged: %+v", out)
	}
}

func TestEvaluateEditFlowDeliversOncePerLevel(t *testing.T) {
	st := &session.State{}
	if r := EvaluateEditFlow(st, 6); r == nil {
		t.Fatal("the audit did not fire at six edits")
	}
	if r := EvaluateEditFlow(st, 6); r != nil {
		t.Fatalf("the audit repeated at the same level: %+v", r)
	}
	if r := EvaluateEditFlow(st, 7); r == nil {
		t.Fatal("the audit did not fire again at a higher level")
	}
}

func TestEvaluateWasteDeniesIdenticalCall(t *testing.T) {
	turn := &transcript.Turn{CallSigs: []string{"s1", "s1"}}
	if r := EvaluateWaste(turn, "s1", "", ""); r == nil || !r.Deny {
		t.Fatalf("a duplicated call produced %+v", r)
	}
}

func TestMergeKeepsTheFloor(t *testing.T) {
	long := strings.Repeat("가", 4000)
	out := Merge([]string{long}, 900_000)
	if len([]rune(out)) < FloorChars {
		t.Fatalf("merged notice is %d runes, below the floor %d", len([]rune(out)), FloorChars)
	}
}
