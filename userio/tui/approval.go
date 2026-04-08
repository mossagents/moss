package tui

import (
	"encoding/json"
	configpkg "github.com/mossagents/moss/config"
	intr "github.com/mossagents/moss/kernel/interaction"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	approvalChoiceAllowOnce    = "Allow once"
	approvalChoiceAllowSession = "Allow for this session"
	approvalChoiceAllowProject = "Always allow for this project"
	approvalChoiceDeny         = "Deny"
)

type approvalRuleScope string

const (
	approvalRuleScopeSession approvalRuleScope = "session"
	approvalRuleScopeProject approvalRuleScope = "project"
)

type approvalDisplay struct {
	Title               string
	ToolName            string
	Risk                string
	Reason              string
	ActionLabel         string
	ActionValue         string
	ScopeLabel          string
	ScopeValue          string
	SessionID           string
	RuleKey             string
	RuleLabel           string
	SessionDecisionNote string
	ProjectDecisionNote string
	DiffPreview         string // 文件写入类工具的 diff 预览（可为空）
}

type approvalMemoryRule struct {
	Scope     approvalRuleScope
	SessionID string
	ToolName  string
	Key       string
	Label     string
	CreatedAt time.Time
}

func buildApprovalDisplay(req *intr.ApprovalRequest, currentSessionID string) approvalDisplay {
	if req == nil {
		return approvalDisplay{
			Title:       "Approval required",
			Reason:      "This action requires approval by policy.",
			ActionLabel: "Action",
			ActionValue: "Allow requested action?",
		}
	}
	display := approvalDisplay{
		Title:               "Approval required",
		ToolName:            strings.TrimSpace(req.ToolName),
		Risk:                strings.TrimSpace(req.Risk),
		Reason:              strings.TrimSpace(req.Reason),
		ActionLabel:         strings.TrimSpace(req.ActionLabel),
		ActionValue:         strings.TrimSpace(req.ActionValue),
		ScopeLabel:          strings.TrimSpace(req.ScopeLabel),
		ScopeValue:          strings.TrimSpace(req.ScopeValue),
		RuleKey:             strings.TrimSpace(req.CacheKey),
		RuleLabel:           strings.TrimSpace(req.CacheLabel),
		SessionDecisionNote: strings.TrimSpace(req.SessionDecisionNote),
		ProjectDecisionNote: strings.TrimSpace(req.ProjectDecisionNote),
	}
	if display.Reason == "" {
		display.Reason = "This action requires approval by policy."
	}
	display.SessionID = approvalSessionID(req, currentSessionID)
	if display.ActionLabel != "" && display.ActionValue != "" {
		if display.ScopeLabel == "" && display.RuleKey != "" {
			display.ScopeLabel = "Matching rule"
		}
		if display.RuleLabel == "" && display.ScopeValue != "" {
			display.RuleLabel = display.ScopeValue
		}
		return display
	}
	switch display.ToolName {
	case "run_command":
		command, pattern := parseApprovalCommand(req)
		if command != "" {
			display.ActionLabel = "Command"
			display.ActionValue = command
		}
		if pattern != "" {
			display.ScopeLabel = "Matching rule"
			display.ScopeValue = pattern
			display.RuleLabel = pattern
			display.RuleKey = "run_command|" + pattern
			display.SessionDecisionNote = "Future matching commands in this session will be approved automatically."
			display.ProjectDecisionNote = "Future matching commands in this project will be approved automatically."
		}
	case "http_request":
		requestLine, pattern := parseApprovalRequestTarget(req)
		if requestLine != "" {
			display.ActionLabel = "Request"
			display.ActionValue = requestLine
		}
		if pattern != "" {
			display.ScopeLabel = "Matching rule"
			display.ScopeValue = pattern
			display.RuleLabel = pattern
			display.RuleKey = "http_request|" + pattern
			display.SessionDecisionNote = "Future matching requests in this session will be approved automatically."
			display.ProjectDecisionNote = "Future matching requests in this project will be approved automatically."
		}
	default:
		if preview := parseApprovalGenericPreview(req); preview != "" {
			display.ActionLabel = "Action"
			display.ActionValue = preview
		}
		if display.ToolName != "" {
			display.ScopeLabel = "Matching rule"
			display.ScopeValue = display.ToolName
			display.RuleLabel = display.ToolName
			display.RuleKey = "tool|" + display.ToolName
			display.SessionDecisionNote = "Future matching actions in this session will be approved automatically."
			display.ProjectDecisionNote = "Future matching actions in this project will be approved automatically."
		}
	}
	if display.ActionLabel == "" {
		display.ActionLabel = "Action"
		display.ActionValue = strings.TrimSpace(req.Prompt)
	}
	if display.ScopeLabel == "" && display.RuleKey != "" {
		display.ScopeLabel = "Matching rule"
		display.ScopeValue = "This tool"
		display.RuleLabel = "this tool"
		display.RuleKey = "tool|" + display.ToolName
		display.SessionDecisionNote = "Future matching actions in this session will be approved automatically."
		display.ProjectDecisionNote = "Future matching actions in this project will be approved automatically."
	}
	// 文件写入类工具：生成 diff 预览
	if req != nil && len(req.Input) > 0 {
		if diff := buildApprovalDiffPreview(req.ToolName, req.Input); diff != "" {
			display.DiffPreview = diff
		}
	}
	return display
}

