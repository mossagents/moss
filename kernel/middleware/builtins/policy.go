package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

// PolicyDecision 表示权限决策。
type PolicyDecision = port.PolicyDecision

const (
	Allow           PolicyDecision = port.PolicyAllow
	Deny            PolicyDecision = port.PolicyDeny
	RequireApproval PolicyDecision = port.PolicyRequireApproval
)

type PolicyContext = port.PolicyContext
type PolicyResult = port.PolicyResult
type EnforcementMode = port.EnforcementMode

const (
	EnforcementHardBlock       EnforcementMode = port.EnforcementHardBlock
	EnforcementRequireApproval EnforcementMode = port.EnforcementRequireApproval
	EnforcementSoftLimit       EnforcementMode = port.EnforcementSoftLimit
)

// ErrDenied 表示工具调用被 Policy 拒绝。
var ErrDenied = errors.New("tool call denied by policy")

// PolicyDeniedError 是带结构化策略原因的拒绝错误。
type PolicyDeniedError struct {
	ToolName    string
	ReasonCode  string
	Reason      string
	Enforcement EnforcementMode
}

func (e *PolicyDeniedError) Error() string {
	return ErrDenied.Error()
}

func (e *PolicyDeniedError) Unwrap() error {
	return ErrDenied
}

func (e *PolicyDeniedError) AsKernelError() *kerrors.Error {
	err := kerrors.New(kerrors.ErrPolicyDenied, e.Error()).
		WithMeta("tool", e.ToolName).
		WithMeta("reason_code", e.ReasonCode).
		WithMeta("reason", e.Reason).
		WithMeta("enforcement", string(e.Enforcement))
	return err
}

// PolicyRule 评估单个工具调用的权限。
type PolicyRule func(ctx PolicyContext) PolicyResult

// PolicyCheck 构造 policy middleware，遍历 rules 取最严格决策（Deny > RequireApproval > Allow）。
func PolicyCheck(rules ...PolicyRule) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeToolCall || mc.Tool == nil {
			return next(ctx)
		}

		policyCtx := buildPolicyContext(mc)
		result := evaluatePolicyRules(policyCtx, rules)
		emitPolicyRuleMatchedEvent(ctx, mc, result)
		if err := applyPolicyDecision(ctx, mc, result); err != nil {
			return err
		}

		return next(ctx)
	}
}

func evaluatePolicyRules(policyCtx PolicyContext, rules []PolicyRule) PolicyResult {
	result := allowResult()
	for _, rule := range rules {
		nextResult := normalizePolicyResult(rule(policyCtx))
		if stricterThan(nextResult.Decision, result.Decision) || preferPolicyResult(nextResult, result) {
			result = nextResult
		}
	}
	return result
}

func emitPolicyRuleMatchedEvent(ctx context.Context, mc *middleware.Context, result PolicyResult) {
	if mc == nil || mc.Observer == nil || mc.Tool == nil || len(result.Meta) == 0 {
		return
	}
	data := copyPolicyMeta(result.Meta)
	data["reason"] = result.Reason.Message
	data["reason_code"] = result.Reason.Code
	for k, v := range extractPolicyInputDetails(mc.Tool.Name, mc.Input) {
		data[k] = v
	}
	sessionID := ""
	if mc.Session != nil {
		sessionID = mc.Session.ID
	}
	port.ObserveExecutionEvent(ctx, mc.Observer, port.ExecutionEvent{
		Type:        port.ExecutionPolicyRuleMatched,
		SessionID:   sessionID,
		Timestamp:   time.Now().UTC(),
		ToolName:    mc.Tool.Name,
		Risk:        string(mc.Tool.Risk),
		ReasonCode:  result.Reason.Code,
		Enforcement: result.Enforcement,
		Data:        data,
	})
}

func applyPolicyDecision(ctx context.Context, mc *middleware.Context, result PolicyResult) error {
	switch result.Decision {
	case Deny:
		if mc.IO != nil {
			_ = mc.IO.Send(ctx, port.OutputMessage{
				Type: port.OutputText,
				Content: port.FormatDeniedMessage(
					mc.Tool.Name,
					result.Reason.Message,
					result.Reason.Code,
					result.Enforcement,
				),
			})
		}
		return policyDeniedError(mc, result)
	case RequireApproval:
		if mc.IO == nil {
			return nil
		}
		return handlePolicyApproval(ctx, mc, result)
	default:
		return nil
	}
}

