package rules

import (
	"regexp"
	"strings"
)

// The reasoning that precedes an action has to close four questions: what
// caused this, what was pointed out, what follows from the two together, and
// what procedure satisfies that conclusion. Reasoning that skips them is the
// loop the user described — act, re-analyze, act again, with no step ever
// settled.
var (
	reCause = regexp.MustCompile(`(?i)원인|때문|이유|왜냐|탓|기인|유발|` +
		`\bbecause\b|\bcause[ds]?\b|\bdue to\b|\bstems? from\b|\broot\b`)

	reConclusion = regexp.MustCompile(`(?i)따라서|그러므로|결론|즉,|그래서|이므로|해야|맞다|아니다|` +
		`\btherefore\b|\bthus\b|\bhence\b|\bmeans\b|\bfollows\b|\bmust\b|\bshould\b`)

	reProcedure = regexp.MustCompile(`(?i)한다$|하겠|할\s*것|절차|순서|먼저|다음|고친다|바꾼다|넣는다|지운다|` +
		`\bwill\b|\bfirst\b|\bthen\b|\bnext\b|\bsteps?\b|\breplace\b|\bremove\b|\badd\b|\bfix\b`)

	// Deciding to act while the reasoning still reads as exploratory.
	reActing = regexp.MustCompile(`(?i)해보|시도해|일단|우선\s*해|보고\s*나서|확인만|` +
		`\btry(ing)?\b|\bsee if\b|\blet'?s\b|\bfor now\b|\bjust\b`)

	// Re-analysis after an action has already been taken this turn.
	reReanalyze = regexp.MustCompile(`(?i)다시\s*(분석|파악|확인|살펴|점검|검토)|재분석|재확인|또\s*확인|` +
		`\bre-?(analyz|check|verif|examin|review|read)|\bagain\b.{0,20}\b(check|look|verif)`)

	// A last check that the intended change is the right one: whether it
	// matches what was asked, what it breaks, whether it is the only edit
	// needed. Re-analysis asks "what is going on"; this asks "is this right".
	reSelfCheck = regexp.MustCompile(`(?i)맞는지|맞나|올바른지|적절한지|타당한지|문제\s*없는지|괜찮은지|` +
		`영향|부작용|깨지|어긋|충돌|누락|빠진|놓친|정말|과연|확실|` +
		`\bcorrect\b|\bright (one|thing|fix)\b|\bbreak(s|ing)?\b|\bside ?effects?\b|` +
		`\bimpact\b|\bmiss(ing|ed)?\b|\bconflict\b|\bsure\b|\bonly (edit|change)\b`)
)

// EvaluateProcedure requires the four steps once a turn has already acted.
// The first call of a turn is exempt: reading in order to find the target is
// how the cause gets established, and demanding a conclusion before any
// evidence would force the guessing this rule exists to prevent.
func EvaluateProcedure(thinking string, actedAlready bool, isMutation bool) *Result {
	if strings.TrimSpace(thinking) == "" {
		return nil
	}
	if actedAlready && reReanalyze.MatchString(thinking) && !reConclusion.MatchString(thinking) {
		return &Result{Deny: true, Message: "[REANALYSIS_LOOP] You already acted this turn and are now re-analyzing without stating what changed.\nName what the last result settled, then take the next step — do not re-derive it."}
	}
	if !isMutation {
		return nil
	}
	// A mutation is the point of no return, so its basis must be complete.
	if !reSelfCheck.MatchString(thinking) {
		return &Result{Deny: true, Message: "[UNCHECKED_ACTION] You are changing a file without asking whether this change is the right one.\nAnswer first: does it match what was asked, what does it break, is it the only edit needed."}
	}
	var missing []string
	if !reCause.MatchString(thinking) {
		missing = append(missing, "cause")
	}
	if !reConclusion.MatchString(thinking) {
		missing = append(missing, "conclusion")
	}
	if !reProcedure.MatchString(thinking) {
		missing = append(missing, "procedure")
	}
	if !reSelfCheck.MatchString(thinking) {
		return &Result{Deny: true, Message: "[UNCHECKED_ACTION] You are about to change a file without asking whether this change is the right one.\nBefore acting: does it match what was asked, what does it break, is it the only edit needed? Answer that, then act."}
	}
	if len(missing) < 2 {
		if reActing.MatchString(thinking) && !reConclusion.MatchString(thinking) {
			return &Result{Deny: true, Message: "[EXPLORATORY_MUTATION] You are changing a file while the reasoning still reads as trying something out.\nState what the change follows from, then make it."}
		}
		return nil
	}
	return &Result{Deny: true, Message: "[UNGROUNDED_ACTION] The reasoning behind this change never settled: " + strings.Join(missing, ", ") + ".\nState the cause, what follows from it, and the procedure — in that order — then act once."}
}
