package product

import (
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/tool"
	"strings"
)

const (
	ApprovalModeReadOnly = "read-only"
	ApprovalModeConfirm  = "confirm"
	ApprovalModeFullAuto = "full-auto"
)

func NormalizeApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "confirm", "ask", "safe":
		return ApprovalModeConfirm
	case "read-only", "readonly", "ro":
		return ApprovalModeReadOnly
	case "full-auto", "full", "auto":
		return ApprovalModeFullAuto
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func ValidateApprovalMode(mode string) error {
	switch NormalizeApprovalMode(mode) {
	case ApprovalModeReadOnly, ApprovalModeConfirm, ApprovalModeFullAuto:
		return nil
	default:
		return fmt.Errorf("unknown approval mode %q (supported: read-only, confirm, full-auto)", strings.TrimSpace(mode))
	}
}

func ApprovalModePolicyRules(mode string) ([]builtins.PolicyRule, error) {
	return ApprovalModePolicyRulesForTrust("trusted", mode)
}

func ApprovalModePolicyRulesForTrust(trust, mode string) ([]builtins.PolicyRule, error) {
	mode = NormalizeApprovalMode(mode)
	if err := ValidateApprovalMode(mode); err != nil {
		return nil, err
	}
	policy := runtime.ResolveExecutionPolicyForWorkspace("", trust, mode)
	rules := approvalModePolicyRulesForPolicy(mode, policy)
	return rules, nil
}

func approvalModePolicyRulesForPolicy(mode string, policy runtime.ExecutionPolicy) []builtins.PolicyRule {
	rules := append([]builtins.PolicyRule{}, runtime.ExecutionPolicyRules(policy)...)
	switch mode {
	case ApprovalModeReadOnly:
		rules = append(rules,
			builtins.DenyEffects(tool.EffectWritesWorkspace, tool.EffectWritesMemory, tool.EffectGraphMutation),
			builtins.DenyApprovalClasses(tool.ApprovalClassSupervisorOnly),
			builtins.DefaultAllow(),
		)
	case ApprovalModeConfirm:
		rules = append(rules,
			builtins.RequireApprovalForPathPrefix(".git", ".moss"),
			builtins.RequireApprovalForEffects(tool.EffectWritesWorkspace, tool.EffectWritesMemory, tool.EffectGraphMutation),
			builtins.RequireApprovalForApprovalClasses(tool.ApprovalClassExplicitUser),
			builtins.DenyApprovalClasses(tool.ApprovalClassSupervisorOnly),
			builtins.DefaultAllow(),
		)
	case ApprovalModeFullAuto:
		rules = append(rules,
			builtins.DenyApprovalClasses(tool.ApprovalClassSupervisorOnly),
			builtins.DefaultAllow(),
		)
	}
	return rules
}

func ApplyApprovalMode(k *kernel.Kernel, mode string) (string, error) {
	return ApplyApprovalModeWithTrust(k, "trusted", mode)
}

func ApplyApprovalModeWithTrust(k *kernel.Kernel, trust, mode string) (string, error) {
	mode = NormalizeApprovalMode(mode)
	if err := ValidateApprovalMode(mode); err != nil {
		return "", err
	}
	policy := runtime.ResolveExecutionPolicyForKernel(k, trust, mode)
	runtime.SetExecutionPolicy(k, policy)
	rules := approvalModePolicyRulesForPolicy(mode, policy)
	if len(rules) == 0 {
		return mode, nil
	}
	k.WithPolicy(rules...)
	return mode, nil
}

func ApplyResolvedProfile(k *kernel.Kernel, profile runtime.ResolvedProfile) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	mode := NormalizeApprovalMode(profile.ApprovalMode)
	if err := ValidateApprovalMode(mode); err != nil {
		return err
	}
	runtime.SetExecutionPolicy(k, profile.ExecutionPolicy)
	rules := approvalModePolicyRulesForPolicy(mode, profile.ExecutionPolicy)
	if len(rules) > 0 {
		k.WithPolicy(rules...)
	}
	return nil
}

func EvaluatePolicy(rules []builtins.PolicyRule, spec tool.ToolSpec, input json.RawMessage) builtins.PolicyDecision {
	decision := builtins.Allow
	for _, rule := range rules {
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

func PersistProjectApprovalAmendment(workspace, profile string, amendment *io.ExecPolicyAmendment) error {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return fmt.Errorf("workspace is required")
	}
	if amendment == nil {
		return fmt.Errorf("policy amendment is required")
	}
	cfgPath := appconfig.DefaultProjectConfigPath(workspace)
	cfg, err := appconfig.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load project config: %w", err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]appconfig.ProfileConfig{}
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = strings.TrimSpace(cfg.DefaultProfile)
	}
	if profile == "" {
		profile = "default"
	}
	profileCfg := cfg.Profiles[profile]
	if cmd := amendment.CommandRule; cmd != nil && strings.TrimSpace(cmd.Match) != "" {
		next := appconfig.CommandRuleConfig{
			Name:   strings.TrimSpace(cmd.Name),
			Match:  strings.TrimSpace(cmd.Match),
			Access: "allow",
		}
		if !hasProjectCommandRule(profileCfg.Execution.CommandRules, next) {
			profileCfg.Execution.CommandRules = append(profileCfg.Execution.CommandRules, next)
		}
	}
	if http := amendment.HTTPRule; http != nil && strings.TrimSpace(http.Match) != "" {
		next := appconfig.HTTPRuleConfig{
			Name:    strings.TrimSpace(http.Name),
			Match:   strings.TrimSpace(http.Match),
			Methods: append([]string(nil), http.Methods...),
			Access:  "allow",
		}
		if !hasProjectHTTPRule(profileCfg.Execution.HTTPRules, next) {
			profileCfg.Execution.HTTPRules = append(profileCfg.Execution.HTTPRules, next)
		}
	}
	cfg.Profiles[profile] = profileCfg
	if strings.TrimSpace(cfg.DefaultProfile) == "" {
		cfg.DefaultProfile = profile
	}
	if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
		return fmt.Errorf("save project config: %w", err)
	}
	return nil
}

func hasProjectCommandRule(rules []appconfig.CommandRuleConfig, target appconfig.CommandRuleConfig) bool {
	for _, rule := range rules {
		if strings.EqualFold(strings.TrimSpace(rule.Match), strings.TrimSpace(target.Match)) &&
			strings.EqualFold(strings.TrimSpace(rule.Access), strings.TrimSpace(target.Access)) {
			return true
		}
	}
	return false
}

func hasProjectHTTPRule(rules []appconfig.HTTPRuleConfig, target appconfig.HTTPRuleConfig) bool {
	for _, rule := range rules {
		if !strings.EqualFold(strings.TrimSpace(rule.Match), strings.TrimSpace(target.Match)) ||
			!strings.EqualFold(strings.TrimSpace(rule.Access), strings.TrimSpace(target.Access)) {
			continue
		}
		if len(rule.Methods) != len(target.Methods) {
			continue
		}
		matched := true
		for i := range rule.Methods {
			if !strings.EqualFold(strings.TrimSpace(rule.Methods[i]), strings.TrimSpace(target.Methods[i])) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
