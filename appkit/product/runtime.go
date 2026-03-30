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
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/skill"
)

func SessionStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "sessions")
}

func CheckpointStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "checkpoints")
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
	App        string                 `json:"app"`
	Timestamp  string                 `json:"timestamp"`
	Workspace  string                 `json:"workspace"`
	Config     DoctorConfigReport     `json:"config"`
	Governance DoctorGovernanceReport `json:"governance"`
	Paths      DoctorPathsReport      `json:"paths"`
	Health     DoctorHealthReport     `json:"health"`
}

type DoctorConfigReport struct {
	ExplicitFlags []string `json:"explicit_flags,omitempty"`
	DetectedEnv   []string `json:"detected_env,omitempty"`
	GlobalConfig  string   `json:"global_config"`
	GlobalExists  bool     `json:"global_exists"`
	ProjectConfig string   `json:"project_config"`
	ProjectExists bool     `json:"project_exists"`
	APIType       string   `json:"api_type"`
	Provider      string   `json:"provider"`
	Name          string   `json:"name"`
	Model         string   `json:"model,omitempty"`
	BaseURLSet    bool     `json:"base_url_set"`
	APIKeySet     bool     `json:"api_key_set"`
	Trust         string   `json:"trust"`
	ApprovalMode  string   `json:"approval_mode"`
}

type DoctorPathsReport struct {
	AppDir             PathStatus `json:"app_dir"`
	SessionStoreDir    PathStatus `json:"session_store_dir"`
	MemoryDir          PathStatus `json:"memory_dir"`
	TaskRuntimeDir     PathStatus `json:"task_runtime_dir"`
	WorkspaceIsolation PathStatus `json:"workspace_isolation_dir"`
	AuditLog           PathStatus `json:"audit_log"`
	DebugLog           PathStatus `json:"debug_log"`
}

type DoctorGovernanceReport struct {
	Model GovernanceReport `json:"model"`
}

type DoctorHealthReport struct {
	Sessions   DoctorSessionHealth   `json:"sessions"`
	Tasks      DoctorTaskHealth      `json:"tasks"`
	Workspace  DoctorWorkspaceHealth `json:"workspace"`
	Repo       DoctorRepoHealth      `json:"repo"`
	Snapshots  DoctorSnapshotHealth  `json:"snapshots"`
	Extensions DoctorExtensionHealth `json:"extensions"`
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
	Configured       int    `json:"configured"`
	Enabled          int    `json:"enabled"`
	Disabled         int    `json:"disabled"`
	MCPServers       int    `json:"mcp_servers"`
	PromptSkills     int    `json:"prompt_skills"`
	DiscoveredSkills int    `json:"discovered_skills"`
	Error            string `json:"error,omitempty"`
}

