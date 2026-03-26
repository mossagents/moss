package builtins

import (
	"context"
	"encoding/json"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
)

// RBACAction 表示 RBAC 规则的动作。
type RBACAction string

const (
	RBACAllow RBACAction = "allow"
	RBACDeny  RBACAction = "deny"
)

// RBACRule 定义一条角色-工具的访问控制规则。
// Tools 支持 "*" 通配符匹配所有工具。
type RBACRule struct {
	Role   string     `json:"role"`
	Tools  []string   `json:"tools"`
	Action RBACAction `json:"action"`
}

// matchesTool 检查规则是否匹配指定工具名。
func (r RBACRule) matchesTool(toolName string) bool {
	for _, t := range r.Tools {
		if t == "*" || t == toolName {
			return true
		}
	}
	return false
}

// identityKey 用于在 Session.State 中存取 Identity 的 key。
const identityKey = "__identity__"

// SetIdentity 将认证身份存入 Session 的 State。
// 通常在 OnSessionStart 阶段由认证 middleware 调用。
func SetIdentity(state map[string]any, id *port.Identity) {
	if state != nil {
		state[identityKey] = id
	}
}

// GetIdentity 从 Session 的 State 中取出认证身份。
func GetIdentity(state map[string]any) *port.Identity {
	if state == nil {
		return nil
	}
	if v, ok := state[identityKey]; ok {
		if id, ok := v.(*port.Identity); ok {
			return id
		}
	}
	return nil
}

// RBAC 构造基于角色的工具访问控制 middleware。
// 规则按顺序匹配，第一条匹配的规则决定访问权限。
// 无匹配规则时默认允许（开放策略）。
func RBAC(rules []RBACRule) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeToolCall || mc.Tool == nil {
			return next(ctx)
		}

		identity := GetIdentity(mc.Session.State)
		if identity == nil {
			// 无身份信息时放行（由上层决定是否必须认证）
			return next(ctx)
		}

		for _, rule := range rules {
			if !identity.HasRole(rule.Role) {
				continue
			}
			if !rule.matchesTool(mc.Tool.Name) {
				continue
			}
			// 第一条匹配的规则
			if rule.Action == RBACDeny {
				return ErrDenied
			}
			return next(ctx)
		}

		// 无匹配规则：默认允许
		return next(ctx)
	}
}

// AuthMiddleware 在 OnSessionStart 阶段执行认证。
// 从 Session.Config.Metadata["auth_token"] 取出 token 进行认证，
// 认证成功后将 Identity 存入 Session.State。
func AuthMiddleware(auth port.Authenticator) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.OnSessionStart {
			return next(ctx)
		}

		token, _ := mc.Session.Config.Metadata["auth_token"].(string)
		if token == "" {
			return next(ctx)
		}

		identity, err := auth.Authenticate(ctx, token)
		if err != nil {
			return err
		}

		if mc.Session.State == nil {
			mc.Session.State = make(map[string]any)
		}
		SetIdentity(mc.Session.State, identity)
		return next(ctx)
	}
}

// RiskBasedPolicy 创建一个基于工具风险等级的 PolicyRule。
// 对指定风险级别及以上的工具调用执行指定决策。
func RiskBasedPolicy(minRisk tool.RiskLevel, decision PolicyDecision) PolicyRule {
	return func(spec tool.ToolSpec, _ json.RawMessage) PolicyDecision {
		if riskSeverity(spec.Risk) >= riskSeverity(minRisk) {
			return decision
		}
		return Allow
	}
}

func riskSeverity(r tool.RiskLevel) int {
	switch r {
	case tool.RiskLow:
		return 0
	case tool.RiskMedium:
		return 1
	case tool.RiskHigh:
		return 2
	default:
		return 0
	}
}
