package product

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/appkit"
	appruntime "github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/skill"
)

const disableStateCatalogEnv = "MOSSCODE_DISABLE_STATE_CATALOG"

func SessionStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "sessions")
}

func StateStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "state")
}

func StateEventDir() string {
	return filepath.Join(StateStoreDir(), "events")
}

func StateCatalogEnabled() bool {
	value := strings.TrimSpace(os.Getenv(disableStateCatalogEnv))
	if value == "" {
		return true
	}
	value = strings.ToLower(value)
	return value != "1" && value != "true" && value != "yes" && value != "on"
}

func CheckpointStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "checkpoints")
}

func ChangeStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "changes")
}

func MemoryDir() string {
	return filepath.Join(appconfig.AppDir(), "memories")
}

func TaskRuntimeDir() string {
	return filepath.Join(appconfig.AppDir(), "tasks")
}

func WorkspaceIsolationDir() string {
	return filepath.Join(appconfig.AppDir(), "workspaces")
}

func ListResumeCandidates(ctx context.Context, workspace string) ([]session.SessionSummary, map[string]int, error) {
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		return nil, nil, fmt.Errorf("session store: %w", err)
	}
	summaries, err := store.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list sessions: %w", err)
	}
	counts, err := SnapshotCountsBySession(ctx, workspace)
	if err != nil {
		return nil, nil, err
	}
	return summaries, counts, nil
}

func SelectResumeSummary(summaries []session.SessionSummary, sessionID string, latest bool) (*session.SessionSummary, []session.SessionSummary, error) {
	recoverable := make([]session.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Recoverable {
			recoverable = append(recoverable, summary)
		}
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		for i := range summaries {
			if summaries[i].ID != sessionID {
				continue
			}
			if !summaries[i].Recoverable {
				return nil, recoverable, fmt.Errorf("session %q is not recoverable (status=%s)", sessionID, summaries[i].Status)
			}
			return &summaries[i], recoverable, nil
		}
		return nil, recoverable, fmt.Errorf("session %q not found", sessionID)
	}
	if latest {
		if len(recoverable) == 0 {
			return nil, nil, fmt.Errorf("no recoverable sessions found")
		}
		return &recoverable[0], recoverable, nil
	}
	return nil, recoverable, nil
}

func SnapshotCountsBySession(ctx context.Context, workspace string) (map[string]int, error) {
	snapshots, err := listSnapshots(ctx, workspace)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.SessionID == "" {
			continue
		}
		counts[snapshot.SessionID]++
	}
	return counts, nil
}

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
	Model GovernanceReport `json:"model"`
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
	Configured       int                   `json:"configured"`
	Enabled          int                   `json:"enabled"`
	Disabled         int                   `json:"disabled"`
	MCPServers       int                   `json:"mcp_servers"`
	MCPServerStatus  []MCPServerConfigView `json:"mcp_server_status,omitempty"`
	PromptSkills     int                   `json:"prompt_skills"`
	DiscoveredSkills int                   `json:"discovered_skills"`
	Error            string                `json:"error,omitempty"`
}

