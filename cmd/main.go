package main

import (
	"os"
	"path/filepath"
)

// claudeDir resolves the global Claude configuration directory. CLAUDE_DIR
// overrides it so the binary can be exercised against a scratch tree.
func claudeDir() string {
	if v := os.Getenv("CLAUDE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".claude")
}

func stateDir() string {
	return filepath.Join(claudeDir(), "state", "censor")
}

func patternsPath() string {
	if v := os.Getenv("CENSOR_PATTERNS"); v != "" {
		return v
	}
	return filepath.Join(claudeDir(), "thinking.json")
}

func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "mcp":
		os.Exit(runMCP())
	case "version", "--version", "-v", "about":
		os.Exit(printAbout())
	default:
		os.Exit(runHook())
	}
}
