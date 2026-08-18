package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"nekoi_mcp/internal/lang"
	"nekoi_mcp/internal/rules"
	"nekoi_mcp/internal/session"
)

const protocolVersion = "2026-07-28"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	ResultType string        `json:"resultType"`
	Content    []contentItem `json:"content"`
	IsError    bool          `json:"isError"`
}

func objectSchema(props map[string]any, required []string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func toolCatalog() []toolDef {
	return []toolDef{
		{
			Name:        "censor_session_status",
			Description: "Report the censor's tally for one session: English/Japanese thinking counts, denials, tool calls, and current streak. Omit session_id for the most recently active session.",
			InputSchema: objectSchema(map[string]any{
				"session_id": map[string]any{"type": "string", "description": "Session id; defaults to the most recent."},
			}, nil),
		},
		{
			Name:        "censor_sessions",
			Description: "List supervised sessions, newest first, with each one's violation tally.",
			InputSchema: objectSchema(map[string]any{
				"limit": map[string]any{"type": "integer", "description": "Maximum rows (default 10)."},
			}, nil),
		},
		{
			Name:        "censor_check_text",
			Description: "Classify text as Korean or English reasoning using the same rule the hook enforces, and report which banned thinking.json patterns it matches.",
			InputSchema: objectSchema(map[string]any{
				"text": map[string]any{"type": "string", "description": "Text to classify."},
			}, []string{"text"}),
		},
	}
}

func runMCP() int {
	store := session.NewStore(stateDir())
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 1<<20), 32<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		resp := dispatch(store, req)
		if resp == nil { // notification: no reply
			continue
		}
		b, err := json.Marshal(resp)
		if err != nil {
			continue
		}
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}
	return 0
}

func dispatch(store *session.Store, req rpcRequest) *rpcResponse {
	if len(req.ID) == 0 {
		return nil // notification
	}
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		// Echo the version the client asked for. Pinning our own would make
		// the handshake fail against any client on a different revision.
		var init struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &init)
		if init.ProtocolVersion == "" {
			init.ProtocolVersion = protocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": init.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo": map[string]any{
				"name":    AppName,
				"version": AppVersion,
				"author":  AppAuthor,
				"contact": AppEmail,
			},
		}
	case "tools/list":
		resp.Result = map[string]any{"resultType": "complete", "tools": toolCatalog()}
	case "tools/call":
		resp.Result = callTool(store, req.Params)
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func callTool(store *session.Store, params json.RawMessage) callResult {
	var p struct {
		Name      string `json:"name"`
		Arguments struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
			Limit     int    `json:"limit"`
		} `json:"arguments"`
	}
	_ = json.Unmarshal(params, &p)

	switch p.Name {
	case "censor_session_status":
		return textResult(statusReport(store, p.Arguments.SessionID), false)
	case "censor_sessions":
		return textResult(sessionsReport(store, p.Arguments.Limit), false)
	case "censor_check_text":
		return textResult(checkReport(p.Arguments.Text), false)
	default:
		return textResult("unknown tool: "+p.Name, true)
	}
}

func textResult(s string, isErr bool) callResult {
	return callResult{
		ResultType: "complete",
		Content:    []contentItem{{Type: "text", Text: s}},
		IsError:    isErr,
	}
}

func statusReport(store *session.Store, id string) string {
	var st *session.State
	if id != "" {
		st = store.Load(id)
	} else if all := store.List(); len(all) > 0 {
		st = all[0]
	}
	if st == nil || st.SessionID == "" {
		return "no supervised session on record"
	}
	return fmt.Sprintf("session=%s model=%s\nEN=%d JA=%d denied=%d/%d calls=%d streak=%d last=%s\ncontext=%d out=%d notices=%d injected=%d chars\ncwd=%s",
		st.SessionID, st.Model, st.ENCount, st.JACount, st.DenyCount,
		rules.DenyLimit, st.ToolCalls, st.Streak, st.LastVerdict,
		st.ContextTokens, st.OutputTokens, st.Notices, st.InjectedChars, st.CWD)
}

func sessionsReport(store *session.Store, limit int) string {
	if limit <= 0 {
		limit = 10
	}
	all := store.List()
	if len(all) == 0 {
		return "no supervised sessions"
	}
	if len(all) > limit {
		all = all[:limit]
	}
	var b strings.Builder
	for _, st := range all {
		fmt.Fprintf(&b, "%s EN=%d JA=%d denied=%d calls=%d %s\n",
			st.SessionID, st.ENCount, st.JACount, st.DenyCount, st.ToolCalls,
			st.UpdatedAt.Format("01-02 15:04"))
	}
	return strings.TrimRight(b.String(), "\n")
}

func checkReport(text string) string {
	if strings.TrimSpace(text) == "" {
		return "empty text"
	}
	v := lang.Classify(text)
	var b strings.Builder
	fmt.Fprintf(&b, "verdict=%s", v)
	if spans := lang.EnglishSpans(text, 3); len(spans) > 0 {
		b.WriteString("\nenglish sentences:")
		for _, s := range spans {
			fmt.Fprintf(&b, "\n> %s", s)
		}
	}
	if hit := rules.ScanPatterns(patternsPath(), text); hit != nil {
		fmt.Fprintf(&b, "\nbanned pattern: %s", hit.Name)
	}
	return b.String()
}
