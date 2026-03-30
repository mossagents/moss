package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
)

// PolicyDecision 表示权限决策。
type PolicyDecision string

const (
	Allow           PolicyDecision = "allow"
	Deny            PolicyDecision = "deny"
	RequireApproval PolicyDecision = "require_approval"
)

// ErrDenied 表示工具调用被 Policy 拒绝。
var ErrDenied = errors.New("tool call denied by policy")

// PolicyRule 评估单个工具调用的权限。
type PolicyRule func(spec tool.ToolSpec, input json.RawMessage) PolicyDecision

// PolicyCheck 构造 policy middleware，遍历 rules 取最严格决策（Deny > RequireApproval > Allow）。
func PolicyCheck(rules ...PolicyRule) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeToolCall || mc.Tool == nil {
			return next(ctx)
		}

		decision := Allow
		for _, rule := range rules {
			d := rule(*mc.Tool, mc.Input)
			if stricterThan(d, decision) {
				decision = d
			}
		}

		switch decision {
		case Deny:
			return ErrDenied
		case RequireApproval:
			if mc.IO != nil {
				approval := buildApprovalRequest(mc, "policy requires approval")
				observer := mc.Observer
				if observer == nil {
					observer = port.NoOpObserver{}
				}
				observer.OnApproval(ctx, port.ApprovalEvent{
					SessionID: approval.SessionID,
					Type:      "requested",
					Request:   *approval,
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
				if !resolved.Approved {
					return ErrDenied
				}
			}
		}

		return next(ctx)
	}
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

func buildApprovalRequest(mc *middleware.Context, reason string) *port.ApprovalRequest {
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
		Prompt:      prompt,
		Reason:      reason,
		Input:       append(json.RawMessage(nil), mc.Input...),
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

// DenyTool 创建拒绝指定工具的 PolicyRule。
func DenyTool(names ...string) PolicyRule {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(spec tool.ToolSpec, _ json.RawMessage) PolicyDecision {
		if _, ok := set[spec.Name]; ok {
			return Deny
		}
		return Allow
	}
}

// RequireApprovalFor 创建需要审批的 PolicyRule。
func RequireApprovalFor(names ...string) PolicyRule {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(spec tool.ToolSpec, _ json.RawMessage) PolicyDecision {
		if _, ok := set[spec.Name]; ok {
			return RequireApproval
		}
		return Allow
	}
}

// DefaultAllow 创建默认放行的 PolicyRule。
func DefaultAllow() PolicyRule {
	return func(_ tool.ToolSpec, _ json.RawMessage) PolicyDecision {
		return Allow
	}
}
