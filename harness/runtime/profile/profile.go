package profile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/collaboration"
	"github.com/mossagents/moss/harness/runtime/permissions"
	policypack "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/x/stringutil"
)

type ProfileResolveOptions struct {
	Workspace        string
	RequestedProfile string
	Trust            string
	ApprovalMode     string
}

type ResolvedProfile struct {
	RequestedName   string
	Name            string
	Label           string
	TaskMode        string
	Trust           string
	ApprovalMode    string
	ToolPolicy      policypack.ToolPolicy
	SessionDefaults appconfig.SessionProfileConfig
}

type SessionPosture struct {
	Profile           string
	EffectiveTrust    string
	EffectiveApproval string
	TaskMode          string
	ToolPolicy        policypack.ToolPolicy
	HasToolPolicy     bool
}

func ProfileNamesForWorkspace(workspace, trust string) ([]string, error) {
	seen := map[string]struct{}{
		"default":  {},
		"coding":   {},
		"research": {},
		"planning": {},
		"readonly": {},
	}
	globalCfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	for name := range globalCfg.Profiles {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	if appconfig.ProjectAssetsAllowed(trust) {
		projectCfg, err := appconfig.LoadProjectConfig(workspace)
		if err != nil {
			return nil, err
		}
		for name := range projectCfg.Profiles {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				seen[trimmed] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func ResolveProfileForWorkspace(opts ProfileResolveOptions) (ResolvedProfile, error) {
	globalCfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		return ResolvedProfile{}, err
	}
	baseTrust := appconfig.NormalizeTrustLevel(opts.Trust)
	projectCfg := &appconfig.Config{}
	if appconfig.ProjectAssetsAllowed(baseTrust) {
		projectCfg, err = appconfig.LoadProjectConfig(opts.Workspace)
		if err != nil {
			return ResolvedProfile{}, err
		}
	}
	requested := strings.TrimSpace(opts.RequestedProfile)
	if requested == "" {
		requested = stringutil.FirstNonEmpty(
			strings.TrimSpace(projectCfg.DefaultProfile),
			strings.TrimSpace(globalCfg.DefaultProfile),
			"default",
		)
	}
	resolvedCfg, ok := resolveProfileConfig(requested, globalCfg, projectCfg)
	if !ok {
		return ResolvedProfile{}, fmt.Errorf("unknown profile %q", requested)
	}
	trust := appconfig.NormalizeTrustLevel(stringutil.FirstNonEmpty(strings.TrimSpace(opts.Trust), strings.TrimSpace(resolvedCfg.Trust), appconfig.TrustTrusted))
	approval := policypack.NormalizeApprovalMode(stringutil.FirstNonEmpty(strings.TrimSpace(opts.ApprovalMode), strings.TrimSpace(resolvedCfg.Approval), "confirm"))
	policy := policypack.ResolveToolPolicyForWorkspace(opts.Workspace, trust, approval)
	var overrideErr error
	policy, overrideErr = ApplyProfileToolPolicy(policy, resolvedCfg.Execution)
	if overrideErr != nil {
		return ResolvedProfile{}, overrideErr
	}
	return ResolvedProfile{
		RequestedName:   requested,
		Name:            requested,
		Label:           stringutil.FirstNonEmpty(strings.TrimSpace(resolvedCfg.Label), requested),
		TaskMode:        stringutil.FirstNonEmpty(strings.TrimSpace(resolvedCfg.TaskMode), requested),
		Trust:           trust,
		ApprovalMode:    approval,
		ToolPolicy:      policy,
		SessionDefaults: resolvedCfg.Session,
	}, nil
}

func ResolveProfileFromPosture(profileName string, posture SessionPosture) (ResolvedProfile, error) {
	trust := appconfig.NormalizeTrustLevel(stringutil.FirstNonEmpty(posture.EffectiveTrust, appconfig.TrustTrusted))
	approval := policypack.NormalizeApprovalMode(stringutil.FirstNonEmpty(posture.EffectiveApproval, "confirm"))
	policy := posture.ToolPolicy
	if !posture.HasToolPolicy {
		policy = policypack.ResolveToolPolicyForWorkspace("", trust, approval)
	}
	return ResolvedProfile{
		RequestedName: strings.TrimSpace(profileName),
		Name:          stringutil.FirstNonEmpty(strings.TrimSpace(profileName), "default"),
		Label:         stringutil.FirstNonEmpty(strings.TrimSpace(profileName), "Default"),
		TaskMode:      stringutil.FirstNonEmpty(posture.TaskMode, strings.TrimSpace(profileName), "coding"),
		Trust:         trust,
		ApprovalMode:  approval,
		ToolPolicy:    policy,
	}, nil
}

func SessionPostureFromResolvedProfile(resolved ResolvedProfile) SessionPosture {
	return SessionPosture{
		Profile:           strings.TrimSpace(resolved.Name),
		EffectiveTrust:    appconfig.NormalizeTrustLevel(resolved.Trust),
		EffectiveApproval: policypack.NormalizeApprovalMode(resolved.ApprovalMode),
		TaskMode:          stringutil.FirstNonEmpty(strings.TrimSpace(resolved.TaskMode), strings.TrimSpace(resolved.Name), "coding"),
		ToolPolicy:        policypack.CloneToolPolicy(resolved.ToolPolicy),
		HasToolPolicy:     true,
	}
}

func ResolveSessionPostureForWorkspace(opts ProfileResolveOptions) (SessionPosture, ResolvedProfile, error) {
	resolved, err := ResolveProfileForWorkspace(opts)
	if err != nil {
		return SessionPosture{}, ResolvedProfile{}, err
	}
	return SessionPostureFromResolvedProfile(resolved), resolved, nil
}

func ApplyResolvedProfileToSessionConfig(cfg session.SessionConfig, resolved ResolvedProfile) session.SessionConfig {
	cfg.Profile = resolved.Name
	if cfg.TrustLevel == "" {
		cfg.TrustLevel = resolved.Trust
	}
	if cfg.SessionSpec == nil || cfg.ResolvedSessionSpec == nil {
		cfg = applyTypedSessionProjection(cfg, resolved)
	}
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}
	cfg.Metadata[session.MetadataEffectiveTrust] = resolved.Trust
	cfg.Metadata[session.MetadataEffectiveApproval] = resolved.ApprovalMode
	cfg.Metadata[session.MetadataTaskMode] = resolved.TaskMode
	toolPolicyMetadata, err := policypack.EncodeToolPolicyMetadata(resolved.ToolPolicy)
	if err != nil {
		panic(fmt.Sprintf("encode tool policy metadata: %v", err))
	}
	cfg.Metadata[session.MetadataToolPolicy] = toolPolicyMetadata
	cfg.Metadata[session.MetadataToolPolicySummary] = session.EncodeToolPolicySummary(policypack.SummarizeToolPolicy(resolved.ToolPolicy))
	if cfg.MaxSteps == 0 && resolved.SessionDefaults.MaxSteps > 0 {
		cfg.MaxSteps = resolved.SessionDefaults.MaxSteps
	}
	if cfg.MaxTokens == 0 && resolved.SessionDefaults.MaxTokens > 0 {
		cfg.MaxTokens = resolved.SessionDefaults.MaxTokens
	}
	return cfg
}

func applyTypedSessionProjection(cfg session.SessionConfig, resolved ResolvedProfile) session.SessionConfig {
	trust := appconfig.NormalizeTrustLevel(stringutil.FirstNonEmpty(cfg.TrustLevel, resolved.Trust, appconfig.TrustTrusted))
	runMode := strings.TrimSpace(cfg.Mode)
	if runMode == "" {
		runMode = "interactive"
	}
	preset := strings.TrimSpace(resolved.Name)
	collaborationMode := legacyCollaborationMode(resolved.TaskMode, resolved.Name)
	permissionProfileName := legacyPermissionProfileName(resolved.Name)
	sessionPolicyName := "legacy-session:" + sanitizeLegacyName(runMode)
	modelProfileName := "legacy-model:" + sanitizeLegacyName(stringutil.FirstNonEmpty(cfg.ModelConfig.Model, "default-model"))
	compiledPolicy, err := permissions.Compile(permissions.Profile{
		Name:                    permissionProfileName,
		ApprovalPolicy:          strings.TrimSpace(resolved.ApprovalMode),
		Command:                 resolved.ToolPolicy.Command,
		HTTP:                    resolved.ToolPolicy.HTTP,
		WorkspaceWriteAccess:    resolved.ToolPolicy.WorkspaceWriteAccess,
		MemoryWriteAccess:       resolved.ToolPolicy.MemoryWriteAccess,
		GraphMutationAccess:     resolved.ToolPolicy.GraphMutationAccess,
		ProtectedPathPrefixes:   append([]string(nil), resolved.ToolPolicy.ProtectedPathPrefixes...),
		ApprovalRequiredClasses: append([]tool.ApprovalClass(nil), resolved.ToolPolicy.ApprovalRequiredClasses...),
		DeniedClasses:           append([]tool.ApprovalClass(nil), resolved.ToolPolicy.DeniedClasses...),
	}, trust)
	if err != nil {
		return cfg
	}
	policyJSON, err := json.Marshal(compiledPolicy)
	if err != nil {
		return cfg
	}
	if cfg.SessionSpec == nil {
		cfg.SessionSpec = &session.SessionSpec{
			Workspace: session.SessionWorkspace{Trust: trust},
			Intent: session.SessionIntent{
				CollaborationMode: collaborationMode,
				PromptPack:        strings.TrimSpace(resolved.Name),
			},
			Runtime: session.SessionRuntime{
				RunMode:           runMode,
				PermissionProfile: permissionProfileName,
				SessionPolicy:     sessionPolicyName,
				ModelProfile:      modelProfileName,
			},
			Origin: session.SessionOrigin{Preset: preset},
		}
	}
	if cfg.ResolvedSessionSpec == nil {
		cfg.ResolvedSessionSpec = &session.ResolvedSessionSpec{
			Workspace: session.ResolvedWorkspace{Trust: trust},
			Intent: session.ResolvedIntent{
				CollaborationMode: collaborationMode,
				PromptPack: session.PromptPackRef{
					ID:     strings.TrimSpace(resolved.Name),
					Source: "legacy:" + strings.TrimSpace(resolved.Name),
				},
				CapabilityCeiling: capabilityStrings(collaboration.CeilingForMode(collaboration.NormalizeMode(collaborationMode)).Slice()),
			},
			Runtime: session.ResolvedRuntime{
				RunMode:           runMode,
				PermissionProfile: permissionProfileName,
				PermissionPolicy:  policyJSON,
				SessionPolicyName: sessionPolicyName,
				SessionPolicy: session.SessionPolicySpec{
					MaxSteps:             cfg.MaxSteps,
					MaxTokens:            cfg.MaxTokens,
					Timeout:              cfg.Timeout,
					AutoCompactThreshold: cfg.ModelConfig.AutoCompactTokenLimit,
				},
				ModelProfile: modelProfileName,
				ModelConfig:  cfg.ModelConfig,
			},
			Prompt: session.ResolvedPrompt{BasePackID: strings.TrimSpace(resolved.Name)},
			Origin: session.ResolvedOrigin{Preset: preset},
		}
	}
	return cfg
}

func legacyCollaborationMode(taskMode, profileName string) string {
	switch strings.ToLower(strings.TrimSpace(stringutil.FirstNonEmpty(taskMode, profileName))) {
	case "planning", "plan":
		return "plan"
	case "research", "investigate":
		return "investigate"
	default:
		return "execute"
	}
}

func legacyPermissionProfileName(profileName string) string {
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = "default"
	}
	return "legacy:" + sanitizeLegacyName(name)
}

func sanitizeLegacyName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "\\", "-")
	if value == "" {
		return "default"
	}
	return value
}

