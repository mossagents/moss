package product

import (
	"context"
	"fmt"
	"github.com/mossagents/moss/agent"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/internal/strutil"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	appruntime "github.com/mossagents/moss/runtime"
	"github.com/mossagents/moss/skill"
	"github.com/mossagents/moss/userio/prompting"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type InspectThreadSummary struct {
	ID              string `json:"id"`
	Goal            string `json:"goal,omitempty"`
	Status          string `json:"status,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Profile         string `json:"profile,omitempty"`
	TaskMode        string `json:"task_mode,omitempty"`
	Source          string `json:"source,omitempty"`
	ParentID        string `json:"parent_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	Preview         string `json:"preview,omitempty"`
	ActivityKind    string `json:"activity_kind,omitempty"`
	Recoverable     bool   `json:"recoverable"`
	Archived        bool   `json:"archived"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	EndedAt         string `json:"ended_at,omitempty"`
	CheckpointCount int    `json:"checkpoint_count,omitempty"`
	ChangeCount     int    `json:"change_count,omitempty"`
	TaskCount       int    `json:"task_count,omitempty"`
}

type InspectThreadReport struct {
	Summary     InspectThreadSummary   `json:"summary"`
	Children    []InspectThreadSummary `json:"children,omitempty"`
	Checkpoints []CheckpointSummary    `json:"checkpoints,omitempty"`
	Changes     []InspectStateItem     `json:"changes,omitempty"`
	Tasks       []InspectStateItem     `json:"tasks,omitempty"`
}

type InspectPromptReport struct {
	SessionID            string            `json:"session_id"`
	BaseSource           string            `json:"base_source,omitempty"`
	InstructionProfile   string            `json:"instruction_profile,omitempty"`
	ProfileName          string            `json:"profile_name,omitempty"`
	TaskMode             string            `json:"task_mode,omitempty"`
	SessionInstructions  bool              `json:"session_instructions"`
	SystemPromptChars    int               `json:"system_prompt_chars,omitempty"`
	EnabledTokenEstimate int               `json:"enabled_token_estimate,omitempty"`
	DynamicSections      []string          `json:"dynamic_sections,omitempty"`
	EnabledLayers        []string          `json:"enabled_layers,omitempty"`
	SuppressedLayers     []string          `json:"suppressed_layers,omitempty"`
	SuppressionReasons   map[string]string `json:"suppression_reasons,omitempty"`
	LayerTokenEstimates  map[string]int    `json:"layer_token_estimates,omitempty"`
	SourceChain          []string          `json:"source_chain,omitempty"`
}

type InspectCapabilityReport struct {
	UpdatedAt time.Time               `json:"updated_at,omitempty"`
	Items     []InspectCapabilityItem `json:"items,omitempty"`
}

type InspectCapabilityItem struct {
	Capability string    `json:"capability"`
	Kind       string    `json:"kind,omitempty"`
	Name       string    `json:"name,omitempty"`
	State      string    `json:"state,omitempty"`
	Critical   bool      `json:"critical,omitempty"`
	Source     string    `json:"source,omitempty"`
	Details    string    `json:"details,omitempty"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

func buildInspectThreads(ctx context.Context, workspace string, catalog *appruntime.StateCatalog, changeStore *FileChangeStore, limit int) ([]InspectThreadSummary, error) {
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		return nil, err
	}
	summaries, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	checkpointCounts := checkpointCountsBySession(ctx)
	changeCounts := changeCountsBySession(ctx, changeStore)
	out := make([]InspectThreadSummary, 0, len(summaries))
	for _, summary := range summaries {
		item := inspectThreadSummaryFromSession(summary)
		item.CheckpointCount = checkpointCounts[summary.ID]
		item.ChangeCount = changeCounts[summary.ID]
		item.TaskCount = countStateEntries(catalog, appruntime.StateKindTask, summary.ID)
		out = append(out, item)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func buildInspectThread(ctx context.Context, workspace string, catalog *appruntime.StateCatalog, changeStore *FileChangeStore, target string) (*InspectThreadReport, error) {
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		return nil, err
	}
	summaries, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := resolveInspectSessionSummary(summaries, target)
	if err != nil {
		return nil, err
	}
	report := &InspectThreadReport{Summary: inspectThreadSummaryFromSession(*selected)}
	checkpointCounts := checkpointCountsBySession(ctx)
	changeCounts := changeCountsBySession(ctx, changeStore)
	report.Summary.CheckpointCount = checkpointCounts[selected.ID]
	report.Summary.ChangeCount = changeCounts[selected.ID]
	report.Summary.TaskCount = countStateEntries(catalog, appruntime.StateKindTask, selected.ID)
	for _, summary := range summaries {
		if summary.ParentID != selected.ID {
			continue
		}
		child := inspectThreadSummaryFromSession(summary)
		child.CheckpointCount = checkpointCounts[summary.ID]
		child.ChangeCount = changeCounts[summary.ID]
		child.TaskCount = countStateEntries(catalog, appruntime.StateKindTask, summary.ID)
		report.Children = append(report.Children, child)
	}
	sort.Slice(report.Children, func(i, j int) bool { return report.Children[i].UpdatedAt > report.Children[j].UpdatedAt })
	cpStore, err := checkpoint.NewFileCheckpointStore(CheckpointStoreDir())
	if err == nil {
		if items, err := cpStore.FindBySession(ctx, selected.ID); err == nil {
			report.Checkpoints = SummarizeCheckpoints(items)
			if len(report.Checkpoints) > 5 {
				report.Checkpoints = report.Checkpoints[:5]
			}
		}
	}
	if changeStore != nil {
		report.Changes = inspectChangesForSession(ctx, changeStore, selected.ID, 10)
	}
	if catalog != nil {
		if page, err := catalog.Query(appruntime.StateQuery{Kinds: []appruntime.StateKind{appruntime.StateKindTask}, SessionID: selected.ID, Limit: 10}); err == nil {
			report.Tasks = inspectStateItems(page.Items)
		}
	}
	return report, nil
}

func buildInspectPrompt(ctx context.Context, target string) (*InspectPromptReport, error) {
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		return nil, err
	}
	summaries, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := resolveInspectSessionSummary(summaries, target)
	if err != nil {
		return nil, err
	}
	sess, err := store.Load(ctx, selected.ID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("session %q not found", selected.ID)
	}
	profileName, taskMode, err := prompting.ProfileModeFromMetadata(sess.Config.Metadata)
	if err != nil {
		return nil, err
	}
	sessionInstructions, err := prompting.SessionInstructionsFromMetadata(sess.Config.Metadata)
	if err != nil {
		return nil, err
	}
	debug, err := prompting.ComposeDebugMetaFromMetadata(sess.Config.Metadata)
	if err != nil {
		return nil, err
	}
	report := &InspectPromptReport{
		SessionID:           sess.ID,
		BaseSource:          debug.BaseSource,
		InstructionProfile:  debug.InstructionProfile,
		ProfileName:         profileName,
		TaskMode:            taskMode,
		SessionInstructions: strings.TrimSpace(sessionInstructions) != "",
		SystemPromptChars:   len(strings.TrimSpace(sess.Config.SystemPrompt)),
		DynamicSections:     append([]string(nil), debug.DynamicSectionID...),
		EnabledLayers:       append([]string(nil), debug.EnabledLayers...),
		SuppressedLayers:    append([]string(nil), debug.SuppressedLayers...),
		SuppressionReasons:  cloneStringMap(debug.SuppressionReasons),
		LayerTokenEstimates: cloneIntMap(debug.LayerTokenEstimates),
		SourceChain:         append([]string(nil), debug.SourceChain...),
	}
	for _, id := range report.EnabledLayers {
		report.EnabledTokenEstimate += report.LayerTokenEstimates[id]
	}
	return report, nil
}

func buildInspectCapabilities(workspace, trust string) (*InspectCapabilityReport, error) {
	report := &InspectCapabilityReport{}
	indexed := map[string]InspectCapabilityItem{}
	if snapshot, err := appruntime.LoadCapabilitySnapshot(appruntime.CapabilityStatusPath()); err == nil {
		report.UpdatedAt = snapshot.UpdatedAt
		for _, item := range snapshot.Items {
			indexed[item.Capability] = InspectCapabilityItem{
				Capability: item.Capability,
				Kind:       item.Kind,
				Name:       item.Name,
				State:      item.State,
				Critical:   item.Critical,
				Error:      item.Error,
				UpdatedAt:  item.UpdatedAt,
			}
		}
	}
	if surface := appruntime.ProbeExecutionSurface(workspace, WorkspaceIsolationDir(), true); surface != nil {
		for _, status := range surface.CapabilityStatuses() {
			item := indexed[status.Capability]
			item.Capability = status.Capability
			item.Kind = status.Kind
			item.Name = status.Name
			item.State = status.State
			item.Critical = status.Critical
			if item.Error == "" {
				item.Error = status.Error
			}
			switch status.Capability {
			case appruntime.CapabilityExecutionIsolation:
				item.Source = surface.IsolationRoot
			default:
				item.Source = surface.WorkspaceRoot
			}
			if item.Details == "" {
				switch status.Capability {
				case appruntime.CapabilityExecutionWorkspace, appruntime.CapabilityExecutionExecutor:
					item.Details = "local sandbox"
				case appruntime.CapabilityExecutionRepoState:
					item.Details = "git repo capture"
				case appruntime.CapabilityExecutionPatchApply:
					item.Details = "git apply"
				case appruntime.CapabilityExecutionPatchRevert:
					item.Details = "git revert/reset"
				case appruntime.CapabilityExecutionWorktreeStates:
					item.Details = "ghost-state snapshots"
				case appruntime.CapabilityExecutionIsolation:
					item.Details = "task-scoped workspace leases"
				}
			}
			indexed[status.Capability] = item
		}
	}
	if _, ok := indexed["builtin-tools"]; !ok {
		indexed["builtin-tools"] = InspectCapabilityItem{Capability: "builtin-tools", Kind: "builtin", Name: "builtin-tools", State: "unknown", Critical: true}
	}
	if servers, err := ListMCPServers(workspace, trust); err == nil {
		for _, server := range servers {
			key := "mcp:" + server.Name
			item := indexed[key]
			item.Capability = key
			item.Kind = "mcp"
			item.Name = server.Name
			item.Source = string(server.Source)
			item.Details = strutil.FirstNonEmpty(server.Target, server.Transport)
			if item.State == "" {
				item.State = server.Status
			}
			indexed[key] = item
		}
	}
	for _, mf := range skill.DiscoverSkillManifestsForTrust(workspace, trust) {
		key := "skill:" + mf.Name
		item := indexed[key]
		item.Capability = key
		item.Kind = "skill"
		item.Name = mf.Name
		item.Source = mf.Source
		if item.State == "" {
			item.State = "discoverable"
		}
		indexed[key] = item
	}
	for _, dir := range collectInspectableAgentDirs(workspace, trust) {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		configs, err := agent.LoadConfigsFromDir(dir)
		key := "agents:" + dir
		item := indexed[key]
		item.Capability = key
		item.Kind = "agents"
		item.Name = filepath.Base(dir)
		item.Source = dir
		if err != nil {
			item.State = "degraded"
			item.Error = err.Error()
			indexed[key] = item
			continue
		}
		if item.State == "" {
			item.State = "ready"
		}
		indexed[key] = item
		for _, cfg := range configs {
			subKey := "subagent:" + cfg.Name
			sub := indexed[subKey]
			sub.Capability = subKey
			sub.Kind = "subagent"
			sub.Name = cfg.Name
			sub.Source = dir
			sub.Details = cfg.TrustLevel
			if sub.State == "" {
				sub.State = "ready"
			}
			indexed[subKey] = sub
		}
	}
	report.Items = make([]InspectCapabilityItem, 0, len(indexed))
	for _, item := range indexed {
		report.Items = append(report.Items, item)
	}
	sort.Slice(report.Items, func(i, j int) bool {
		if report.Items[i].Kind == report.Items[j].Kind {
			return report.Items[i].Capability < report.Items[j].Capability
		}
		return report.Items[i].Kind < report.Items[j].Kind
	})
	return report, nil
}

func renderInspectThreadReport(b *strings.Builder, report InspectThreadReport) {
	fmt.Fprintf(b, "Thread:      %s\n", report.Summary.ID)
	fmt.Fprintf(b, "Status:      %s recoverable=%t archived=%t\n", report.Summary.Status, report.Summary.Recoverable, report.Summary.Archived)
	fmt.Fprintf(b, "Lineage:     source=%s parent=%s task=%s activity=%s\n",
		strutil.FirstNonEmpty(report.Summary.Source, "(none)"),
		strutil.FirstNonEmpty(report.Summary.ParentID, "(none)"),
		strutil.FirstNonEmpty(report.Summary.TaskID, "(none)"),
		strutil.FirstNonEmpty(report.Summary.ActivityKind, "(none)"),
	)
	fmt.Fprintf(b, "Profile:     profile=%s task_mode=%s\n", strutil.FirstNonEmpty(report.Summary.Profile, "(none)"), strutil.FirstNonEmpty(report.Summary.TaskMode, "(none)"))
	fmt.Fprintf(b, "Checkpoints: %d | Changes: %d | Tasks: %d\n", report.Summary.CheckpointCount, report.Summary.ChangeCount, report.Summary.TaskCount)
	fmt.Fprintf(b, "Preview:     %s\n", strutil.FirstNonEmpty(report.Summary.Preview, "(none)"))
	if len(report.Children) > 0 {
		b.WriteString("Children:\n")
		for _, child := range report.Children {
			fmt.Fprintf(b, "- %s | status=%s | source=%s | task=%s | updated=%s\n",
				child.ID, child.Status, strutil.FirstNonEmpty(child.Source, "(none)"), strutil.FirstNonEmpty(child.TaskID, "(none)"), strutil.FirstNonEmpty(child.UpdatedAt, "(none)"))
		}
	}
	if len(report.Checkpoints) > 0 {
		b.WriteString("Recent checkpoints:\n")
		for _, item := range report.Checkpoints {
			fmt.Fprintf(b, "- %s | snapshot=%s | patches=%d | note=%s\n", item.ID, strutil.FirstNonEmpty(item.SnapshotID, "(none)"), item.PatchCount, strutil.FirstNonEmpty(item.Note, "(none)"))
		}
	}
	if len(report.Changes) > 0 {
		b.WriteString("Recent changes:\n")
		for _, item := range report.Changes {
			fmt.Fprintf(b, "- %s | status=%s | title=%s\n", item.RecordID, strutil.FirstNonEmpty(item.Status, "(none)"), strutil.FirstNonEmpty(item.Title, "(none)"))
		}
	}
	if len(report.Tasks) > 0 {
		b.WriteString("Related tasks:\n")
		for _, item := range report.Tasks {
			fmt.Fprintf(b, "- %s | status=%s | title=%s\n", item.RecordID, strutil.FirstNonEmpty(item.Status, "(none)"), strutil.FirstNonEmpty(item.Title, "(none)"))
		}
	}
}

func renderInspectPromptReport(b *strings.Builder, report InspectPromptReport) {
	fmt.Fprintf(b, "Prompt session: %s\n", report.SessionID)
	fmt.Fprintf(b, "Profile:        profile=%s task_mode=%s instruction_profile=%s\n",
		strutil.FirstNonEmpty(report.ProfileName, "(none)"),
		strutil.FirstNonEmpty(report.TaskMode, "(none)"),
		strutil.FirstNonEmpty(report.InstructionProfile, "(none)"),
	)
	fmt.Fprintf(b, "Base source:    %s | session_instructions=%t | system_prompt_chars=%d | enabled_tokens=%d\n",
		strutil.FirstNonEmpty(report.BaseSource, "(none)"),
		report.SessionInstructions,
		report.SystemPromptChars,
		report.EnabledTokenEstimate,
	)
	fmt.Fprintf(b, "Enabled layers: %s\n", strutil.FirstNonEmpty(strings.Join(report.EnabledLayers, ","), "(none)"))
	fmt.Fprintf(b, "Suppressed:     %s\n", strutil.FirstNonEmpty(strings.Join(report.SuppressedLayers, ","), "(none)"))
	if len(report.SuppressionReasons) > 0 {
		keys := make([]string, 0, len(report.SuppressionReasons))
		for key := range report.SuppressionReasons {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteString("Suppression reasons:\n")
		for _, key := range keys {
			fmt.Fprintf(b, "- %s=%s\n", key, report.SuppressionReasons[key])
		}
	}
	fmt.Fprintf(b, "Dynamic sections: %s\n", strutil.FirstNonEmpty(strings.Join(report.DynamicSections, ","), "(none)"))
	fmt.Fprintf(b, "Source chain:    %s\n", strutil.FirstNonEmpty(strings.Join(report.SourceChain, " -> "), "(none)"))
}

func renderInspectCapabilityReport(b *strings.Builder, report InspectCapabilityReport) {
	if !report.UpdatedAt.IsZero() {
		fmt.Fprintf(b, "Capability snapshot: %s\n", report.UpdatedAt.UTC().Format(time.RFC3339))
	}
	if len(report.Items) == 0 {
		b.WriteString("Capabilities: none\n")
		return
	}
	b.WriteString("Capabilities:\n")
	for _, item := range report.Items {
		fmt.Fprintf(b, "- %s | kind=%s | state=%s | critical=%t | source=%s",
			strutil.FirstNonEmpty(item.Name, item.Capability),
			strutil.FirstNonEmpty(item.Kind, "(none)"),
			strutil.FirstNonEmpty(item.State, "(unknown)"),
			item.Critical,
			strutil.FirstNonEmpty(item.Source, "(none)"),
		)
		if item.Details != "" {
			fmt.Fprintf(b, " | details=%s", item.Details)
		}
		if item.Error != "" {
			fmt.Fprintf(b, " | err=%s", item.Error)
		}
		b.WriteString("\n")
	}
}

func resolveInspectSessionSummary(summaries []session.SessionSummary, target string) (*session.SessionSummary, error) {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "latest") {
		if len(summaries) == 0 {
			return nil, fmt.Errorf("no session is available")
		}
		return &summaries[0], nil
	}
	for i := range summaries {
		if summaries[i].ID == target {
			return &summaries[i], nil
		}
	}
	return nil, fmt.Errorf("session %q not found", target)
}

func inspectThreadSummaryFromSession(summary session.SessionSummary) InspectThreadSummary {
	return InspectThreadSummary{
		ID:           summary.ID,
		Goal:         summary.Goal,
		Status:       string(summary.Status),
		Mode:         summary.Mode,
		Profile:      summary.Profile,
		TaskMode:     summary.TaskMode,
		Source:       summary.Source,
		ParentID:     summary.ParentID,
		TaskID:       summary.TaskID,
		Preview:      summary.Preview,
		ActivityKind: summary.ActivityKind,
		Recoverable:  summary.Recoverable,
		Archived:     summary.Archived,
		CreatedAt:    summary.CreatedAt,
		UpdatedAt:    summary.UpdatedAt,
		EndedAt:      summary.EndedAt,
	}
}

func countStateEntries(catalog *appruntime.StateCatalog, kind appruntime.StateKind, sessionID string) int {
	if catalog == nil || !catalog.Enabled() || strings.TrimSpace(sessionID) == "" {
		return 0
	}
	page, err := catalog.Query(appruntime.StateQuery{Kinds: []appruntime.StateKind{kind}, SessionID: sessionID, Limit: 200})
	if err != nil {
		return 0
	}
	return len(page.Items)
}

func collectInspectableAgentDirs(workspace, trust string) []string {
	var dirs []string
	if appconfig.ProjectAssetsAllowed(trust) {
		dirs = append(dirs, filepath.Join(workspace, ".agents", "agents"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		dirs = append(dirs, filepath.Join(home, ".moss", "agents"))
	}
	return dirs
}

func checkpointCountsBySession(ctx context.Context) map[string]int {
	store, err := checkpoint.NewFileCheckpointStore(CheckpointStoreDir())
	if err != nil {
		return map[string]int{}
	}
	items, err := store.List(ctx)
	if err != nil {
		return map[string]int{}
	}
	counts := make(map[string]int, len(items))
	for _, item := range items {
		sessionID := checkpointSessionID(item)
		if strings.TrimSpace(sessionID) == "" {
			continue
		}
		counts[sessionID]++
	}
	return counts
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