func handlePolicyApproval(ctx context.Context, mc *middleware.Context, result PolicyResult) error {
	approval := buildApprovalRequest(mc, result)
	observer := approvalObserver(mc)
	port.ObserveApproval(ctx, observer, port.ApprovalEvent{
		SessionID: approval.SessionID,
		Type:      "requested",
		Request:   *approval,
	})
	port.ObserveExecutionEvent(ctx, observer, port.ExecutionEvent{
		Type:        port.ExecutionApprovalRequest,
		SessionID:   approval.SessionID,
		Timestamp:   time.Now().UTC(),
		ToolName:    approval.ToolName,
		Risk:        approval.Risk,
		ReasonCode:  approval.ReasonCode,
		Enforcement: approval.Enforcement,
		Data:        approvalRequestData(approval, mc.Input, result.Meta),
	})
	if auto := autoApprovalDecision(mc, approval); auto != nil {
		resolved := port.NormalizeApprovalDecisionForRequest(approval, auto)
		port.ObserveApproval(ctx, observer, port.ApprovalEvent{
			SessionID: approval.SessionID,
			Type:      "resolved",
			Request:   *approval,
			Decision:  resolved,
		})
		port.ObserveExecutionEvent(ctx, observer, port.ExecutionEvent{
			Type:        port.ExecutionApprovalResolved,
			SessionID:   approval.SessionID,
			Timestamp:   time.Now().UTC(),
			ToolName:    approval.ToolName,
			Risk:        approval.Risk,
			ReasonCode:  approval.ReasonCode,
			Enforcement: approval.Enforcement,
			Data:        approvalResolvedData(approval, resolved, mc.Input, result.Meta),
		})
		applyApprovalDecision(mc, approval, resolved)
		return nil
	}
	resp, err := mc.IO.Ask(ctx, port.InputRequest{
		Type:     port.InputConfirm,
		Prompt:   approval.Prompt,
		Approval: approval,
		Meta: map[string]any{
			"tool":        mc.Tool.Name,
			"input":       mc.Input,
			"approval_id": approval.ID,
			"reason":      approval.Reason,
			"reason_code": approval.ReasonCode,
			"risk":        approval.Risk,
		},
	})
	if err != nil {
		return err
	}
	resolved := normalizeApprovalDecision(resp, approval)
	port.ObserveApproval(ctx, observer, port.ApprovalEvent{
		SessionID: approval.SessionID,
		Type:      "resolved",
		Request:   *approval,
		Decision:  resolved,
	})
	port.ObserveExecutionEvent(ctx, observer, port.ExecutionEvent{
		Type:        port.ExecutionApprovalResolved,
		SessionID:   approval.SessionID,
		Timestamp:   time.Now().UTC(),
		ToolName:    approval.ToolName,
		Risk:        approval.Risk,
		ReasonCode:  approval.ReasonCode,
		Enforcement: approval.Enforcement,
		Data:        approvalResolvedData(approval, resolved, mc.Input, result.Meta),
	})
	if !resolved.Approved {
		return policyDeniedError(mc, result)
	}
	applyApprovalDecision(mc, approval, resolved)
	return nil
}

func approvalObserver(mc *middleware.Context) port.Observer {
	if mc != nil && mc.Observer != nil {
		return mc.Observer
	}
	return port.NoOpObserver{}
}

func copyPolicyMeta(meta map[string]any) map[string]any {
	data := map[string]any{}
	for k, v := range meta {
		data[k] = v
	}
	return data
}

func approvalRequestData(approval *port.ApprovalRequest, input []byte, meta map[string]any) map[string]any {
	data := map[string]any{
		"approval_id": approval.ID,
		"reason":      approval.Reason,
		"reason_code": approval.ReasonCode,
		"category":    approval.Category,
		"cache_key":   approval.CacheKey,
	}
	for k, v := range extractPolicyInputDetails(approval.ToolName, input) {
		data[k] = v
	}
	for k, v := range meta {
		data[k] = v
	}
	return data
}

func approvalResolvedData(approval *port.ApprovalRequest, resolved *port.ApprovalDecision, input []byte, meta map[string]any) map[string]any {
	data := map[string]any{
		"approval_id": approval.ID,
		"approved":    resolved.Approved,
		"decision":    resolved.Type,
		"source":      resolved.Source,
		"reason":      approval.Reason,
		"reason_code": approval.ReasonCode,
		"category":    approval.Category,
		"cache_key":   approval.CacheKey,
	}
	for k, v := range extractPolicyInputDetails(approval.ToolName, input) {
		data[k] = v
	}
	for k, v := range meta {
		data[k] = v
	}
	return data
}

