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

const digestChars = 32

var (
	reWhich   = regexp.MustCompile(`(?:^|[;&|]\s*)(?:which|command\s+-v|type\s+-p)\s+([A-Za-z0-9_.-]+)`)
	reVersion = regexp.MustCompile(`([A-Za-z0-9_.-]+)\s+(?:--version|-V)(?:\s|$)`)
)

func NormalizePath(s string) string {
	s = strings.ReplaceAll(s, `\\`, "/")
	s = strings.ReplaceAll(s, `\`, "/")
	s = strings.ToLower(strings.TrimSpace(s))
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func Text(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:digestChars]
}

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
	return hex.EncodeToString(h.Sum(nil))[:digestChars]
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

func Command(name string, input json.RawMessage) string {
	if name != "Bash" && name != "PowerShell" {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &in)
	return in.Command
}

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
