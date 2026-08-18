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

func buildMessage(st *session.State, v lang.Verdict, th transcript.Thought) string {
	count := st.ENCount
	label := "EN"
	if v == lang.VerdictJapanse {
		count, label = st.JACount, "JA"
	}
	class := "THINKING_NOT_KOREAN/" + label
	if count > RepeatSuppressAfter {
		// The full text was already delivered; quote the offending line and
		// stop re-sending the explanation that is still in context.
		quote := ""
		if spans := lang.EnglishSpans(th.Text, 1); len(spans) > 0 {
			quote = "\n> " + truncate(spans[0], quoteChars)
		}
		return Terse(class, count) + quote
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
	b.WriteString("You wrote that. Reply language is a separate channel and does not cover it.\n")
	b.WriteString("Discard it, think the same problem through again in Korean at the same depth, then call. Translating or summarizing it is not re-reasoning.")
	if st.Streak >= 2 {
		fmt.Fprintf(&b, "\n[REPEAT x%d] Every prior notice was followed by another English block.", st.Streak)
	}
	return b.String()
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
