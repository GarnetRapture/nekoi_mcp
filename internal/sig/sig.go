// Package sig derives stable identities for tool calls. Both the live hook
// input and the recorded transcript are reduced through the same functions,
// so a call and its record produce the same string.
package sig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var (
	reWhich   = regexp.MustCompile(`(?:^|[;&|]\s*)(?:which|command\s+-v|type\s+-p)\s+([A-Za-z0-9_.-]+)`)
	reVersion = regexp.MustCompile(`([A-Za-z0-9_.-]+)\s+(?:--version|-V)(?:\s|$)`)
)

// Call returns a signature for one tool call: the name plus its input in a
// canonical order, so an identical reissue hashes identically.
func Call(name string, input json.RawMessage) string {
	if name == "" {
		return ""
	}
	var parsed any
	if len(input) > 0 {
		_ = json.Unmarshal(input, &parsed)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|", name)
	writeCanonical(h, parsed)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func writeCanonical(w io.Writer, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s=", k)
			writeCanonical(w, t[k])
			fmt.Fprint(w, ";")
		}
	case []any:
		for _, e := range t {
			writeCanonical(w, e)
			fmt.Fprint(w, ",")
		}
	case string:
		fmt.Fprint(w, strings.Join(strings.Fields(t), " "))
	default:
		fmt.Fprintf(w, "%v", t)
	}
}

// Probe identifies a command that only asks whether a binary exists or what
// version it is. Those facts cannot change within a session, so asking twice
// yields nothing.
func Probe(name string, input json.RawMessage) string {
	if name != "Bash" && name != "PowerShell" {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &in)
	cmd := strings.Join(strings.Fields(in.Command), " ")
	if m := reWhich.FindStringSubmatch(cmd); m != nil {
		return "exists:" + strings.ToLower(m[1])
	}
	if m := reVersion.FindStringSubmatch(cmd); m != nil {
		return "version:" + strings.ToLower(m[1])
	}
	return ""
}
