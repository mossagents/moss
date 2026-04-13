package builtins

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
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
func SetIdentity(state map[string]any, id *io.Identity) {
	if state != nil {
		state[identityKey] = id
	}
}

// GetIdentity 从 Session 的 State 中取出认证身份。
func GetIdentity(state map[string]any) *io.Identity {
	if state == nil {
		return nil
	}
	if v, ok := state[identityKey]; ok {
		if id, ok := v.(*io.Identity); ok {
			return id
		}
	}
	return nil
}

// RBAC 构造基于角色的工具访问控制 hook。
// 规则按顺序匹配，第一条匹配的规则决定访问权限。
// 配置规则后默认拒绝：缺失身份或无匹配规则都会被拦截。
func RBAC(rules []RBACRule) hooks.Hook[hooks.ToolEvent] {
	return func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev == nil || ev.Stage != hooks.ToolLifecycleBefore || ev.Tool == nil {
			return nil
		}
		if len(rules) == 0 {
			return nil
		}

		identity := GetIdentity(ev.Session.State)
		if identity == nil {
			return &PolicyDeniedError{
				ToolName:    ev.Tool.Name,
				ReasonCode:  "rbac.identity_required",
				Reason:      "authenticated identity is required by RBAC policy",
				Enforcement: EnforcementHardBlock,
			}
		}

		for _, rule := range rules {
			if !identity.HasRole(rule.Role) {
				continue
			}
			if !rule.matchesTool(ev.Tool.Name) {
				continue
			}
			if rule.Action == RBACDeny {
				return &PolicyDeniedError{
					ToolName:    ev.Tool.Name,
					ReasonCode:  "rbac.role_denied",
					Reason:      "tool access denied by RBAC role policy",
					Enforcement: EnforcementHardBlock,
				}
			}
			return nil
		}

		return &PolicyDeniedError{
			ToolName:    ev.Tool.Name,
			ReasonCode:  "rbac.no_matching_rule",
			Reason:      "tool access denied because no RBAC rule matched",
			Enforcement: EnforcementHardBlock,
		}
	}
}

// AuthMiddleware 在 Session started 生命周期阶段执行认证。
// 从 Session.Config.Metadata["auth_token"] 取出 token 进行认证，
// 认证成功后将 Identity 存入 Session.State。
func AuthMiddleware(auth io.Authenticator) hooks.Hook[session.LifecycleEvent] {
	return func(ctx context.Context, ev *session.LifecycleEvent) error {
		if ev == nil || ev.Stage != session.LifecycleStarted {
			return nil
		}
		v, _ := ev.Session.GetMetadata("auth_token")
		token, _ := v.(string)
		if token == "" {
			return fmt.Errorf("auth token is required")
		}

		identity, err := auth.Authenticate(ctx, token)
		if err != nil {
			return err
		}

		ev.Session.SetState(identityKey, identity)
		return nil
	}
}

// RiskBasedPolicy 创建一个基于工具风险等级的 PolicyRule。
func RiskBasedPolicy(minRisk tool.RiskLevel, decision PolicyDecision) PolicyRule {
	return func(ctx PolicyContext) PolicyResult {
		if riskSeverity(ctx.Tool.Risk) < riskSeverity(minRisk) {
			return allowResult()
		}
		switch decision {
		case Deny:
			return denyResult("risk.threshold", "tool risk exceeds configured threshold")
		case RequireApproval:
			return requireApprovalResult("risk.threshold", "tool risk exceeds approval threshold")
		default:
			return allowResult()
		}
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
