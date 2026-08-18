package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

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

const transcriptTail = 400

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
		if r := rules.EvaluateEditFlow(turn.Edited); r != nil {
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

	if r := rules.EvaluateThinking(st, turn); r != nil {
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

	switch in.HookEventName {
	case "Stop", "SubagentStop", "StopFailure":
		emit(stopOutput{Decision: "block", Reason: body})
	case "PreToolUse":
		var out preToolUseOutput
		out.HookSpecificOutput.HookEventName = "PreToolUse"
		if deny {
			out.HookSpecificOutput.PermissionDecision = "deny"
			out.HookSpecificOutput.PermissionDecisionReason = body
		} else {
			out.HookSpecificOutput.AdditionalContext = body
		}
		emit(out)
	default:
		var out preToolUseOutput
		out.HookSpecificOutput.HookEventName = in.HookEventName
		out.HookSpecificOutput.AdditionalContext = body
		emit(out)
	}
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

