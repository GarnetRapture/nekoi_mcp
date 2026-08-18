package rules

import (
	"fmt"
	"strings"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/session"
	"nekoi_mcp/internal/transcript"
)

// Result is what the hook layer turns into a JSON response.
type Result struct {
	Deny    bool
	Message string
}

const (
	// DenyLimit bounds how many times a single session may be denied, so a
	// model that cannot recover never wedges the session permanently.
	DenyLimit = 300
	// quoteLimit bounds how much of the offending reasoning is quoted back.
	quoteLimit = 2
	// quoteChars bounds each quoted sentence.
	quoteChars = 160
)

// EvaluateThinking judges the thinking blocks produced since the last notice
// and advances the session cursor so a block is never charged twice.
func EvaluateThinking(st *session.State, turn *transcript.Turn) *Result {
	fresh := turn.Thoughts
	if st.Cursor > 0 {
		base := turn.TotalThought - len(turn.Thoughts)
		if skip := st.Cursor - base; skip > 0 {
			if skip >= len(fresh) {
				fresh = nil
			} else {
				fresh = fresh[skip:]
			}
		}
	}
	if len(fresh) == 0 {
		return nil
	}

	var bad []transcript.Thought
	verdict := lang.VerdictKorean
	for _, th := range fresh {
		v := lang.Classify(th.Text)
		if v == lang.VerdictEnglish && strings.HasPrefix(th.Model, "claude-sonnet") {
			continue // documented Sonnet5 model defect; JA still counts
		}
		if v == lang.VerdictEnglish || v == lang.VerdictJapanse {
			bad = append(bad, th)
			verdict = v
		}
	}
	if len(bad) == 0 {
		st.Streak = 0
		st.LastVerdict = string(lang.VerdictKorean)
		st.Cursor = turn.TotalThought
		return nil
	}

	st.Cursor = turn.TotalThought
	st.Streak++
	st.LastVerdict = string(verdict)
	if verdict == lang.VerdictJapanse {
		st.JACount++
	} else {
		st.ENCount++
	}

	offender := bad[len(bad)-1]
	return &Result{
		Deny:    st.DenyCount < DenyLimit,
		Message: buildMessage(st, verdict, offender),
	}
}

// EvaluateWatchBlock acts on what the watcher found between hook invocations.
// The hook runs before a tool call and at turn end; reasoning written in
// between reaches neither, and by the time this runs the watcher has already
// settled the verdict. The flag is cleared as it is consumed, because a flag
// left standing would deny every later call and wedge the session.
func EvaluateWatchBlock(st *session.State) *Result {
	if !st.WatchBlock {
		return nil
	}
	verdict, quote := st.WatchVerdict, st.WatchQuote
	st.WatchBlock = false
	st.WatchVerdict = ""
	st.WatchQuote = ""

	count := st.WatchEN
	if verdict == string(lang.VerdictJapanse) {
		count = st.WatchJA
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[WATCH_%s #%d] The transcript watcher read this while you were reasoning, before any hook could run.\n", verdict, count)
	if quote != "" {
		fmt.Fprintf(&b, "> %s\n", truncate(quote, quoteChars))
	}
	b.WriteString("This call is denied on that block alone. Discard it, think the same problem through again in Korean at the same depth, then call.")
	// Unconditional: this is the one verdict the watcher already settled from
	// the written transcript, so no tally may soften it into a mere notice.
	return &Result{Deny: true, Message: b.String()}
}

func buildMessage(st *session.State, v lang.Verdict, th transcript.Thought) string {
	// The watcher reads the transcript continuously and catches blocks written
	// between hook invocations, which this count would otherwise omit.
	count := st.ENCount + st.WatchEN
	label := "EN"
	if v == lang.VerdictJapanse {
		count, label = st.JACount+st.WatchJA, "JA"
	}
	class := "THINKING_NOT_KOREAN/" + label
	if count > RepeatSuppressAfter {
		// The explanation is dropped, the instruction is not: a violation on
		// its Nth repeat is proof that the earlier text stopped steering.
		quote := ""
		if spans := lang.EnglishSpans(th.Text, 1); len(spans) > 0 {
			quote = "\n> " + truncate(spans[0], quoteChars)
		}
		return Terse(class, count, "Your thinking block, not your reply — a Korean reply does not settle it. Discard that block and think the same problem through again in Korean, at the same depth, before this call.") + quote
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s #%d this session]\n", class, count)

	quotes := lang.EnglishSpans(th.Text, quoteLimit)
	if v == lang.VerdictJapanse || len(quotes) == 0 {
		quotes = []string{truncate(collapseWS(th.Text), quoteChars)}
	}
	for _, q := range quotes {
		fmt.Fprintf(&b, "> %s\n", truncate(q, quoteChars))
	}
	b.WriteString("That is your thinking block, not your reply. Writing the reply in Korean does not make the thinking compliant — they are two channels and this notice is about the thinking one.\n")
	b.WriteString("Discard it, think the same problem through again in Korean at the same depth, then call. Translating or summarizing it is not re-reasoning.")
	if st.Streak >= 2 {
		fmt.Fprintf(&b, "\n[REPEAT x%d] Every prior notice was followed by another English block.", st.Streak)
	}
	return b.String()
}

// EvaluateUnresolved catches the gap the cursor leaves open: once a block has
// been charged the cursor moves past it, so calling again without reasoning at
// all produces no fresh block for EvaluateThinking to judge, and the notice
// passes unanswered. Acknowledging a violation and proceeding anyway is the
// same as never reading it.
func EvaluateUnresolved(st *session.State, fresh string) *Result {
	last := st.LastVerdict
	if last != string(lang.VerdictEnglish) && last != string(lang.VerdictJapanse) {
		return nil
	}
	if strings.TrimSpace(fresh) != "" {
		return nil
	}
	return &Result{Deny: true, Message: fmt.Sprintf(
		"[UNRESOLVED_%s] The last thinking block was flagged and you are calling again without having reasoned since.\nThe notice is answered by reasoning through it again in Korean, not by proceeding. Do that first, then call.",
		last)}
}

// EvaluateRepeat catches the loop where the same call is reissued instead of
// acting on the notice that stopped it.
func EvaluateRepeat(st *session.State, sig string) *Result {
	if sig == "" {
		return nil
	}
	if sig != st.RepeatSig {
		st.RepeatSig = sig
		st.RepeatCount = 1
		return nil
	}
	st.RepeatCount++
	if st.RepeatCount < 3 {
		return nil
	}
	return &Result{
		Deny: true,
		Message: fmt.Sprintf(
			"[REPEAT_CALL x%d] Identical call reissued %d times. Repetition is not progress and the state has not changed.\nRead what stopped it, fix that, or report the blocker in one line.",
			st.RepeatCount, st.RepeatCount),
	}
}

func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
