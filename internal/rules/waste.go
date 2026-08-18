package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"nekoi_mcp/internal/transcript"
)

// ToolBudget is how many calls one user instruction may consume before the
// investigation is treated as non-converging.
const ToolBudget = 35

var (
	reBackend = regexp.MustCompile(`(^|/)(server|backend|api|routes?|controllers?|services?|repositories|prisma|migrations?|handlers?)(/|$)|\.(sql|prisma)$`)
	reFront   = regexp.MustCompile(`(^|/)(client|frontend|components?|pages?|views?|screens?|hooks|styles?)(/|$)|\.(tsx|jsx|vue|svelte|css|scss|sass|less)$`)
	reRoot    = regexp.MustCompile(`^(?:[A-Za-z]:[/\\]|/[a-z]/)[^/\\]+$`)
)

// EvaluateWriteTarget guards the Write tool: it replaces a file whole, so a
// path that already exists loses every line not reproduced.
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
			"[WRITE_OVERWRITES_EXISTING] %s exists. Write replaces it whole, deleting every line you did not reproduce.\nRead it and change it with Edit.", in.FilePath)}
	}
	if reRoot.MatchString(strings.ReplaceAll(in.FilePath, `\`, "/")) {
		return &Result{Deny: true, Message: fmt.Sprintf(
			"[STRAY_TEMP_ARTIFACT] %s sits at a drive root, owned by no project and cleaned up by nothing.\nUse the project's temp location, or write no file at all.", in.FilePath)}
	}
	// A new file that lands without these answers becomes debt immediately.
	return &Result{Message: fmt.Sprintf(
		"[NEW_FILE_DESIGN_CHECK] Creating %s. Settle these before it lands:\n"+
			"FLOW: what runs before it, what consumes it after.\n"+
			"CENTRALIZATION: do these types/utilities already exist elsewhere? Import them instead of writing a second version.\n"+
			"ROLE: one responsibility per file; mixed ones introduced at creation are never separated later.\n"+
			"State where it sits in the flow in one line, then write it.", in.FilePath)}
}

// EvaluateAskUser blocks handing the decision back to the user.
func EvaluateAskUser(toolName string) *Result {
	if toolName != "AskUserQuestion" {
		return nil
	}
	return &Result{Deny: true, Message: "[ASK_USER_FORBIDDEN] Asking the user to choose or approve is refusal to work, and this tool is denied by permissions anyway.\nPick the best-supported path, execute it, and state any assumption in one line."}
}

// EvaluateWaste catches the loops that burn budget without advancing the
// deliverable: an identical call already made this turn, re-probing an
// environment fact already established, and an investigation that has stopped
// converging.
func EvaluateWaste(turn *transcript.Turn, sig, probe string) *Result {
	if n := countEqual(turn.CallSigs, sig); sig != "" && n >= 2 {
		return &Result{Deny: true, Message: fmt.Sprintf(
			"[REDUNDANT_CALL x%d] Identical arguments were already run this turn, so the result is already in this conversation.\nAct on it, or change the arguments to ask something new.", n)}
	}
	if probe != "" {
		if n := countEqual(turn.ProbeKeys, probe); n >= 2 {
			return &Result{Deny: true, Message: fmt.Sprintf(
				"[ENVIRONMENT_REPROBE x%d] The toolchain does not change mid-session; this was settled earlier.\nTreat it as fact and run the actual work command.", n)}
		}
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

// editSide reports "front" or "back" when every edited file falls on one side
// of the stack, and "" when the edits are mixed or unclassifiable.
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

// Probe identity lives in internal/sig, shared with the transcript parser so
// a live call and its recorded form reduce identically.