func BuildDoctorReport(ctx context.Context, appName, workspace string, flags *appkit.AppFlags, explicitFlags []string, approvalMode string, governanceCfg GovernanceConfig) DoctorReport {
	globalConfigPath := appconfig.DefaultGlobalConfigPath()
	projectConfigPath := appconfig.DefaultProjectConfigPath(workspace)
	report := DoctorReport{
		App:       appName,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Workspace: workspace,
		Config: DoctorConfigReport{
			ExplicitFlags: explicitFlags,
			DetectedEnv:   detectedEnvVars(),
			GlobalConfig:  globalConfigPath,
			GlobalExists:  pathExists(globalConfigPath),
			ProjectConfig: projectConfigPath,
			ProjectExists: pathExists(projectConfigPath),
			APIType:       flags.EffectiveAPIType(),
			Provider:      flags.Provider,
			Name:          flags.DisplayProviderName(),
			Model:         flags.Model,
			BaseURLSet:    strings.TrimSpace(flags.BaseURL) != "",
			APIKeySet:     strings.TrimSpace(flags.APIKey) != "",
			Trust:         flags.Trust,
			ApprovalMode:  NormalizeApprovalMode(approvalMode),
		},
		Governance: DoctorGovernanceReport{
			Model: BuildGovernanceReport(workspace, governanceCfg),
		},
		Paths: DoctorPathsReport{
			AppDir:             checkWritableDir(appconfig.AppDir()),
			SessionStoreDir:    checkWritableDir(SessionStoreDir()),
			MemoryDir:          checkWritableDir(MemoryDir()),
			TaskRuntimeDir:     checkWritableDir(TaskRuntimeDir()),
			WorkspaceIsolation: checkWritableDir(WorkspaceIsolationDir()),
			AuditLog:           checkWritableFile(AuditLogPath()),
			DebugLog:           checkWritableFile(DebugLogPath()),
		},
	}

	sessionStore, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		report.Health.Sessions.Error = err.Error()
	} else if summaries, err := sessionStore.List(ctx); err != nil {
		report.Health.Sessions.Error = err.Error()
	} else {
		report.Health.Sessions.Total = len(summaries)
		for _, summary := range summaries {
			if summary.Recoverable {
				report.Health.Sessions.Recoverable++
			}
		}
	}

	if _, err := port.NewFileTaskRuntime(TaskRuntimeDir()); err != nil {
		report.Health.Tasks = DoctorTaskHealth{Type: "file", Ready: false, Error: err.Error()}
	} else {
		report.Health.Tasks = DoctorTaskHealth{Type: "file", Ready: true}
	}

	if _, err := sandbox.NewLocalWorkspaceIsolation(WorkspaceIsolationDir()); err != nil {
		report.Health.Workspace = DoctorWorkspaceHealth{Type: "local", Ready: false, Error: err.Error()}
	} else {
		report.Health.Workspace = DoctorWorkspaceHealth{Type: "local", Ready: true}
	}

	globalCfg, globalErr := appconfig.LoadGlobalConfig()
	projectCfg, projectErr := appconfig.LoadConfig(projectConfigPath)
	if globalErr != nil {
		report.Health.Extensions.Error = globalErr.Error()
	} else if projectErr != nil {
		report.Health.Extensions.Error = projectErr.Error()
	} else {
		merged := appconfig.MergeConfigs(globalCfg, projectCfg)
		report.Health.Extensions.Configured = len(merged.Skills)
		for _, sc := range merged.Skills {
			if sc.IsEnabled() {
				report.Health.Extensions.Enabled++
			} else {
				report.Health.Extensions.Disabled++
			}
			if sc.IsMCP() {
				report.Health.Extensions.MCPServers++
			} else {
				report.Health.Extensions.PromptSkills++
			}
		}
		report.Health.Extensions.DiscoveredSkills = len(skill.DiscoverSkillManifests(workspace))
	}

	capture, err := sandbox.NewGitRepoStateCapture(workspace).Capture(ctx)
	if err != nil {
		report.Health.Repo = DoctorRepoHealth{Available: false, Error: err.Error()}
	} else {
		report.Health.Repo = DoctorRepoHealth{
			Available: true,
			Root:      capture.RepoRoot,
			Head:      capture.HeadSHA,
			Branch:    capture.Branch,
			Dirty:     len(capture.Staged) > 0 || len(capture.Unstaged) > 0 || len(capture.Untracked) > 0,
		}
	}

	snapshots, err := listSnapshots(ctx, workspace)
	if err != nil {
		report.Health.Snapshots = DoctorSnapshotHealth{Available: false, Error: err.Error()}
	} else {
		indexedSessions := map[string]struct{}{}
		recoverableSet := map[string]struct{}{}
		recoverableMatches := 0
		if sessionStore != nil {
			if summaries, err := sessionStore.List(ctx); err == nil {
				for _, summary := range summaries {
					if summary.Recoverable {
						recoverableSet[summary.ID] = struct{}{}
					}
				}
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
		report.Health.Snapshots = DoctorSnapshotHealth{
			Available:          true,
			Total:              len(snapshots),
			SessionIndexed:     len(indexedSessions),
			RecoverableMatches: recoverableMatches,
		}
	}

	return report
}

func RenderDoctorReport(report DoctorReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mosscode doctor\n")
	fmt.Fprintf(&b, "Workspace: %s\n", report.Workspace)
	fmt.Fprintf(&b, "Provider: %s | model=%s | trust=%s | approval=%s\n",
		report.Config.Name, firstNonEmpty(report.Config.Model, "(default)"), report.Config.Trust, report.Config.ApprovalMode)
	fmt.Fprintf(&b, "Config sources: flags=%s env=%s global=%t project=%t\n",
		renderList(report.Config.ExplicitFlags), renderList(report.Config.DetectedEnv), report.Config.GlobalExists, report.Config.ProjectExists)
	fmt.Fprintf(&b, "Model governance: retry=%t retries=%d initial=%s max=%s breaker=%t failures=%d reset=%s router=%s",
		report.Governance.Model.RetryEnabled,
		report.Governance.Model.RetryMaxRetries,
		firstNonEmpty(report.Governance.Model.RetryInitialDelay, "-"),
		firstNonEmpty(report.Governance.Model.RetryMaxDelay, "-"),
		report.Governance.Model.BreakerEnabled,
		report.Governance.Model.BreakerMaxFailures,
		firstNonEmpty(report.Governance.Model.BreakerResetAfter, "-"),
		firstNonEmpty(report.Governance.Model.RouterConfig, "(disabled)"))
	if report.Governance.Model.RouterEnabled {
		fmt.Fprintf(&b, " default=%s models=%d",
			firstNonEmpty(report.Governance.Model.RouterDefaultModel, "(unspecified)"),
			report.Governance.Model.RouterModels)
	}
	if report.Governance.Model.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Governance.Model.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Session store: %s\n", renderPathStatus(report.Paths.SessionStoreDir))
	fmt.Fprintf(&b, "Memory dir: %s\n", renderPathStatus(report.Paths.MemoryDir))
	fmt.Fprintf(&b, "Audit log: %s\n", renderPathStatus(report.Paths.AuditLog))
	fmt.Fprintf(&b, "Debug log: %s\n", renderPathStatus(report.Paths.DebugLog))
	fmt.Fprintf(&b, "Task runtime: type=%s ready=%t", report.Health.Tasks.Type, report.Health.Tasks.Ready)
	if report.Health.Tasks.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Tasks.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Workspace isolation: type=%s ready=%t", report.Health.Workspace.Type, report.Health.Workspace.Ready)
	if report.Health.Workspace.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Workspace.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Sessions: recoverable=%d total=%d", report.Health.Sessions.Recoverable, report.Health.Sessions.Total)
	if report.Health.Sessions.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Sessions.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Extensions: configured=%d enabled=%d disabled=%d mcp=%d prompt=%d discovered=%d",
		report.Health.Extensions.Configured, report.Health.Extensions.Enabled, report.Health.Extensions.Disabled,
		report.Health.Extensions.MCPServers, report.Health.Extensions.PromptSkills, report.Health.Extensions.DiscoveredSkills)
	if report.Health.Extensions.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Extensions.Error)
	}
	b.WriteString("\n")
	if report.Health.Repo.Available {
		fmt.Fprintf(&b, "Repo: available=true root=%s branch=%s dirty=%t\n", report.Health.Repo.Root, firstNonEmpty(report.Health.Repo.Branch, "(detached)"), report.Health.Repo.Dirty)
	} else {
		fmt.Fprintf(&b, "Repo: available=false err=%s\n", report.Health.Repo.Error)
	}
	fmt.Fprintf(&b, "Snapshots: available=%t total=%d indexed_sessions=%d recoverable_matches=%d",
		report.Health.Snapshots.Available, report.Health.Snapshots.Total, report.Health.Snapshots.SessionIndexed, report.Health.Snapshots.RecoverableMatches)
	if report.Health.Snapshots.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Snapshots.Error)
	}
	b.WriteString("\n")
	return b.String()
}

