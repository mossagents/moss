package product

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/workspace"
	appruntime "github.com/mossagents/moss/runtime"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/skill"
)

type DoctorReport struct {
	App        string                      `json:"app"`
	Timestamp  string                      `json:"timestamp"`
	Workspace  string                      `json:"workspace"`
	Config     DoctorConfigReport          `json:"config"`
	Execution  DoctorExecutionPolicyReport `json:"execution"`
	Governance DoctorGovernanceReport      `json:"governance"`
	Paths      DoctorPathsReport           `json:"paths"`
	Health     DoctorHealthReport          `json:"health"`
}

type DoctorConfigReport struct {
	ExplicitFlags        []string `json:"explicit_flags,omitempty"`
	DetectedEnv          []string `json:"detected_env,omitempty"`
	GlobalConfig         string   `json:"global_config"`
	GlobalExists         bool     `json:"global_exists"`
	ProjectConfig        string   `json:"project_config"`
	ProjectExists        bool     `json:"project_exists"`
	ProjectAssetsAllowed bool     `json:"project_assets_allowed"`
	ProjectConfigActive  bool     `json:"project_config_active"`
	Provider             string   `json:"provider"`
	Name                 string   `json:"name"`
	Model                string   `json:"model,omitempty"`
	BaseURLSet           bool     `json:"base_url_set"`
	APIKeySet            bool     `json:"api_key_set"`
	Trust                string   `json:"trust"`
	ApprovalMode         string   `json:"approval_mode"`
}

type DoctorExecutionPolicyReport struct {
	CommandAccess             string   `json:"command_access"`
	HTTPAccess                string   `json:"http_access"`
	CommandRuleCount          int      `json:"command_rule_count"`
	HTTPRuleCount             int      `json:"http_rule_count"`
	CommandNetworkMode        string   `json:"command_network_mode"`
	CommandNetworkEnforcement string   `json:"command_network_enforcement"`
	CommandNetworkDegraded    bool     `json:"command_network_degraded"`
	CommandTimeoutSeconds     int      `json:"command_timeout_seconds"`
	CommandAllowedPaths       int      `json:"command_allowed_paths"`
	HTTPMethods               []string `json:"http_methods,omitempty"`
	HTTPSchemes               []string `json:"http_schemes,omitempty"`
	HTTPHostPolicy            string   `json:"http_host_policy"`
	HTTPMaxTimeoutSeconds     int      `json:"http_max_timeout_seconds"`
	HTTPFollowRedirects       bool     `json:"http_follow_redirects"`
}

type DoctorPathsReport struct {
	AppDir             PathStatus `json:"app_dir"`
	StateStoreDir      PathStatus `json:"state_store_dir"`
	StateEventDir      PathStatus `json:"state_event_dir"`
	SessionStoreDir    PathStatus `json:"session_store_dir"`
	MemoryDir          PathStatus `json:"memory_dir"`
	TaskRuntimeDir     PathStatus `json:"task_runtime_dir"`
	WorkspaceIsolation PathStatus `json:"workspace_isolation_dir"`
	PricingCatalog     PathStatus `json:"pricing_catalog"`
	AuditLog           PathStatus `json:"audit_log"`
	DebugLog           PathStatus `json:"debug_log"`
}

type DoctorGovernanceReport struct {
	Model    GovernanceReport         `json:"model"`
	Adaptive *InspectGovernanceReport `json:"adaptive,omitempty"`
}

type DoctorHealthReport struct {
	State      DoctorStateCatalogHealth `json:"state"`
	Sessions   DoctorSessionHealth      `json:"sessions"`
	Tasks      DoctorTaskHealth         `json:"tasks"`
	Workspace  DoctorWorkspaceHealth    `json:"workspace"`
	Repo       DoctorRepoHealth         `json:"repo"`
	Snapshots  DoctorSnapshotHealth     `json:"snapshots"`
	Extensions DoctorExtensionHealth    `json:"extensions"`
}

type DoctorStateCatalogHealth struct {
	Enabled   bool   `json:"enabled"`
	Ready     bool   `json:"ready"`
	Entries   int    `json:"entries"`
	Degraded  bool   `json:"degraded"`
	LastError string `json:"last_error,omitempty"`
}

type PathStatus struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
	Error    string `json:"error,omitempty"`
}

type DoctorSessionHealth struct {
	Recoverable int    `json:"recoverable"`
	Total       int    `json:"total"`
	Error       string `json:"error,omitempty"`
}

