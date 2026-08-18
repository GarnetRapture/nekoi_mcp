# Rules

[한국어](Rules-ko) · [Home](Home)

---

Every rule reads something the model already produced. None of them guess.
A rule either **denies** — the action does not happen — or **notices**, which
injects text without stopping anything.

## Reasoning language

Thinking blocks must be Korean. The check is deliberately conservative, because
a false positive on a technical block would be worse than a missed one.

1. **Strip what is exempt.** Backtick spans, filesystem paths, dotted
   identifiers, snake_case and CamelCase are removed first. Code identifiers are
   allowed to stay in their original form, so they must not influence the
   verdict.
2. **Split into sentences** on `.!?。` followed by whitespace, or on newlines.
3. **Classify each sentence.** English requires all three: no Hangul at all, at
   least three alphabetic words, and at least one function word from a fixed
   list of ~120 (`the`, `is`, `should`, `because`, `writing`, …).
4. **Compare.** The block is English only if English sentences *outnumber*
   Korean ones. A block with any kana is Japanese immediately.

A borrowed term or a stray identifier therefore does not trip it — a single
English sentence among Korean ones leaves the verdict Korean.

| Outcome | Verdict |
| --- | --- |
| Empty block | `NOTHINK` |
| Contains kana | `JA` |
| English sentences > Korean sentences | `EN` |
| Otherwise | `OK` |

`EN` on a `claude-sonnet*` model is skipped as a documented model defect; `JA`
still counts.

**Denies.** The notice quotes the offending sentences, names the count for the
session, and states that the reply channel does not cover the thinking channel —
a Korean reply leaves the violation standing.

### Unresolved violations

`EvaluateUnresolved` closes the gap the cursor opens. If the last verdict was
`EN` or `JA` and this call carries no fresh reasoning at all, the notice was
proceeded past rather than answered. **Denies.**

## Reasoning procedure

Before a file changes, four things must be settled: what caused this, what was
pointed out, what follows from the two, and what procedure satisfies that
conclusion.

| Trip | Condition | Result |
| --- | --- | --- |
| `REANALYSIS_LOOP` | Already acted this turn, reasoning re-analyzes, no conclusion stated | Deny |
| `UNCHECKED_ACTION` | Mutating a file with no self-check — does it match, what breaks, is it the only edit | Deny |
| `EXPLORATORY_MUTATION` | Mutating while the reasoning still reads as trying something out | Deny |
| `UNGROUNDED_ACTION` | Two or more of cause / conclusion / procedure missing | Deny |

The first call of a turn is exempt from the four-part requirement: reading in
order to find the target is *how* the cause gets established, and demanding a
conclusion before any evidence would force the guessing the rule exists to
prevent.

## Wasted calls

| Trip | Condition | Result |
| --- | --- | --- |
| `REDUNDANT_CALL` | Identical arguments already ran this turn | Deny |
| `ENVIRONMENT_REPROBE` | A binary's presence or version already established this session | Deny |
| `TOOL_BUDGET` | 35 calls for one instruction | Notice |
| `STACK_BOUNDARY` | Every file edited this turn is on one side of the stack | Notice |

Call identity comes from `sig.Call`: the tool name plus its input reduced to a
canonical order, whitespace collapsed, then hashed. Reordering keys or changing
spacing does not disguise a repeat.

Probe identity comes from `sig.Probe`, which recognises `which x`,
`command -v x`, `type -p x` and `x --version` / `x -V`. Those facts cannot
change within a session.

Only calls that actually executed count — see [failed calls](Architecture#reading-a-turn).

## Tool choice

Read from the call itself, so they fire before the command runs.

| Trip | Condition |
| --- | --- |
| `TERMINAL_NOT_GIT_BASH` | Leaving Bash for PowerShell |
| `POWERSHELL_DEVICE_STATE` | A command that reaches a connected device — notice, not deny |
| `PYTHON3_COMMAND_FORBIDDEN` | `python3` where `python` is pinned |
| `JSON_TOOL_NOT_JQ` | Python's `json` module where `jq` is pinned |
| `PYTHON_FILE_IO` | Python writing files instead of Edit/Write |
| `STRAY_TEMP_ARTIFACT` | Output redirected to a drive root |
| `PARTIAL_DATA_JUDGMENT` | `head`/`tail`/`sed -n`/slicing truncating data a conclusion follows from |

All deny except the device-state notice.

Bash is the framing because Claude Code runs its shell in a POSIX environment on
Windows too, so every path, quote and pipe here is written against it and
PowerShell's own conventions do not carry back. The exception is a command
reaching a connected device, which is not a shell-dialect question — that state
lives outside the repository and outside any diff, so it asks rather than
blocks.

Two more guard the Write tool specifically:

- `WRITE_OVERWRITES_EXISTING` — Write on a path that exists. It replaces the
  file whole, deleting every line not reproduced. **Denies.**
- `NEW_FILE_DESIGN_CHECK` — a genuinely new file. Asks for flow, centralization
  and role before it lands. **Notice.**

And one blocks handing the decision back: `ASK_USER_FORBIDDEN` on
`AskUserQuestion`. **Denies.**

## Reporting

Runs on Stop, where the reply is complete. Each check compares the reply against
`Turn.Evidence`.

| Trip | Condition |
| --- | --- |
| `UNVERIFIED_FILE_REFERENCE` | A path with a source extension cited in the reply that appears nowhere in evidence |
| `FABRICATED_TOOL_OUTPUT` | Output only a command can produce, quoted when `Bashed == 0` |
| `UNBACKED_EDIT_CLAIM` | A change reported as done with `Edited == 0` |
| `VERIFICATION_HANDED_TO_USER` | The reply splits the work into confirmed and unconfirmed |

All notices. The file-reference check strips a trailing `:line,col` before
matching, so citing a real file at a new line still counts as backed.

## Turn end

For turns that called no tool.

| Trip | Condition |
| --- | --- |
| `EMPTY_STANDBY_FILLER` | Reply is nothing but filler once the filler is removed — understood, standing by, will proceed |
| `QUESTION_BACK_TO_USER` | Reply ends asking the user to confirm, choose or approve |
| `EDIT_FLOW_AUDIT` | More than five file modifications this turn |

`EDIT_FLOW_AUDIT` is not about volume. Past a handful, each change shifts
assumptions the others rest on, so it asks whether an earlier edit invalidated
a caller or a type.

## Banned patterns

`thinking.json` holds a list of `{name, regex, negative_regex, message}`. A
match on the reasoning denies the call and injects the pattern's own message.
The shipped set is embedded in the binary; an external file replaces it without
a rebuild. An entry whose regex does not compile is dropped rather than failing
the whole check, so one bad rule cannot disable the guard.

The same scan runs against the visible reply on Stop: a bypass stated in the
answer is still a bypass.

One pattern is compiled in rather than configurable — `USER_ANGER_ANALYSIS`.
Reasoning that turns the user's emotional state into the subject is avoidance,
not repair, and it advances the actual problem by zero. **Denies.**

## Watcher verdicts

`EvaluateWatchBlock` acts on what the [watcher](MCP-Server#the-watcher) found
between hook invocations. It denies **unconditionally** — no tally softens it —
because the verdict was already settled from the written transcript. The flag is
cleared as it is consumed; a flag left standing would deny every later call and
wedge the session.
