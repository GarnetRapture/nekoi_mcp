# MCP Server

[한국어](MCP-Server-ko) · [Home](Home)

---

```
claude mcp add --scope user nekoi_mcp -- /path/to/nekoi_mcp mcp
```

That line is the whole installation. The server speaks JSON-RPC over stdio,
lives for the whole session, and does three things the hook cannot: it exposes
the tally as tools, it tails transcripts continuously, and it writes its own
hook registration.

## Protocol

Protocol version `2026-07-28`. The `initialize` reply echoes whatever version
the client asked for — pinning our own would fail the handshake against a client
on a different revision.

```json
{ "protocolVersion": "<echoed>",
  "capabilities": { "tools": { "listChanged": false }, "logging": {} },
  "serverInfo": { "name": "Nekoi_MCP", "version": "1.0.0",
                  "author": "Nekoi", "contact": "garnet@everlib.pro" } }
```

`logging` is declared because it is what lets the watcher's verdict reach the
client: `notifications/message` is only honoured when the server declares it.

| Method | Behaviour |
| --- | --- |
| `initialize` | Handshake, capabilities |
| `tools/list` | The three tools below |
| `tools/call` | Dispatch, after the watcher's pending check |
| `ping` | Empty result |

Requests with no `id` are notifications and get no reply.

## Tools

| Tool | Argument | Returns |
| --- | --- | --- |
| `censor_session_status` | `session_id` (optional) | One session's tally: EN/JA counts, denials, calls, streak, last verdict, watcher counts, context and output tokens, notices, injected characters, cwd |
| `censor_sessions` | `limit` (default 10) | Recorded sessions, newest first |
| `censor_check_text` | `text` (required) | Classifies text with the same rule the hook uses, quotes the English sentences it found, and names any banned pattern it matches |

Omitting `session_id` uses the most recently updated session.

`censor_check_text` is the one way to exercise the classifier without producing
a violation — it runs `lang.Classify`, `lang.EnglishSpans` and `ScanPatterns`
over arbitrary text.

## The watcher

The hook fires before a tool call and at turn end. Reasoning produced between
those points reaches neither. The server outlives every hook invocation, so a
goroutine there can tail the transcript and settle each block as it lands.

**Polling.** Every 300 ms, every `*.jsonl` under `CLAUDE_DIR/projects/*/` is
stat'd and only the bytes appended since the previous pass are read. A file that
shrank was rotated, so its offset resets to zero.

**First sight.** A transcript seen for the first time has its offset set to the
current size *without being judged*. Everything already on disk was written
before this server existed; charging a past session's reasoning against the call
being made right now would be wrong, and on startup it would flag every historic
block at once.

**Partial lines.** `consume` stops at the last complete line. A line still being
written is left for the next pass, so a half-written JSON record is never
parsed.

**What it does on a hit.** In order: append to the in-memory `recent` ring,
store `pending` for this process to act on, then load the session state, raise
`WatchEN`/`WatchJA`/`WatchSeen`, set `WatchBlock` with the verdict and the
offending excerpt, save, and finally notify the client.

State is written *before* the notification, so a client that ignores the message
still leaves the tally and the flag for the next hook.

**Ownership.** `Streak` and `LastVerdict` belong to the hook — it resets them
when a turn comes back clean, and a second writer would make its repeat count
wrong. The watcher owns only its own fields.

### What the watcher can and cannot do

It can settle the verdict before any hook runs, record it durably, refuse this
server's own tool calls, and push a message to the client.

It cannot stop the model. Only a hook can deny a call. Elicitation is sent
*during the processing of a request the server received*, not on the server's
own initiative, and `notifications/cancelled` is restricted to tearing down a
subscription stream. So the watcher's job is to make the verdict already true by
the time a hook runs, and to record the full count rather than whatever a late
single pass happens to catch.

### Immediate refusal

`callTool` checks `TakePending()` before dispatching. If the watcher caught a
violation since the last call, the tool call is refused with the offending block
quoted — no hook involved. The pending flag is cleared as it is taken, because
a flag left standing would refuse every later call.

### Notifications

Both the response loop and the watcher write to the same stdout, so every write
goes through one mutex. Two writers interleaving would produce JSON neither side
can parse.

```json
{ "jsonrpc": "2.0", "method": "notifications/message",
  "params": { "level": "error", "logger": "Nekoi_MCP",
              "data": "[WATCH_EN] The transcript watcher read this thinking block as it was written:\n> …" } }
```

`Stop()` waits for the polling goroutine to finish before returning, because the
caller flushes and closes the connection the watcher writes on.

## Self-registration

The two modes are separate registrations: `mcp` on the command line reaches the
server, no argument reaches the hook. Registering only the server would yield
the tools and none of the enforcement.

So on startup `selfreg.Ensure` adds the binary to `PreToolUse`, `Stop` and
`MessageDisplay` — only to the ones whose entries do not already run this path.

- Unknown top-level keys are held as `json.RawMessage`, so nothing outside
  `hooks` is rewritten by the round trip.
- Existing hook entries are inspected rather than compared whole, because a
  matcher may carry fields this package does not model.
- Path comparison goes through `sig.NormalizePath`: Windows accepts either
  separator and ignores case, and an entry written by hand may use the other one.
- Nothing is written when nothing changed.
- The write goes to a temporary file that is then renamed, so a failure partway
  leaves the original settings intact.

A failure is not fatal — the MCP tools still work and the next startup tries
again.
