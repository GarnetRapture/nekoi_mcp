# 아키텍처

[English](Architecture) · [홈](Home-ko)

---

## 패키지 구성

```
cmd/
  main.go        모드 분기, 경로 결정
  hook.go        훅 모드: 턴을 읽고 규칙을 돌려 결정을 내보냄
  mcp.go         MCP 서버: JSON-RPC 루프, 도구, 클라이언트 알림
  about.go       실행 파일에 박히는 이름·버전·저작

internal/
  lang/          문장 단위 언어 판정
  transcript/    JSONL 파싱, 턴 추출, 사용량 집계
  session/       세션별 상태, 원자적 저장
  sig/           호출·환경 조회·경로의 정규 식별자
  rules/         규칙, 주입 예산, 내장 패턴
  watch/         세션 기록 실시간 추적
  selfreg/       훅 자기 등록
```

의존은 한 방향입니다. `lang`, `sig`, `session`은 프로젝트 내부의 무엇도 참조하지
않습니다. `transcript`는 `sig`를 씁니다. `rules`는 `lang`, `session`,
`transcript`, `sig`를 씁니다. `watch`는 `lang`과 `session`을, `selfreg`는 `sig`를
씁니다. `cmd`가 그 위에 올라갑니다.

## 모드 분기

`main`은 `os.Args[1]` 하나만 봅니다.

| 인자 | 함수 | 등록 형태 |
| --- | --- | --- |
| `mcp` | `runMCP` | `claude mcp add … -- <바이너리> mcp` |
| `version` / `--version` / `-v` / `about` | `printAbout` | — |
| 그 외 (인자 없음 포함) | `runHook` | `settings.json`의 `hooks` 항목 |

