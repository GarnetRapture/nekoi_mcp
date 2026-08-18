<div align="center">

<img src="garnet2.png" alt="Nekoi_MCP" width="300">

# Nekoi_MCP

**Blocks a Claude Code tool call when the reasoning behind it breaks a rule.**

[![Go](https://img.shields.io/badge/Go-1.26-8B1A2B?style=flat-square&labelColor=1A1A1A)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-2026--07--28-8B1A2B?style=flat-square&labelColor=1A1A1A)](https://modelcontextprotocol.io)
[![Platforms](https://img.shields.io/badge/windows%20·%20linux%20·%20macos-D4AF37?style=flat-square&labelColor=1A1A1A)](#install)
[![License](https://img.shields.io/badge/License-MIT-D4AF37?style=flat-square&labelColor=1A1A1A)](LICENSE)

**English** · [한국어](README.ko.md)

</div>

---

A guard for Claude Code that reads what the model actually wrote before a tool
call and blocks the call when the reasoning behind it breaks a rule.

It ships as one binary that runs in two modes: a hook the editor invokes on
every tool call and turn end, and an MCP server that reports what the guard has
recorded for a session.

## What it stops

The guard reads the session transcript, so its evidence is the model's own
recorded output rather than an inference about it.

**Reasoning language.** Thinking blocks must be Korean. The check strips code
identifiers, paths and backtick spans first, then counts sentences: a sentence
is English only if it has no Hangul, at least three alphabetic words and a
function word. A block is flagged only when English sentences outnumber Korean
ones, so an identifier or a borrowed term does not trip it. Once a block has
been flagged it is consumed by a cursor and never charged again.

**Reasoning procedure.** Before a file changes, the reasoning has to close four
questions: what caused this, what was pointed out, what follows, what procedure
satisfies it. Missing two or more of them denies the call. So does changing a
file while the reasoning still reads as trying something out, and re-analyzing
after acting without saying what the last result settled.

**Wasted calls.** An identical call already made this turn, a re-probe of a
binary's presence or version already established in the session, and an
investigation past 35 calls for one instruction.

**Tool choice.** PowerShell for anything but ADB, `python3` where `python` is
pinned, Python's `json` module where `jq` is pinned, Python file I/O in place of
Edit, output written to a drive root, and `head`/`tail`/`sed -n`/slicing that
truncates data a conclusion is then drawn from.

**Reporting.** File paths cited as evidence that nothing this turn opened,
command output quoted when no command ran, a change reported as done with no
Edit call, and work reported as a split between confirmed and unconfirmed.

**Turn end.** A turn that calls no tool and ends on filler, or on a question
handed back to the user.

**Custom patterns.** `thinking.json` holds the pattern set — a regex, an
optional negative regex, and the message to inject. The shipped set is compiled
into the binary; an external file at `~/.claude/thinking.json`, or the path in
`CENSOR_PATTERNS`, replaces it without a rebuild.

## Token control

Everything the guard injects is billed on every later request in the session,
so the notice budget narrows as the prompt grows. The size is read from the
API's own usage accounting in the transcript, not estimated from text.

| Context tokens | Per-notice budget |
| --- | --- |
| under 80k | 700 chars |
| 80k – 200k | 460 chars |
| 200k – 400k | 320 chars |
| over 400k | 220 chars |

220 characters is a floor, never crossed: a notice too short to say what
happened and what to do costs tokens without changing anything. After the same
violation has been spelled out twice, later notices collapse to one line plus
the offending sentence, since the full text is already in context.

## Install

Prebuilt binaries are committed under `dist/`, so a Go toolchain is not
required — download the one for your platform and run it. On Linux and macOS,
mark it executable first with `chmod +x`.

To build from source:

```
git clone https://github.com/GarnetRapture/nekoi_mcp
cd nekoi_mcp
make            # or: go build -o nekoi_mcp ./cmd
```

`make` builds all four targets into `dist/`. Windows additionally needs
`windres` (MinGW) for the icon and version block; the other targets need
nothing beyond the Go toolchain.

| Target | Output |
| --- | --- |
| `make windows` | `dist/windows-amd64/nekoi_mcp.exe` |
| `make linux` | `dist/linux-amd64/nekoi_mcp` |
| `make darwin` | `dist/darwin-amd64/nekoi_mcp` |
| `make darwin-arm64` | `dist/darwin-arm64/nekoi_mcp` |

## Register

As an MCP server:

```
claude mcp add --scope user nekoi_mcp -- /path/to/nekoi_mcp mcp
```

That line is the whole installation; the hooks need no separate entry. On its
first startup the server opens `~/.claude/settings.json` and adds itself to
`PreToolUse`, `Stop` and `MessageDisplay` — only to the ones whose entries do
not already run this path. When they all do, the file is left alone, and every
other setting and hook entry is carried across verbatim either way. The write
goes to a temporary file that is then renamed, so a failure partway leaves the
original settings in place.

One binary, two modes. With no argument it acts as a hook, reading the payload
on stdin; with `mcp` it speaks JSON-RPC over stdio. The registration above
passes `mcp`, so it is the server; the hook side is invoked with no argument.

`MessageDisplay` runs while the answer is still streaming. A thinking block is
written to the JSONL before it is rendered, so by then the reasoning just
produced can be read where it lies, and a violation caught there cuts the
answer off mid-sentence. With only `PreToolUse` and `Stop` in place, the
stretch where reasoning continues without any tool call goes entirely
unwatched.

## MCP tools

| Tool | Returns |
| --- | --- |
| `censor_session_status` | One session's tally: language violations, denials, calls, streak, context size |
| `censor_sessions` | Every recorded session, newest first |
| `censor_check_text` | Classifies text with the same rule the hook uses, and names any pattern it matches |

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CLAUDE_DIR` | `~/.claude` | Configuration root |
| `CENSOR_PATTERNS` | `$CLAUDE_DIR/thinking.json` | Pattern file; falls back to the embedded set |

State lives in `$CLAUDE_DIR/state/censor/`, one JSON file per session.

## Layout

```
cmd/                 hook mode, MCP server, build resources
internal/lang/       sentence-level language classification
internal/transcript/ JSONL parsing, usage accounting
internal/session/    per-session state
internal/sig/        canonical tool-call and probe identity
internal/rules/      the rules, notice budget, embedded patterns
internal/watch/      live transcript tailing
internal/selfreg/    self-registration as a hook
```

## License

MIT. See [LICENSE](LICENSE).

<div align="center">

**Nekoi**

[garnet@everlib.pro](mailto:garnet@everlib.pro) · [github.com/GarnetRapture](https://github.com/GarnetRapture)

</div>
