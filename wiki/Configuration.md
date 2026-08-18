# Configuration

[한국어](Configuration-ko) · [Home](Home)

---

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `CLAUDE_DIR` | `~/.claude` | Configuration root. Everything else derives from it |
| `CENSOR_PATTERNS` | `$CLAUDE_DIR/thinking.json` | Pattern file; falls back to the set embedded in the binary |

`CLAUDE_DIR` exists so the binary can be exercised against a scratch tree
without touching a real configuration.

## Paths

| Path | Written by | Contents |
| --- | --- | --- |
| `$CLAUDE_DIR/settings.json` | `selfreg` on server startup | Hook registrations |
| `$CLAUDE_DIR/thinking.json` | you | Custom banned patterns |
| `$CLAUDE_DIR/projects/*/*.jsonl` | Claude Code | Session transcripts, read only |
| `$CLAUDE_DIR/state/censor/*.json` | `session.Store` | One record per session |

## Session state

One JSON file per session, written through a temporary file and renamed. The
session id is sanitized to `[A-Za-z0-9_-]` and truncated at 80 characters before
becoming a filename; an empty id becomes `nosession`.

| Field | Owner | Meaning |
| --- | --- | --- |
| `en_count`, `ja_count` | hook | Language violations charged |
| `deny_count` | hook | Calls actually denied |
| `tool_calls` | hook | Calls seen |
| `cursor` | hook | Thinking blocks already judged |
| `streak` | hook | Consecutive violating verdicts |
| `last_verdict` | hook | `OK` / `EN` / `JA` |
| `repeat_sig`, `repeat_count` | hook | Identical consecutive call tracking |
| `watch_en`, `watch_ja`, `watch_seen` | watcher | Violations caught between hooks |
| `watch_block`, `watch_verdict`, `watch_quote` | watcher | Pending verdict for the next hook |
| `context_tokens`, `output_tokens` | hook | Billed accounting from the API |
| `injected_chars`, `notices` | hook | How much this guard has cost |

The two owners never write each other's fields — that separation is what keeps
one block from being counted twice.

## Custom patterns

```json
{
  "patterns": [
    {
      "name": "SCOPE_REDUCTION",
      "regex": "일단\\s*(핵심|일부|중요한)|for now,? (just|only)",
      "negative_regex": "전체를\\s*처리",
      "message": "You handled part of the instructed scope and reclassified the rest as separate or later.\nProcess every instructed target now."
    }
  ]
}
```

| Field | Required | Behaviour |
| --- | --- | --- |
| `name` | yes | Appears as `[INTERRUPTED: <name>]` |
| `regex` | yes | Compiled case-insensitive. An entry that does not compile is dropped |
| `negative_regex` | no | A match here cancels the hit |
| `message` | no | Injected text; a generic line is used when empty |

Patterns are compiled once per process. The first match wins. The same set is
scanned against the visible reply on Stop, so a bypass stated in the answer is
caught too.

Dropping an entry rather than failing the file is deliberate: one bad rule must
not be able to disable the guard.

## Build

```
git clone https://github.com/GarnetRapture/nekoi_mcp
cd nekoi_mcp
make            # or: go build -o nekoi_mcp ./cmd
```

`make` builds four targets into `dist/`. Windows additionally needs `windres`
(MinGW) for the icon and version block; the resource object is named
`*_windows_amd64.syso` so the Go toolchain links it into that build only.

| Target | Output |
| --- | --- |
| `make windows` | `dist/windows-amd64/nekoi_mcp.exe` |
| `make linux` | `dist/linux-amd64/nekoi_mcp` |
| `make darwin` | `dist/darwin-amd64/nekoi_mcp` |
| `make darwin-arm64` | `dist/darwin-arm64/nekoi_mcp` |

`make check` runs `go vet ./...`.

## Limits

`DenyLimit` is 300 per session. Past it, language rules degrade to notices so a
model that cannot recover never wedges the session permanently. The watcher's
verdict is the one exception and denies unconditionally.

`ToolBudget` is 35 calls for one instruction — a notice, not a denial.

`transcriptTail` is 2000 records for the hook and 40 for `MessageDisplay`. The
first is wide enough that a long turn's early tool calls stay inside the window;
when the tail cuts them off, their paths vanish from evidence and a file the
turn genuinely edited reads as an unbacked citation.
