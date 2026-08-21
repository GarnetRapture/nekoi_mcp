package rules

import (
	"fmt"
	"regexp"
	"strings"

	"nekoi_mcp/internal/session"
)

var (
	reFiller = regexp.MustCompile(`(?i)작업(을|를)?\s*(중단|중지|멈추)|여기(서|서는)?\s*(멈추|중단|중지)|` +
		`더\s*이상\s*진행하지\s*않겠|멈추(겠습니다|었습니다)|중지(하겠습니다|합니다)|중단(하겠습니다|합니다)|` +
		`지시\s*(를|을)?\s*기다리|지시\s*대기|기다리(겠습니다|고\s*있겠습니다)|기다립니다|` +
		`용건\s*(을|를)?\s*말씀해|무엇\s*(을|를)?\s*도와\s*?드릴까요|` +
		`계속\s*진행하겠|계속하겠|이어서\s*진행하겠|그대로\s*진행하겠|진행하겠습니다|` +
		`\(\s*대기\s*\)|대기\s*중|대기(합니다|하겠습니다)|알겠(습니다|어요)|이해했(습니다|어요)|` +
		`\bstopping here\b|\bi(\s|')?ll stop\b|\bhalting\b|\bstanding by\b|\bawaiting\b|\bunderstood\b`)

	reQuestionBack = regexp.MustCompile(`(?i)할까요\s*\?|드릴까요|하시겠(습니까|어요|나요)|맞(나요|습니까|을까요)|` +
		`괜찮(을까요|겠습니까|나요)|어떻게\s*할까요|어떤\s*(걸|것을?|방법|방식|쪽)(으로)?\s*(할까요|선택|골라)|` +
		`진행할까요|해도\s*될까요|해야\s*할까요|필요할까요|원하시나요|알려\s*주(세요|시겠)|선택해\s*주(세요|시겠)|` +
		`\bwhich\s+(one|option|way|approach)\b|\bshould\s+i\s+(proceed|continue|go ahead)\b|` +
		`\bwould you like me to\b|\bshall i\b|\bdo you want me to\b|\blet me know\b`)

	reSplitReport = regexp.MustCompile(`(?i)확인(이)?\s*된\s*것[과와].{0,12}확인(되지|이\s*되지)?\s*않은\s*것|` +
		`반영(이)?\s*확인된\s*것[과와]|검증(된|되지\s*않은)\s*것[을를과와].{0,20}구분|구분해\s*(서\s*)?보고|` +
		`(확인|검증|반영)\s*(여부|상태)[를을]\s*구분|` +
		`\bverified\s+(vs\.?|and|versus)\s+unverified\b|\bconfirmed\s+(vs\.?|and|versus)\s+unconfirmed\b`)

	reMeaningful = regexp.MustCompile(`[가-힣A-Za-z0-9]`)

	reHandOff = regexp.MustCompile(`(?i)직접\s*(지우|삭제|실행|수정|고치|처리|해\s*주|하시)|` +
		`탐색기(에서|로)|수동으로\s*(지우|삭제|처리|실행)|` +
		`(지우|삭제|수정|실행)(시면|하시면)\s*(됩니다|돼요|된다)|` +
		`(해|처리해|정리해)\s*주(세요|시면|십시오)|` +
		`제\s*(쪽에서|힘으로)(는)?\s*(안|못|불가)|더\s*시도할\s*수\s*없|진행되지\s*않습니다|` +
		`\byou (can|should|could|need to|will have to) (delete|remove|run|fix|do) (it|them|this)\b|` +
		`\bdo it (manually|by hand)\b|\bi (cannot|can't) proceed\b`)

	reBlocked = regexp.MustCompile(`(?i)블로커[:：]|차단(되어|돼서|됨).{0,20}(못|없|불가)|` +
		`남았습니다\s*$|미완(입니다|입니다\.)|진행할\s*수\s*없(습니다|어요)|` +
		`\bblocked\b|\bblocker\b|\bcannot continue\b`)
)

func EvaluateTurnEnd(replyText string, hasToolUse bool) []*Result {
	text := strings.TrimSpace(replyText)
	if text == "" {
		return nil
	}
	var out []*Result

	if reHandOff.MatchString(text) || reBlocked.MatchString(text) {
		out = append(out, &Result{Message: "[TASK_HANDED_TO_USER] The reply ends by telling the user to finish the work by hand, or by declaring the task blocked.\nA denied call rejects that call, never the task: the notice that stopped it names what to address, and the same goal remains reachable by a different tool, a different route, or a narrower step."})
	}
	if hasToolUse {
		return out
	}

	stripped := reFiller.ReplaceAllString(text, "")
	if len(reMeaningful.FindAllString(text, -1)) > 0 &&
		len(reMeaningful.FindAllString(stripped, -1)) == 0 {
		out = append(out, &Result{Message: "[EMPTY_STANDBY_FILLER] This turn ended on filler — understood, standing by, proceeding — with no tool call and no substantive answer.\nAnnouncing that work will proceed leaves the same state behind as saying nothing."})
	}
	if reQuestionBack.MatchString(text) {
		out = append(out, &Result{Message: "[QUESTION_BACK_TO_USER] This turn ended asking the user to confirm, choose, or approve, with no tool call.\nThe decision the question defers is one the evidence in this turn already supports making."})
	}
	return out
}

func EvaluateSplitReport(text string) *Result {
	if !reSplitReport.MatchString(text) {
		return nil
	}
	return &Result{Message: "[VERIFICATION_HANDED_TO_USER] This reply splits its own work into confirmed and unconfirmed and reports the split.\nThe unconfirmed half is verifiable by the same tools that produced the confirmed half, so the reader is being asked to do what the turn could have settled."}
}

func EvaluateEditFlow(st *session.State, edits int) *Result {
	if edits <= 5 || edits <= st.AuditedEdits {
		return nil
	}
	st.AuditedEdits = edits
	return &Result{Message: fmt.Sprintf(
		"[EDIT_FLOW_AUDIT] %d file modifications in this turn. Edits applied in sequence without re-reading the flow are individually correct and collectively broken.\nWhat settles that is whether anything already edited depends on the next one, and whether an earlier edit invalidated a caller or a type.",
		edits)}
}
