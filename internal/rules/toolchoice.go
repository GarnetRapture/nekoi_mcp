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
			return &Result{Deny: true, Message: "[TERMINAL_NOT_GIT_BASH] In this project PowerShell is reserved for ADB, and every other shell task runs through Bash (git bash). The same command through Bash, with POSIX equivalents for any PowerShell-only cmdlet, is the supported form."}
		}
		return &Result{Message: "[POWERSHELL_DEVICE_STATE] PowerShell for ADB reaches a connected device, whose state lives outside the repository and outside any diff."}
	}
	if toolName != "Bash" || cmd == "" {
		return nil
	}

	switch {
	case rePython3.MatchString(cmd):
		return &Result{Deny: true, Message: "[PYTHON3_COMMAND_FORBIDDEN] This environment is pinned to `python`; `python3` resolves to a different alias and version path here. The same script under `python` is the supported form."}
	case rePython.MatchString(cmd) && rePyJSON.MatchString(cmd):
		return &Result{Deny: true, Message: "[JSON_TOOL_NOT_JQ] JSON handling here is pinned to jq. Python's json module encodes differently and corrupts non-ASCII text, so the same query expressed in jq is the supported form."}
	case rePython.MatchString(cmd) && rePyFileIO.MatchString(cmd):
		return &Result{Deny: true, Message: "[PYTHON_FILE_IO] Python here is for analysis that prints to stdout. A file written from Python leaves no diff and never reaches review, whereas Read plus Edit does, and Write covers a genuinely new file."}
	case reDriveRootOut.MatchString(cmd):
		return &Result{Deny: true, Message: "[STRAY_TEMP_ARTIFACT] This redirects output to a drive root, where the file belongs to no project and nothing cleans it up. The project's temp location and stdout are the two destinations that stay accounted for."}
	case rePartial.MatchString(cmd):
		return &Result{Deny: true, Message: "[PARTIAL_DATA_JUDGMENT] head, tail, sed -n and slicing truncate the data, so a conclusion drawn from the result rests on the portion that was read while the rest stayed unseen. The full range, or an aggregate through jq (count, group_by), covers the whole set."}
	}
	return nil
}
