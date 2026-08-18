# 훅 프로토콜

[English](Hook-Protocol) · [홈](Home-ko)

---

편집기는 실행 파일을 인자 없이 돌리고, 표준 입력으로 JSON 페이로드를 넣고, 종료
코드와 표준 출력·오류를 읽습니다. 세 이벤트가 등록됩니다.

## 이벤트

| 이벤트 | 발화 시점 | 차단 채널 |
| --- | --- | --- |
| `PreToolUse` | 도구 호출 실행 직전 | exit 2 |
| `Stop` | 턴이 끝나려 할 때 | exit 2 |
| `MessageDisplay` | 어시스턴트 텍스트가 흐르는 동안 | JSON `continue:false` |

사고가 쓰이는 그 순간에 닿는 것은 `MessageDisplay`입니다. 사고 블록은 화면에
그려지기 전에 이미 JSONL에 들어가므로, 이 이벤트가 도는 시점이면 그 블록을 있는
자리에서 읽을 수 있습니다. 이 이벤트에서는 exit 2가 효력이 없어 중단이 JSON으로
가야 합니다.

## 입력

```json
{
  "session_id": "2c5012e5-…",
  "cwd": "n:/nekoi_mcp",
  "hook_event_name": "PreToolUse",
  "tool_name": "Edit",
  "tool_input": { "file_path": "…", "old_string": "…", "new_string": "…" },
  "transcript_path": "C:/Users/…/projects/<슬러그>/<uuid>.jsonl"
}
```

`hook_event_name`이 없으면 추론합니다. `tool_name`을 실은 페이로드는
`PreToolUse`, 그렇지 않으면 `Stop`입니다. `transcript_path`가 비어 있으면 즉시
0으로 끝납니다 — 기록이 없으면 판정할 것이 없습니다.

## 출력

### 차단

```
exit 2
stderr: [규칙명] 무엇이 걸렸는가
        무엇을 해야 하는가
        [denied #12/300]
```

stderr는 모델에 전달되고 그 행동은 진행되지 않습니다. 표준 출력을 어떻게
해석하든 무관합니다. 이만큼 확정된 결정은 JSON 파싱에 맡기지 않습니다.

`DenyCount`는 `DenyLimit`(300)에서 멈춥니다. 그 이후 언어 규칙은 통지로
내려갑니다 — 회복하지 못하는 모델이 세션을 영구히 잠그는 일이 없도록 하기
위해서입니다. 감시자 판정만이 예외이며 무조건 거부합니다.

### 비차단, PreToolUse

```json
{ "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "additionalContext": "[TOOL_BUDGET 35/35] …"
} }
```

### 비차단, Stop

```json
{ "decision": "block", "reason": "[UNVERIFIED_FILE_REFERENCE] …" }
```

Stop의 `decision: block`은 무언가를 끝내는 것이 아니라 턴이 끝나지 못하게
합니다. 그래야 할 일이 남은 상태에서 통지가 모델 앞에 놓입니다.

### MessageDisplay

```json
{ "continue": false,
  "stopReason": "[HALTED: THINKING_NOT_KOREAN/EN — caught as it was written, #7 this session]\n> …\nStopped here, mid-reasoning, without waiting for a tool call. …" }
```

`continue:false`는 이벤트별 결정 필드보다 우선합니다.

## runDisplay

`MessageDisplay`는 본 파이프라인에 들어오지 않습니다. `runDisplay`로 빠지며,
이 이벤트가 스트리밍 메시지의 매 갱신마다 돌기 때문에 일부러 좁게 만들어져
있습니다.

1. 마지막 40개 레코드만 읽습니다.
2. 가장 최근 사고 블록을 집습니다.
3. 판정합니다. Sonnet 모델의 `EN`은 즉시 반환하고, `EN`도 `JA`도 아니면 즉시
   반환합니다.
4. **한 번만 과금합니다.** `TotalThought <= Cursor`면 이미 센 블록이므로
   반환합니다. 아니면 커서를 전진시킵니다.
5. 집계를 갱신하고 저장한 뒤, 문제가 된 문장을 인용해 `continue:false`를
   내보냅니다.

4단계가 스트리밍 메시지가 매 프레임마다 새 위반으로 세어지는 것을 막습니다.

## 등록

```json
{
  "hooks": {
    "PreToolUse":     [ { "matcher": "*", "hooks": [ { "type": "command", "command": "/path/to/nekoi_mcp" } ] } ],
    "Stop":           [ { "matcher": "*", "hooks": [ { "type": "command", "command": "/path/to/nekoi_mcp" } ] } ],
    "MessageDisplay": [ { "matcher": "*", "hooks": [ { "type": "command", "command": "/path/to/nekoi_mcp" } ] } ]
  }
}
```

이것을 손으로 넣을 필요는 없습니다 — MCP 서버가 기동할 때 씁니다.
[자기 등록](MCP-Server-ko#자기-등록) 참조.
