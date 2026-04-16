package policy

import (
	"encoding/json"

	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/kernel/tool"
)

func CompileRules(policy ToolPolicy) []governance.PolicyRule {
	policy = NormalizeToolPolicy(policy)
	rules := make([]governance.PolicyRule, 0, 10)
	if rule := compileCommandRules(policy); rule != nil {
		rules = append(rules, rule)
	}
	if rule := compileHTTPRules(policy); rule != nil {
		rules = append(rules, rule)
	}
	rules = append(rules, governance.DenyCommandContaining("rm -rf /", "format c:", "del /f /q c:\\"))
	if len(policy.ProtectedPathPrefixes) > 0 {
		rules = append(rules, governance.RequireApprovalForPathPrefix(policy.ProtectedPathPrefixes...))
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
		rules = append(rules, governance.RequireApprovalForApprovalClasses(policy.ApprovalRequiredClasses...))
	}
	if len(policy.DeniedClasses) > 0 {
		rules = append(rules, governance.DenyApprovalClasses(policy.DeniedClasses...))
	}
	// 自动执行工具声明的审批类（如 ApprovalClassExplicitUser 要求用户审批）。
	rules = append(rules, governance.AutoEnforceApprovalClass())
	rules = append(rules, governance.DefaultAllow())
	return rules
}

func Evaluate(policy ToolPolicy, spec tool.ToolSpec, input json.RawMessage) governance.PolicyDecision {
	decision := governance.Allow
	for _, rule := range CompileRules(policy) {
		next := rule(governance.PolicyContext{
			Tool:  spec,
			Input: append([]byte(nil), input...),
		})
		if next.Decision == governance.Deny {
			return governance.Deny
		}
		if next.Decision == governance.RequireApproval {
			decision = governance.RequireApproval
		}
	}
	return decision
}

func compileCommandRules(policy ToolPolicy) governance.PolicyRule {
	if len(policy.Command.Rules) == 0 && policy.Command.Access == ToolAccessAllow {
		return nil
	}
	converted := make([]governance.CommandPatternRule, 0, len(policy.Command.Rules))
	for _, rule := range policy.Command.Rules {
		converted = append(converted, governance.CommandPatternRule{
			Name:   rule.Name,
			Match:  rule.Match,
			Access: toolAccessDecision(rule.Access),
		})
	}
	return governance.CommandRulesWithDefault(toolAccessDecision(policy.Command.Access), converted...)
}

func compileHTTPRules(policy ToolPolicy) governance.PolicyRule {
	if len(policy.HTTP.Rules) == 0 && policy.HTTP.Access == ToolAccessAllow {
		return nil
	}
	converted := make([]governance.HTTPPatternRule, 0, len(policy.HTTP.Rules))
	for _, rule := range policy.HTTP.Rules {
		converted = append(converted, governance.HTTPPatternRule{
			Name:    rule.Name,
			Match:   rule.Match,
			Methods: append([]string(nil), rule.Methods...),
			Access:  toolAccessDecision(rule.Access),
		})
	}
	return governance.HTTPRulesWithDefault(toolAccessDecision(policy.HTTP.Access), converted...)
}

func compileEffectRule(access ToolAccess, effect tool.Effect) governance.PolicyRule {
	switch access {
	case ToolAccessDeny:
		return governance.DenyEffects(effect)
	case ToolAccessRequireApproval:
		return governance.RequireApprovalForEffects(effect)
	default:
		return nil
	}
}

func toolAccessDecision(access ToolAccess) governance.PolicyDecision {
	switch access {
	case ToolAccessDeny:
		return governance.Deny
	case ToolAccessRequireApproval:
		return governance.RequireApproval
	default:
		return governance.Allow
	}
}
