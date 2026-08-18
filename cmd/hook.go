package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/rules"
	"nekoi_mcp/internal/session"
	"nekoi_mcp/internal/sig"
	"nekoi_mcp/internal/transcript"
)

// hookInput is the stdin payload Claude Code hands to a hook.
type hookInput struct {
	SessionID      string          `json:"session_id"`
	CWD            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	TranscriptPath string          `json:"transcript_path"`
}

type preToolUseOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision,omitempty"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
		AdditionalContext        string `json:"additionalContext,omitempty"`
	} `json:"hookSpecificOutput"`
}

type stopOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// haltOutput stops a run that is still streaming. It is the only channel that
// reaches a violation while it is being written: the block is in the transcript
// before it is rendered, and exit 2 carries no weight on this event, so the
// decision has to travel as JSON.
type haltOutput struct {
	Continue   bool   `json:"continue"`
	StopReason string `json:"stopReason"`
}

const (
	// Wide enough that a long turn's early tool calls stay inside the window.
	// When the tail cuts them off, their file paths vanish from Evidence and
	// a file this turn genuinely edited reads as an unbacked citation.
	transcriptTail = 2000
	// displayTail is shorter because this hook fires on every redraw of a
	// streaming message and only ever reads the block being written.
	displayTail = 40
)

func runHook() int {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 0
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return 0
	}
	if in.HookEventName == "" {
		if in.ToolName != "" {
			in.HookEventName = "PreToolUse"
		} else {
			in.HookEventName = "Stop"
		}
	}

	store := session.NewStore(stateDir())
	st := store.Load(in.SessionID)
	st.CWD = in.CWD

	if in.HookEventName == "MessageDisplay" {
		return runDisplay(store, st, in)
	}

	if in.TranscriptPath == "" {
		return 0
	}
	turn, err := transcript.Load(in.TranscriptPath, transcriptTail)
	if err != nil {
		return 0
	}
	if turn.Model != "" {
		st.Model = turn.Model
	}
	if turn.ContextTokens > st.ContextTokens {
		st.ContextTokens = turn.ContextTokens
	}
	st.OutputTokens = turn.OutputTokens

	var msgs []string
	deny := false

	// Selected before EvaluateThinking, which advances the cursor these
	// blocks are chosen by.
	fresh := freshThinking(st, turn)

	if in.HookEventName == "PreToolUse" {
		st.ToolCalls++
		if r := rules.EvaluateToolChoice(in.ToolName, in.ToolInput); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		isMutation := in.ToolName == "Edit" || in.ToolName == "Write" || in.ToolName == "NotebookEdit"
		if r := rules.EvaluateProcedure(fresh, turn.HasToolUse, isMutation); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateAskUser(in.ToolName); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateWriteTarget(in.ToolName, in.ToolInput); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateWaste(turn, sig.Call(in.ToolName, in.ToolInput), sig.Probe(in.ToolName, in.ToolInput)); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateRepeat(st, sig.Call(in.ToolName, in.ToolInput)); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateUnresolved(st, fresh); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
	}

	if in.HookEventName == "Stop" {
		for _, r := range rules.EvaluateReporting(turn) {
			msgs = append(msgs, r.Message)
		}
		for _, r := range rules.EvaluateTurnEnd(turn.AssistantTxt, turn.HasToolUse) {
			msgs = append(msgs, r.Message)
		}
		if r := rules.EvaluateSplitReport(turn.AssistantTxt); r != nil {
			msgs = append(msgs, r.Message)
		}
		if r := rules.EvaluateEditFlow(st, turn.Edited); r != nil {
			msgs = append(msgs, r.Message)
		}
		// The visible reply is scanned for the same banned patterns as the
		// reasoning: a bypass stated in the answer is still a bypass.
		if hit := rules.ScanPatterns(patternsPath(), turn.AssistantTxt); hit != nil {
			msgs = append(msgs, fmt.Sprintf("[%s in reply]\n%s", hit.Name, strings.TrimSpace(hit.Message)))
		}
	}

	if r := rules.EvaluateAnger(fresh); r != nil {
		msgs = append(msgs, r.Message)
		deny = deny || r.Deny
	}
	if r := rules.EvaluatePatterns(patternsPath(), fresh); r != nil {
		msgs = append(msgs, r.Message)
		deny = deny || r.Deny
	}

	// Consumed on whichever hook fires first, so a block the watcher caught
	// mid-reasoning is answered at the next boundary rather than waiting for a
	// tool call that may never come.
	if r := rules.EvaluateWatchBlock(st); r != nil {
		msgs = append(msgs, r.Message)
		deny = deny || r.Deny
	}

	if r := rules.EvaluateThinking(st, turn); r != nil {
		msgs = append(msgs, r.Message)
		deny = deny || r.Deny
	}

	if len(msgs) == 0 {
		_ = store.Save(st)
		return 0
	}

	body := rules.Merge(msgs, st.ContextTokens)
	if deny {
		st.DenyCount++
		body += fmt.Sprintf("\n[denied #%d/%d]", st.DenyCount, rules.DenyLimit)
	}
	st.Notices++
	st.InjectedChars += int64(len([]rune(body)))
	_ = store.Save(st)

	// Exit code 2 is the blocking channel: stderr goes to the model and the
	// action does not proceed, whatever a reader makes of stdout. A decision
	// this settled is not left to JSON parsing.
	if deny {
		fmt.Fprintln(os.Stderr, body)
		return 2
	}

	switch in.HookEventName {
	case "Stop", "SubagentStop", "StopFailure":
		emit(stopOutput{Decision: "block", Reason: body})
	case "PreToolUse":
		// Only the non-blocking path reaches here; a denial left through
		// stderr with exit 2 above.
		var out preToolUseOutput
		out.HookSpecificOutput.HookEventName = "PreToolUse"
		out.HookSpecificOutput.AdditionalContext = body
		emit(out)
	default:
		var out preToolUseOutput
		out.HookSpecificOutput.HookEventName = in.HookEventName
		out.HookSpecificOutput.AdditionalContext = body
		emit(out)
	}
	return 0
}