func allowResult() PolicyResult {
	return PolicyResult{Decision: Allow}
}

func denyResult(code, message string) PolicyResult {
	return PolicyResult{
		Decision:    Deny,
		Enforcement: EnforcementHardBlock,
		Reason: port.PolicyReason{
			Code:    code,
			Message: message,
		},
	}
}

func requireApprovalResult(code, message string) PolicyResult {
	return PolicyResult{
		Decision:    RequireApproval,
		Enforcement: EnforcementRequireApproval,
		Reason: port.PolicyReason{
			Code:    code,
			Message: message,
		},
	}
}

func normalizePolicyResult(result PolicyResult) PolicyResult {
	if result.Decision == "" {
		result.Decision = Allow
	}
	if result.Enforcement == "" {
		switch result.Decision {
		case Deny:
			result.Enforcement = EnforcementHardBlock
		case RequireApproval:
			result.Enforcement = EnforcementRequireApproval
		}
	}
	return result
}

func preferPolicyResult(a, b PolicyResult) bool {
	if a.Decision != b.Decision {
		return false
	}
	if len(b.Meta) == 0 && len(a.Meta) > 0 {
		return true
	}
	if b.Reason.Code == "" && a.Reason.Code != "" {
		return true
	}
	if b.Reason.Message == "" && a.Reason.Message != "" {
		return true
	}
	return false
}

func stricterThan(a, b PolicyDecision) bool {
	return severity(a) > severity(b)
}

func severity(d PolicyDecision) int {
	switch d {
	case Deny:
		return 2
	case RequireApproval:
		return 1
	default:
		return 0
	}
}

func buildPolicyContext(mc *middleware.Context) PolicyContext {
	ctx := PolicyContext{
		Tool:  *mc.Tool,
		Input: append([]byte(nil), mc.Input...),
	}
	if mc.Session != nil {
		ctx.SessionID = mc.Session.ID
		ctx.SessionState = mc.Session.State
		ctx.Identity = GetIdentity(mc.Session.State)
	}
	return ctx
}

func buildApprovalRequest(mc *middleware.Context, result PolicyResult) *port.ApprovalRequest {
	sessionID := ""
	if mc.Session != nil {
		sessionID = mc.Session.ID
	}
	risk := ""
	toolName := ""
	prompt := "Allow requested action?"
	if mc.Tool != nil {
		toolName = mc.Tool.Name
		risk = string(mc.Tool.Risk)
		prompt = "Allow tool " + mc.Tool.Name + "?"
	}
	category, actionLabel, actionValue, scopeLabel, scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, perms, amendment := describeApproval(toolName, mc.Input)
	req := &port.ApprovalRequest{
		ID:                  fmt.Sprintf("approval-%d", time.Now().UnixNano()),
		Kind:                port.ApprovalKindTool,
		Category:            category,
		SessionID:           sessionID,
		ToolName:            toolName,
		Risk:                risk,
		Prompt:              port.FormatApprovalPrompt(&port.ApprovalRequest{ToolName: toolName, Risk: risk, Prompt: prompt, Reason: result.Reason.Message, ReasonCode: result.Reason.Code, Enforcement: result.Enforcement}),
		Reason:              result.Reason.Message,
		ReasonCode:          result.Reason.Code,
		Enforcement:         result.Enforcement,
		Input:               append([]byte(nil), mc.Input...),
		ActionLabel:         actionLabel,
		ActionValue:         actionValue,
		ScopeLabel:          scopeLabel,
		ScopeValue:          scopeValue,
		CacheKey:            cacheKey,
		CacheLabel:          cacheLabel,
		SessionDecisionNote: sessionNote,
		ProjectDecisionNote: projectNote,
		ProposedPermissions: perms,
		ProposedAmendment:   amendment,
		RequestedAt:         time.Now().UTC(),
	}
	if amendment != nil {
		req.RuleBinding = &port.RuleBinding{
			Category:    category,
			ToolName:    toolName,
			Label:       scopeLabel,
			Match:       scopeValue,
			CacheKey:    cacheKey,
			CommandRule: amendment.CommandRule,
			HTTPRule:    amendment.HTTPRule,
		}
	}
	return port.NormalizeApprovalRequest(req)
}

