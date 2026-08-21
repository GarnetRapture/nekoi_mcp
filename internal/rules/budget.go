package rules

import (
	"fmt"
	"strings"
)

const (
	MaxNoticeChars      = 700
	MaxTotalChars       = 1100
	RepeatSuppressAfter = 2
	FloorChars          = 220
)

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

func Terse(class string, occurrence int, action string) string {
	head := fmt.Sprintf("[%s #%d] Same finding as before.", class, occurrence)
	if action = strings.TrimSpace(action); action == "" {
		return head
	}
	return head + "\n" + action
}
