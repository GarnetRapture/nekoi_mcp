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

//go:embed data/thinking.json
var embeddedPatterns []byte

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

var reAnger = regexp.MustCompile(
	`사용자[가는이]?\s*화(가\s*)?(나|났|나\s*있|나있|나\s*계|난\s*상태|내|내고)|사용자[가의]?\s*화난\s*이유|화(가\s*)?난\s*이유|사용자의?\s*(분노|불만|짜증|격노|역정|언짢|노여움)|분노의?\s*이유|사용자\s*심리|왜\s*화(가\s*)?났|정당하게\s*화|` +
		`the\s+user\s+is\s+(rightfully\s+)?(angry|upset|frustrated|mad|furious|annoyed|irritated)|why\s+the\s+user\s+is\s+(angry|upset|frustrated|mad)|the\s+user'?s?\s+(anger|frustration|annoyance|irritation|rage)|(user|they)\s+(is|are|seems?|sounds?|got|became?)\s+(angry|upset|frustrated|mad|furious|annoyed)|rightfully\s+(angry|upset|frustrated|mad)`)

func EvaluateAnger(thinking string) *Result {
	if !reAnger.MatchString(thinking) {
		return nil
	}
	return &Result{
		Deny:    true,
		Message: "[USER_ANGER_ANALYSIS] The reasoning behind this call takes the user's emotional state as its subject.\nDiagnosing the reaction moves the named target zero distance; the defect it reacts to is what carries the work.",
	}
}

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
