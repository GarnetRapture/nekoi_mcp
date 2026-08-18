package lang

import (
	"regexp"
	"strings"
	"unicode"
)

type Verdict string

const (
	VerdictNone    Verdict = "NONE"
	VerdictEmpty   Verdict = "NOTHINK"
	VerdictKorean  Verdict = "OK"
	VerdictEnglish Verdict = "EN"
	VerdictJapanse Verdict = "JA"
)

var (
	reBacktick   = regexp.MustCompile("`[^`]*`")
	rePath       = regexp.MustCompile(`[A-Za-z0-9_.:-]*[/\\][A-Za-z0-9_.:/\\-]*`)
	reDotted     = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z0-9_.]+`)
	reSnake      = regexp.MustCompile(`[A-Za-z]+_[A-Za-z0-9_]+`)
	reCamel      = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]*)+\b`)
	reSentence   = regexp.MustCompile(`(?:[.!?。]\s+|\n+)`)
	reWord       = regexp.MustCompile(`\b[A-Za-z]{2,}\b`)
	funcWordsSet = buildFuncWords()
)

func buildFuncWords() map[string]struct{} {
	words := []string{
		"i", "we", "you", "they", "he", "she", "it", "the", "a", "an", "this", "that",
		"these", "those", "there", "here", "is", "are", "was", "were", "be", "been",
		"being", "am", "do", "does", "did", "have", "has", "had", "to", "of", "in",
		"on", "at", "by", "from", "with", "without", "for", "about", "into", "over",
		"under", "and", "or", "but", "so", "if", "then", "than", "because", "since",
		"while", "although", "though", "however", "therefore", "moreover", "instead",
		"need", "needs", "needed", "should", "will", "would", "can", "could", "must",
		"may", "might", "shall", "not", "now", "just", "only", "also", "still", "yet",
		"actually", "really", "let", "wait", "looking", "based", "writing", "reading",
		"finishing", "checking", "implementing", "designing", "building", "running",
		"calculating", "parsing", "extracting", "transforming", "computing", "adding",
		"processing", "rewriting", "removing", "replacing", "updating", "modifying",
		"applying", "defining", "converting", "trying", "going", "making", "getting",
		"seeing", "using", "keeping", "starting", "moving", "setting", "handling",
		"my", "me", "our", "your", "their", "its", "his", "her", "what", "which",
		"who", "when", "where", "why", "how", "all", "some", "any", "each", "both",
	}
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}

func countHangul(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			n++
		}
	}
	return n
}

func hasKana(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30FF {
			return true
		}
	}
	return false
}

// Prose strips code identifiers, paths and backtick spans so that only
// narration remains. Those forms are allowed to stay in their original
// script by the rule, so they must not influence the language verdict.
func Prose(s string) string {
	s = reBacktick.ReplaceAllString(s, " ")
	s = rePath.ReplaceAllString(s, " ")
	s = reDotted.ReplaceAllString(s, " ")
	s = reSnake.ReplaceAllString(s, " ")
	s = reCamel.ReplaceAllString(s, " ")
	return s
}

func hasFuncWord(words []string) bool {
	for _, w := range words {
		if _, ok := funcWordsSet[strings.ToLower(w)]; ok {
			return true
		}
	}
	return false
}

// IsEnglishSentence reports whether a single sentence is English narration:
// no Hangul, at least three alphabetic words, and at least one function word.
func IsEnglishSentence(s string) bool {
	if countHangul(s) > 0 {
		return false
	}
	words := reWord.FindAllString(s, -1)
	if len(words) < 3 {
		return false
	}
	return hasFuncWord(words)
}

// Classify returns the verdict for one raw thinking block.
func Classify(text string) Verdict {
	if strings.TrimSpace(text) == "" {
		return VerdictEmpty
	}
	if hasKana(text) {
		return VerdictJapanse
	}
	sentences := reSentence.Split(Prose(text), -1)
	en, ko := 0, 0
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		switch {
		case IsEnglishSentence(s):
			en++
		case countHangul(s) > 0:
			ko++
		}
	}
	if en == 0 {
		return VerdictKorean
	}
	if en > ko {
		return VerdictEnglish
	}
	return VerdictKorean
}

// EnglishSpans returns the English sentences found in the text, so the
// notice can quote exactly what tripped the check instead of the whole block.
func EnglishSpans(text string, limit int) []string {
	var out []string
	for _, s := range reSentence.Split(Prose(text), -1) {
		s = strings.TrimSpace(s)
		if s == "" || !IsEnglishSentence(s) {
			continue
		}
		out = append(out, collapse(s))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func collapse(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteRune(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}
