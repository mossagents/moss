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
	kruntime "github.com/mossagents/moss/kernel/runtime"
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

// ApplyApprovalMode 将 approval mode 字符串应用为 kernel tool policy。
//
// Deprecated: 新路径中请使用 RegisterBlueprintPolicyApplier + blueprint.EffectiveToolPolicy。
// 直接调用 ApplyApprovalModeWithTrust 是旧路径的一部分（§阶段4 删除 approval mode runtime application）。
// 仅在非 blueprint 路径（如测试、工具模式）中继续使用。
func ApplyApprovalMode(k *kernel.Kernel, mode string) (string, error) {
	return ApplyApprovalModeWithTrust(k, "trusted", mode)
}

// ApplyApprovalModeWithTrust 将 trust + approval mode 字符串转换为 ToolPolicy 并应用到 kernel。
//
// Deprecated: 新路径中请使用 RegisterBlueprintPolicyApplier + blueprint.EffectiveToolPolicy。
// 此函数是"approval mode runtime application"旧路径的核心（§阶段4 待删除），
// 即"runtime 从 approval mode 字符串推导最终 policy"的实现。
// 新路径中 policy 由 PolicyCompiler 在 blueprint 创建时编译，运行时通过
// blueprintPolicyApplier 钩子注入，不再从 approval mode 字符串重新推导。
// 仅在非 blueprint 路径（如 legacy session 创建、测试）中继续使用。
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

// ────────────────────────────────────────────────────────────────────
// §阶段4: Blueprint policy applier — 替代 approval mode runtime application
// ────────────────────────────────────────────────────────────────────

// ApplyBlueprintPolicy 将 blueprint 的 EffectiveToolPolicy 应用到 kernel 的 policystate。
// 这是 blueprint path 中替代 ApplyApprovalModeWithTrust 的新路径实现：
//   - blueprint.EffectiveToolPolicy.Raw["profile"] 携带 PolicyCompiler 编译时的 permission profile 名
//   - blueprint.EffectiveToolPolicy.Raw["workspace_trust"] 携带 workspace trust level
//   - 二者组合恢复出等效的 ToolPolicy 并应用到 kernel
//
// 若 Raw 字段缺失，则 fallback 到 blueprint.EffectiveToolPolicy.TrustLevel 映射。
// 该函数由 RegisterBlueprintPolicyApplier 注册的 hook 调用，不需要手动调用。
func ApplyBlueprintPolicy(k *kernel.Kernel, bp kruntime.SessionBlueprint) {
	if k == nil {
		return
	}
	raw := bp.EffectiveToolPolicy.Raw
	profile := ""
	trust := ""
	if raw != nil {
		if v, ok := raw["profile"].(string); ok {
			profile = strings.TrimSpace(v)
		}
		if v, ok := raw["workspace_trust"].(string); ok {
			trust = strings.TrimSpace(v)
		}
	}
	// profile 名到 approval mode 的映射（与 DefaultPolicyCompiler 别名对称）
	approvalMode := profileToApprovalMode(profile)
	// workspace_trust 映射到 harness trust level
	if trust == "" {
		trust = trustLevelToHarnessTrust(bp.EffectiveToolPolicy.TrustLevel)
	}
	if trust == "" {
		trust = appconfig.TrustTrusted
	}
	policy := runtimepolicy2.ResolveToolPolicyForWorkspace("", trust, approvalMode)
	_ = runtimepolicy2.Apply(k, policy)
}

// RegisterBlueprintPolicyApplier 在 kernel 上注册 blueprint policy applier hook。
// 调用方（如 buildKernel）调用此函数后，每次 RunAgentFromBlueprint 时都会以
// blueprint 编译后的 EffectiveToolPolicy 刷新 kernel policystate，
// 而不再依赖 buildKernel 时的 ApplyApprovalModeWithTrust 调用（旧路径）。
func RegisterBlueprintPolicyApplier(k *kernel.Kernel) {
	if k == nil {
		return
	}
	k.SetBlueprintPolicyApplier(func(bp kruntime.SessionBlueprint) {
		ApplyBlueprintPolicy(k, bp)
	})
}

// profileToApprovalMode 将 DefaultPolicyCompiler 的 profile 名映射到 harness approval mode 字符串。
func profileToApprovalMode(profile string) string {
	switch strings.TrimSpace(strings.ToLower(profile)) {
	case "full", "full-auto":
		return "full-auto"
	case "read-only":
		return "read-only"
	default:
		// "workspace-write", "confirm", "" 等均映射到 confirm（需要显式审批）
		return "confirm"
	}
}

// trustLevelToHarnessTrust 将 EffectiveToolPolicy.TrustLevel（"low"/"medium"/"high"）
// 映射到 harness config trust 字符串（"restricted"/"trusted"）。
func trustLevelToHarnessTrust(level string) string {
	switch strings.TrimSpace(strings.ToLower(level)) {
	case "low":
		return appconfig.TrustRestricted
	default:
		// "medium", "high", "" 均映射到 trusted
		return appconfig.TrustTrusted
	}
}
