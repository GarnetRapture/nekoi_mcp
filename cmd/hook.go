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

type haltOutput struct {
	Continue   bool   `json:"continue"`
	StopReason string `json:"stopReason"`
}

const (
	transcriptTail = 2000
	displayTail    = 40
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

	freshBlocks := rules.FreshThoughts(st, turn)
	fresh := rules.JoinThoughts(freshBlocks)

	thinkingResult := rules.EvaluateThinking(st, freshBlocks)
	if thinkingResult == nil {
		thinkingResult = rules.EvaluatePending(st)
	}
	if thinkingResult != nil {
		msgs = append(msgs, thinkingResult.Message)
		deny = deny || thinkingResult.Deny
	}

	if in.HookEventName == "PreToolUse" {
		st.ToolCalls++
		if r := rules.EvaluateToolChoice(in.ToolName, in.ToolInput); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateNoThinking(len(turn.Thoughts), turn.HasToolUse); r != nil {
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
		if r := rules.EvaluateWaste(turn,
			sig.Call(in.ToolName, in.ToolInput),
			sig.Probe(in.ToolName, in.ToolInput),
			sig.Command(in.ToolName, in.ToolInput)); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
		if r := rules.EvaluateRepeat(st, sig.Call(in.ToolName, in.ToolInput), deny); r != nil {
			msgs = append(msgs, r.Message)
			deny = deny || r.Deny
		}
	}

	if in.HookEventName == "Stop" {
		for _, r := range rules.EvaluateReporting(st, turn) {
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

	if len(msgs) == 0 {
		_ = store.Save(st)
		return 0
	}

	body := rules.Merge(msgs, st.ContextTokens)
	if in.HookEventName == "PreToolUse" && deny {
		st.DenyCount++
		body += fmt.Sprintf("\n[denied #%d/%d]", st.DenyCount, rules.DenyLimit)
	}
	st.Notices++
	st.InjectedChars += int64(len([]rune(body)))
	_ = store.Save(st)

	if deny {
		fmt.Fprintln(os.Stderr, body)
		return 2
	}

	switch in.HookEventName {
	case "Stop", "SubagentStop", "StopFailure":
		emit(stopOutput{Decision: "block", Reason: body})
	default:
		var out preToolUseOutput
		out.HookSpecificOutput.HookEventName = in.HookEventName
		out.HookSpecificOutput.AdditionalContext = body
		emit(out)
	}
	return 0
}

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
		return 0
	}
	if v != lang.VerdictEnglish && v != lang.VerdictJapanse {
		return 0
	}
	if !st.MarkJudged(th.Sig) {
		return 0
	}

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

	quote := ""
	if spans := lang.EnglishSpans(th.Text, 1); len(spans) > 0 {
		quote = "\n> " + spans[0]
	}
	reason := fmt.Sprintf(
		"[HALTED: THINKING_NOT_KOREAN/%s — read from the transcript as it was written, #%d this session]%s\n"+
			"This halt came mid-reasoning, with no tool call in between. That text is from the thinking channel, which is separate from the reply and is checked on its own.\n"+
			"The rule for this project is that reasoning is written in Korean, at the same depth as the block being replaced.",
		label, st.ENCount+st.JACount, quote)
	st.PendingBlock = true
	st.PendingReason = reason
	_ = store.Save(st)

	emit(haltOutput{Continue: false, StopReason: reason})
	return 0
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(v)
}