func capabilityStrings(values []collaboration.Capability) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(string(value)))
	}
	return out
}

func SessionPostureFromSession(sess *session.Session) SessionPosture {
	posture := SessionPosture{}
	if sess == nil {
		return posture
	}
	_, preset, workspaceTrust, collaborationMode, _, permissionProfile, _, _ := session.SessionFacetValues(sess)
	posture.Profile = stringutil.FirstNonEmpty(strings.TrimSpace(sess.Config.Profile), preset, permissionProfile)
	posture.EffectiveTrust = appconfig.NormalizeTrustLevel(stringutil.FirstNonEmpty(metadataString(sess.Config.Metadata, session.MetadataEffectiveTrust), workspaceTrust, sess.Config.TrustLevel))
	posture.EffectiveApproval = policypack.NormalizeApprovalMode(metadataString(sess.Config.Metadata, session.MetadataEffectiveApproval))
	posture.TaskMode = stringutil.FirstNonEmpty(metadataString(sess.Config.Metadata, session.MetadataTaskMode), collaborationMode, posture.Profile)
	if policy, ok := metadataToolPolicy(sess.Config.Metadata); ok {
		posture.ToolPolicy = policy
		posture.HasToolPolicy = true
		if posture.EffectiveApproval == "" {
			posture.EffectiveApproval = policypack.NormalizeApprovalMode(policy.ApprovalMode)
		}
		if posture.EffectiveTrust == "" {
			posture.EffectiveTrust = appconfig.NormalizeTrustLevel(policy.Trust)
		}
	} else if compiled, ok := resolvedCompiledPolicy(sess.Config.ResolvedSessionSpec); ok {
		posture.ToolPolicy = policypack.CloneToolPolicy(compiled.Policy)
		posture.HasToolPolicy = true
		if posture.EffectiveApproval == "" {
			posture.EffectiveApproval = policypack.NormalizeApprovalMode(compiled.Policy.ApprovalMode)
		}
		if posture.EffectiveTrust == "" {
			posture.EffectiveTrust = appconfig.NormalizeTrustLevel(stringutil.FirstNonEmpty(compiled.Policy.Trust, compiled.Trust))
		}
	}
	if posture.EffectiveApproval == "" {
		posture.EffectiveApproval = "confirm"
	}
	if posture.EffectiveTrust == "" {
		posture.EffectiveTrust = appconfig.NormalizeTrustLevel(sess.Config.TrustLevel)
	}
	if posture.EffectiveTrust == "" {
		posture.EffectiveTrust = appconfig.TrustTrusted
	}
	if posture.TaskMode == "" {
		posture.TaskMode = stringutil.FirstNonEmpty(collaborationMode, "coding")
	}
	return posture
}

