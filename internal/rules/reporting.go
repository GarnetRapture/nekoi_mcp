package rules

import (
	"fmt"
	"regexp"
	"strings"

	"nekoi_mcp/internal/session"
	"nekoi_mcp/internal/sig"
	"nekoi_mcp/internal/transcript"
)

var (
	reFileRef = regexp.MustCompile(
		`[A-Za-z0-9_@][A-Za-z0-9_@./+-]*\.(?:tsx|ts|jsx|json|js|mjs|cjs|prisma|md|sh|ps1|py|go|cpp|cc|hpp|h|rs|cs|php|sql|ya?ml|toml)(?::[0-9]+(?:,[0-9]+)*)?`)
	reLineSuffix = regexp.MustCompile(`:[0-9]+(?:,[0-9]+)*$`)

	reToolOutput = regexp.MustCompile(
		`P[0-9]{4}:|error TS[0-9]{4}|TS[0-9]{4}:|exit code [0-9]+|Database schema is up to date|[0-9]+ migrations? found|Compiled successfully|Cannot find module [^\s]+|panic: |go: cannot`)

	reEditClaim = regexp.MustCompile(
		`추가했|교체했|수정했|생성했|삭제했|제거했|반영했|적용했|중앙화했|바꿨|고쳤|만들었|작성했|갱신했|해소(했|됐)|수정\s*완료|구현\s*완료|처리\s*완료|(으로|로)\s*교체|(replaced|added|created|updated|fixed|removed)\s+(the\s+)?[A-Za-z0-9_@./-]+\.(ts|tsx|js|json|prisma|md|sh|py|go)`)
)

const maxCited = 8

func EvaluateReporting(st *session.State, turn *transcript.Turn) []*Result {
	var fresh []string
	for _, b := range turn.AssistantBlocks {
		if st.MarkReported(b.Sig) {
			fresh = append(fresh, b.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(fresh, "\n"))
	if text == "" {
		return nil
	}
	var out []*Result

	if cited := unbacked(reFileRef, text, turn.Evidence, true); len(cited) > 0 {
		out = append(out, &Result{Message: fmt.Sprintf(
			"[UNVERIFIED_FILE_REFERENCE] These paths are cited as evidence, and nothing in this turn opened them nor did the user supply them:\n%s\nA path named without having been read carries the weight of an observation that never happened.",
			strings.Join(cited, ", "))})
	}

	if turn.Bashed == 0 {
		if cited := unbacked(reToolOutput, text, turn.Evidence, false); len(cited) > 0 {
			out = append(out, &Result{Message: fmt.Sprintf(
				"[FABRICATED_TOOL_OUTPUT] This reply quotes output only a command execution produces, and no command ran in this turn:\n%s",
				strings.Join(cited, ", "))})
		}
	}

	if turn.Edited == 0 && reEditClaim.MatchString(text) {
		out = append(out, &Result{Message: "[UNBACKED_EDIT_CLAIM] This reply states a change as already made, and the turn holds zero Edit/Write calls.\nThe file on disk is unchanged, so the claim and the repository disagree."})
	}
	return out
}

func unbacked(re *regexp.Regexp, text, evidence string, stripLines bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(text, -1) {
		norm := sig.NormalizePath(m)
		if seen[norm] {
			continue
		}
		seen[norm] = true
		probe := norm
		if stripLines {
			probe = reLineSuffix.ReplaceAllString(probe, "")
		}
		if strings.Contains(evidence, probe) {
			continue
		}
		out = append(out, m)
		if len(out) >= maxCited {
			break
		}
	}
	return out
}
