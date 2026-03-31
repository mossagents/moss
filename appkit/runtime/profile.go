package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
)

const DisableProfilesEnv = "MOSSCODE_DISABLE_PROFILES"

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
	ExecutionPolicy ExecutionPolicy
	SessionDefaults appconfig.SessionProfileConfig
}

type SessionPosture struct {
	Profile           string
	EffectiveTrust    string
	EffectiveApproval string
	TaskMode          string
	ExecutionPolicy   ExecutionPolicy
	HasExecution      bool
	Legacy            bool
}

func ProfilesEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(strings.TrimSpace(getenv(DisableProfilesEnv))))
	return value == "" || (value != "1" && value != "true" && value != "yes" && value != "on")
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

var getenv = func(key string) string {
	return os.Getenv(key)
}

func ResolveProfileForWorkspace(opts ProfileResolveOptions) (ResolvedProfile, error) {
	if !ProfilesEnabled() {
		if strings.TrimSpace(opts.RequestedProfile) != "" {
			return ResolvedProfile{}, fmt.Errorf("profiles are disabled by %s", DisableProfilesEnv)
		}
		return resolveLegacyProfile(opts), nil
	}
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
		requested = firstNonEmptyProfile(
			strings.TrimSpace(projectCfg.DefaultProfile),
			strings.TrimSpace(globalCfg.DefaultProfile),
			"default",
		)
	}
	resolvedCfg, ok := resolveProfileConfig(requested, globalCfg, projectCfg)
	if !ok {
		return ResolvedProfile{}, fmt.Errorf("unknown profile %q", requested)
	}
	trust := appconfig.NormalizeTrustLevel(firstNonEmptyProfile(strings.TrimSpace(opts.Trust), strings.TrimSpace(resolvedCfg.Trust), appconfig.TrustTrusted))
	approval := normalizeExecutionApprovalMode(firstNonEmptyProfile(strings.TrimSpace(opts.ApprovalMode), strings.TrimSpace(resolvedCfg.Approval), "confirm"))
	policy := ResolveExecutionPolicyForWorkspace(opts.Workspace, trust, approval)
	var overrideErr error
	policy, overrideErr = ApplyProfileExecution(policy, resolvedCfg.Execution)
	if overrideErr != nil {
		return ResolvedProfile{}, overrideErr
	}
	return ResolvedProfile{
		RequestedName:   requested,
		Name:            requested,
		Label:           firstNonEmptyProfile(strings.TrimSpace(resolvedCfg.Label), requested),
		TaskMode:        firstNonEmptyProfile(strings.TrimSpace(resolvedCfg.TaskMode), requested),
		Trust:           trust,
		ApprovalMode:    approval,
		ExecutionPolicy: policy,
		SessionDefaults: resolvedCfg.Session,
	}, nil
}

func ResolveProfileFromPosture(profileName string, posture SessionPosture) (ResolvedProfile, error) {
	trust := appconfig.NormalizeTrustLevel(firstNonEmptyProfile(posture.EffectiveTrust, appconfig.TrustTrusted))
	approval := normalizeExecutionApprovalMode(firstNonEmptyProfile(posture.EffectiveApproval, "confirm"))
	policy := posture.ExecutionPolicy
	if !posture.HasExecution {
		policy = ResolveExecutionPolicyForWorkspace("", trust, approval)
	}
	return ResolvedProfile{
		RequestedName:   strings.TrimSpace(profileName),
		Name:            firstNonEmptyProfile(strings.TrimSpace(profileName), "legacy"),
		Label:           firstNonEmptyProfile(strings.TrimSpace(profileName), "legacy"),
		TaskMode:        firstNonEmptyProfile(posture.TaskMode, strings.TrimSpace(profileName), "coding"),
		Trust:           trust,
		ApprovalMode:    approval,
		ExecutionPolicy: policy,
	}, nil
}

func ApplyResolvedProfileToSessionConfig(cfg session.SessionConfig, resolved ResolvedProfile) session.SessionConfig {
	cfg.Profile = resolved.Name
	if cfg.TrustLevel == "" {
		cfg.TrustLevel = resolved.Trust
	}
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}
	cfg.Metadata[session.MetadataEffectiveTrust] = resolved.Trust
	cfg.Metadata[session.MetadataEffectiveApproval] = resolved.ApprovalMode
	cfg.Metadata[session.MetadataTaskMode] = resolved.TaskMode
	cfg.Metadata[session.MetadataExecutionPolicy] = resolved.ExecutionPolicy
	if cfg.MaxSteps == 0 && resolved.SessionDefaults.MaxSteps > 0 {
		cfg.MaxSteps = resolved.SessionDefaults.MaxSteps
	}
	if cfg.MaxTokens == 0 && resolved.SessionDefaults.MaxTokens > 0 {
		cfg.MaxTokens = resolved.SessionDefaults.MaxTokens
	}
	return cfg
}

