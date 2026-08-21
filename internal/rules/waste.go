package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"nekoi_mcp/internal/transcript"
)

const ToolBudget = 35

var (
	reBackend = regexp.MustCompile(`(^|/)(server|backend|api|routes?|controllers?|services?|repositories|prisma|migrations?|handlers?)(/|$)|\.(sql|prisma)$`)
	reFront   = regexp.MustCompile(`(^|/)(client|frontend|components?|pages?|views?|screens?|hooks|styles?)(/|$)|\.(tsx|jsx|vue|svelte|css|scss|sass|less)$`)
	reRoot    = regexp.MustCompile(`^(?:[A-Za-z]:[/\\]|/[a-z]/)[^/\\]+$`)
)

func EvaluateWriteTarget(toolName string, toolInput json.RawMessage) *Result {
	if toolName != "Write" {
		return nil
	}
	var in struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(toolInput, &in)
	if in.FilePath == "" {
		return nil
	}
	if st, err := os.Stat(in.FilePath); err == nil && !st.IsDir() {
		return &Result{Deny: true, Message: fmt.Sprintf(
			"[WRITE_OVERWRITES_EXISTING] %s already exists, and Write replaces a file whole: every line not reproduced in the call is gone.\nRead plus Edit changes the same file without that loss.", in.FilePath)}
	}
	if reRoot.MatchString(strings.ReplaceAll(in.FilePath, `\`, "/")) {
		return &Result{Deny: true, Message: fmt.Sprintf(
			"[STRAY_TEMP_ARTIFACT] %s sits at a drive root, where it belongs to no project and nothing cleans it up.\nThe project's own temp location is where a working file survives review.", in.FilePath)}
	}
	return &Result{Message: fmt.Sprintf(
		"[NEW_FILE_DESIGN_CHECK] %s does not exist yet, so this call creates it.\n"+
			"FLOW: what runs before it, what consumes it after.\n"+
			"CENTRALIZATION: types and utilities that already exist elsewhere are imported; a second version is duplication.\n"+
			"ROLE: one responsibility per file, since mixed ones introduced at creation are never separated later.", in.FilePath)}
}

func EvaluateAskUser(toolName string) *Result {
	if toolName != "AskUserQuestion" {
		return nil
	}
	return &Result{Deny: true, Message: "[ASK_USER_FORBIDDEN] This tool asks the user to choose or approve, and it is denied by this project's permissions.\nThe best-supported path executed directly, with any assumption stated in one line, is what carries the work forward here."}
}

func EvaluateWaste(turn *transcript.Turn, sig, probe, cmd string) *Result {
	if n := countEqual(turn.CallSigs, sig); sig != "" && n >= 2 {
		return &Result{Deny: true, Message: fmt.Sprintf(
			"[REDUNDANT_CALL x%d] Identical arguments already ran this turn, so the result is in this conversation.\n"+
				"This denies the repetition, not the goal: reaching it another way remains part of the task.", n)}
	}
	if probe != "" {
		if n := countEqual(turn.ProbeKeys, probe); n >= 2 {
			return &Result{Deny: true, Message: fmt.Sprintf(
				"[ENVIRONMENT_REPROBE x%d] The toolchain does not change mid-session, so this was settled the first time it was asked and the answer is already in this conversation.", n)}
		}
	}
	if r := settledValue(turn, cmd); r != nil {
		return r
	}
	if turn.ToolCalls >= ToolBudget {
		return &Result{Message: fmt.Sprintf(
			"[TOOL_BUDGET %d/%d] This many calls for one instruction means the investigation is not converging.\nBuild the deliverable from the evidence you hold; name any single item that genuinely lacks it.",
			turn.ToolCalls, ToolBudget)}
	}
	if turn.Edited >= 2 {
		if side := editSide(turn.EditFiles); side != "" {
			return &Result{Message: fmt.Sprintf(
				"[STACK_BOUNDARY %s-only] Every file changed this turn is %s. If this crosses a contract — response shape, field name, status code, validation — the other side is now wrong, and a clean type check will not show it.\nName what it reads from and confirm it, or say in one line that the change is self-contained.",
				side, side)}
		}
	}
	return nil
}

func countEqual(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}

func editSide(files []string) string {
	front, back := 0, 0
	for _, f := range files {
		p := strings.ToLower(strings.ReplaceAll(f, `\`, "/"))
		switch {
		case reBackend.MatchString(p):
			back++
		case reFront.MatchString(p):
			front++
		}
	}
	switch {
	case front > 0 && back == 0:
		return "front"
	case back > 0 && front == 0:
		return "back"
	}
	return ""
}
