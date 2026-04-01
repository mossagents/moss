package tui

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

type approvalDisplay struct {
	Title        string
	ToolName     string
	Risk         string
	Reason       string
	ActionLabel  string
	ActionValue  string
	ScopeLabel   string
	ScopeValue   string
	SessionID    string
	RuleKey      string
	RuleLabel    string
	DecisionNote string
}

type approvalMemoryRule struct {
	SessionID string
	ToolName  string
	Key       string
	Label     string
	CreatedAt time.Time
}

func buildApprovalDisplay(req *port.ApprovalRequest, currentSessionID string) approvalDisplay {
	if req == nil {
		return approvalDisplay{
			Title:       "Approval required",
			Reason:      "This action requires approval by policy.",
			ActionLabel: "Action",
			ActionValue: "Allow requested action?",
		}
	}
	display := approvalDisplay{
		Title:    "Approval required",
		ToolName: strings.TrimSpace(req.ToolName),
		Risk:     strings.TrimSpace(req.Risk),
		Reason:   strings.TrimSpace(req.Reason),
	}
	if display.Reason == "" {
		display.Reason = "This action requires approval by policy."
	}
	display.SessionID = approvalSessionID(req, currentSessionID)
	switch display.ToolName {
	case "run_command":
		command, pattern := parseApprovalCommand(req)
		if command != "" {
			display.ActionLabel = "Command"
			display.ActionValue = command
		}
		if pattern != "" {
			display.ScopeLabel = "Remember for this thread"
			display.ScopeValue = pattern
			display.RuleLabel = pattern
			display.RuleKey = "run_command|" + pattern
			display.DecisionNote = "Future matching commands in this thread will be approved automatically."
		}
	case "http_request":
		requestLine, pattern := parseApprovalRequestTarget(req)
		if requestLine != "" {
			display.ActionLabel = "Request"
			display.ActionValue = requestLine
		}
		if pattern != "" {
			display.ScopeLabel = "Remember for this thread"
			display.ScopeValue = pattern
			display.RuleLabel = pattern
			display.RuleKey = "http_request|" + pattern
			display.DecisionNote = "Future matching requests in this thread will be approved automatically."
		}
	default:
		if preview := parseApprovalGenericPreview(req); preview != "" {
			display.ActionLabel = "Action"
			display.ActionValue = preview
		}
		if display.ToolName != "" {
			display.ScopeLabel = "Remember for this thread"
			display.ScopeValue = display.ToolName
			display.RuleLabel = display.ToolName
			display.RuleKey = "tool|" + display.ToolName
			display.DecisionNote = "Future matching actions in this thread will be approved automatically."
		}
	}
	if display.ActionLabel == "" {
		display.ActionLabel = "Action"
		display.ActionValue = strings.TrimSpace(req.Prompt)
	}
	if display.ScopeLabel == "" {
		display.ScopeLabel = "Remember for this thread"
		display.ScopeValue = "This tool"
		display.RuleLabel = "this tool"
		display.RuleKey = "tool|" + display.ToolName
		display.DecisionNote = "Future matching actions in this thread will be approved automatically."
	}
	return display
}

func approvalSessionID(req *port.ApprovalRequest, currentSessionID string) string {
	if req != nil && strings.TrimSpace(req.SessionID) != "" {
		return strings.TrimSpace(req.SessionID)
	}
	return strings.TrimSpace(currentSessionID)
}

func parseApprovalCommand(req *port.ApprovalRequest) (string, string) {
	if req == nil || len(req.Input) == 0 {
		return "", ""
	}
	var payload struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(req.Input, &payload); err != nil {
		return "", ""
	}
	parts := make([]string, 0, len(payload.Args)+1)
	if strings.TrimSpace(payload.Command) != "" {
		parts = append(parts, quoteApprovalToken(payload.Command))
	}
	for _, arg := range payload.Args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		parts = append(parts, quoteApprovalToken(arg))
	}
	commandLine := strings.Join(parts, " ")
	if commandLine == "" {
		return "", ""
	}
	patternParts := []string{}
	if strings.TrimSpace(payload.Command) != "" {
		patternParts = append(patternParts, payload.Command)
	}
	if len(payload.Args) > 0 && strings.TrimSpace(payload.Args[0]) != "" {
		patternParts = append(patternParts, payload.Args[0])
	}
	return commandLine, strings.Join(patternParts, " ")
}

func parseApprovalRequestTarget(req *port.ApprovalRequest) (string, string) {
	if req == nil || len(req.Input) == 0 {
		return "", ""
	}
	var payload struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(req.Input, &payload); err != nil {
		return "", ""
	}
	rawURL := strings.TrimSpace(payload.URL)
	if rawURL == "" {
		return "", ""
	}
	method := strings.ToUpper(strings.TrimSpace(payload.Method))
	if method == "" {
		method = "GET"
	}
	requestLine := method + " " + rawURL
	parsed, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return requestLine, method
	}
	return requestLine, method + " " + parsed.Host
}

func parseApprovalGenericPreview(req *port.ApprovalRequest) string {
	if req == nil {
		return ""
	}
	if len(req.Input) == 0 {
		return ""
	}
	var payload any
	if err := json.Unmarshal(req.Input, &payload); err != nil {
		return ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	if len(raw) > 220 {
		raw = append(raw[:217], '.', '.', '.')
	}
	return string(raw)
}

func quoteApprovalToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if strings.ContainsAny(token, " \t\r\n\"'") {
		return strconv.Quote(token)
	}
	return token
}

func approvalMemoryRuleFor(req *port.ApprovalRequest, currentSessionID string, now time.Time) (approvalMemoryRule, bool) {
	display := buildApprovalDisplay(req, currentSessionID)
	if display.SessionID == "" || display.RuleKey == "" {
		return approvalMemoryRule{}, false
	}
	return approvalMemoryRule{
		SessionID: display.SessionID,
		ToolName:  display.ToolName,
		Key:       display.RuleKey,
		Label:     display.RuleLabel,
		CreatedAt: now.UTC(),
	}, true
}

func (r approvalMemoryRule) matches(req *port.ApprovalRequest, currentSessionID string) bool {
	if req == nil {
		return false
	}
	if strings.TrimSpace(r.SessionID) == "" || strings.TrimSpace(r.Key) == "" {
		return false
	}
	display := buildApprovalDisplay(req, currentSessionID)
	return display.SessionID == r.SessionID && display.RuleKey == r.Key
}
