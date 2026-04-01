package builtins

import (
	"context"
	"errors"
	"fmt"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
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
		result := allowResult()
		for _, rule := range rules {
			nextResult := normalizePolicyResult(rule(policyCtx))
			if stricterThan(nextResult.Decision, result.Decision) || preferPolicyResult(nextResult, result) {
				result = nextResult
			}
		}
		if mc.Observer != nil && len(result.Meta) > 0 {
			data := map[string]any{}
			for k, v := range result.Meta {
				data[k] = v
			}
			data["reason"] = result.Reason.Message
			data["reason_code"] = result.Reason.Code
			for k, v := range extractPolicyInputDetails(mc.Tool.Name, mc.Input) {
				data[k] = v
			}
			sessionID := ""
			if mc.Session != nil {
				sessionID = mc.Session.ID
			}
			mc.Observer.OnExecutionEvent(ctx, port.ExecutionEvent{
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
			if mc.IO != nil {
				approval := buildApprovalRequest(mc, result)
				observer := mc.Observer
				if observer == nil {
					observer = port.NoOpObserver{}
				}
				observer.OnApproval(ctx, port.ApprovalEvent{
					SessionID: approval.SessionID,
					Type:      "requested",
					Request:   *approval,
				})
				requestData := map[string]any{
					"approval_id": approval.ID,
					"reason":      approval.Reason,
					"reason_code": approval.ReasonCode,
				}
				for k, v := range extractPolicyInputDetails(approval.ToolName, mc.Input) {
					requestData[k] = v
				}
				for k, v := range result.Meta {
					requestData[k] = v
				}
				observer.OnExecutionEvent(ctx, port.ExecutionEvent{
					Type:        port.ExecutionApprovalRequest,
					SessionID:   approval.SessionID,
					Timestamp:   time.Now().UTC(),
					ToolName:    approval.ToolName,
					Risk:        approval.Risk,
					ReasonCode:  approval.ReasonCode,
					Enforcement: approval.Enforcement,
					Data:        requestData,
				})
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
				observer.OnApproval(ctx, port.ApprovalEvent{
					SessionID: approval.SessionID,
					Type:      "resolved",
					Request:   *approval,
					Decision:  resolved,
				})
				resolvedData := map[string]any{
					"approval_id": approval.ID,
					"approved":    resolved.Approved,
					"source":      resolved.Source,
					"reason":      approval.Reason,
					"reason_code": approval.ReasonCode,
				}
				for k, v := range extractPolicyInputDetails(approval.ToolName, mc.Input) {
					resolvedData[k] = v
				}
				for k, v := range result.Meta {
					resolvedData[k] = v
				}
				observer.OnExecutionEvent(ctx, port.ExecutionEvent{
					Type:        port.ExecutionApprovalResolved,
					SessionID:   approval.SessionID,
					Timestamp:   time.Now().UTC(),
					ToolName:    approval.ToolName,
					Risk:        approval.Risk,
					ReasonCode:  approval.ReasonCode,
					Enforcement: approval.Enforcement,
					Data:        resolvedData,
				})
				if !resolved.Approved {
					return policyDeniedError(mc, result)
				}
			}
		}

		return next(ctx)
	}
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
	return &port.ApprovalRequest{
		ID:          fmt.Sprintf("approval-%d", time.Now().UnixNano()),
		Kind:        port.ApprovalKindTool,
		SessionID:   sessionID,
		ToolName:    toolName,
		Risk:        risk,
		Prompt:      port.FormatApprovalPrompt(&port.ApprovalRequest{ToolName: toolName, Risk: risk, Prompt: prompt, Reason: result.Reason.Message, ReasonCode: result.Reason.Code, Enforcement: result.Enforcement}),
		Reason:      result.Reason.Message,
		ReasonCode:  result.Reason.Code,
		Enforcement: result.Enforcement,
		Input:       append([]byte(nil), mc.Input...),
		RequestedAt: time.Now().UTC(),
	}
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
		return &decision
	}
	return &port.ApprovalDecision{
		RequestID: req.ID,
		Approved:  resp.Approved,
		Source:    "user_io",
		DecidedAt: time.Now().UTC(),
	}
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
