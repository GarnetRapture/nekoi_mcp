package selfreg

import (
	"encoding/json"
	"os"
	"path/filepath"

	"nekoi_mcp/internal/sig"
)

// Events are the hook events this binary handles. MessageDisplay is the one
// that fires mid-stream, while reasoning is being written; the other two
// bracket tool calls and the end of a turn.
var Events = []string{"PreToolUse", "Stop", "MessageDisplay"}

// matcherless are the events that always fire and accept no matcher. A
// "matcher" key on one of these does not widen the registration — it makes the
// entry malformed, and the hook never runs.
var matcherless = map[string]bool{"Stop": true, "MessageDisplay": true}

// Ensure adds this binary to every event in Events that does not already run
// it, leaving every other setting and every existing hook entry untouched. It
// reports whether the file was changed.
func Ensure(settingsPath, exePath string) (bool, error) {
	exe := sig.NormalizePath(exePath)
	if exe == "" {
		return false, nil
	}

	// Unknown top-level keys stay as raw JSON so nothing outside hooks is
	// rewritten by a round trip through this process.
	root := map[string]json.RawMessage{}
	if b, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(b, &root); err != nil {
			return false, err
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}

	hooks := map[string][]json.RawMessage{}
	if raw, ok := root["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return false, err
		}
	}

	changed := false
	for _, event := range Events {
		if runsExe(hooks[event], exe) {
			continue
		}
		entry, err := json.Marshal(map[string]any{
			"matcher": "*",
			"hooks":   []any{map[string]string{"type": "command", "command": exePath}},
		})
		if err != nil {
			return false, err
		}
		hooks[event] = append(hooks[event], entry)
		changed = true
	}
	if !changed {
		return false, nil
	}

	rawHooks, err := json.Marshal(hooks)
	if err != nil {
		return false, err
	}
	root["hooks"] = rawHooks

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return false, err
	}
	// Written aside and renamed, so a reader never sees a half-written file
	// and a failure partway leaves the original settings intact.
	tmp := settingsPath + ".censor.tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, settingsPath); err != nil {
		return false, err
	}
	return true, nil
}

// runsExe reports whether any entry already invokes this binary. Each entry is
// inspected rather than compared whole, because a matcher may carry fields this
// package does not model.
func runsExe(entries []json.RawMessage, exe string) bool {
	for _, raw := range entries {
		var entry struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		for _, h := range entry.Hooks {
			if sig.NormalizePath(h.Command) == exe {
				return true
			}
		}
	}
	return false
}
