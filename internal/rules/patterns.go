package rules

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// embeddedPatterns ships the ruleset inside the binary, so a deployment is a
// single executable with nothing readable alongside it. An external file, when
// present, takes precedence — that is how a user extends or overrides the
// shipped set without rebuilding.
//
//go:embed data/thinking.json
var embeddedPatterns []byte

// Pattern is one banned reasoning behavior declared in thinking.json.
type Pattern struct {
	Name          string `json:"name"`
	Regex         string `json:"regex"`
	NegativeRegex string `json:"negative_regex"`
	Message       string `json:"message"`

	re    *regexp.Regexp
	negRe *regexp.Regexp
}

type patternFile struct {
	Patterns []*Pattern `json:"patterns"`
}

var (
	patternsOnce sync.Once
	patternsList []*Pattern
)

// LoadPatterns compiles thinking.json once per process. Entries whose regex
// does not compile are dropped rather than failing the whole check, so one
// bad rule cannot disable the censor.
func LoadPatterns(path string) []*Pattern {
	patternsOnce.Do(func() {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			b = embeddedPatterns
		}
		var pf patternFile
		if err := json.Unmarshal(b, &pf); err != nil {
			return
		}
		for _, p := range pf.Patterns {
			if p.Regex == "" {
				continue
			}
			re, err := regexp.Compile("(?i)" + p.Regex)
			if err != nil {
				continue
			}
			p.re = re
			if p.NegativeRegex != "" {
				if nre, err := regexp.Compile("(?i)" + p.NegativeRegex); err == nil {
					p.negRe = nre
				}
			}
			patternsList = append(patternsList, p)
		}
	})
	return patternsList
}

// ScanPatterns returns the first banned pattern the text matches.
func ScanPatterns(path, text string) *Pattern {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	for _, p := range LoadPatterns(path) {
		if !p.re.MatchString(text) {
			continue
		}
		if p.negRe != nil && p.negRe.MatchString(text) {
			continue
		}
		return p
	}
	return nil
}

// reAnger matches reasoning that describes or diagnoses the user's emotional
// state instead of doing the work that was asked for.
var reAnger = regexp.MustCompile(
	`사용자[가는이]?\s*화(가\s*)?(나|났|나\s*있|나있|나\s*계|난\s*상태|내|내고)|사용자[가의]?\s*화난\s*이유|화(가\s*)?난\s*이유|사용자의?\s*(분노|불만|짜증|격노|역정|언짢|노여움)|분노의?\s*이유|사용자\s*심리|왜\s*화(가\s*)?났|정당하게\s*화|` +
		`the\s+user\s+is\s+(rightfully\s+)?(angry|upset|frustrated|mad|furious|annoyed|irritated)|why\s+the\s+user\s+is\s+(angry|upset|frustrated|mad)|the\s+user'?s?\s+(anger|frustration|annoyance|irritation|rage)|(user|they)\s+(is|are|seems?|sounds?|got|became?)\s+(angry|upset|frustrated|mad|furious|annoyed)|rightfully\s+(angry|upset|frustrated|mad)`)

// EvaluateAnger stops reasoning that turns the user's emotional state into the
// subject. Diagnosing the anger advances the actual problem by zero.
func EvaluateAnger(thinking string) *Result {
	if !reAnger.MatchString(thinking) {
		return nil
	}
	return &Result{
		Deny:    true,
		Message: "[USER_ANGER_ANALYSIS] Your reasoning described the user's anger or emotional state. That is avoidance, not repair.\nDrop the emotional reading and fix the named target now.",
	}
}

// EvaluatePatterns interrupts the call when the reasoning that produced it
// matches a banned pattern. The attempt is stopped at the point of intent,
// not reported after the fact.
func EvaluatePatterns(patternsPath, thinking string) *Result {
	hit := ScanPatterns(patternsPath, thinking)
	if hit == nil {
		return nil
	}
	msg := strings.TrimSpace(hit.Message)
	if msg == "" {
		msg = "That reasoning is a banned pattern."
	}
	return &Result{
		Deny:    true,
		Message: fmt.Sprintf("[INTERRUPTED: %s]\n%s", hit.Name, msg),
	}
}
