package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

type AutoApprovalFunc func(context.Context, *hooks.ToolEvent, *io.ApprovalRequest) *io.ApprovalDecision

// PolicyDecision 表示权限决策。
type PolicyDecision = io.PolicyDecision

const (
	Allow           PolicyDecision = io.PolicyAllow
	Deny            PolicyDecision = io.PolicyDeny
	RequireApproval PolicyDecision = io.PolicyRequireApproval
)

type PolicyContext = io.PolicyContext
type PolicyResult = io.PolicyResult
type EnforcementMode = io.EnforcementMode

const (
	EnforcementHardBlock       EnforcementMode = io.EnforcementHardBlock
	EnforcementRequireApproval EnforcementMode = io.EnforcementRequireApproval
	EnforcementSoftLimit       EnforcementMode = io.EnforcementSoftLimit
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

// PolicyCheck 构造 policy hook，仅在工具调用 before 阶段执行权限决策。
func PolicyCheck(rules ...PolicyRule) hooks.Hook[hooks.ToolEvent] {
	return PolicyCheckWithAutoApprove(nil, rules...)
}

func PolicyCheckWithAutoApprove(autoApprove AutoApprovalFunc, rules ...PolicyRule) hooks.Hook[hooks.ToolEvent] {
	return func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev == nil || ev.Stage != hooks.ToolLifecycleBefore || ev.Tool == nil {
			return nil
		}

		policyCtx := buildPolicyContext(ev)
		result := evaluatePolicyRules(policyCtx, rules)
		emitPolicyRuleMatchedEvent(ctx, ev, result)
		if err := applyPolicyDecision(ctx, ev, result, autoApprove); err != nil {
			return err
		}

		return nil
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

func emitPolicyRuleMatchedEvent(ctx context.Context, ev *hooks.ToolEvent, result PolicyResult) {
	if ev == nil || ev.Observer == nil || ev.Tool == nil || len(result.Meta) == 0 {
		return
	}
	data := copyPolicyMeta(result.Meta)
	data["reason"] = result.Reason.Message
	data["reason_code"] = result.Reason.Code
	for k, v := range policyToolSemantics(ev.Tool) {
		data[k] = v
	}
	for k, v := range extractPolicyInputDetails(ev.Tool.Name, ev.Input) {
		data[k] = v
	}
	sessionID := ""
	if ev.Session != nil {
		sessionID = ev.Session.ID
	}
	observe.ObserveExecutionEvent(ctx, ev.Observer, observe.ExecutionEvent{
		Type:        observe.ExecutionPolicyRuleMatched,
		SessionID:   sessionID,
		Timestamp:   time.Now().UTC(),
		ToolName:    ev.Tool.Name,
		Risk:        string(ev.Tool.Risk),
		ReasonCode:  result.Reason.Code,
		Enforcement: result.Enforcement,
		Metadata:    data,
	})
}

func applyPolicyDecision(ctx context.Context, ev *hooks.ToolEvent, result PolicyResult, autoApprove AutoApprovalFunc) error {
	switch result.Decision {
	case Deny:
		if ev.IO != nil {
			_ = ev.IO.Send(ctx, io.OutputMessage{
				Type: io.OutputText,
				Content: io.FormatDeniedMessage(
					ev.Tool.Name,
					result.Reason.Message,
					result.Reason.Code,
					result.Enforcement,
				),
			})
		}
		return policyDeniedError(ev, result)
	case RequireApproval:
		if ev.IO == nil {
			return nil
		}
		return handlePolicyApproval(ctx, ev, result, autoApprove)
	default:
		return nil
	}
}

func handlePolicyApproval(ctx context.Context, ev *hooks.ToolEvent, result PolicyResult, autoApprove AutoApprovalFunc) error {
	approval := buildApprovalRequest(ev, result)
	observer := approvalObserver(ev)
	observe.ObserveApproval(ctx, observer, io.ApprovalEvent{
		SessionID: approval.SessionID,
		Type:      "requested",
		Request:   *approval,
	})
	observe.ObserveExecutionEvent(ctx, observer, observe.ExecutionEvent{
		Type:        observe.ExecutionApprovalRequest,
		SessionID:   approval.SessionID,
		Timestamp:   time.Now().UTC(),
		ToolName:    approval.ToolName,
		Risk:        approval.Risk,
		ReasonCode:  approval.ReasonCode,
		Enforcement: approval.Enforcement,
		Metadata:    approvalRequestData(approval, ev.Input, result.Meta),
	})
	if auto := autoApprovalDecision(ctx, ev, approval, autoApprove); auto != nil {
		resolved := io.NormalizeApprovalDecisionForRequest(approval, auto)
		observe.ObserveApproval(ctx, observer, io.ApprovalEvent{
			SessionID: approval.SessionID,
			Type:      "resolved",
			Request:   *approval,
			Decision:  resolved,
		})
		observe.ObserveExecutionEvent(ctx, observer, observe.ExecutionEvent{
			Type:        observe.ExecutionApprovalResolved,
			SessionID:   approval.SessionID,
			Timestamp:   time.Now().UTC(),
			ToolName:    approval.ToolName,
			Risk:        approval.Risk,
			ReasonCode:  approval.ReasonCode,
			Enforcement: approval.Enforcement,
			Metadata:    approvalResolvedData(approval, resolved, ev.Input, result.Meta),
		})
		applyApprovalDecision(ev, approval, resolved)
		return nil
	}
	resp, err := ev.IO.Ask(ctx, io.InputRequest{
		Type:     io.InputConfirm,
		Prompt:   approval.Prompt,
		Approval: approval,
		Meta: map[string]any{
			"tool":        ev.Tool.Name,
			"input":       ev.Input,
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
	observe.ObserveApproval(ctx, observer, io.ApprovalEvent{
		SessionID: approval.SessionID,
		Type:      "resolved",
		Request:   *approval,
		Decision:  resolved,
	})
	observe.ObserveExecutionEvent(ctx, observer, observe.ExecutionEvent{
		Type:        observe.ExecutionApprovalResolved,
		SessionID:   approval.SessionID,
		Timestamp:   time.Now().UTC(),
		ToolName:    approval.ToolName,
		Risk:        approval.Risk,
		ReasonCode:  approval.ReasonCode,
		Enforcement: approval.Enforcement,
		Metadata:    approvalResolvedData(approval, resolved, ev.Input, result.Meta),
	})
	if !resolved.Approved {
		return policyDeniedError(ev, result)
	}
	applyApprovalDecision(ev, approval, resolved)
	return nil
}

func approvalObserver(ev *hooks.ToolEvent) observe.Observer {
	if ev != nil && ev.Observer != nil {
		return ev.Observer
	}
	return observe.NoOpObserver{}
}

func copyPolicyMeta(meta map[string]any) map[string]any {
	data := map[string]any{}
	for k, v := range meta {
		data[k] = v
	}
	return data
}

func approvalRequestData(approval *io.ApprovalRequest, input []byte, meta map[string]any) map[string]any {
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

func approvalResolvedData(approval *io.ApprovalRequest, resolved *io.ApprovalDecision, input []byte, meta map[string]any) map[string]any {
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
		Reason: io.PolicyReason{
			Code:    code,
			Message: message,
		},
	}
}

func requireApprovalResult(code, message string) PolicyResult {
	return PolicyResult{
		Decision:    RequireApproval,
		Enforcement: EnforcementRequireApproval,
		Reason: io.PolicyReason{
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

func buildPolicyContext(ev *hooks.ToolEvent) PolicyContext {
	ctx := PolicyContext{
		Tool:  *ev.Tool,
		Input: append([]byte(nil), ev.Input...),
	}
	if ev.Session != nil {
		ctx.SessionID = ev.Session.ID
		ctx.SessionState = ev.Session.CopyState()
		ctx.Identity = GetIdentity(ev.Session)
	}
	return ctx
}

func policyToolSemantics(spec *tool.ToolSpec) map[string]any {
	if spec == nil {
		return nil
	}
	data := map[string]any{
		"side_effect_class":  string(spec.EffectiveSideEffectClass()),
		"approval_class":     string(spec.EffectiveApprovalClass()),
		"planner_visibility": string(spec.EffectivePlannerVisibility()),
		"idempotent":         spec.Idempotent,
	}
	if effects := spec.EffectiveEffects(); len(effects) > 0 {
		data["effects"] = effectsToStrings(effects)
	}
	if len(spec.ResourceScope) > 0 {
		data["resource_scope"] = append([]string(nil), spec.ResourceScope...)
	}
	if len(spec.LockScope) > 0 {
		data["lock_scope"] = append([]string(nil), spec.LockScope...)
	}
	return data
}

func effectsToStrings(effects []tool.Effect) []string {
	out := make([]string, 0, len(effects))
	for _, effect := range effects {
		if effect == "" {
			continue
		}
		out = append(out, string(effect))
	}
	return out
}

func buildApprovalRequest(ev *hooks.ToolEvent, result PolicyResult) *io.ApprovalRequest {
	sessionID := ""
	if ev.Session != nil {
		sessionID = ev.Session.ID
	}
	risk := ""
	toolName := ""
	prompt := "Allow requested action?"
	if ev.Tool != nil {
		toolName = ev.Tool.Name
		risk = string(ev.Tool.Risk)
		prompt = "Allow tool " + ev.Tool.Name + "?"
	}
	category, actionLabel, actionValue, scopeLabel, scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, perms, amendment := describeApproval(toolName, ev.Input)
	req := &io.ApprovalRequest{
		ID:                  fmt.Sprintf("approval-%d", time.Now().UnixNano()),
		Kind:                io.ApprovalKindTool,
		Category:            category,
		SessionID:           sessionID,
		ToolName:            toolName,
		Risk:                risk,
		Prompt:              io.FormatApprovalPrompt(&io.ApprovalRequest{ToolName: toolName, Risk: risk, Prompt: prompt, Reason: result.Reason.Message, ReasonCode: result.Reason.Code, Enforcement: result.Enforcement}),
		Reason:              result.Reason.Message,
		ReasonCode:          result.Reason.Code,
		Enforcement:         result.Enforcement,
		Input:               append([]byte(nil), ev.Input...),
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
		req.RuleBinding = &io.RuleBinding{
			Category:    category,
			ToolName:    toolName,
			Label:       scopeLabel,
			Match:       scopeValue,
			CacheKey:    cacheKey,
			CommandRule: amendment.CommandRule,
			HTTPRule:    amendment.HTTPRule,
		}
	}
	return io.NormalizeApprovalRequest(req)
}

func normalizeApprovalDecision(resp io.InputResponse, req *io.ApprovalRequest) *io.ApprovalDecision {
	if resp.Decision != nil {
		decision := *resp.Decision
		if decision.RequestID == "" {
			decision.RequestID = req.ID
		}
		if decision.DecidedAt.IsZero() {
			decision.DecidedAt = time.Now().UTC()
		}
		return io.NormalizeApprovalDecisionForRequest(req, &decision)
	}
	decisionType := io.ApprovalDecisionDeny
	if resp.Approved {
		decisionType = io.ApprovalDecisionApprove
	}
	return io.NormalizeApprovalDecisionForRequest(req, &io.ApprovalDecision{
		RequestID: req.ID,
		Type:      decisionType,
		Approved:  resp.Approved,
		Source:    "user_io",
		DecidedAt: time.Now().UTC(),
	})
}

func autoApprovalDecision(ctx context.Context, ev *hooks.ToolEvent, req *io.ApprovalRequest, autoApprove AutoApprovalFunc) *io.ApprovalDecision {
	if ev == nil || req == nil {
		return nil
	}
	if ev.Session != nil {
		if rule, ok := session.MatchingApprovalRule(ev.Session, req); ok {
			return io.NormalizeApprovalDecisionForRequest(req, &io.ApprovalDecision{
				RequestID: req.ID,
				Type:      rule.Type,
				Approved:  true,
				Reason:    "remembered approval in session state",
				Source:    "session-policy-cache",
				DecidedAt: time.Now().UTC(),
			})
		}
		if session.PermissionProfileCovers(session.GrantedPermissionsOf(ev.Session), req.ProposedPermissions) {
			return io.NormalizeApprovalDecisionForRequest(req, &io.ApprovalDecision{
				RequestID: req.ID,
				Type:      io.ApprovalDecisionGrantPermission,
				Approved:  true,
				Reason:    "required permissions already granted for this session",
				Source:    "session-permissions",
				DecidedAt: time.Now().UTC(),
			})
		}
	}
	if autoApprove != nil {
		if decision := autoApprove(ctx, ev, req); decision != nil {
			return io.NormalizeApprovalDecisionForRequest(req, decision)
		}
	}
	return nil
}

func applyApprovalDecision(ev *hooks.ToolEvent, req *io.ApprovalRequest, decision *io.ApprovalDecision) {
	if ev == nil || ev.Session == nil || req == nil || decision == nil || !decision.Approved {
		return
	}
	switch decision.Type {
	case io.ApprovalDecisionApproveSession:
		session.RememberApprovalRule(ev.Session, req, decision.Type, decision.DecidedAt)
	case io.ApprovalDecisionGrantPermission:
		perms := decision.GrantedPermissions
		if perms == nil {
			perms = req.ProposedPermissions
		}
		session.MergeGrantedPermissions(ev.Session, perms)
		session.RememberApprovalRule(ev.Session, req, decision.Type, decision.DecidedAt)
	case io.ApprovalDecisionPolicyAmendment:
		if req.ProposedPermissions != nil {
			session.MergeGrantedPermissions(ev.Session, req.ProposedPermissions)
		}
		session.RememberApprovalRule(ev.Session, req, decision.Type, decision.DecidedAt)
	}
}

func describeApproval(toolName string, input []byte) (io.ApprovalCategory, string, string, string, string, string, string, string, string, *io.PermissionProfile, *io.ExecPolicyAmendment) {
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
		var amendment *io.ExecPolicyAmendment
		if pattern != "" {
			amendment = &io.ExecPolicyAmendment{
				CommandRule: &io.ExecPolicyCommandRule{
					Name:  "allow-" + sanitizeApprovalName(pattern),
					Match: pattern + "*",
				},
			}
		}
		return io.ApprovalCategoryCommand, "Command", actionValue, "Matching rule", scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, nil, amendment
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
		var perms *io.PermissionProfile
		var amendment *io.ExecPolicyAmendment
		if host != "" {
			cacheKey = "http_request|" + strings.ToUpper(method) + " " + host
			cacheLabel = strings.ToUpper(method) + " " + host
			scopeValue = cacheLabel
			sessionNote = "This host will be allowed for the rest of the session."
			projectNote = "This host will be added to the project's execution policy."
			perms = &io.PermissionProfile{HTTPHosts: []string{host}}
			amendment = &io.ExecPolicyAmendment{
				HTTPRule: &io.ExecPolicyHTTPRule{
					Name:    "allow-" + sanitizeApprovalName(host),
					Match:   host,
					Methods: []string{strings.ToUpper(method)},
				},
			}
		}
		return io.ApprovalCategoryHTTP, "Request", actionValue, "Matching rule", scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, perms, amendment
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
			inputHash := shortInputHash(input)
			cacheKey = "tool|" + toolName + "|" + inputHash
			cacheLabel = toolName
			scopeValue = toolName
			sessionNote = "This exact action will be approved automatically in this session."
			projectNote = "Future matching actions in this project will follow the saved execution rule."
		}
		return io.ApprovalCategoryTool, "Action", preview, "Matching rule", scopeValue, cacheKey, cacheLabel, sessionNote, projectNote, nil, nil
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

func policyDeniedError(ev *hooks.ToolEvent, result PolicyResult) error {
	toolName := ""
	if ev != nil && ev.Tool != nil {
		toolName = ev.Tool.Name
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

// DenyEffects denies tools whose effective effects intersect the provided set.
func DenyEffects(effects ...tool.Effect) PolicyRule {
	set := make(map[tool.Effect]struct{}, len(effects))
	for _, effect := range effects {
		if effect != "" {
			set[effect] = struct{}{}
		}
	}
	return func(ctx PolicyContext) PolicyResult {
		for _, effect := range ctx.Tool.EffectiveEffects() {
			if _, ok := set[effect]; ok {
				result := denyResult("tool.effect_denied", "tool effect is denied by policy")
				result.Meta = map[string]any{"effect": string(effect)}
				return result
			}
		}
		return allowResult()
	}
}

// RequireApprovalForEffects requires approval when a tool exposes matching effects.
func RequireApprovalForEffects(effects ...tool.Effect) PolicyRule {
	set := make(map[tool.Effect]struct{}, len(effects))
	for _, effect := range effects {
		if effect != "" {
			set[effect] = struct{}{}
		}
	}
	return func(ctx PolicyContext) PolicyResult {
		for _, effect := range ctx.Tool.EffectiveEffects() {
			if _, ok := set[effect]; ok {
				result := requireApprovalResult("tool.effect_requires_approval", "tool effect requires approval by policy")
				result.Meta = map[string]any{"effect": string(effect)}
				return result
			}
		}
		return allowResult()
	}
}

// DenyApprovalClasses denies tools whose effective approval class matches the provided set.
func DenyApprovalClasses(classes ...tool.ApprovalClass) PolicyRule {
	set := make(map[tool.ApprovalClass]struct{}, len(classes))
	for _, class := range classes {
		if class != "" {
			set[class] = struct{}{}
		}
	}
	return func(ctx PolicyContext) PolicyResult {
		class := ctx.Tool.EffectiveApprovalClass()
		if _, ok := set[class]; ok {
			result := denyResult("tool.approval_class_denied", "tool approval class is denied by policy")
			result.Meta = map[string]any{"approval_class": string(class)}
			return result
		}
		return allowResult()
	}
}

// RequireApprovalForApprovalClasses requires approval for matching approval classes.
func RequireApprovalForApprovalClasses(classes ...tool.ApprovalClass) PolicyRule {
	set := make(map[tool.ApprovalClass]struct{}, len(classes))
	for _, class := range classes {
		if class != "" {
			set[class] = struct{}{}
		}
	}
	return func(ctx PolicyContext) PolicyResult {
		class := ctx.Tool.EffectiveApprovalClass()
		if _, ok := set[class]; ok {
			result := requireApprovalResult("tool.approval_class_requires_approval", "tool approval class requires approval by policy")
			result.Meta = map[string]any{"approval_class": string(class)}
			return result
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

// shortInputHash 返回 input 内容的 8 位十六进制 FNV-1a 指纹。
func shortInputHash(input []byte) string {
	h := fnv.New32a()
	_, _ = h.Write(input)
	return fmt.Sprintf("%08x", h.Sum32())
}
