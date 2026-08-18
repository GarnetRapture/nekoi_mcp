<div align="center">

<img src="https://raw.githubusercontent.com/GarnetRapture/nekoi_mcp/main/garnet2.png" alt="Nekoi_MCP" width="220">

# Nekoi_MCP

**사고가 규칙에 어긋나면 Claude Code의 도구 호출을 막습니다.**

[English](Home) · **한국어**

</div>

---

Nekoi_MCP는 Claude Code가 디스크에 적는 세션 기록 — 사고 블록, 답변 텍스트, 도구
호출 인자, 각 응답과 함께 돌아온 토큰 집계 — 을 읽고 다음 행동을 통과시킬지
정합니다.

실행 파일 하나가 두 가지로 동작하고, 등록은 한 번으로 끝납니다.

| 모드 | 실행 형태 | 진입점 | 역할 |
| --- | --- | --- | --- |
| 훅 | 인자 없이, 표준 입력 | `runHook` | 턴을 읽고 판정해 행동을 허용하거나 막음 |
| MCP 서버 | `mcp`, 표준 입출력 JSON-RPC | `runMCP` | 집계를 도구로 노출하고 기록을 계속 추적 |

## 왜 기록인가

겨냥하는 실패가 문법적인 것이 아니라서 diff에는 드러나지 않습니다. 사고가 아직
"일단 해보자" 상태인데 파일을 고치는 것, 열어 보지도 않은 파일을 근거로 인용하는
것, 편집 한 번 없이 완료를 보고하는 것. 이런 것이 드러나는 자리는 행동 직전에
모델이 쓴 글입니다.

그 글은 훅이 도는 시점에 이미 디스크에 있으므로 결과에 맞춰 고쳐 쓸 수 없습니다.

## 개입 지점

```
사용자 프롬프트
    │
    ├── 모델이 사고 ────────────────┐
    │                               │  사고 블록이 JSONL에 기록됨
    │                               ▼
    │                        MessageDisplay 훅 ── continue:false ─▶ 턴 중단
    │                        감시자 (MCP 서버) ── 플래그 + 알림
    │
    ├── 모델이 도구 호출
    │        │
    │        ▼
    │   PreToolUse 훅 ── exit 2 ─▶ 호출 차단, stderr가 모델에 전달
    │        │
    │        ▼
    │   도구 실행
    │
    └── 턴 종료
             │
             ▼
        Stop 훅 ── exit 2 ─▶ 턴이 끝나지 않음
```

`PreToolUse`와 `Stop`은 도구 호출과 턴 경계를 감싸므로, 아무것도 부르지 않고
사고만 이어가는 구간은 둘 다 닿지 못합니다. `MessageDisplay`는 답변이 흐르는
동안 돌고, MCP 서버의 감시자는 자체 주기로 JSONL을 따라가며 그 구간을 덮습니다.

## 문서

| 문서 | 내용 |
| --- | --- |
| [아키텍처](Architecture-ko) | 패키지, 데이터 흐름, 판정 파이프라인 |
| [규칙](Rules-ko) | 규칙 전량, 발동 조건, 출력 |
| [훅 프로토콜](Hook-Protocol-ko) | 이벤트, 페이로드, 종료 코드, JSON 출력 |
| [MCP 서버](MCP-Server-ko) | 도구, 감시자, 자기 등록 |
| [설정](Configuration-ko) | 환경 변수, 상태 파일, 사용자 패턴 |

## 저장소

[github.com/GarnetRapture/nekoi_mcp](https://github.com/GarnetRapture/nekoi_mcp)
· MIT · Nekoi · [garnet@everlib.pro](mailto:garnet@everlib.pro)