type DoctorTaskHealth struct {
	Type  string `json:"type"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

type DoctorWorkspaceHealth struct {
	Type  string `json:"type"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

type DoctorRepoHealth struct {
	Available bool   `json:"available"`
	Root      string `json:"root,omitempty"`
	Head      string `json:"head,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Dirty     bool   `json:"dirty"`
	Error     string `json:"error,omitempty"`
}

type DoctorSnapshotHealth struct {
	Available          bool   `json:"available"`
	Total              int    `json:"total"`
	SessionIndexed     int    `json:"session_indexed"`
	RecoverableMatches int    `json:"recoverable_matches"`
	Error              string `json:"error,omitempty"`
}

type DoctorExtensionHealth struct {
	Configured       int                           `json:"configured"`
	Enabled          int                           `json:"enabled"`
	Disabled         int                           `json:"disabled"`
	MCPServers       int                           `json:"mcp_servers"`
	MCPServerStatus  []MCPServerConfigView         `json:"mcp_server_status,omitempty"`
	PromptSkills     int                           `json:"prompt_skills"`
	DiscoveredSkills int                           `json:"discovered_skills"`
	CapabilityStatus []appruntime.CapabilityStatus `json:"capability_status,omitempty"`
	Error            string                        `json:"error,omitempty"`
}

func BuildDoctorReport(ctx context.Context, appName, workspaceDir string, flags *appkit.AppFlags, explicitFlags []string, approvalMode string, governanceCfg GovernanceConfig) DoctorReport {
	globalConfigPath := appconfig.DefaultGlobalConfigPath()
	projectConfigPath := appconfig.DefaultProjectConfigPath(workspaceDir)
	trust := appconfig.NormalizeTrustLevel(flags.Trust)
	normalizedApprovalMode := NormalizeApprovalMode(approvalMode)
	executionPolicy := resolveDoctorExecutionPolicy(workspaceDir, flags, explicitFlags, trust, normalizedApprovalMode)
	projectAssetsAllowed := appconfig.ProjectAssetsAllowed(trust)
	commandNetworkEnforcement := "none"
	commandNetworkDegraded := false
	if executionPolicy.Command.Access == appruntime.ExecutionAccessDeny {
		commandNetworkEnforcement = "disabled"
	} else if executionPolicy.Command.Network.Mode == workspace.ExecNetworkDisabled {
		commandNetworkEnforcement = "soft-limit"
		commandNetworkDegraded = true
	}
	httpHostPolicy := "open"
	if len(executionPolicy.HTTP.AllowedHosts) > 0 {
		httpHostPolicy = fmt.Sprintf("allowlist(%d)", len(executionPolicy.HTTP.AllowedHosts))
	}
	report := DoctorReport{
		App:       appName,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Workspace: workspaceDir,
		Config: DoctorConfigReport{
			ExplicitFlags:        explicitFlags,
			DetectedEnv:          detectedEnvVars(),
			GlobalConfig:         globalConfigPath,
			GlobalExists:         pathExists(globalConfigPath),
			ProjectConfig:        projectConfigPath,
			ProjectExists:        pathExists(projectConfigPath),
			ProjectAssetsAllowed: projectAssetsAllowed,
			ProjectConfigActive:  projectAssetsAllowed && pathExists(projectConfigPath),
			Provider:             flags.EffectiveAPIType(),
			Name:                 flags.DisplayProviderName(),
			Model:                flags.Model,
			BaseURLSet:           strings.TrimSpace(flags.BaseURL) != "",
			APIKeySet:            strings.TrimSpace(flags.APIKey) != "",
			Trust:                trust,
			ApprovalMode:         normalizedApprovalMode,
		},
		Execution: DoctorExecutionPolicyReport{
			CommandAccess:             string(executionPolicy.Command.Access),
			HTTPAccess:                string(executionPolicy.HTTP.Access),
			CommandRuleCount:          len(executionPolicy.Command.Rules),
			HTTPRuleCount:             len(executionPolicy.HTTP.Rules),
			CommandNetworkMode:        string(executionPolicy.Command.Network.Mode),
			CommandNetworkEnforcement: commandNetworkEnforcement,
			CommandNetworkDegraded:    commandNetworkDegraded,
			CommandTimeoutSeconds:     int(executionPolicy.Command.DefaultTimeout / time.Second),
			CommandAllowedPaths:       len(executionPolicy.Command.AllowedPaths),
			HTTPMethods:               append([]string(nil), executionPolicy.HTTP.AllowedMethods...),
			HTTPSchemes:               append([]string(nil), executionPolicy.HTTP.AllowedSchemes...),
			HTTPHostPolicy:            httpHostPolicy,
			HTTPMaxTimeoutSeconds:     int(executionPolicy.HTTP.MaxTimeout / time.Second),
			HTTPFollowRedirects:       executionPolicy.HTTP.FollowRedirects,
		},
		Governance: DoctorGovernanceReport{
			Model: BuildGovernanceReport(workspaceDir, governanceCfg),
		},
		Paths: DoctorPathsReport{
			AppDir:             checkWritableDir(appconfig.AppDir()),
			StateStoreDir:      checkWritableDir(StateStoreDir()),
			StateEventDir:      checkWritableDir(StateEventDir()),
			SessionStoreDir:    checkWritableDir(SessionStoreDir()),
			MemoryDir:          checkWritableDir(MemoryDir()),
			TaskRuntimeDir:     checkWritableDir(TaskRuntimeDir()),
			WorkspaceIsolation: checkWritableDir(WorkspaceIsolationDir()),
			PricingCatalog:     checkWritableFile(defaultPricingCatalogPath(workspaceDir, governanceCfg.PricingCatalogPath)),
			AuditLog:           checkWritableFile(AuditLogPath()),
			DebugLog:           checkWritableFile(DebugLogPath()),
		},
	}

	report.Health.State = buildDoctorStateHealth()
	if catalog, err := OpenStateCatalog(); err == nil {
		if adaptive, err := buildInspectGovernance(ctx, workspaceDir, catalog, 200); err == nil {
			report.Governance.Adaptive = adaptive
		}
	}
	sessionStore, summaries := populateDoctorSessionsHealth(ctx, &report)
	report.Health.Tasks = buildDoctorTaskHealth()
	report.Health.Workspace = buildDoctorWorkspaceHealth()
	report.Health.Extensions = buildDoctorExtensionHealth(workspaceDir, trust, projectAssetsAllowed, projectConfigPath)
	report.Health.Repo = buildDoctorRepoHealth(ctx, workspaceDir)
	report.Health.Snapshots = buildDoctorSnapshotHealth(ctx, workspaceDir, sessionStore, summaries)

	return report
}

func buildDoctorStateHealth() DoctorStateCatalogHealth {
	stateCatalog, stateErr := OpenStateCatalog()
	if stateErr != nil {
		return DoctorStateCatalogHealth{
			Enabled:   true,
			Ready:     false,
			Degraded:  true,
			LastError: stateErr.Error(),
		}
	}
	if stateCatalog == nil {
		return DoctorStateCatalogHealth{}
	}
	health := stateCatalog.Health()
	return DoctorStateCatalogHealth{
		Enabled:   health.Enabled,
		Ready:     health.Ready,
		Entries:   health.Entries,
		Degraded:  health.Degraded,
		LastError: health.LastError,
	}
}

func populateDoctorSessionsHealth(ctx context.Context, report *DoctorReport) (session.SessionStore, []session.SessionSummary) {
	if report == nil {
		return nil, nil
	}
	sessionStore, err := OpenSessionStore()
	if err != nil {
		report.Health.Sessions.Error = err.Error()
		return nil, nil
	}
	summaries, err := sessionStore.List(ctx)
	if err != nil {
		report.Health.Sessions.Error = err.Error()
		return sessionStore, nil
	}
	report.Health.Sessions.Total = len(summaries)
	for _, summary := range summaries {
		if summary.Recoverable {
			report.Health.Sessions.Recoverable++
		}
	}
	return sessionStore, summaries
}

func buildDoctorTaskHealth() DoctorTaskHealth {
	if _, err := OpenTaskRuntime(); err != nil {
		return DoctorTaskHealth{Type: "file", Ready: false, Error: err.Error()}
	}
	return DoctorTaskHealth{Type: "file", Ready: true}
}

func buildDoctorWorkspaceHealth() DoctorWorkspaceHealth {
	if _, err := OpenWorkspaceIsolation(); err != nil {
		return DoctorWorkspaceHealth{Type: "local", Ready: false, Error: err.Error()}
	}
	return DoctorWorkspaceHealth{Type: "local", Ready: true}
}

func buildDoctorExtensionHealth(workspace, trust string, projectAssetsAllowed bool, projectConfigPath string) DoctorExtensionHealth {
	health := DoctorExtensionHealth{}
	globalCfg, globalErr := appconfig.LoadGlobalConfig()
	var (
		projectCfg *appconfig.Config
		projectErr error
	)
	if projectAssetsAllowed {
		projectCfg, projectErr = appconfig.LoadConfig(projectConfigPath)
	}
	if globalErr != nil {
		health.Error = globalErr.Error()
		return health
	}
	if projectErr != nil {
		health.Error = projectErr.Error()
		return health
	}

	merged := appconfig.MergeConfigs(globalCfg, projectCfg)
	health.Configured = len(merged.Skills)
	for _, sc := range merged.Skills {
		if sc.IsEnabled() {
			health.Enabled++
		} else {
			health.Disabled++
		}
		if sc.IsMCP() {
			health.MCPServers++
		} else {
			health.PromptSkills++
		}
	}
	if servers, err := ListMCPServers(workspace, trust); err != nil {
		health.Error = err.Error()
		return health
	} else {
		health.MCPServerStatus = servers
	}
	health.DiscoveredSkills = len(skill.DiscoverSkillManifestsForTrust(workspace, trust))
	snapshot, err := appruntime.LoadCapabilitySnapshot(appruntime.CapabilityStatusPath())
	if err == nil {
		health.CapabilityStatus = snapshot.Items
	}
	if surface := appruntime.ProbeExecutionSurface(workspace, WorkspaceIsolationDir(), true); surface != nil {
		health.CapabilityStatus = mergeCapabilityStatuses(health.CapabilityStatus, surface.CapabilityStatuses())
	}
	return health
}

func buildDoctorRepoHealth(ctx context.Context, workspace string) DoctorRepoHealth {
	capture, err := sandbox.NewGitRepoStateCapture(workspace).Capture(ctx)
	if err != nil {
		return DoctorRepoHealth{Available: false, Error: err.Error()}
	}
	return DoctorRepoHealth{
		Available: true,
		Root:      capture.RepoRoot,
		Head:      capture.HeadSHA,
		Branch:    capture.Branch,
		Dirty:     len(capture.Staged) > 0 || len(capture.Unstaged) > 0 || len(capture.Untracked) > 0,
	}
}

func buildDoctorSnapshotHealth(ctx context.Context, workspace string, sessionStore session.SessionStore, summaries []session.SessionSummary) DoctorSnapshotHealth {
	snapshots, err := listSnapshots(ctx, workspace)
	if err != nil {
		return DoctorSnapshotHealth{Available: false, Error: err.Error()}
	}
	indexedSessions := map[string]struct{}{}
	recoverableSet := map[string]struct{}{}
	recoverableMatches := 0
	if len(summaries) == 0 && sessionStore != nil {
		if listed, listErr := sessionStore.List(ctx); listErr == nil {
			summaries = listed
		}
	}
	for _, summary := range summaries {
		if summary.Recoverable {
			recoverableSet[summary.ID] = struct{}{}
		}
	}
	for _, snapshot := range snapshots {
		if snapshot.SessionID == "" {
			continue
		}
		indexedSessions[snapshot.SessionID] = struct{}{}
		if _, ok := recoverableSet[snapshot.SessionID]; ok {
			recoverableMatches++
		}
	}
	return DoctorSnapshotHealth{
		Available:          true,
		Total:              len(snapshots),
		SessionIndexed:     len(indexedSessions),
		RecoverableMatches: recoverableMatches,
	}
}

func mergeCapabilityStatuses(base []appruntime.CapabilityStatus, extra []appruntime.CapabilityStatus) []appruntime.CapabilityStatus {
	if len(extra) == 0 {
		return base
	}
	indexed := make(map[string]appruntime.CapabilityStatus, len(base)+len(extra))
	for _, item := range base {
		indexed[item.Capability] = item
	}
	for _, item := range extra {
		current := indexed[item.Capability]
		if current.Capability == "" || strings.HasPrefix(item.Capability, "execution:") {
			indexed[item.Capability] = item
		}
	}
	out := make([]appruntime.CapabilityStatus, 0, len(indexed))
	for _, item := range indexed {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Capability < out[j].Capability
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func resolveDoctorExecutionPolicy(workspace string, flags *appkit.AppFlags, explicitFlags []string, trust, approvalMode string) appruntime.ExecutionPolicy {
	if flags == nil {
		return appruntime.ResolveExecutionPolicyForWorkspace(workspace, trust, approvalMode)
	}
	trustOverride := ""
	if containsString(explicitFlags, "trust") || envConfigured("MOSSCODE_TRUST", "MOSS_TRUST") {
		trustOverride = trust
	}
	approvalOverride := ""
	if containsString(explicitFlags, "approval") || envConfigured("MOSSCODE_APPROVAL_MODE", "MOSS_APPROVAL_MODE") {
		approvalOverride = approvalMode
	}
	resolved, err := appruntime.ResolveProfileForWorkspace(appruntime.ProfileResolveOptions{
		Workspace:        workspace,
		RequestedProfile: flags.Profile,
		Trust:            trustOverride,
		ApprovalMode:     approvalOverride,
	})
	if err == nil {
		return resolved.ExecutionPolicy
	}
	return appruntime.ResolveExecutionPolicyForWorkspace(workspace, trust, approvalMode)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func envConfigured(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}
