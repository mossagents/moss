package product

import (
	"encoding/json"
	"fmt"
	"strings"

	appconfig "github.com/mossagents/moss/config"
	runtimepolicy2 "github.com/mossagents/moss/runtime/policy"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/runtime"
)

const (
	ApprovalModeReadOnly = "read-only"
	ApprovalModeConfirm  = "confirm"
	ApprovalModeFullAuto = "full-auto"
)

func approvalModeToolPolicyForTrust(trust, mode string) (runtime.ToolPolicy, error) {
	mode = runtime.NormalizeApprovalMode(mode)
	if err := runtime.ValidateApprovalMode(mode); err != nil {
		return runtime.ToolPolicy{}, err
	}
	return runtime.ResolveToolPolicyForWorkspace("", trust, mode), nil
}

func ApplyApprovalMode(k *kernel.Kernel, mode string) (string, error) {
	return ApplyApprovalModeWithTrust(k, "trusted", mode)
}

func ApplyApprovalModeWithTrust(k *kernel.Kernel, trust, mode string) (string, error) {
	mode = runtime.NormalizeApprovalMode(mode)
	if err := runtime.ValidateApprovalMode(mode); err != nil {
		return "", err
	}
	policy, err := approvalModeToolPolicyForTrust(trust, mode)
	if err != nil {
		return "", err
	}
	return mode, runtimepolicy2.Apply(k, policy)
}

func ApplyResolvedProfile(k *kernel.Kernel, profile runtime.ResolvedProfile) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	mode := runtime.NormalizeApprovalMode(profile.ApprovalMode)
	if err := runtime.ValidateApprovalMode(mode); err != nil {
		return err
	}
	return runtimepolicy2.Apply(k, profile.ToolPolicy)
}

func EvaluateToolPolicy(policy runtime.ToolPolicy, spec tool.ToolSpec, input json.RawMessage) builtins.PolicyDecision {
	return runtimepolicy2.Evaluate(policy, spec, input)
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

