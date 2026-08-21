package rules

import (
	"regexp"
	"strings"
)

var (
	reCause = regexp.MustCompile(`(?i)원인|때문|이유|왜냐|탓|기인|유발|` +
		`\bbecause\b|\bcause[ds]?\b|\bdue to\b|\bstems? from\b|\broot\b`)

	reConclusion = regexp.MustCompile(`(?i)따라서|그러므로|결론|즉,|그래서|이므로|해야|맞다|아니다|` +
		`\btherefore\b|\bthus\b|\bhence\b|\bmeans\b|\bfollows\b|\bmust\b|\bshould\b`)

	reProcedure = regexp.MustCompile(`(?i)한다$|하겠|할\s*것|절차|순서|먼저|다음|고친다|바꾼다|넣는다|지운다|` +
		`\bwill\b|\bfirst\b|\bthen\b|\bnext\b|\bsteps?\b|\breplace\b|\bremove\b|\badd\b|\bfix\b`)

	reActing = regexp.MustCompile(`(?i)해보|시도해|일단|우선\s*해|보고\s*나서|확인만|` +
		`\btry(ing)?\b|\bsee if\b|\blet'?s\b|\bfor now\b|\bjust\b`)

	reReanalyze = regexp.MustCompile(`(?i)다시\s*(분석|파악|확인|살펴|점검|검토)|재분석|재확인|또\s*확인|` +
		`\bre-?(analyz|check|verif|examin|review|read)|\bagain\b.{0,20}\b(check|look|verif)`)

	reSelfCheck = regexp.MustCompile(`(?i)맞는지|맞나|올바른지|적절한지|타당한지|문제\s*없는지|괜찮은지|` +
		`영향|부작용|깨지|어긋|충돌|누락|빠진|놓친|정말|과연|확실|` +
		`\bcorrect\b|\bright (one|thing|fix)\b|\bbreak(s|ing)?\b|\bside ?effects?\b|` +
		`\bimpact\b|\bmiss(ing|ed)?\b|\bconflict\b|\bsure\b|\bonly (edit|change)\b`)
)

func EvaluateNoThinking(blocksThisTurn int, actedAlready bool) *Result {
	if !actedAlready || blocksThisTurn > 0 {
		return nil
	}
	return &Result{Deny: true, Message: "[NO_REASONING] This turn has already acted, and this call is preceded by no thinking block in the transcript.\nThe reasoning channel is where the basis for an action is recorded, so an empty one leaves the action with no stated basis. A standing language flag also stays raised, because a Korean thinking block is what clears it."}
}

func EvaluateProcedure(thinking string, actedAlready bool, isMutation bool) *Result {
	if strings.TrimSpace(thinking) == "" {
		return nil
	}
	if actedAlready && reReanalyze.MatchString(thinking) && !reConclusion.MatchString(thinking) {
		return &Result{Deny: true, Message: "[REANALYSIS_LOOP] This turn already acted, and the reasoning behind this call re-analyzes without naming what the last result settled.\nRe-deriving a fact the transcript already holds produces the same answer at full cost."}
	}
	if !isMutation {
		return nil
	}
	if !reSelfCheck.MatchString(thinking) {
		return &Result{Deny: true, Message: "[UNCHECKED_ACTION] This call changes a file, and the reasoning behind it never asks whether the change is the right one.\nThe three questions that settle that are whether it matches what was asked, what it breaks, and whether it is the only edit needed."}
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
	if len(missing) < 2 {
		if reActing.MatchString(thinking) && !reConclusion.MatchString(thinking) {
			return &Result{Deny: true, Message: "[EXPLORATORY_MUTATION] This call changes a file while the reasoning behind it still reads as trying something out.\nA change that lands without what it follows from is indistinguishable from a guess, and the file keeps the result either way."}
		}
		return nil
	}
	return &Result{Deny: true, Message: "[UNGROUNDED_ACTION] The reasoning behind this change never settled: " + strings.Join(missing, ", ") + ".\nCause, what follows from it, and the procedure are what make one edit sufficient instead of the first of several corrections."}
}