func SessionSummaryFields(sess *session.Session) (profile, effectiveTrust, effectiveApproval, taskMode string) {
	posture := SessionPostureFromSession(sess)
	return posture.Profile, posture.EffectiveTrust, posture.EffectiveApproval, posture.TaskMode
}

func ApplyProfileToolPolicy(policy policypack.ToolPolicy, override appconfig.ExecutionProfileConfig) (policypack.ToolPolicy, error) {
	policy = policypack.CloneToolPolicy(policy)
	if access := normalizeProfileAccess(override.CommandAccess); access != "" {
		policy.Command.Access = access
	}
	if access := normalizeProfileAccess(override.HTTPAccess); access != "" {
		policy.HTTP.Access = access
	}
	if strings.TrimSpace(override.CommandTimeout) != "" {
		dur, err := time.ParseDuration(strings.TrimSpace(override.CommandTimeout))
		if err != nil {
			return policypack.ToolPolicy{}, fmt.Errorf("parse command timeout: %w", err)
		}
		policy.Command.DefaultTimeout = dur
		policy.Command.MaxTimeout = dur
	}
	if strings.TrimSpace(override.HTTPTimeout) != "" {
		dur, err := time.ParseDuration(strings.TrimSpace(override.HTTPTimeout))
		if err != nil {
			return policypack.ToolPolicy{}, fmt.Errorf("parse http timeout: %w", err)
		}
		policy.HTTP.DefaultTimeout = dur
		policy.HTTP.MaxTimeout = dur
	}
	if len(override.CommandRules) > 0 {
		rules, err := normalizeProfileCommandRules(override.CommandRules)
		if err != nil {
			return policypack.ToolPolicy{}, err
		}
		policy.Command.Rules = rules
	}
	if len(override.HTTPRules) > 0 {
		rules, err := normalizeProfileHTTPRules(override.HTTPRules)
		if err != nil {
			return policypack.ToolPolicy{}, err
		}
		policy.HTTP.Rules = rules
	}
	return policy, nil
}