func normalizeApprovalDecision(resp port.InputResponse, req *port.ApprovalRequest) *port.ApprovalDecision {
	if resp.Decision != nil {
		decision := *resp.Decision
		if decision.RequestID == "" {
			decision.RequestID = req.ID
		}
		if decision.DecidedAt.IsZero() {
			decision.DecidedAt = time.Now().UTC()
		}
		return port.NormalizeApprovalDecisionForRequest(req, &decision)
	}
	decisionType := port.ApprovalDecisionDeny
	if resp.Approved {
		decisionType = port.ApprovalDecisionApprove
	}
	return port.NormalizeApprovalDecisionForRequest(req, &port.ApprovalDecision{
		RequestID: req.ID,
		Type:      decisionType,
		Approved:  resp.Approved,
		Source:    "user_io",
		DecidedAt: time.Now().UTC(),
	})
}

func autoApprovalDecision(mc *middleware.Context, req *port.ApprovalRequest) *port.ApprovalDecision {
	if mc == nil || mc.Session == nil || req == nil {
		return nil
	}
	if rule, ok := session.MatchingApprovalRule(mc.Session, req); ok {
		return port.NormalizeApprovalDecisionForRequest(req, &port.ApprovalDecision{
			RequestID: req.ID,
			Type:      rule.Type,
			Approved:  true,
			Reason:    "remembered approval in session state",
			Source:    "session-policy-cache",
			DecidedAt: time.Now().UTC(),
		})
	}
	if session.PermissionProfileCovers(session.GrantedPermissionsOf(mc.Session), req.ProposedPermissions) {
		return port.NormalizeApprovalDecisionForRequest(req, &port.ApprovalDecision{
			RequestID: req.ID,
			Type:      port.ApprovalDecisionGrantPermission,
			Approved:  true,
			Reason:    "required permissions already granted for this session",
			Source:    "session-permissions",
			DecidedAt: time.Now().UTC(),
		})
	}
	return nil
}

func applyApprovalDecision(mc *middleware.Context, req *port.ApprovalRequest, decision *port.ApprovalDecision) {
	if mc == nil || mc.Session == nil || req == nil || decision == nil || !decision.Approved {
		return
	}
	switch decision.Type {
	case port.ApprovalDecisionApproveSession:
		session.RememberApprovalRule(mc.Session, req, decision.Type, decision.DecidedAt)
	case port.ApprovalDecisionGrantPermission:
		perms := decision.GrantedPermissions
		if perms == nil {
			perms = req.ProposedPermissions
		}
		session.MergeGrantedPermissions(mc.Session, perms)
		session.RememberApprovalRule(mc.Session, req, decision.Type, decision.DecidedAt)
	case port.ApprovalDecisionPolicyAmendment:
		if req.ProposedPermissions != nil {
			session.MergeGrantedPermissions(mc.Session, req.ProposedPermissions)
		}
		session.RememberApprovalRule(mc.Session, req, decision.Type, decision.DecidedAt)
	}
}

