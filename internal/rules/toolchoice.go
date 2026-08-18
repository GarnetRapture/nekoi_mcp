package rules

import (
	"encoding/json"
	"regexp"
)

var (
	reADB          = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_./-])adb(\.exe)?($|\s)`)
	rePython3      = regexp.MustCompile(`(^|[^A-Za-z0-9_./-])python3([^A-Za-z0-9_]|$)`)
	rePython       = regexp.MustCompile(`python`)
	rePyJSON       = regexp.MustCompile(`import\s+json|json\.(load|loads|dump|dumps)`)
	rePyFileIO     = regexp.MustCompile(`open\s*\([^)]*["'][rwax]\+?[bt]?["']|\.(write|writelines|write_text|write_bytes|read_text|read_bytes)\s*\(|shutil\.(copy|copy2|copyfile|move|rmtree)|os\.(remove|unlink|rename|replace|mkdir|makedirs|rmdir)`)
	reDriveRootOut = regexp.MustCompile(`>>?\s*["']?(?:[A-Za-z]:[/\\]|/[a-z]/)[^/\\ "']+\.[A-Za-z0-9]{1,8}|(?:-o|--output|--out|tee)\s+["']?(?:[A-Za-z]:[/\\]|/[a-z]/)[^/\\ "']+\.[A-Za-z0-9]{1,8}`)
	rePartial      = regexp.MustCompile(`\bhead\s+-?n?\s*[0-9]+|\btail\s+-?n?\s*[0-9]+|\|\s*head\b|\|\s*tail\b|sed\s+-n\s*['"]?[0-9]*,?[0-9]*p|\[\s*:\s*[0-9]+\s*\]|\[\s*-?[0-9]+\s*:\s*\]`)
)

// EvaluateToolChoice enforces which tool may run a given command. It reads the
// call itself, so it fires before the command executes.
func EvaluateToolChoice(toolName string, toolInput json.RawMessage) *Result {
	var in struct {
		Command string `json:"command"`
	}
	if len(toolInput) > 0 {
		_ = json.Unmarshal(toolInput, &in)
	}
	cmd := in.Command

	if toolName == "PowerShell" {
		if !reADB.MatchString(cmd) {
			return &Result{Deny: true, Message: "[TERMINAL_NOT_GIT_BASH] PowerShell is reserved for ADB; every other shell task runs through Bash (git bash).\nReissue this through Bash, using POSIX equivalents."}
		}
		return &Result{Message: "[POWERSHELL_ADB_CHECK] PowerShell for ADB touches device state outside this repo.\nProceed only if the user asked for it; otherwise cancel and do the requested work."}
	}
	if toolName != "Bash" || cmd == "" {
		return nil
	}

	switch {
	case rePython3.MatchString(cmd):
		return &Result{Deny: true, Message: "[PYTHON3_COMMAND_FORBIDDEN] This environment is pinned to `python`, not `python3`.\nReplace python3 with python and rerun unchanged."}
	case rePython.MatchString(cmd) && rePyJSON.MatchString(cmd):
		return &Result{Deny: true, Message: "[JSON_TOOL_NOT_JQ] JSON is pinned to jq; Python's json module encodes differently and corrupts text.\nRewrite the query as a jq expression."}
	case rePython.MatchString(cmd) && rePyFileIO.MatchString(cmd):
		return &Result{Deny: true, Message: "[PYTHON_FILE_IO] Python here is for analysis that prints to stdout. A file written from Python leaves no diff and is never reviewed.\nUse Read plus Edit, or Write for a genuinely new file."}
	case reDriveRootOut.MatchString(cmd):
		return &Result{Deny: true, Message: "[STRAY_TEMP_ARTIFACT] This writes a file at a drive root, where it belongs to no project and nothing cleans it up.\nRedirect to the project's temp location, or print to stdout instead."}
	case rePartial.MatchString(cmd):
		return &Result{Deny: true, Message: "[PARTIAL_DATA_JUDGMENT] head/tail/sed -n/slicing truncates the data, so any conclusion is drawn while the rest is unseen.\nQuery the full range, or aggregate with jq (count, group_by)."}
	}
	return nil
}