func BuildDoctorReport(ctx context.Context, appName, workspace string, flags *appkit.AppFlags, explicitFlags []string, approvalMode string, governanceCfg GovernanceConfig) DoctorReport {
	globalConfigPath := appconfig.DefaultGlobalConfigPath()
	projectConfigPath := appconfig.DefaultProjectConfigPath(workspace)
	trust := appconfig.NormalizeTrustLevel(flags.Trust)
	normalizedApprovalMode := NormalizeApprovalMode(approvalMode)
	executionPolicy := resolveDoctorExecutionPolicy(workspace, flags, explicitFlags, trust, normalizedApprovalMode)
	projectAssetsAllowed := appconfig.ProjectAssetsAllowed(trust)
	commandNetworkEnforcement := "none"
	commandNetworkDegraded := false
	if executionPolicy.Command.Access == appruntime.ExecutionAccessDeny {
		commandNetworkEnforcement = "disabled"
	} else if executionPolicy.Command.Network.Mode == port.ExecNetworkDisabled {
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
		Workspace: workspace,
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
			Model: BuildGovernanceReport(workspace, governanceCfg),
		},
		Paths: DoctorPathsReport{
			AppDir:             checkWritableDir(appconfig.AppDir()),
			StateStoreDir:      checkWritableDir(StateStoreDir()),
			StateEventDir:      checkWritableDir(StateEventDir()),
			SessionStoreDir:    checkWritableDir(SessionStoreDir()),
			MemoryDir:          checkWritableDir(MemoryDir()),
			TaskRuntimeDir:     checkWritableDir(TaskRuntimeDir()),
			WorkspaceIsolation: checkWritableDir(WorkspaceIsolationDir()),
			PricingCatalog:     checkWritableFile(defaultPricingCatalogPath(workspace, governanceCfg.PricingCatalogPath)),
			AuditLog:           checkWritableFile(AuditLogPath()),
			DebugLog:           checkWritableFile(DebugLogPath()),
		},
	}

	report.Health.State = buildDoctorStateHealth()
	sessionStore, summaries := populateDoctorSessionsHealth(ctx, &report)
	report.Health.Tasks = buildDoctorTaskHealth()
	report.Health.Workspace = buildDoctorWorkspaceHealth()
	report.Health.Extensions = buildDoctorExtensionHealth(workspace, trust, projectAssetsAllowed, projectConfigPath)
	report.Health.Repo = buildDoctorRepoHealth(ctx, workspace)
	report.Health.Snapshots = buildDoctorSnapshotHealth(ctx, workspace, sessionStore, summaries)

	return report
}

