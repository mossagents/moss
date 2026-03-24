package builtins

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/tool"
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
				resp, err := mc.IO.Ask(ctx, port.InputRequest{
					Type:   port.InputConfirm,
					Prompt: "Allow tool " + mc.Tool.Name + "?",
					Meta:   map[string]any{"tool": mc.Tool.Name, "input": mc.Input},
				})
				if err != nil {
					return err
				}
				if !resp.Approved {
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
