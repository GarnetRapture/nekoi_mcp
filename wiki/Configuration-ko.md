# 설정

[English](Configuration) · [홈](Home-ko)

---

## 환경 변수

| 변수 | 기본값 | 용도 |
| --- | --- | --- |
| `CLAUDE_DIR` | `~/.claude` | 설정 뿌리. 나머지 경로가 전부 여기서 나옴 |
| `CENSOR_PATTERNS` | `$CLAUDE_DIR/thinking.json` | 패턴 파일. 없으면 실행 파일에 내장된 세트 |

`CLAUDE_DIR`이 있는 이유는 실제 설정을 건드리지 않고 임시 트리에서 바이너리를
시험할 수 있게 하기 위해서입니다.

## 경로

| 경로 | 쓰는 주체 | 내용 |
| --- | --- | --- |
| `$CLAUDE_DIR/settings.json` | 서버 기동 시 `selfreg` | 훅 등록 |
| `$CLAUDE_DIR/thinking.json` | 사용자 | 사용자 금지 패턴 |
| `$CLAUDE_DIR/projects/*/*.jsonl` | Claude Code | 세션 기록. 읽기 전용 |
| `$CLAUDE_DIR/state/censor/*.json` | `session.Store` | 세션마다 레코드 하나 |

## 세션 상태

세션마다 JSON 파일 하나. 임시 파일에 쓴 뒤 이름을 바꿉니다. 세션 id는
`[A-Za-z0-9_-]`로 정제하고 80자에서 잘라 파일명이 됩니다. 빈 id는 `nosession`이
됩니다.

| 필드 | 소유 | 의미 |
| --- | --- | --- |
| `en_count`, `ja_count` | 훅 | 과금된 언어 위반 |
| `deny_count` | 훅 | 실제로 거부된 호출 |
| `tool_calls` | 훅 | 관측된 호출 |
| `cursor` | 훅 | 판정을 마친 사고 블록 수 |
| `streak` | 훅 | 연속 위반 판정 |
| `last_verdict` | 훅 | `OK` / `EN` / `JA` |
| `repeat_sig`, `repeat_count` | 훅 | 연속 동일 호출 추적 |
| `watch_en`, `watch_ja`, `watch_seen` | 감시자 | 훅 사이에서 잡은 위반 |
| `watch_block`, `watch_verdict`, `watch_quote` | 감시자 | 다음 훅이 처리할 판정 |
| `context_tokens`, `output_tokens` | 훅 | API가 준 과금 집계 |
| `injected_chars`, `notices` | 훅 | 이 검사기가 쓴 비용 |

두 소유자는 서로의 필드를 쓰지 않습니다. 그 분리가 한 블록이 두 번 세어지는 것을
막습니다.

## 사용자 패턴

```json
{
  "patterns": [
    {
      "name": "SCOPE_REDUCTION",
      "regex": "일단\\s*(핵심|일부|중요한)|for now,? (just|only)",
      "negative_regex": "전체를\\s*처리",
      "message": "지시된 범위의 일부만 처리하고 나머지를 별건이나 나중으로 돌렸습니다.\n지시된 모든 대상을 지금 처리하십시오."
    }
  ]
}
```

| 필드 | 필수 | 동작 |
| --- | --- | --- |
| `name` | 예 | `[INTERRUPTED: <name>]` 형태로 표시됨 |
| `regex` | 예 | 대소문자 무시로 컴파일. 컴파일되지 않는 항목은 버려짐 |
| `negative_regex` | 아니오 | 여기 매치되면 적중이 취소됨 |
| `message` | 아니오 | 주입할 텍스트. 비면 일반 문구를 씀 |

패턴은 프로세스당 한 번 컴파일됩니다. 첫 매치가 이깁니다. 같은 세트를 Stop에서
답변 텍스트에도 돌리므로, 답변에 적은 우회도 잡힙니다.

파일 전체를 실패시키지 않고 항목만 버리는 것은 의도된 설계입니다. 규칙 하나가
검사기 전체를 무력화할 수 있으면 안 됩니다.

## 빌드

```
git clone https://github.com/GarnetRapture/nekoi_mcp
cd nekoi_mcp
make            # 또는: go build -o nekoi_mcp ./cmd
```

`make`는 네 가지 대상을 `dist/`에 만듭니다. Windows만 아이콘과 버전 블록 때문에
`windres`(MinGW)가 추가로 필요하며, 리소스 오브젝트는 `*_windows_amd64.syso`로
이름 붙어 Go 툴체인이 그 빌드에만 링크합니다.

| 명령 | 산출물 |
| --- | --- |
| `make windows` | `dist/windows-amd64/nekoi_mcp.exe` |
| `make linux` | `dist/linux-amd64/nekoi_mcp` |
| `make darwin` | `dist/darwin-amd64/nekoi_mcp` |
| `make darwin-arm64` | `dist/darwin-arm64/nekoi_mcp` |

`make check`는 `go vet ./...`를 돌립니다.

## 한계값

`DenyLimit`은 세션당 300입니다. 그 이후 언어 규칙은 통지로 내려갑니다 — 회복하지
못하는 모델이 세션을 영구히 잠그지 않도록 하기 위해서입니다. 감시자 판정만
예외이며 무조건 거부합니다.

`ToolBudget`은 지시 하나에 35회입니다. 거부가 아니라 통지입니다.

`transcriptTail`은 훅이 2000 레코드, `MessageDisplay`가 40입니다. 앞의 값은 긴
턴의 초반 도구 호출이 창 안에 남을 만큼 넓습니다. 꼬리가 그것들을 잘라내면 그
경로가 Evidence에서 사라지고, 이번 턴이 실제로 고친 파일이 근거 없는 인용으로
읽힙니다.