func buildDoctorStateHealth() DoctorStateCatalogHealth {
	stateCatalog, stateErr := appruntime.NewStateCatalog(StateStoreDir(), StateEventDir(), StateCatalogEnabled())
	if stateErr != nil {
		return DoctorStateCatalogHealth{
			Enabled:   StateCatalogEnabled(),
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
	sessionStore, err := session.NewFileStore(SessionStoreDir())
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
	if _, err := port.NewFileTaskRuntime(TaskRuntimeDir()); err != nil {
		return DoctorTaskHealth{Type: "file", Ready: false, Error: err.Error()}
	}
	return DoctorTaskHealth{Type: "file", Ready: true}
}

func buildDoctorWorkspaceHealth() DoctorWorkspaceHealth {
	if _, err := sandbox.NewLocalWorkspaceIsolation(WorkspaceIsolationDir()); err != nil {
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

type CheckpointSummary struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id,omitempty"`
	SnapshotID   string    `json:"snapshot_id,omitempty"`
	Note         string    `json:"note,omitempty"`
	PatchCount   int       `json:"patch_count"`
	LineageDepth int       `json:"lineage_depth"`
	CreatedAt    time.Time `json:"created_at"`
}

type CheckpointDetail struct {
	ID           string                      `json:"id"`
	SessionID    string                      `json:"session_id,omitempty"`
	SnapshotID   string                      `json:"snapshot_id,omitempty"`
	Note         string                      `json:"note,omitempty"`
	PatchIDs     []string                    `json:"patch_ids,omitempty"`
	PatchCount   int                         `json:"patch_count"`
	Lineage      []port.CheckpointLineageRef `json:"lineage,omitempty"`
	LineageDepth int                         `json:"lineage_depth"`
	Metadata     map[string]any              `json:"metadata,omitempty"`
	MetadataKeys []string                    `json:"metadata_keys,omitempty"`
	CreatedAt    time.Time                   `json:"created_at"`
}

func SummarizeCheckpoint(item port.CheckpointRecord) CheckpointSummary {
	sessionID := checkpointSessionID(item)
	return CheckpointSummary{
		ID:           item.ID,
		SessionID:    sessionID,
		SnapshotID:   item.WorktreeSnapshotID,
		Note:         item.Note,
		PatchCount:   len(item.PatchIDs),
		LineageDepth: len(item.Lineage),
		CreatedAt:    item.CreatedAt,
	}
}

func checkpointSessionID(item port.CheckpointRecord) string {
	sessionID := item.SessionID
	for _, ref := range item.Lineage {
		if ref.Kind == port.CheckpointLineageSession && strings.TrimSpace(ref.ID) != "" {
			sessionID = ref.ID
			break
		}
	}
	return sessionID
}

func DescribeCheckpoint(item port.CheckpointRecord) CheckpointDetail {
	keys := make([]string, 0, len(item.Metadata))
	for key := range item.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return CheckpointDetail{
		ID:           item.ID,
		SessionID:    checkpointSessionID(item),
		SnapshotID:   item.WorktreeSnapshotID,
		Note:         item.Note,
		PatchIDs:     append([]string(nil), item.PatchIDs...),
		PatchCount:   len(item.PatchIDs),
		Lineage:      append([]port.CheckpointLineageRef(nil), item.Lineage...),
		LineageDepth: len(item.Lineage),
		Metadata:     cloneAnyMap(item.Metadata),
		MetadataKeys: keys,
		CreatedAt:    item.CreatedAt,
	}
}

func SummarizeCheckpoints(items []port.CheckpointRecord) []CheckpointSummary {
	out := make([]CheckpointSummary, 0, len(items))
	for _, item := range items {
		out = append(out, SummarizeCheckpoint(item))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func ListCheckpoints(ctx context.Context, limit int) ([]CheckpointSummary, error) {
	store, err := port.NewFileCheckpointStore(CheckpointStoreDir())
	if err != nil {
		return nil, fmt.Errorf("checkpoint store: %w", err)
	}
	items, err := store.List(ctx)
	if err != nil {
		if err == port.ErrCheckpointUnavailable {
			return nil, nil
		}
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	out := SummarizeCheckpoints(items)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func ResolveCheckpointRecord(ctx context.Context, store port.CheckpointStore, selector string) (*port.CheckpointRecord, error) {
	if store == nil {
		return nil, port.ErrCheckpointUnavailable
	}
	selector = strings.TrimSpace(selector)
	if selector == "" || strings.EqualFold(selector, "latest") {
		items, err := store.List(ctx)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, port.ErrCheckpointNotFound
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
		item := items[0]
		return &item, nil
	}
	item, err := store.Load(ctx, selector)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, port.ErrCheckpointNotFound
	}
	return item, nil
}

func LoadCheckpointWithStore(ctx context.Context, store port.CheckpointStore, selector string) (*CheckpointDetail, error) {
	item, err := ResolveCheckpointRecord(ctx, store, selector)
	if err != nil {
		return nil, err
	}
	detail := DescribeCheckpoint(*item)
	return &detail, nil
}

func LoadCheckpoint(ctx context.Context, checkpointID string) (*CheckpointDetail, error) {
	store, err := port.NewFileCheckpointStore(CheckpointStoreDir())
	if err != nil {
		return nil, fmt.Errorf("checkpoint store: %w", err)
	}
	return LoadCheckpointWithStore(ctx, store, checkpointID)
}

func listChangeOperationsByRepoRoot(ctx context.Context, repoRoot string, limit int) ([]ChangeSummary, error) {
	store, err := OpenChangeStore()
	if err != nil {
		return nil, err
	}
	items, err := store.ListByRepoRoot(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return SummarizeChanges(items), nil
}

func RenderCheckpointSummaries(items []CheckpointSummary) string {
	if len(items) == 0 {
		return "Checkpoints: none"
	}
	var b strings.Builder
	b.WriteString("Checkpoints:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- %s | created=%s | session=%s | snapshot=%s | patches=%d | lineage=%d | note=%s\n",
			item.ID,
			item.CreatedAt.UTC().Format(time.RFC3339),
			firstNonEmpty(item.SessionID, "(none)"),
			firstNonEmpty(item.SnapshotID, "(none)"),
			item.PatchCount,
			item.LineageDepth,
			firstNonEmpty(item.Note, "(none)"),
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func RenderCheckpointDetail(item *CheckpointDetail) string {
	if item == nil {
		return "Checkpoint: not found"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Checkpoint: %s\n", item.ID)
	fmt.Fprintf(&b, "  created:  %s\n", item.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "  session:  %s\n", firstNonEmpty(item.SessionID, "(none)"))
	fmt.Fprintf(&b, "  snapshot: %s\n", firstNonEmpty(item.SnapshotID, "(none)"))
	fmt.Fprintf(&b, "  patches:  %d", item.PatchCount)
	if len(item.PatchIDs) > 0 {
		fmt.Fprintf(&b, " (%s)", renderCheckpointPatchOverview(item.PatchIDs, 5))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  lineage:  %d\n", item.LineageDepth)
	fmt.Fprintf(&b, "  note:     %s\n", firstNonEmpty(item.Note, "(none)"))
	if len(item.MetadataKeys) == 0 {
		b.WriteString("  metadata: (none)\n")
	} else {
		fmt.Fprintf(&b, "  metadata: %s\n", strings.Join(item.MetadataKeys, ", "))
	}
	b.WriteString("  lineage refs:\n")
	if len(item.Lineage) == 0 {
		b.WriteString("    - (none)\n")
	} else {
		for _, ref := range item.Lineage {
			fmt.Fprintf(&b, "    - %s %s\n", ref.Kind, firstNonEmpty(strings.TrimSpace(ref.ID), "(none)"))
		}
	}
	b.WriteString("Next:\n")
	fmt.Fprintf(&b, "  - mosscode checkpoint fork --checkpoint %s\n", item.ID)
	fmt.Fprintf(&b, "  - mosscode checkpoint replay --checkpoint %s --mode resume\n", item.ID)
	return strings.TrimRight(b.String(), "\n")
}

func renderCheckpointPatchOverview(ids []string, limit int) string {
	if len(ids) == 0 {
		return ""
	}
	if limit <= 0 || len(ids) <= limit {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s, ... (+%d more)", strings.Join(ids[:limit], ", "), len(ids)-limit)
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func listSnapshots(ctx context.Context, workspace string) ([]port.WorktreeSnapshot, error) {
	store := sandbox.NewGitWorktreeSnapshotStore(workspace)
	snapshots, err := store.List(ctx)
	if err != nil {
		if err == port.ErrWorktreeSnapshotUnavailable {
			return nil, nil
		}
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return snapshots, nil
}

func defaultPricingCatalogPath(workspace, explicit string) string {
	path := ResolvePricingCatalogPath(workspace, explicit)
	if path != "" {
		return path
	}
	candidates := pricingCatalogCandidates(workspace)
	if len(candidates) == 0 {
		return filepath.Join(appconfig.AppDir(), "pricing.yaml")
	}
	return candidates[0]
}

func detectedEnvVars() []string {
	keys := []string{
		"MOSSCODE_API_TYPE", "MOSSCODE_PROVIDER", "MOSSCODE_NAME", "MOSSCODE_MODEL", "MOSSCODE_WORKSPACE", "MOSSCODE_TRUST", "MOSSCODE_API_KEY", "MOSSCODE_BASE_URL",
		"MOSS_API_TYPE", "MOSS_PROVIDER", "MOSS_NAME", "MOSS_MODEL", "MOSS_WORKSPACE", "MOSS_TRUST", "MOSS_API_KEY", "MOSS_BASE_URL",
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := os.LookupEnv(key); ok {
			out = append(out, key)
		}
	}
	return out
}

func checkWritableDir(path string) PathStatus {
	status := PathStatus{Path: path}
	if strings.TrimSpace(path) == "" {
		status.Error = "path is empty"
		return status
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	f, err := os.CreateTemp(path, "doctor-*")
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Writable = true
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return status
}

func checkWritableFile(path string) PathStatus {
	status := PathStatus{Path: path}
	if strings.TrimSpace(path) == "" {
		status.Error = "path is empty"
		return status
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = pathExists(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	status.Writable = true
	_ = f.Close()
	return status
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func MarshalJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