func resolveProfileConfig(name string, globalCfg, projectCfg *appconfig.Config) (appconfig.ProfileConfig, bool) {
	resolved := builtInProfile(name)
	ok := !isZeroProfileConfig(resolved)
	if globalCfg != nil {
		if cfg, exists := globalCfg.Profiles[name]; exists {
			resolved = mergeProfileConfig(resolved, cfg)
			ok = true
		}
	}
	if projectCfg != nil {
		if cfg, exists := projectCfg.Profiles[name]; exists {
			resolved = mergeProfileConfig(resolved, cfg)
			ok = true
		}
	}
	return resolved, ok
}

func builtInProfile(name string) appconfig.ProfileConfig {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "default":
		return appconfig.ProfileConfig{
			Label:    "Default",
			TaskMode: "coding",
			Trust:    appconfig.TrustTrusted,
			Approval: "confirm",
		}
	case "coding":
		return appconfig.ProfileConfig{
			Label:    "Coding",
			TaskMode: "coding",
			Trust:    appconfig.TrustTrusted,
			Approval: "full-auto",
			Session:  appconfig.SessionProfileConfig{MaxSteps: 200},
			Execution: appconfig.ExecutionProfileConfig{
				CommandAccess:  "allow",
				HTTPAccess:     "allow",
				CommandTimeout: "30s",
			},
		}
	case "research":
		return appconfig.ProfileConfig{
			Label:    "Research",
			TaskMode: "research",
			Trust:    appconfig.TrustTrusted,
			Approval: "confirm",
			Execution: appconfig.ExecutionProfileConfig{
				CommandAccess: "require-approval",
				HTTPAccess:    "allow",
			},
		}
	case "planning":
		return appconfig.ProfileConfig{
			Label:    "Planning",
			TaskMode: "planning",
			Trust:    appconfig.TrustTrusted,
			Approval: "confirm",
		}
	case "readonly":
		return appconfig.ProfileConfig{
			Label:    "Read Only",
			TaskMode: "readonly",
			Trust:    appconfig.TrustRestricted,
			Approval: "read-only",
		}
	default:
		return appconfig.ProfileConfig{}
	}
}