func SessionPostureFromSession(sess *session.Session) SessionPosture {
	posture := SessionPosture{}
	if sess == nil {
		return posture
	}
	posture.Profile = strings.TrimSpace(sess.Config.Profile)
	posture.EffectiveTrust = appconfig.NormalizeTrustLevel(firstNonEmptyProfile(metadataString(sess.Config.Metadata, session.MetadataEffectiveTrust), sess.Config.TrustLevel))
	posture.EffectiveApproval = normalizeExecutionApprovalMode(metadataString(sess.Config.Metadata, session.MetadataEffectiveApproval))
	posture.TaskMode = firstNonEmptyProfile(metadataString(sess.Config.Metadata, session.MetadataTaskMode), posture.Profile)
	if policy, ok := metadataExecutionPolicy(sess.Config.Metadata); ok {
		posture.ExecutionPolicy = policy
		posture.HasExecution = true
		if posture.EffectiveApproval == "" {
			posture.EffectiveApproval = normalizeExecutionApprovalMode(policy.ApprovalMode)
		}
		if posture.EffectiveTrust == "" {
			posture.EffectiveTrust = appconfig.NormalizeTrustLevel(policy.Trust)
		}
	}
	posture.Legacy = posture.Profile == "" && metadataString(sess.Config.Metadata, session.MetadataEffectiveTrust) == "" && metadataString(sess.Config.Metadata, session.MetadataEffectiveApproval) == ""
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
		posture.TaskMode = "coding"
	}
	return posture
}

func SessionSummaryFields(sess *session.Session) (profile, effectiveTrust, effectiveApproval, taskMode string) {
	posture := SessionPostureFromSession(sess)
	return posture.Profile, posture.EffectiveTrust, posture.EffectiveApproval, posture.TaskMode
}

func ApplyProfileExecution(policy ExecutionPolicy, override appconfig.ExecutionProfileConfig) (ExecutionPolicy, error) {
	policy = cloneExecutionPolicy(policy)
	if access := normalizeProfileAccess(override.CommandAccess); access != "" {
		policy.Command.Access = access
	}
	if access := normalizeProfileAccess(override.HTTPAccess); access != "" {
		policy.HTTP.Access = access
	}
	if strings.TrimSpace(override.CommandTimeout) != "" {
		dur, err := time.ParseDuration(strings.TrimSpace(override.CommandTimeout))
		if err != nil {
			return ExecutionPolicy{}, fmt.Errorf("parse command timeout: %w", err)
		}
		policy.Command.DefaultTimeout = dur
		policy.Command.MaxTimeout = dur
	}
	if strings.TrimSpace(override.HTTPTimeout) != "" {
		dur, err := time.ParseDuration(strings.TrimSpace(override.HTTPTimeout))
		if err != nil {
			return ExecutionPolicy{}, fmt.Errorf("parse http timeout: %w", err)
		}
		policy.HTTP.DefaultTimeout = dur
		policy.HTTP.MaxTimeout = dur
	}
	return policy, nil
}

func resolveLegacyProfile(opts ProfileResolveOptions) ResolvedProfile {
	trust := appconfig.NormalizeTrustLevel(firstNonEmptyProfile(opts.Trust, appconfig.TrustTrusted))
	approval := normalizeExecutionApprovalMode(firstNonEmptyProfile(opts.ApprovalMode, "confirm"))
	return ResolvedProfile{
		RequestedName:   "",
		Name:            "default",
		Label:           "Default",
		TaskMode:        "coding",
		Trust:           trust,
		ApprovalMode:    approval,
		ExecutionPolicy: ResolveExecutionPolicyForWorkspace(opts.Workspace, trust, approval),
	}
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
				CommandAccess: "allow",
				HTTPAccess:    "allow",
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
		strings.TrimSpace(cfg.Execution.HTTPTimeout) == ""
}

func normalizeProfileAccess(value string) ExecutionAccess {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "allow":
		return ExecutionAccessAllow
	case "require-approval", "confirm", "ask":
		return ExecutionAccessRequireApproval
	case "deny", "read-only", "readonly":
		return ExecutionAccessDeny
	default:
		return ""
	}
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

func metadataExecutionPolicy(meta map[string]any) (ExecutionPolicy, bool) {
	if meta == nil {
		return ExecutionPolicy{}, false
	}
	value, ok := meta[session.MetadataExecutionPolicy]
	if !ok || value == nil {
		return ExecutionPolicy{}, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ExecutionPolicy{}, false
	}
	var policy ExecutionPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return ExecutionPolicy{}, false
	}
	return policy, true
}

func firstNonEmptyProfile(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
