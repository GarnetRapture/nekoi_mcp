package rules

import (
	"fmt"
	"regexp"
	"strings"

	"nekoi_mcp/internal/transcript"
)

var (
	reCountingCmd = regexp.MustCompile(`(?i)\bwc\s+-l\b|\|\s*wc\b|\bfind\b[^|]*\|\s*wc\b|` +
		`\bls\b[^|]*\|\s*wc\b|\|\s*count\b|\bmeasure-object\b|\blength\b\s*$`)

	reVersionCmd = regexp.MustCompile(`(?i)(^|[;&|]\s*)[A-Za-z0-9_.-]+\s+(--version|-V|version)(\s|$)`)

	reLabeledCount = regexp.MustCompile(`(?i)(?:count|total|lines?|files?|entries|matches|results?)\D{0,12}(\d[\d,]{2,})|` +
		`(\d[\d,]{2,})\s*(?:개|줄|건|행|lines?|files?|entries|matches|results?)`)
)

func settledValue(turn *transcript.Turn, cmd string) *Result {
	if turn.Evidence == "" || cmd == "" {
		return nil
	}

	switch {
	case reVersionCmd.MatchString(cmd):
		if tool := versionSubject(cmd); tool != "" && strings.Contains(turn.Evidence, tool) {
			return &Result{Deny: true, Message: fmt.Sprintf(
				"[SETTLED_VALUE] %q appears in what this turn already read, so its presence is established. A version string is a separate fact from what the work needs, and nothing in the task so far turns on it.",
				tool)}
		}
	case reCountingCmd.MatchString(cmd):
		if n := countsInEvidence(turn.Evidence); n != "" {
			return &Result{Deny: true, Message: fmt.Sprintf(
				"[SETTLED_VALUE] This turn already read a labelled count (%s) from the project's own files. Counting the same thing again yields either that number or one that contradicts what the project states as authoritative.",
				n)}
		}
	}
	return nil
}

func versionSubject(cmd string) string {
	m := reVersionCmd.FindStringSubmatch(cmd)
	if m == nil {
		return ""
	}
	fields := strings.Fields(strings.TrimLeft(m[0], ";&| "))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

func countsInEvidence(evidence string) string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reLabeledCount.FindAllStringSubmatch(evidence, -1) {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		if len(out) >= 3 {
			break
		}
	}
	return strings.Join(out, ", ")
}