두 등록은 서로 별개입니다. 그래서 서버가 기동할 때 훅 항목을 스스로 써넣습니다 —
[MCP 서버](MCP-Server-ko#자기-등록) 참조.

경로는 모두 한 뿌리에서 나옵니다.

```
CLAUDE_DIR (기본 ~/.claude)
├── settings.json          훅 등록. selfreg가 씀
├── thinking.json          사용자 패턴 (CENSOR_PATTERNS가 우선)
├── projects/              작업 트리마다 디렉터리 하나
│   └── <슬러그>/<uuid>.jsonl  세션 기록. watch가 따라감
└── state/censor/          세션마다 JSON 파일 하나
```

## 턴 읽기

`transcript.Load(path, maxLines)`는 JSONL의 꼬리를 파싱해 `Turn`을 만듭니다.
단순한 역직렬화가 아닙니다.

**링 버퍼.** 마지막 `maxLines`개 레코드만 남기므로 세션이 길어져도 파싱 비용이
무한정 늘지 않습니다. 훅은 2000, `MessageDisplay` 경로는 40을 씁니다 — 후자는
지금 쓰이고 있는 블록 하나만 보면 되기 때문입니다.

**턴 경계.** 뒤에서부터 마지막 실제 사용자 프롬프트를 찾아 `start`를 정합니다.
메타가 아니고 비어 있지 않은 텍스트 블록을 가진 `user` 레코드가 기준입니다.
`start` 이후는 이번 턴, 이전은 맥락입니다.

**실패한 호출 제외.** 훅이 막은 호출도 트랜스크립트에는 `tool_use` 블록으로
남고, 오류 `tool_result`가 그 답으로 붙습니다. 실행된 적이 없으니 관측한 것도
소비한 것도 없습니다. `failedCalls`가 오류로 끝난 `tool_use`의 id를 모으고, 본
순회는 그 호출들을 전부 건너뜁니다 — 서명도, 호출 수도, 프로브 키도, 편집 경로도
집계하지 않습니다. 이 처리가 없으면 한 번의 거부가 그 명령을 "이미 실행됨"으로
만들어 모든 재시도를 막습니다.

**Evidence.** 사용자 프롬프트, 모든 `tool_result` 본문, 모든 `tool_use` 인자를
이어 붙이고 `sig.NormalizePath`로 정규화합니다. 이것이 이번 턴이 실제로 관측한
것의 전부이고, [보고 규칙](Rules-ko#보고)이 인용을 대조하는 대상입니다.

**사용량.** `input_tokens`, `output_tokens`, 캐시 두 항목은 각 어시스턴트
메시지와 함께 기록된 API 응답에서 읽습니다. 컨텍스트 크기는 글자 수로 어림잡지
않고 실측값을 씁니다.

### Turn이 담는 것

| 필드 | 의미 |
| --- | --- |
| `UserPrompt` | 마지막 실제 사용자 메시지 |
| `Thoughts` | 이번 턴의 사고 블록. 각각 모델 id를 가짐 |
| `TotalThought` | 창 전체의 사고 블록 수 — 커서의 기준 |
| `AssistantTxt` | 이번 턴의 답변 텍스트 |
| `Evidence` | 관측한 모든 것. 정규화·소문자화됨 |
| `ToolCalls`, `CallSigs` | 이번 턴에 실행된 호출과 그 서명 |
| `ProbeKeys` | 환경 조회. 세션 전역 |
| `EditFiles`, `Edited`, `Probed`, `Bashed` | 종류별 집계 |
| `ContextTokens`, `OutputTokens` | 과금 집계 |

## 커서

사고 블록은 한 번만 과금됩니다. `session.State.Cursor`가 판정을 마친 블록 수를
들고 있고, `freshThinking`과 `EvaluateThinking`이 창 앞쪽에서 그만큼 건너뛴 뒤
봅니다.

여기서 생기는 구멍을 규칙이 따로 막습니다. 한 번 과금되면 커서가 지나가므로,
*사고를 전혀 하지 않고* 다시 호출하면 판정할 새 블록이 없습니다.
`EvaluateUnresolved`가 정확히 그것을 잡습니다 — 마지막 판정이 EN이나 JA인데 새
사고가 비어 있다면, 통지를 답한 것이 아니라 지나친 것입니다.

## 판정 파이프라인

`runHook`은 메시지 목록 하나와 거부 플래그 하나를 만듭니다.

```
                    ┌── PreToolUse 전용 ──────────────────────┐
                    │  EvaluateToolChoice   (도구 + 명령)     │
                    │  EvaluateProcedure    (새 사고)         │
                    │  EvaluateAskUser      (도구 이름)       │
                    │  EvaluateWriteTarget  (파일 경로)       │
                    │  EvaluateWaste        (턴 서명)         │
                    │  EvaluateRepeat       (세션 상태)       │
                    │  EvaluateUnresolved   (상태 + 새 사고)  │
                    └─────────────────────────────────────────┘
                    ┌── Stop 전용 ────────────────────────────┐
                    │  EvaluateReporting    (답변 대 Evidence)│
                    │  EvaluateTurnEnd      (답변, 도구 사용) │
                    │  EvaluateSplitReport  (답변)            │
                    │  EvaluateEditFlow     (편집 수)         │
                    │  ScanPatterns         (답변)            │
                    └─────────────────────────────────────────┘
                    ┌── 모든 이벤트 ──────────────────────────┐
                    │  EvaluateAnger        (새 사고)         │
                    │  EvaluatePatterns     (새 사고)         │
                    │  EvaluateWatchBlock   (감시자 플래그)   │
                    │  EvaluateThinking     (새 블록)         │
                    └─────────────────────────────────────────┘
                                    │
                            rules.Merge(msgs, contextTokens)
                                    │
                    거부? ── 예 ─▶ stderr + exit 2
                       │
                       아니오 ─▶ stdout JSON (additionalContext / decision)
```

`MessageDisplay`는 이 파이프라인에 들어오지 않습니다. `runDisplay`로 빠져 가장
최근 사고 블록 하나만 판정하고 `continue:false`로 답합니다 — 그 이벤트에서는
exit 2가 효력이 없기 때문입니다.

## 주입 예산

주입한 것은 그 세션의 이후 모든 요청에서 다시 과금됩니다. 그래서 프롬프트가
커질수록 허용량을 좁힙니다. 기준은 실측 컨텍스트 크기입니다.

| 컨텍스트 토큰 | 통지당 예산 |
| --- | --- |
| 8만 미만 | 700자 |
| 8만 ~ 20만 | 460자 |
| 20만 ~ 40만 | 320자 |
| 40만 초과 | 220자 |

220자는 절대 넘지 않는 하한입니다. 무슨 일이 있었고 무엇을 해야 하는지조차 담지
못하는 통지는 토큰만 쓰고 아무것도 바꾸지 못합니다. 여러 규칙이 한꺼번에 걸리면
합친 길이를 통지당 한도의 두 배, 최대 1100자로 자릅니다.

같은 위반을 두 번 자세히 알린 뒤로는 통지가 접힙니다 — 다만 *지시문*은 접힘에서
살아남습니다. N번째 반복은 앞선 텍스트가 더 이상 작동하지 않는다는 증거이므로,
무엇을 하라는 한 줄을 빼는 것은 정확히 거꾸로입니다.

## 저장

`session.Store`는 `state/censor/` 아래에 세션마다 JSON 파일 하나를 씁니다. 모든
쓰기는 임시 파일을 거쳐 이름을 바꾸므로, 읽는 쪽이 반쯤 쓰인 레코드를 보는 일이
없습니다. 세션 id는 `[A-Za-z0-9_-]`로 정제하고 80자에서 자른 뒤 파일명이 됩니다.

훅과 감시자가 이 파일을 공유하지만 소유 필드가 다릅니다. `ENCount`, `JACount`,
`Streak`, `LastVerdict`, `Cursor`는 훅의 것입니다. `WatchEN`, `WatchJA`,
`WatchSeen`, `WatchBlock`, `WatchVerdict`, `WatchQuote`는 감시자의 것입니다.
이렇게 갈라 두는 것이 독립된 두 판독기가 같은 블록을 두 번 세는 일을 막습니다.
