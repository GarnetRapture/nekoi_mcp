package rules

import (
	"fmt"
	"regexp"
	"strings"

	"nekoi_mcp/internal/session"
)

// A turn that ends without a tool call has to end on work. These patterns
// catch the two ways it ends on nothing instead: filler that announces
// activity, and a question that hands the decision back.
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

	// Reporting one's own work as a split between confirmed and unconfirmed
	// hands the verification back to the reader.
	reSplitReport = regexp.MustCompile(`(?i)확인(이)?\s*된\s*것[과와].{0,12}확인(되지|이\s*되지)?\s*않은\s*것|` +
		`반영(이)?\s*확인된\s*것[과와]|검증(된|되지\s*않은)\s*것[을를과와].{0,20}구분|구분해\s*(서\s*)?보고|` +
		`(확인|검증|반영)\s*(여부|상태)[를을]\s*구분|` +
		`\bverified\s+(vs\.?|and|versus)\s+unverified\b|\bconfirmed\s+(vs\.?|and|versus)\s+unconfirmed\b`)

	// Text that carries no substance once filler is removed.
	reMeaningful = regexp.MustCompile(`[가-힣A-Za-z0-9]`)
)

// EvaluateTurnEnd runs on Stop, for turns that called no tool. A turn that
// produced neither work nor a substantive answer produced nothing.
func EvaluateTurnEnd(replyText string, hasToolUse bool) []*Result {
	text := strings.TrimSpace(replyText)
	if text == "" || hasToolUse {
		return nil
	}
	var out []*Result

	stripped := reFiller.ReplaceAllString(text, "")
	if len(reMeaningful.FindAllString(text, -1)) > 0 &&
		len(reMeaningful.FindAllString(stripped, -1)) == 0 {
		out = append(out, &Result{Message: "[EMPTY_STANDBY_FILLER] The turn ended on filler — understood, standing by, proceeding — with no tool call and no substantive answer.\nAnnouncing that you will proceed is not proceeding. Call the tool that handles the request."})
	}
	if reQuestionBack.MatchString(text) {
		out = append(out, &Result{Message: "[QUESTION_BACK_TO_USER] The turn ended asking the user to confirm, choose, or approve, with no tool call.\nPick the best-supported path, execute it, and report the outcome."})
	}
	return out
}

// EvaluateSplitReport catches verification handed back to the reader.
func EvaluateSplitReport(text string) *Result {
	if !reSplitReport.MatchString(text) {
		return nil
	}
	return &Result{Message: "[VERIFICATION_HANDED_TO_USER] You split your own work into confirmed and unconfirmed and reported the split.\nVerify the unconfirmed items yourself, then state one settled result per item."}
}

// EvaluateEditFlow fires once edits stop being local. Past a handful, each
// change shifts assumptions the others rest on.
// It is delivered once per edit count. The count only grows within a turn, and
// a notice on Stop keeps the turn from ending, so repeating it at the same
// level would hold the turn open however well the audit was answered.
func EvaluateEditFlow(st *session.State, edits int) *Result {
	if edits <= 5 || edits <= st.AuditedEdits {
		return nil
	}
	st.AuditedEdits = edits
	return &Result{Message: fmt.Sprintf(
		"[EDIT_FLOW_AUDIT] %d file modifications this turn. Edits applied in sequence without re-reading the flow are individually correct and collectively broken.\nBefore the next one: does anything already edited depend on it, and did an earlier edit invalidate a caller or a type?",
		edits)}
}