type ReviewReport struct {
	Mode      string                  `json:"mode"`
	Workspace string                  `json:"workspace"`
	Repo      ReviewRepoState         `json:"repo"`
	Snapshots []ReviewSnapshotSummary `json:"snapshots,omitempty"`
	Snapshot  *ReviewSnapshotSummary  `json:"snapshot,omitempty"`
}

type ReviewRepoState struct {
	Available bool                 `json:"available"`
	Root      string               `json:"root,omitempty"`
	Head      string               `json:"head,omitempty"`
	Branch    string               `json:"branch,omitempty"`
	Dirty     bool                 `json:"dirty"`
	Staged    []port.RepoFileState `json:"staged,omitempty"`
	Unstaged  []port.RepoFileState `json:"unstaged,omitempty"`
	Untracked []string             `json:"untracked,omitempty"`
	Ignored   []string             `json:"ignored,omitempty"`
	Error     string               `json:"error,omitempty"`
}

type ReviewSnapshotSummary struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id,omitempty"`
	Mode       string    `json:"mode"`
	Head       string    `json:"head,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Note       string    `json:"note,omitempty"`
	PatchCount int       `json:"patch_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func BuildReviewReport(ctx context.Context, workspace string, args []string) (ReviewReport, error) {
	mode := "status"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}
	report := ReviewReport{
		Mode:      mode,
		Workspace: workspace,
	}
	capture, err := sandbox.NewGitRepoStateCapture(workspace).Capture(ctx)
	if err != nil {
		report.Repo = ReviewRepoState{Available: false, Error: err.Error()}
	} else {
		report.Repo = ReviewRepoState{
			Available: true,
			Root:      capture.RepoRoot,
			Head:      capture.HeadSHA,
			Branch:    capture.Branch,
			Dirty:     capture.IsDirty,
			Staged:    capture.Staged,
			Unstaged:  capture.Unstaged,
			Untracked: append([]string(nil), capture.Untracked...),
			Ignored:   append([]string(nil), capture.Ignored...),
		}
	}
	snapshots, err := listSnapshots(ctx, workspace)
	if err != nil && report.Repo.Error == "" {
		report.Repo.Error = err.Error()
	}

	switch mode {
	case "status":
		return report, nil
	case "snapshots":
		report.Snapshots = summarizeSnapshots(snapshots)
		return report, nil
	case "snapshot":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return ReviewReport{}, fmt.Errorf("usage: mosscode review snapshot <id>")
		}
		for _, snapshot := range snapshots {
			if snapshot.ID == args[1] {
				summary := SummarizeSnapshot(snapshot)
				report.Snapshot = &summary
				return report, nil
			}
		}
		return ReviewReport{}, fmt.Errorf("snapshot %q not found", args[1])
	default:
		return ReviewReport{}, fmt.Errorf("unknown review mode %q (supported: status, snapshots, snapshot)", mode)
	}
}

