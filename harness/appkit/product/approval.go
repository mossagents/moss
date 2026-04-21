package product

import (
	"encoding/json"
	"fmt"
	"strings"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/harness/runtime/permissions"
	runtimepolicy2 "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const (
	ApprovalModeReadOnly = "read-only"
	ApprovalModeConfirm  = "confirm"
	ApprovalModeFullAuto = "full-auto"
)

func approvalModeToolPolicyForTrust(trust, mode string) (runtimepolicy2.ToolPolicy, error) {
	mode = runtimepolicy2.NormalizeApprovalMode(mode)
	if err := runtimepolicy2.ValidateApprovalMode(mode); err != nil {
		return runtimepolicy2.ToolPolicy{}, err
	}
	return runtimepolicy2.ResolveToolPolicyForWorkspace("", trust, mode), nil
}

func ApplyApprovalMode(k *kernel.Kernel, mode string) (string, error) {
	return ApplyApprovalModeWithTrust(k, "trusted", mode)
}

func ApplyApprovalModeWithTrust(k *kernel.Kernel, trust, mode string) (string, error) {
	mode = runtimepolicy2.NormalizeApprovalMode(mode)
	if err := runtimepolicy2.ValidateApprovalMode(mode); err != nil {
		return "", err
	}
	policy, err := approvalModeToolPolicyForTrust(trust, mode)
	if err != nil {
		return "", err
	}
	return mode, runtimepolicy2.Apply(k, policy)
}

func ApplyToolPolicy(k *kernel.Kernel, policy runtimepolicy2.ToolPolicy) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	return runtimepolicy2.Apply(k, policy)
}

func ApplyCompiledPolicy(k *kernel.Kernel, compiled permissions.CompiledPolicy) error {
	return ApplyToolPolicy(k, compiled.Policy)
}

func ApplyResolvedSessionSpec(k *kernel.Kernel, spec *session.ResolvedSessionSpec) error {
	policy, ok, err := ToolPolicyFromResolvedSessionSpec(spec)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("resolved session spec permission policy is unavailable")
	}
	return ApplyToolPolicy(k, policy)
}

func ApplySessionConfig(k *kernel.Kernel, cfg session.SessionConfig) error {
	policy, ok, err := ToolPolicyForSessionConfig(cfg)
	if err != nil {
		return err
	}
	if ok {
		return ApplyToolPolicy(k, policy)
	}
	trust := strings.TrimSpace(cfg.TrustLevel)
	if trust == "" {
		trust = metadataString(cfg.Metadata, session.MetadataEffectiveTrust)
	}
	if trust == "" {
		trust = appconfig.TrustTrusted
	}
	approval := runtimepolicy2.NormalizeApprovalMode(metadataString(cfg.Metadata, session.MetadataEffectiveApproval))
	if approval == "" {
		approval = ApprovalModeConfirm
	}
	_, err = ApplyApprovalModeWithTrust(k, trust, approval)
	return err
}

func EvaluateToolPolicy(policy runtimepolicy2.ToolPolicy, spec tool.ToolSpec, input json.RawMessage) governance.PolicyDecision {
	return runtimepolicy2.Evaluate(policy, spec, input)
}

func PersistProjectApprovalAmendment(workspace string, amendment *io.ExecPolicyAmendment) error {
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
	profile := strings.TrimSpace(cfg.DefaultProfile)
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

func ToolPolicyForSessionConfig(cfg session.SessionConfig) (runtimepolicy2.ToolPolicy, bool, error) {
	if cfg.ResolvedSessionSpec != nil {
		policy, ok, err := ToolPolicyFromResolvedSessionSpec(cfg.ResolvedSessionSpec)
		if err != nil {
			return runtimepolicy2.ToolPolicy{}, false, err
		}
		if ok {
			return policy, true, nil
		}
	}
	if policy, ok := toolPolicyFromMetadata(cfg.Metadata); ok {
		return policy, true, nil
	}
	return runtimepolicy2.ToolPolicy{}, false, nil
}

func ToolPolicyFromResolvedSessionSpec(spec *session.ResolvedSessionSpec) (runtimepolicy2.ToolPolicy, bool, error) {
	if spec == nil || len(spec.Runtime.PermissionPolicy) == 0 {
		return runtimepolicy2.ToolPolicy{}, false, nil
	}
	var compiled permissions.CompiledPolicy
	if err := json.Unmarshal(spec.Runtime.PermissionPolicy, &compiled); err == nil {
		if err := runtimepolicy2.ValidateToolPolicy(compiled.Policy); err == nil {
			return runtimepolicy2.NormalizeToolPolicy(compiled.Policy), true, nil
		}
	}
	var policy runtimepolicy2.ToolPolicy
	if err := json.Unmarshal(spec.Runtime.PermissionPolicy, &policy); err != nil {
		return runtimepolicy2.ToolPolicy{}, false, fmt.Errorf("decode resolved session tool policy: %w", err)
	}
	if err := runtimepolicy2.ValidateToolPolicy(policy); err != nil {
		return runtimepolicy2.ToolPolicy{}, false, fmt.Errorf("validate resolved session tool policy: %w", err)
	}
	return runtimepolicy2.NormalizeToolPolicy(policy), true, nil
}

func toolPolicyFromMetadata(meta map[string]any) (runtimepolicy2.ToolPolicy, bool) {
	if meta == nil {
		return runtimepolicy2.ToolPolicy{}, false
	}
	value, ok := meta[session.MetadataToolPolicy]
	if !ok || value == nil {
		return runtimepolicy2.ToolPolicy{}, false
	}
	return runtimepolicy2.DecodeToolPolicyMetadata(value)
}

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