// runDisplay judges the reasoning that is being displayed right now and halts
// the run if it is not Korean. This is the only hook that fires between the
// model producing a thinking block and it deciding what to do next, so it is
// the one place a violation can be stopped without waiting for a tool call.
func runDisplay(store *session.Store, st *session.State, in hookInput) int {
	if in.TranscriptPath == "" {
		return 0
	}
	turn, err := transcript.Load(in.TranscriptPath, displayTail)
	if err != nil || len(turn.Thoughts) == 0 {
		return 0
	}
	th := turn.Thoughts[len(turn.Thoughts)-1]

	v := lang.Classify(th.Text)
	if v == lang.VerdictEnglish && strings.HasPrefix(th.Model, "claude-sonnet") {
		return 0 // documented Sonnet5 defect; JA still counts
	}
	if v != lang.VerdictEnglish && v != lang.VerdictJapanse {
		return 0
	}
	// Charged once per block: the same reasoning is displayed repeatedly as
	// it streams, and each redraw would otherwise count as a new violation.
	if turn.TotalThought <= st.Cursor {
		return 0
	}
	st.Cursor = turn.TotalThought

	label := "EN"
	if v == lang.VerdictJapanse {
		st.JACount++
		label = "JA"
	} else {
		st.ENCount++
	}
	st.Streak++
	st.LastVerdict = string(v)
	if th.Model != "" {
		st.Model = th.Model
	}
	_ = store.Save(st)

	quote := ""
	if spans := lang.EnglishSpans(th.Text, 1); len(spans) > 0 {
		quote = "\n> " + spans[0]
	}
	emit(haltOutput{
		Continue: false,
		StopReason: fmt.Sprintf(
			"[HALTED: THINKING_NOT_KOREAN/%s — caught as it was written, #%d this session]%s\n"+
				"Stopped here, mid-reasoning, without waiting for a tool call. "+
				"That is your thinking block, not your reply: writing the reply in Korean leaves this violation exactly where it is. "+
				"Discard that reasoning and think the same problem through again in Korean, at the same depth.",
			label, st.ENCount+st.JACount, quote),
	})
	return 0
}

// freshThinking joins the thinking blocks of this turn that the cursor has
// not yet consumed, so a pattern is charged once rather than on every call.
func freshThinking(st *session.State, turn *transcript.Turn) string {
	blocks := turn.Thoughts
	base := turn.TotalThought - len(turn.Thoughts)
	if skip := st.Cursor - base; skip > 0 {
		if skip >= len(blocks) {
			return ""
		}
		blocks = blocks[skip:]
	}
	parts := make([]string, 0, len(blocks))
	for _, th := range blocks {
		parts = append(parts, th.Text)
	}
	return join(parts)
}

func join(msgs []string) string {
	out := ""
	for i, m := range msgs {
		if i > 0 {
			out += "\n"
		}
		out += m
	}
	return out
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(v)
}