func RenderReviewReport(report ReviewReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mosscode review (%s)\n", report.Mode)
	if report.Repo.Available {
		fmt.Fprintf(&b, "Repo: %s @ %s", report.Repo.Root, firstNonEmpty(report.Repo.Branch, "(detached)"))
		if report.Repo.Dirty {
			b.WriteString(" dirty=true")
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "Repo: unavailable err=%s\n", report.Repo.Error)
	}
	switch report.Mode {
	case "status":
		renderRepoFiles(&b, "Staged", report.Repo.Staged)
		renderRepoFiles(&b, "Unstaged", report.Repo.Unstaged)
		renderStringFiles(&b, "Untracked", report.Repo.Untracked)
		renderStringFiles(&b, "Ignored", report.Repo.Ignored)
	case "snapshots":
		if len(report.Snapshots) == 0 {
			b.WriteString("Snapshots: none\n")
			return b.String()
		}
		b.WriteString("Snapshots:\n")
		for _, snapshot := range report.Snapshots {
			fmt.Fprintf(&b, "- %s | created=%s | head=%s | patches=%d | session=%s | note=%s\n",
				snapshot.ID, snapshot.CreatedAt.UTC().Format(time.RFC3339), firstNonEmpty(snapshot.Head, "(none)"), snapshot.PatchCount, firstNonEmpty(snapshot.SessionID, "(none)"), firstNonEmpty(snapshot.Note, "(none)"))
		}
	case "snapshot":
		if report.Snapshot == nil {
			b.WriteString("Snapshot: not found\n")
			return b.String()
		}
		snapshot := report.Snapshot
		fmt.Fprintf(&b, "Snapshot: %s\n", snapshot.ID)
		fmt.Fprintf(&b, "  session: %s\n", firstNonEmpty(snapshot.SessionID, "(none)"))
		fmt.Fprintf(&b, "  mode:    %s\n", snapshot.Mode)
		fmt.Fprintf(&b, "  branch:  %s\n", firstNonEmpty(snapshot.Branch, "(detached)"))
		fmt.Fprintf(&b, "  head:    %s\n", firstNonEmpty(snapshot.Head, "(none)"))
		fmt.Fprintf(&b, "  patches: %d\n", snapshot.PatchCount)
		fmt.Fprintf(&b, "  note:    %s\n", firstNonEmpty(snapshot.Note, "(none)"))
	}
	return b.String()
}

func SummarizeSnapshot(item port.WorktreeSnapshot) ReviewSnapshotSummary {
	return ReviewSnapshotSummary{
		ID:         item.ID,
		SessionID:  item.SessionID,
		Mode:       string(item.Mode),
		Head:       item.Capture.HeadSHA,
		Branch:     item.Capture.Branch,
		Note:       item.Note,
		PatchCount: len(item.Patches),
		CreatedAt:  item.CreatedAt,
	}
}

func summarizeSnapshots(items []port.WorktreeSnapshot) []ReviewSnapshotSummary {
	out := make([]ReviewSnapshotSummary, 0, len(items))
	for _, item := range items {
		out = append(out, SummarizeSnapshot(item))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
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

func renderPathStatus(status PathStatus) string {
	if status.Error != "" {
		return fmt.Sprintf("%s (exists=%t writable=%t err=%s)", status.Path, status.Exists, status.Writable, status.Error)
	}
	return fmt.Sprintf("%s (exists=%t writable=%t)", status.Path, status.Exists, status.Writable)
}

func renderList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ",")
}

func renderRepoFiles(b *strings.Builder, label string, items []port.RepoFileState) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(b, "- %s (%s)\n", item.Path, item.Status)
	}
}

func renderStringFiles(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
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
