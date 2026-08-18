package rules

import (
	"fmt"
	"regexp"
	"strings"

	"nekoi_mcp/internal/transcript"
)

var (
	// A path-looking token with a source extension, optionally followed by
	// line numbers. Citing one reads as first-hand observation.
	reFileRef = regexp.MustCompile(
		`[A-Za-z0-9_@][A-Za-z0-9_@./+-]*\.(?:ts|tsx|js|jsx|mjs|cjs|json|prisma|md|sh|ps1|py|go|cpp|cc|hpp|h|rs|cs|php|sql|ya?ml|toml)(?::[0-9]+(?:,[0-9]+)*)?`)
	reLineSuffix = regexp.MustCompile(`:[0-9]+(?:,[0-9]+)*$`)

	// Output only a command execution can produce.
	reToolOutput = regexp.MustCompile(
		`P[0-9]{4}:|error TS[0-9]{4}|TS[0-9]{4}:|exit code [0-9]+|Database schema is up to date|[0-9]+ migrations? found|Compiled successfully|Cannot find module [^\s]+|panic: |go: cannot`)

	// Korean and English phrasings that state a change as already made.
	reEditClaim = regexp.MustCompile(
		`추가했|교체했|수정했|생성했|삭제했|제거했|반영했|적용했|중앙화했|바꿨|고쳤|만들었|작성했|갱신했|해소(했|됐)|수정\s*완료|구현\s*완료|처리\s*완료|(으로|로)\s*교체|(replaced|added|created|updated|fixed|removed)\s+(the\s+)?[A-Za-z0-9_@./-]+\.(ts|tsx|js|json|prisma|md|sh|py|go)`)
)

const maxCited = 8

// EvaluateReporting checks the visible reply against what the turn actually
// observed. It runs on Stop, where the reply is complete.
func EvaluateReporting(turn *transcript.Turn) []*Result {
	text := strings.TrimSpace(turn.AssistantTxt)
	if text == "" {
		return nil
	}
	var out []*Result

	if cited := unbacked(reFileRef, text, turn.Evidence, true); len(cited) > 0 {
		out = append(out, &Result{Message: fmt.Sprintf(
			"[UNVERIFIED_FILE_REFERENCE] Cited as evidence, but nothing this turn opened them and the user did not supply them:\n%s\nDescribe only files you actually read.",
			strings.Join(cited, ", "))})
	}

	if turn.Bashed == 0 {
		if cited := unbacked(reToolOutput, text, turn.Evidence, false); len(cited) > 0 {
			out = append(out, &Result{Message: fmt.Sprintf(
				"[FABRICATED_TOOL_OUTPUT] You quoted output only a command can produce, but no command ran this turn:\n%s\nRun it or drop the quote.",
				strings.Join(cited, ", "))})
		}
	}

	if turn.Edited == 0 && reEditClaim.MatchString(text) {
		out = append(out, &Result{Message: "[UNBACKED_EDIT_CLAIM] You reported a change as done with zero Edit/Write calls this turn.\nMake the change now, or delete the claim."})
	}
	return out
}

// unbacked returns the matches of re in text that do not appear in evidence.
// When stripLines is set, a trailing :line,col is ignored when matching, so
// citing a real file at a new line still counts as backed.
func unbacked(re *regexp.Regexp, text, evidence string, stripLines bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(text, -1) {
		norm := strings.ToLower(strings.ReplaceAll(m, `\`, "/"))
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
