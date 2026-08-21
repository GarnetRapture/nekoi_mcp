package rules

import (
	"fmt"
	"strings"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/session"
	"nekoi_mcp/internal/transcript"
)

type Result struct {
	Deny    bool
	Message string
}

const (
	DenyLimit  = 300
	quoteLimit = 2
	quoteChars = 160
)

func FreshThoughts(st *session.State, turn *transcript.Turn) []transcript.Thought {
	var fresh []transcript.Thought
	for _, th := range turn.Thoughts {
		if !st.Judged(th.Sig) {
			fresh = append(fresh, th)
		}
	}
	return fresh
}

func JoinThoughts(blocks []transcript.Thought) string {
	parts := make([]string, 0, len(blocks))
	for _, th := range blocks {
		parts = append(parts, th.Text)
	}
	return strings.Join(parts, "\n")
}

func EvaluateThinking(st *session.State, fresh []transcript.Thought) *Result {
	if len(fresh) == 0 {
		return nil
	}

	var bad []transcript.Thought
	verdict := lang.VerdictKorean
	korean := 0
	for _, th := range fresh {
		st.MarkJudged(th.Sig)
		v := lang.Classify(th.Text)
		if v == lang.VerdictEnglish && strings.HasPrefix(th.Model, "claude-sonnet") {
			v = lang.VerdictKorean
		}
		switch v {
		case lang.VerdictEnglish, lang.VerdictJapanse:
			bad = append(bad, th)
			verdict = v
		case lang.VerdictKorean:
			korean++
		}
	}

	if len(bad) == 0 {
		if korean > 0 {
			st.Streak = 0
			st.LastVerdict = string(lang.VerdictKorean)
			st.PendingBlock = false
			st.PendingReason = ""
		}
		return nil
	}

	st.Streak++
	st.LastVerdict = string(verdict)
	if verdict == lang.VerdictJapanse {
		st.JACount++
	} else {
		st.ENCount++
	}

	reason := buildMessage(st, verdict, bad[len(bad)-1])
	st.PendingBlock = true
	st.PendingReason = reason
	return &Result{
		Deny:    st.DenyCount < DenyLimit,
		Message: reason,
	}
}

func EvaluatePending(st *session.State) *Result {
	if !st.PendingBlock || strings.TrimSpace(st.PendingReason) == "" {
		return nil
	}
	return &Result{Deny: true, Message: st.PendingReason}
}

func buildMessage(st *session.State, v lang.Verdict, th transcript.Thought) string {
	count := st.ENCount + st.WatchEN
	label := "EN"
	if v == lang.VerdictJapanse {
		count, label = st.JACount+st.WatchJA, "JA"
	}
	class := "THINKING_NOT_KOREAN/" + label
	action := "The rule for this project is that reasoning is written in Korean. This block stays flagged until a Korean thinking block appears; a translation, a shortened restatement, or a conclusion carried over leaves the original reasoning in place."
	if count > RepeatSuppressAfter {
		quote := ""
		if spans := lang.EnglishSpans(th.Text, 1); len(spans) > 0 {
			quote = "\n> " + truncate(spans[0], quoteChars)
		}
		return Terse(class, count, action) + quote
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
	b.WriteString("That text is from the thinking channel of this session, which is separate from the reply and is checked on its own.\n")
	b.WriteString(action)
	if st.Streak >= 2 {
		fmt.Fprintf(&b, "\nThis is occurrence %d in an unbroken run: every notice so far was followed by another non-Korean block.", st.Streak)
	}
	return b.String()
}

func EvaluateRepeat(st *session.State, sig string, alreadyDenied bool) *Result {
	if sig == "" || alreadyDenied {
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
			"[REPEAT_CALL x%d] The same call was reissued %d times, so nothing about the state changed between them.\n"+
				"The notice that stopped the first one names what to address; the denial covers this call, not the task, and reaching the same goal by another route remains part of it.",
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