func describeApproval(toolName string, input []byte) (port.ApprovalCategory, string, string, string, string, string, string, string, string, *port.PermissionProfile, *port.ExecPolicyAmendment) {
	switch strings.TrimSpace(toolName) {
	case "run_command":
		commandLine, pattern := parseApprovalCommand(input)
		actionValue := commandLine
		if actionValue == "" {
			actionValue = "Allow requested command?"
		}
		cacheKey := ""
		sessionNote := ""
		projectNote := ""
		scopeValue := ""
		cacheLabel := ""
		if pattern != "" {
			cacheKey = "run_command|" + pattern
			cacheLabel = pattern
			scopeValue = pattern
			sessionNote = "Future matching commands in this session will be approved automatically."
			projectNote = "Future matching commands in this project will follow the saved execution rule."
		}
		var amendment *port.ExecPolicyAmendment
		if pattern != "" {
			amendment = &port.ExecPolicyAmendment{
				CommandRule: &port.ExecPolicyCommandRule{
					Name:  "allow-" + sanitizeApprovalName(pattern),
					Match: pattern + "*",
				},
			}
		}
		return port.ApprovalCategoryCommand, "Command", actionValue, "Matching rule", scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, nil, amendment
	case "http_request":
		requestLine, host, method := parseApprovalRequestTarget(input)
		actionValue := requestLine
		if actionValue == "" {
			actionValue = "Allow requested request?"
		}
		cacheKey := ""
		cacheLabel := ""
		sessionNote := ""
		projectNote := ""
		scopeValue := ""
		var perms *port.PermissionProfile
		var amendment *port.ExecPolicyAmendment
		if host != "" {
			cacheKey = "http_request|" + strings.ToUpper(method) + " " + host
			cacheLabel = strings.ToUpper(method) + " " + host
			scopeValue = cacheLabel
			sessionNote = "This host will be allowed for the rest of the session."
			projectNote = "This host will be added to the project's execution policy."
			perms = &port.PermissionProfile{HTTPHosts: []string{host}}
			amendment = &port.ExecPolicyAmendment{
				HTTPRule: &port.ExecPolicyHTTPRule{
					Name:    "allow-" + sanitizeApprovalName(host),
					Match:   host,
					Methods: []string{strings.ToUpper(method)},
				},
			}
		}
		return port.ApprovalCategoryHTTP, "Request", actionValue, "Matching rule", scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, perms, amendment
	default:
		preview := parseApprovalGenericPreview(input)
		if preview == "" {
			preview = "Allow requested action?"
		}
		cacheKey := ""
		cacheLabel := ""
		sessionNote := ""
		projectNote := ""
		scopeValue := ""
		if strings.TrimSpace(toolName) != "" {
			cacheKey = "tool|" + toolName
			cacheLabel = toolName
			scopeValue = toolName
			sessionNote = "Future matching actions in this session will be approved automatically."
			projectNote = "Future matching actions in this project will follow the saved execution rule."
		}
		return port.ApprovalCategoryTool, "Action", preview, "Matching rule", scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, nil, nil
	}
}

func parseApprovalCommand(input []byte) (string, string) {
	if len(input) == 0 {
		return "", ""
	}
	var payload struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", ""
	}
	parts := make([]string, 0, len(payload.Args)+1)
	command := strings.TrimSpace(payload.Command)
	if command != "" {
		parts = append(parts, quoteApprovalToken(command))
	}
	for _, arg := range payload.Args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		parts = append(parts, quoteApprovalToken(arg))
	}
	patternParts := []string{}
	if command != "" {
		patternParts = append(patternParts, command)
	}
	if len(payload.Args) > 0 && strings.TrimSpace(payload.Args[0]) != "" {
		patternParts = append(patternParts, strings.TrimSpace(payload.Args[0]))
	}
	return strings.Join(parts, " "), strings.Join(patternParts, " ")
}

func parseApprovalRequestTarget(input []byte) (string, string, string) {
	if len(input) == 0 {
		return "", "", ""
	}
	var payload struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", "", ""
	}
	rawURL := strings.TrimSpace(payload.URL)
	if rawURL == "" {
		return "", "", ""
	}
	method := strings.ToUpper(strings.TrimSpace(payload.Method))
	if method == "" {
		method = "GET"
	}
	requestLine := method + " " + rawURL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return requestLine, "", method
	}
	return requestLine, strings.TrimSpace(parsed.Hostname()), method
}

func parseApprovalGenericPreview(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
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

func sanitizeApprovalName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "*", "", "/", "-", "\\", "-", ".", "-", ":", "-", "_", "-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	value = strings.Trim(value, "-")
	if value == "" {
		return "rule"
	}
	return value
}

func policyDeniedError(mc *middleware.Context, result PolicyResult) error {
	toolName := ""
	if mc != nil && mc.Tool != nil {
		toolName = mc.Tool.Name
	}
	return &PolicyDeniedError{
		ToolName:    toolName,
		ReasonCode:  result.Reason.Code,
		Reason:      result.Reason.Message,
		Enforcement: result.Enforcement,
	}
}

// DenyTool 创建拒绝指定工具的 PolicyRule。
func DenyTool(names ...string) PolicyRule {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(ctx PolicyContext) PolicyResult {
		if _, ok := set[ctx.Tool.Name]; ok {
			return denyResult("tool.denied", "tool is denied by policy")
		}
		return allowResult()
	}
}

// RequireApprovalFor 创建需要审批的 PolicyRule。
func RequireApprovalFor(names ...string) PolicyRule {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(ctx PolicyContext) PolicyResult {
		if _, ok := set[ctx.Tool.Name]; ok {
			return requireApprovalResult("tool.requires_approval", "tool requires approval by policy")
		}
		return allowResult()
	}
}

// DefaultAllow 创建默认放行的 PolicyRule。
func DefaultAllow() PolicyRule {
	return func(_ PolicyContext) PolicyResult {
		return allowResult()
	}
}
