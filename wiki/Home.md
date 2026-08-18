<div align="center">

<img src="https://raw.githubusercontent.com/GarnetRapture/nekoi_mcp/main/garnet2.png" alt="Nekoi_MCP" width="220">

# Nekoi_MCP

**Judges Claude Code by its own transcript, and stops the attempt at the point of intent — not after the fact.**

**English** · [한국어](Home-ko)

</div>

---

Nekoi_MCP is a guard that sits between Claude Code and the actions it is about
to take. Its evidence is never an inference about the model: it is the session
transcript the editor has already written to disk — the thinking blocks, the
reply text, the tool inputs, and the billed token accounting that came back
with each response.

One binary runs in two modes, and both are reached from a single registration.

| Mode | Invoked as | Entry point | Role |
| --- | --- | --- | --- |
| Hook | no argument, over stdin | `runHook` | Reads the turn, judges it, allows or blocks the action |
| MCP server | `mcp`, JSON-RPC over stdio | `runMCP` | Exposes the tally as tools, and tails transcripts continuously |

## Why the transcript

A linter reads code. This reads the reasoning that produced the code, because
the failures it targets are not syntactic — a change made while the reasoning
still reads as guesswork, a file cited that was never opened, a completion
claimed with no edit behind it. None of those are visible in the diff. All of
them are visible in what the model wrote just before acting.

The transcript is also the only account that cannot be revised after the fact.
By the time a hook runs, the block it judges is already on disk.

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

The gap that matters is the first one. `PreToolUse` and `Stop` bracket tool
calls and turn boundaries; reasoning that continues without calling anything
reaches neither. `MessageDisplay` fires while the answer is still streaming,
and the MCP server's watcher tails the JSONL on its own clock, so that stretch
is not blind.

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
