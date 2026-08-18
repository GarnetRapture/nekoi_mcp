# Architecture

[한국어](Architecture-ko) · [Home](Home)

---

## Package map

```
cmd/
  main.go        mode dispatch, path resolution
  hook.go        hook mode: read turn, run rules, emit decision
  mcp.go         MCP server: JSON-RPC loop, tools, client notifier
  about.go       compiled-in name, version, authorship

internal/
  lang/          sentence-level language classification
  transcript/    JSONL parsing, turn extraction, usage accounting
  session/       per-session state, atomic persistence
  sig/           canonical identity for calls, probes and paths
  rules/         the rules, notice budget, embedded patterns
  watch/         live transcript tailing
  selfreg/       self-registration as a hook
```

Dependencies run one way. `lang`, `sig` and `session` depend on nothing inside
the project. `transcript` uses `sig`. `rules` uses `lang`, `session`,
`transcript` and `sig`. `watch` uses `lang` and `session`. `selfreg` uses `sig`.
`cmd` sits on top of all of them.

## Mode dispatch

`main` reads `os.Args[1]` and nothing else:

| Argument | Function | Registered as |
| --- | --- | --- |
| `mcp` | `runMCP` | `claude mcp add … -- <binary> mcp` |
| `version` / `--version` / `-v` / `about` | `printAbout` | — |
| anything else, including none | `runHook` | `hooks` entry in `settings.json` |

The two registrations are separate, which is why the server writes the hook
entry itself on startup — see [MCP Server](MCP-Server#self-registration).

Paths all derive from one root:

```
CLAUDE_DIR (default ~/.claude)
├── settings.json          hook registrations, written by selfreg
├── thinking.json          custom patterns (CENSOR_PATTERNS overrides)
├── projects/              one directory per working tree
│   └── <slug>/<uuid>.jsonl  session transcripts, tailed by watch
└── state/censor/          one JSON file per session
```

## Reading a turn

`transcript.Load(path, maxLines)` parses the tail of a JSONL file into a
`Turn`. The work it does is not a plain unmarshal:

**Ring buffer.** Only the last `maxLines` records are kept, so a long session
does not grow the parse cost without bound. The hook uses 2000; the
`MessageDisplay` path uses 40, because it only ever needs the block being
written.

**Turn boundary.** Scanning backwards for the last real user prompt — a `user`
record that is not meta and carries a non-empty text block — fixes `start`.
Records at or after `start` belong to this turn; earlier ones are context.

**Failed calls are excluded.** A call a hook denied still leaves its `tool_use`
block in the transcript, answered by an error `tool_result`. It never ran, so
it observed nothing and consumed nothing. `failedCalls` collects the ids of
every `tool_use` whose result came back an error, and the main pass skips them
entirely — no signature, no tool count, no probe key, no edit path. Without
this, one denial would mark a command as already-run and refuse every retry.

**Evidence.** The user prompt, every `tool_result` body, and every `tool_use`
input are concatenated and normalized through `sig.NormalizePath`. This is the
set of things the turn actually observed, and it is what [reporting
rules](Rules#reporting) check citations against.

**Usage.** `input_tokens`, `output_tokens` and both cache counters come from
the API response recorded with each assistant message. Context size is measured,
never estimated from text length.

### What a Turn carries

| Field | Meaning |
| --- | --- |
| `UserPrompt` | The last real user message |
| `Thoughts` | Thinking blocks in this turn, each with its model id |
| `TotalThought` | Thinking blocks in the whole window — the cursor's scale |
| `AssistantTxt` | Visible reply text of this turn |
| `Evidence` | Everything observed, normalized, lowercased |
| `ToolCalls`, `CallSigs` | Executed calls this turn and their signatures |
| `ProbeKeys` | Environment probes, session-wide |
| `EditFiles`, `Edited`, `Probed`, `Bashed` | Per-category counts |
| `ContextTokens`, `OutputTokens` | Billed accounting |

## The cursor

A thinking block is charged once. `session.State.Cursor` holds how many blocks
have been judged; `freshThinking` and `EvaluateThinking` both skip that many
from the front of the window before looking.

This creates a gap the rules close explicitly: once a block is charged the
cursor moves past it, so calling again *without reasoning at all* leaves no
fresh block to judge. `EvaluateUnresolved` catches exactly that — a last
verdict of EN or JA with empty fresh reasoning is a notice proceeded past
rather than answered.

## Judgement pipeline

`runHook` builds one message list and one deny flag.

```
                    ┌── PreToolUse only ──────────────────────┐
                    │  EvaluateToolChoice   (tool + command)  │
                    │  EvaluateProcedure    (fresh reasoning) │
                    │  EvaluateAskUser      (tool name)       │
                    │  EvaluateWriteTarget  (file path)       │
                    │  EvaluateWaste        (turn signatures) │
                    │  EvaluateRepeat       (session state)   │
                    │  EvaluateUnresolved   (state + fresh)   │
                    └─────────────────────────────────────────┘
                    ┌── Stop only ────────────────────────────┐
                    │  EvaluateReporting    (reply vs evidence)│
                    │  EvaluateTurnEnd      (reply, tool use) │
                    │  EvaluateSplitReport  (reply)           │
                    │  EvaluateEditFlow     (edit count)      │
                    │  ScanPatterns         (reply)           │
                    └─────────────────────────────────────────┘
                    ┌── every event ──────────────────────────┐
                    │  EvaluateAnger        (fresh reasoning) │
                    │  EvaluatePatterns     (fresh reasoning) │
                    │  EvaluateWatchBlock   (watcher flag)    │
                    │  EvaluateThinking     (fresh blocks)    │
                    └─────────────────────────────────────────┘
                                    │
                            rules.Merge(msgs, contextTokens)
                                    │
                    deny? ── yes ─▶ stderr + exit 2
                       │
                       no ─▶ stdout JSON (additionalContext / decision)
```

`MessageDisplay` does not enter this pipeline. It routes to `runDisplay`, which
judges only the newest thinking block and answers with `continue:false` —
exit 2 carries no weight on that event.

## Notice budget

Everything injected is re-billed on every later request in the session, so the
allowance narrows as the prompt grows. The input is the measured context size.

| Context tokens | Per-notice budget |
| --- | --- |
| under 80k | 700 chars |
| 80k – 200k | 460 chars |
| 200k – 400k | 320 chars |
| over 400k | 220 chars |

220 is a floor that is never crossed: a notice too short to say what happened
and what to do costs tokens without changing anything. When several rules fire
at once, the combined text is capped at twice the per-notice limit, up to 1100.

After the same violation class has been spelled out twice, later notices
collapse — but the *instruction* survives the collapse. A violation on its Nth
repeat is evidence that the earlier text stopped steering, so dropping the one
line that says what to do would be exactly backwards.

## Persistence

`session.Store` writes one JSON file per session under `state/censor/`. Every
write goes to a temporary file and is renamed, so a reader never sees a partial
record. Session ids are sanitized to `[A-Za-z0-9_-]` and truncated at 80
characters before becoming a filename.

The hook and the watcher share this file but own different fields. `ENCount`,
`JACount`, `Streak`, `LastVerdict` and `Cursor` belong to the hook. `WatchEN`,
`WatchJA`, `WatchSeen`, `WatchBlock`, `WatchVerdict` and `WatchQuote` belong to
the watcher. Keeping them apart is what stops one block from being counted
twice by two independent readers.