func mergeProfileConfig(base, overlay appconfig.ProfileConfig) appconfig.ProfileConfig {
	if strings.TrimSpace(overlay.Label) != "" {
		base.Label = overlay.Label
	}
	if strings.TrimSpace(overlay.TaskMode) != "" {
		base.TaskMode = overlay.TaskMode
	}
	if strings.TrimSpace(overlay.Trust) != "" {
		base.Trust = overlay.Trust
	}
	if strings.TrimSpace(overlay.Approval) != "" {
		base.Approval = overlay.Approval
	}
	if overlay.Session.MaxSteps != 0 {
		base.Session.MaxSteps = overlay.Session.MaxSteps
	}
	if overlay.Session.MaxTokens != 0 {
		base.Session.MaxTokens = overlay.Session.MaxTokens
	}
	if strings.TrimSpace(overlay.Execution.CommandAccess) != "" {
		base.Execution.CommandAccess = overlay.Execution.CommandAccess
	}
	if strings.TrimSpace(overlay.Execution.HTTPAccess) != "" {
		base.Execution.HTTPAccess = overlay.Execution.HTTPAccess
	}
	if strings.TrimSpace(overlay.Execution.CommandTimeout) != "" {
		base.Execution.CommandTimeout = overlay.Execution.CommandTimeout
	}
	if strings.TrimSpace(overlay.Execution.HTTPTimeout) != "" {
		base.Execution.HTTPTimeout = overlay.Execution.HTTPTimeout
	}
	if len(overlay.Execution.CommandRules) > 0 {
		base.Execution.CommandRules = append([]appconfig.CommandRuleConfig(nil), overlay.Execution.CommandRules...)
	}
	if len(overlay.Execution.HTTPRules) > 0 {
		base.Execution.HTTPRules = append([]appconfig.HTTPRuleConfig(nil), overlay.Execution.HTTPRules...)
	}
	return base
}

