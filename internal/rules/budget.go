package rules

import (
	"fmt"
	"strings"
)

const (
	// MaxNoticeChars caps a single injected notice. Everything the model
	// needs — what it did, what to do — fits well inside this; anything
	// beyond it is repetition that costs input tokens on every later turn.
	MaxNoticeChars = 700
	// MaxTotalChars caps the combined notice when several rules fire at once.
	MaxTotalChars = 1100
	// RepeatSuppressAfter is how many times the same violation class is
	// spelled out in full. Past that the notice collapses to one line: the
	// model has the full text earlier in context already.
	RepeatSuppressAfter = 2
)

// FloorChars is the budget that is never cut below, whatever the session has
// already spent. A notice shorter than this cannot say what happened and what
// to do, and a notice that cannot say that costs tokens without changing
// anything — the work itself must never be starved to save budget.
const FloorChars = 220

// budgetFor narrows the per-notice allowance as the prompt grows. The input
// is the measured context size from the API's own usage accounting: anything
// injected now is re-billed on every later request in the session, so the
// larger the prompt already is, the more a notice costs to keep.
func budgetFor(contextTokens int64) int {
	limit := MaxNoticeChars
	switch {
	case contextTokens > 400_000:
		limit = FloorChars
	case contextTokens > 200_000:
		limit = 320
	case contextTokens > 80_000:
		limit = 460
	}
	return limit
}

func cut(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimSpace(string(r[:limit])) + "…"
}

// Merge joins the notices that fired this call and enforces the total budget,
// tightened by how much this session has already been sent.
func Merge(msgs []string, spentChars int64) string {
	limit := budgetFor(spentChars)
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m = strings.TrimSpace(m); m != "" {
			parts = append(parts, cut(m, limit))
		}
	}
	total := MaxTotalChars
	if l := limit * 2; l < total {
		total = l
	}
	if total < FloorChars {
		total = FloorChars
	}
	return cut(strings.Join(parts, "\n"), total)
}

// Terse collapses a repeated notice to a single line once the full text has
// already been delivered enough times to be present in context.
func Terse(class string, occurrence int) string {
	return fmt.Sprintf("[%s #%d] Same violation as before; the full notice is already in this context. Act on it.", class, occurrence)
}