// buildApprovalDiffPreview 对文件写入类工具生成可视化的 diff 预览（最多 30 行）。
// 支持 write_file（显示新内容前几行）和 edit_file（显示 old_string → new_string）。
func buildApprovalDiffPreview(toolName string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	const maxLines = 30
	switch toolName {
	case "write_file":
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil || params.Content == "" {
			return ""
		}
		lines := strings.Split(strings.TrimRight(params.Content, "\n"), "\n")
		var b strings.Builder
		shown := min(len(lines), maxLines)
		for _, line := range lines[:shown] {
			b.WriteString("+ ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		if len(lines) > maxLines {
			b.WriteString("… ")
			b.WriteString(strconv.Itoa(len(lines) - maxLines))
			b.WriteString(" more lines\n")
		}
		return b.String()
	case "edit_file":
		var params struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return ""
		}
		var b strings.Builder
		lineCount := 0
		for _, line := range strings.Split(strings.TrimRight(params.OldString, "\n"), "\n") {
			if lineCount >= maxLines {
				b.WriteString("… truncated\n")
				break
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
			lineCount++
		}
		for _, line := range strings.Split(strings.TrimRight(params.NewString, "\n"), "\n") {
			if lineCount >= maxLines*2 {
				b.WriteString("… truncated\n")
				break
			}
			b.WriteString("+ ")
			b.WriteString(line)
			b.WriteString("\n")
			lineCount++
		}
		return b.String()
	}
	return ""
}

func approvalSessionID(req *intr.ApprovalRequest, currentSessionID string) string {
	if req != nil && strings.TrimSpace(req.SessionID) != "" {
		return strings.TrimSpace(req.SessionID)
	}
	return strings.TrimSpace(currentSessionID)
}

func parseApprovalCommand(req *intr.ApprovalRequest) (string, string) {
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

func parseApprovalRequestTarget(req *intr.ApprovalRequest) (string, string) {
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

func parseApprovalGenericPreview(req *intr.ApprovalRequest) string {
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

func approvalMemoryRuleFor(req *intr.ApprovalRequest, currentSessionID string, scope approvalRuleScope, now time.Time) (approvalMemoryRule, bool) {
	display := buildApprovalDisplay(req, currentSessionID)
	if display.RuleKey == "" {
		return approvalMemoryRule{}, false
	}
	rule := approvalMemoryRule{
		Scope:     scope,
		SessionID: display.SessionID,
		ToolName:  display.ToolName,
		Key:       display.RuleKey,
		Label:     display.RuleLabel,
		CreatedAt: now.UTC(),
	}
	if scope == approvalRuleScopeSession && strings.TrimSpace(rule.SessionID) == "" {
		return approvalMemoryRule{}, false
	}
	if scope == approvalRuleScopeProject {
		rule.SessionID = ""
	}
	return rule, true
}

func (r approvalMemoryRule) matches(req *intr.ApprovalRequest, currentSessionID string) bool {
	if req == nil {
		return false
	}
	if strings.TrimSpace(r.Key) == "" {
		return false
	}
	display := buildApprovalDisplay(req, currentSessionID)
	if display.RuleKey != r.Key {
		return false
	}
	switch r.scope() {
	case approvalRuleScopeProject:
		return true
	default:
		return strings.TrimSpace(r.SessionID) != "" && display.SessionID == r.SessionID
	}
}

func (r approvalMemoryRule) scope() approvalRuleScope {
	switch r.Scope {
	case approvalRuleScopeProject:
		return approvalRuleScopeProject
	case approvalRuleScopeSession:
		return approvalRuleScopeSession
	default:
		if strings.TrimSpace(r.SessionID) != "" {
			return approvalRuleScopeSession
		}
		return approvalRuleScopeProject
	}
}

func approvalProjectRulesFromConfig(cfg configpkg.TUIConfig) []approvalMemoryRule {
	if len(cfg.ProjectApprovalRules) == 0 {
		return nil
	}
	out := make([]approvalMemoryRule, 0, len(cfg.ProjectApprovalRules))
	seen := map[string]struct{}{}
	for _, item := range cfg.ProjectApprovalRules {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, approvalMemoryRule{
			Scope:    approvalRuleScopeProject,
			ToolName: strings.TrimSpace(item.ToolName),
			Key:      key,
			Label:    strings.TrimSpace(item.Label),
		})
	}
	return out
}

func approvalProjectRuleConfigs(rules []approvalMemoryRule) []configpkg.ApprovalRuleConfig {
	if len(rules) == 0 {
		return nil
	}
	out := make([]configpkg.ApprovalRuleConfig, 0, len(rules))
	seen := map[string]struct{}{}
	for _, rule := range rules {
		if rule.scope() != approvalRuleScopeProject {
			continue
		}
		key := strings.TrimSpace(rule.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, configpkg.ApprovalRuleConfig{
			ToolName: strings.TrimSpace(rule.ToolName),
			Key:      key,
			Label:    strings.TrimSpace(rule.Label),
		})
	}
	return out
}
