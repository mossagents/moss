package policy

import (
	"encoding/json"

	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/toolpolicy"
)

func CompileRules(policy toolpolicy.ToolPolicy) []builtins.PolicyRule {
	policy = toolpolicy.NormalizeToolPolicy(policy)
	rules := make([]builtins.PolicyRule, 0, 10)
	if rule := compileCommandRules(policy); rule != nil {
		rules = append(rules, rule)
	}
	if rule := compileHTTPRules(policy); rule != nil {
		rules = append(rules, rule)
	}
	rules = append(rules, builtins.DenyCommandContaining("rm -rf /", "format c:", "del /f /q c:\\"))
	if len(policy.ProtectedPathPrefixes) > 0 {
		rules = append(rules, builtins.RequireApprovalForPathPrefix(policy.ProtectedPathPrefixes...))
	}
	if rule := compileEffectRule(policy.WorkspaceWriteAccess, tool.EffectWritesWorkspace); rule != nil {
		rules = append(rules, rule)
	}
	if rule := compileEffectRule(policy.MemoryWriteAccess, tool.EffectWritesMemory); rule != nil {
		rules = append(rules, rule)
	}
	if rule := compileEffectRule(policy.GraphMutationAccess, tool.EffectGraphMutation); rule != nil {
		rules = append(rules, rule)
	}
	if len(policy.ApprovalRequiredClasses) > 0 {
		rules = append(rules, builtins.RequireApprovalForApprovalClasses(policy.ApprovalRequiredClasses...))
	}
	if len(policy.DeniedClasses) > 0 {
		rules = append(rules, builtins.DenyApprovalClasses(policy.DeniedClasses...))
	}
	// 自动执行工具声明的审批类（如 ApprovalClassExplicitUser 要求用户审批）。
	rules = append(rules, builtins.AutoEnforceApprovalClass())
	rules = append(rules, builtins.DefaultAllow())
	return rules
}

func Evaluate(policy toolpolicy.ToolPolicy, spec tool.ToolSpec, input json.RawMessage) builtins.PolicyDecision {
	decision := builtins.Allow
	for _, rule := range CompileRules(policy) {
		next := rule(builtins.PolicyContext{
			Tool:  spec,
			Input: append([]byte(nil), input...),
		})
		if next.Decision == builtins.Deny {
			return builtins.Deny
		}
		if next.Decision == builtins.RequireApproval {
			decision = builtins.RequireApproval
		}
	}
	return decision
}

func compileCommandRules(policy toolpolicy.ToolPolicy) builtins.PolicyRule {
	if len(policy.Command.Rules) == 0 && policy.Command.Access == toolpolicy.ToolAccessAllow {
		return nil
	}
	converted := make([]builtins.CommandPatternRule, 0, len(policy.Command.Rules))
	for _, rule := range policy.Command.Rules {
		converted = append(converted, builtins.CommandPatternRule{
			Name:   rule.Name,
			Match:  rule.Match,
			Access: toolAccessDecision(rule.Access),
		})
	}
	return builtins.CommandRulesWithDefault(toolAccessDecision(policy.Command.Access), converted...)
}

func compileHTTPRules(policy toolpolicy.ToolPolicy) builtins.PolicyRule {
	if len(policy.HTTP.Rules) == 0 && policy.HTTP.Access == toolpolicy.ToolAccessAllow {
		return nil
	}
	converted := make([]builtins.HTTPPatternRule, 0, len(policy.HTTP.Rules))
	for _, rule := range policy.HTTP.Rules {
		converted = append(converted, builtins.HTTPPatternRule{
			Name:    rule.Name,
			Match:   rule.Match,
			Methods: append([]string(nil), rule.Methods...),
			Access:  toolAccessDecision(rule.Access),
		})
	}
	return builtins.HTTPRulesWithDefault(toolAccessDecision(policy.HTTP.Access), converted...)
}

func compileEffectRule(access toolpolicy.ToolAccess, effect tool.Effect) builtins.PolicyRule {
	switch access {
	case toolpolicy.ToolAccessDeny:
		return builtins.DenyEffects(effect)
	case toolpolicy.ToolAccessRequireApproval:
		return builtins.RequireApprovalForEffects(effect)
	default:
		return nil
	}
}

func toolAccessDecision(access toolpolicy.ToolAccess) builtins.PolicyDecision {
	switch access {
	case toolpolicy.ToolAccessDeny:
		return builtins.Deny
	case toolpolicy.ToolAccessRequireApproval:
		return builtins.RequireApproval
	default:
		return builtins.Allow
	}
}