func isZeroProfileConfig(cfg appconfig.ProfileConfig) bool {
	return strings.TrimSpace(cfg.Label) == "" &&
		strings.TrimSpace(cfg.TaskMode) == "" &&
		strings.TrimSpace(cfg.Trust) == "" &&
		strings.TrimSpace(cfg.Approval) == "" &&
		cfg.Session.MaxSteps == 0 &&
		cfg.Session.MaxTokens == 0 &&
		strings.TrimSpace(cfg.Execution.CommandAccess) == "" &&
		strings.TrimSpace(cfg.Execution.HTTPAccess) == "" &&
		strings.TrimSpace(cfg.Execution.CommandTimeout) == "" &&
		strings.TrimSpace(cfg.Execution.HTTPTimeout) == "" &&
		len(cfg.Execution.CommandRules) == 0 &&
		len(cfg.Execution.HTTPRules) == 0
}

func normalizeProfileAccess(value string) policypack.ToolAccess {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "allow":
		return policypack.ToolAccessAllow
	case "require-approval", "confirm", "ask":
		return policypack.ToolAccessRequireApproval
	case "deny", "read-only", "readonly":
		return policypack.ToolAccessDeny
	default:
		return ""
	}
}

func normalizeProfileCommandRules(rules []appconfig.CommandRuleConfig) ([]policypack.CommandRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]policypack.CommandRule, 0, len(rules))
	for i, rule := range rules {
		match := strings.TrimSpace(rule.Match)
		if match == "" {
			return nil, fmt.Errorf("command rule %d: match is required", i+1)
		}
		access := normalizeProfileAccess(rule.Access)
		if access == "" {
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				name = fmt.Sprintf("#%d", i+1)
			}
			return nil, fmt.Errorf("command rule %s: access must be allow, require-approval, or deny", name)
		}
		out = append(out, policypack.CommandRule{
			Name:   strings.TrimSpace(rule.Name),
			Match:  match,
			Access: access,
		})
	}
	return out, nil
}

func normalizeProfileHTTPRules(rules []appconfig.HTTPRuleConfig) ([]policypack.HTTPRule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]policypack.HTTPRule, 0, len(rules))
	for i, rule := range rules {
		match := strings.TrimSpace(rule.Match)
		if match == "" {
			return nil, fmt.Errorf("http rule %d: match is required", i+1)
		}
		access := normalizeProfileAccess(rule.Access)
		if access == "" {
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				name = fmt.Sprintf("#%d", i+1)
			}
			return nil, fmt.Errorf("http rule %s: access must be allow, require-approval, or deny", name)
		}
		out = append(out, policypack.HTTPRule{
			Name:    strings.TrimSpace(rule.Name),
			Match:   match,
			Methods: normalizeStringSlice(rule.Methods),
			Access:  access,
		})
	}
	return out, nil
}

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok {
		return ""
	}
	actual, _ := value.(string)
	return strings.TrimSpace(actual)
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func metadataToolPolicy(meta map[string]any) (policypack.ToolPolicy, bool) {
	if meta == nil {
		return policypack.ToolPolicy{}, false
	}
	value, ok := meta[session.MetadataToolPolicy]
	if !ok || value == nil {
		return policypack.ToolPolicy{}, false
	}
	return policypack.DecodeToolPolicyMetadata(value)
}

func resolvedCompiledPolicy(spec *session.ResolvedSessionSpec) (permissions.CompiledPolicy, bool) {
	if spec == nil || len(spec.Runtime.PermissionPolicy) == 0 {
		return permissions.CompiledPolicy{}, false
	}
	var compiled permissions.CompiledPolicy
	if err := json.Unmarshal(spec.Runtime.PermissionPolicy, &compiled); err != nil {
		return permissions.CompiledPolicy{}, false
	}
	return compiled, true
}
