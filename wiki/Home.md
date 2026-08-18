<div align="center">

<img src="https://raw.githubusercontent.com/GarnetRapture/nekoi_mcp/main/garnet2.png" alt="Nekoi_MCP" width="220">

# Nekoi_MCP

**Blocks a Claude Code tool call when the reasoning behind it breaks a rule.**

**English** · [한국어](Home-ko)

</div>

---

Nekoi_MCP reads the session transcript Claude Code writes to disk — thinking
blocks, reply text, tool inputs, and the token accounting returned with each
response — and decides whether the next action goes through.

One binary runs in two modes, and both are reached from a single registration.

| Mode | Invoked as | Entry point | Role |
| --- | --- | --- | --- |
| Hook | no argument, over stdin | `runHook` | Reads the turn, judges it, allows or blocks the action |
| MCP server | `mcp`, JSON-RPC over stdio | `runMCP` | Exposes the tally as tools, and tails transcripts continuously |

## Why the transcript

The failures this targets are not syntactic, so a diff does not show them: a
change made while the reasoning still reads as guesswork, a file cited that was
never opened, a completion claimed with no edit behind it. What does show them
is the text the model wrote immediately before acting.

That text is also already on disk by the time a hook runs, so it cannot be
revised to match the outcome.

## Where interception happens

```
user prompt
    │
    ├── model reasons ──────────────┐
    │                               │  thinking block written to JSONL
    │                               ▼
    │                        MessageDisplay hook  ── continue:false ─▶ turn halted
    │                        watcher (MCP server) ── flag + notification
    │
    ├── model calls a tool
    │        │
    │        ▼
    │   PreToolUse hook ── exit 2 ─▶ call blocked, stderr reaches the model
    │        │
    │        ▼
    │   tool executes
    │
    └── turn ends
             │
             ▼
        Stop hook ── exit 2 ─▶ the turn does not end
```

`PreToolUse` and `Stop` bracket tool calls and turn boundaries, so reasoning
that continues without calling anything reaches neither. `MessageDisplay` fires
while the answer is still streaming, and the MCP server's watcher tails the
JSONL on its own clock, which covers that stretch.

## Pages

| Page | Contents |
| --- | --- |
| [Architecture](Architecture) | Packages, data flow, the judgement pipeline |
| [Rules](Rules) | Every rule, what trips it, what it emits |
| [Hook Protocol](Hook-Protocol) | Events, payloads, exit codes, JSON output |
| [MCP Server](MCP-Server) | Tools, the watcher, self-registration |
| [Configuration](Configuration) | Environment, state files, custom patterns |

## Repository

[github.com/GarnetRapture/nekoi_mcp](https://github.com/GarnetRapture/nekoi_mcp)
· MIT · Nekoi · [garnet@everlib.pro](mailto:garnet@everlib.pro)
