# Hook Protocol

[한국어](Hook-Protocol-ko) · [Home](Home)

---

The editor runs the binary with no argument, writes a JSON payload to stdin,
and reads the exit code, stdout and stderr. Three events are registered.

## Events

| Event | Fires | Blocking channel |
| --- | --- | --- |
| `PreToolUse` | Before a tool call executes | exit 2 |
| `Stop` | When the turn is about to end | exit 2 |
| `MessageDisplay` | While assistant text is streaming | JSON `continue:false` |

`MessageDisplay` is the one that reaches reasoning as it is written. A thinking
block is in the JSONL before it is rendered, so by the time this event fires the
block can be read where it lies. Exit 2 carries no weight on it — the halt has
to travel as JSON.

## Input

```json
{
  "session_id": "2c5012e5-…",
  "cwd": "n:/nekoi_mcp",
  "hook_event_name": "PreToolUse",
  "tool_name": "Edit",
  "tool_input": { "file_path": "…", "old_string": "…", "new_string": "…" },
  "transcript_path": "C:/Users/…/projects/<slug>/<uuid>.jsonl"
}
```

When `hook_event_name` is absent it is inferred: a payload carrying
`tool_name` is `PreToolUse`, otherwise `Stop`. An empty `transcript_path`
returns 0 immediately — there is nothing to judge without the record.

## Output

### Blocking

```
exit 2
stderr: [RULE_NAME] what happened
        what to do instead
        [denied #12/300]
```

stderr reaches the model and the action does not proceed, whatever a reader
makes of stdout. A decision this settled is not left to JSON parsing.

`DenyCount` is capped at `DenyLimit` (300). Past it, language rules degrade to
notices so a model that cannot recover never wedges the session permanently.
The watcher's own verdict is the one exception and denies unconditionally.

### Non-blocking, PreToolUse

```json
{ "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "additionalContext": "[TOOL_BUDGET 35/35] …"
} }
```

### Non-blocking, Stop

```json
{ "decision": "block", "reason": "[UNVERIFIED_FILE_REFERENCE] …" }
```

`decision: block` on Stop does not end anything — it prevents the turn from
ending, which is what puts the notice in front of the model with work still to
do.

### MessageDisplay

```json
{ "continue": false,
  "stopReason": "[HALTED: THINKING_NOT_KOREAN/EN — caught as it was written, #7 this session]\n> …\nStopped here, mid-reasoning, without waiting for a tool call. …" }
```

`continue:false` takes precedence over any event-specific decision field.

## runDisplay

`MessageDisplay` does not enter the main pipeline. It routes to `runDisplay`,
which is deliberately narrow because this event fires on every redraw of a
streaming message:

1. Load only the last 40 records.
2. Take the newest thinking block.
3. Classify it. `EN` on a Sonnet model returns immediately; anything that is
   not `EN` or `JA` returns immediately.
4. **Charge once.** If `TotalThought <= Cursor` the block was already counted —
   return. Otherwise advance the cursor.
5. Update the tally, save, and emit `continue:false` with the offending
   sentence quoted.

Step 4 is what keeps a streaming message from counting as a new violation on
every frame.

## Registration

```json
{
  "hooks": {
    "PreToolUse":     [ { "matcher": "*", "hooks": [ { "type": "command", "command": "/path/to/nekoi_mcp" } ] } ],
    "Stop":           [ { "matcher": "*", "hooks": [ { "type": "command", "command": "/path/to/nekoi_mcp" } ] } ],
    "MessageDisplay": [ { "matcher": "*", "hooks": [ { "type": "command", "command": "/path/to/nekoi_mcp" } ] } ]
  }
}
```

This does not need to be written by hand — the MCP server writes it on startup.
See [self-registration](MCP-Server#self-registration).
