package product

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/harness/appkit/product/changes"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	appconfig "github.com/mossagents/moss/harness/config"
	extcapability "github.com/mossagents/moss/harness/extensions/capability"
	rstate "github.com/mossagents/moss/harness/runtime/state"
	"github.com/mossagents/moss/harness/userio/prompting"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

func TestBuildInspectReportRunSummarizesPlanningAndFailover(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	workspace := t.TempDir()
	store, err := session.NewFileStore(runtimeenv.SessionStoreDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	now := time.Now().UTC()
	if err := store.Save(ctx, &session.Session{
		ID:        "sess-inspect",
		Status:    session.StatusRunning,
		CreatedAt: now,
		Config: session.SessionConfig{
			Goal: "inspect latest run",
		},
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	catalog, err := rstate.NewStateCatalog(runtimeenv.StateStoreDir(), runtimeenv.StateEventDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	events := []observe.ExecutionEvent{
		{
			EventID:      "evt-turn",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         observe.ExecutionEventType("turn.plan_prepared"),
			Timestamp:    now,
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "turn_plan",
			Metadata: map[string]any{
				"iteration":            1,
				"instruction_profile":  "planning",
				"model_lane":           "reasoning",
				"visible_tools_count":  2,
				"hidden_tools_count":   1,
				"approval_tools_count": 1,
			},
		},
		{
			EventID:      "evt-tools",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         observe.ExecutionEventType("tool.route_planned"),
			Timestamp:    now.Add(time.Millisecond),
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "tool_route",
			Metadata: map[string]any{
				"visible_tools":  []string{"read_file", "view"},
				"hidden_tools":   []string{"write_file"},
				"approval_tools": []string{"run_command"},
				"route_digest":   "read_file:visible,run_command:approval-required,write_file:hidden",
				"decisions": []map[string]any{
					{"name": "read_file", "status": "visible", "source": "builtin", "owner": "runtime", "risk": "low", "reason_codes": []string{"visible"}},
					{"name": "write_file", "status": "hidden", "source": "builtin", "owner": "runtime", "risk": "high", "reason_codes": []string{"planning_mode"}},
				},
			},
		},
		{
			EventID:      "evt-model",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         observe.ExecutionEventType("model.route_planned"),
			Timestamp:    now.Add(2 * time.Millisecond),
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "model_route",
			Model:        "gpt-5",
			Metadata: map[string]any{
				"lane":          "reasoning",
				"reason_codes":  []string{"planning_mode"},
				"capabilities":  []model.ModelCapability{model.CapReasoning},
				"max_cost_tier": 0,
				"prefer_cheap":  false,
			},
		},
		{
			EventID:      "evt-failover",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         observe.ExecutionEventType("llm_failover_attempt"),
			Timestamp:    now.Add(3 * time.Millisecond),
			Phase:        "llm",
			Actor:        "runtime",
			PayloadKind:  "llm_attempt",
			Model:        "gpt-5",
			Metadata: map[string]any{
				"candidate_model": "gpt-5",
				"attempt_index":   1,
				"candidate_retry": 0,
				"breaker_state":   "closed",
				"failover_to":     "claude-sonnet",
				"outcome":         "failed",
				"failure_reason":  "rate limited",
			},
		},
	}
	for _, event := range events {
		if err := catalog.AppendExecutionEvent(event); err != nil {
			t.Fatalf("AppendExecutionEvent(%s): %v", event.Type, err)
		}
	}
	changeStore, err := changes.OpenChangeStore()
	if err != nil {
		t.Fatalf("OpenChangeStore: %v", err)
	}
	if err := changeStore.Save(ctx, &changes.ChangeOperation{
		ID:          "change-1",
		RepoRoot:    workspace,
		SessionID:   "sess-inspect",
		Summary:     "update tracked.txt",
		TargetFiles: []string{"tracked.txt"},
		Status:      changes.ChangeStatusApplied,
		CreatedAt:   now.Add(4 * time.Millisecond),
	}); err != nil {
		t.Fatalf("Save change operation: %v", err)
	}
	writeAuditLogEntries(t,
		map[string]any{
			"timestamp":  now.Add(5 * time.Millisecond).UTC().Format(time.RFC3339),
			"type":       "execution_event",
			"session_id": "sess-inspect",
			"data": map[string]any{
				"type":          string(observe.ExecutionContextCompacted),
				"run_id":        "run-1",
				"turn_id":       "run-1-turn-001",
				"phase":         "context",
				"payload_kind":  "context_compaction",
				"reason":        "trigger_tokens",
				"tokens_before": 220,
				"tokens_after":  140,
			},
		},
		map[string]any{
			"timestamp":  now.Add(6 * time.Millisecond).UTC().Format(time.RFC3339),
			"type":       "execution_event",
			"session_id": "sess-inspect",
			"data": map[string]any{
				"type":    string(observe.ExecutionGuardianReviewed),
				"run_id":  "run-1",
				"turn_id": "run-1-turn-001",
				"phase":   "approval",
				"outcome": "auto_approved",
			},
		},
	)

	report, err := BuildInspectReport(ctx, workspace, []string{"run", "latest", "10"})
	if err != nil {
		t.Fatalf("BuildInspectReport: %v", err)
	}
	if report.Run == nil {
		t.Fatal("expected run report")
	}
	if report.Run.SessionID != "sess-inspect" || report.Run.RunID != "run-1" {
		t.Fatalf("unexpected run identity: %+v", report.Run)
	}
	if report.Run.TurnPlan == nil || report.Run.TurnPlan.ModelLane != "reasoning" {
		t.Fatalf("unexpected turn plan: %+v", report.Run.TurnPlan)
	}
	if report.Run.ToolRoute == nil || len(report.Run.ToolRoute.Decisions) != 2 {
		t.Fatalf("unexpected tool route: %+v", report.Run.ToolRoute)
	}
	if report.Run.ModelRoute == nil || report.Run.ModelRoute.Lane != "reasoning" {
		t.Fatalf("unexpected model route: %+v", report.Run.ModelRoute)
	}
	if len(report.Run.Failovers) != 1 || report.Run.Failovers[0].FailoverTo != "claude-sonnet" {
		t.Fatalf("unexpected failovers: %+v", report.Run.Failovers)
	}
	if len(report.Run.Changes) != 1 || report.Run.Changes[0].RecordID != "change-1" {
		t.Fatalf("unexpected changes: %+v", report.Run.Changes)
	}
	if report.Run.Audit == nil || report.Run.Audit.EventCount != 2 || report.Run.Audit.Context.Compactions != 1 || report.Run.Audit.Guardian.AutoApproved != 1 {
		t.Fatalf("unexpected run audit summary: %+v", report.Run.Audit)
	}

	rendered := RenderInspectReport(report)
	for _, want := range []string{
		"moss inspect (run)",
		"Run session: sess-inspect",
		"Turn plan:   iteration=1 instruction_profile=planning lane=reasoning",
		"Model route: configured=gpt-5 lane=reasoning",
		"Tool decisions:",
		"Failover:",
		"Audit: source=audit_log events=2",
		"Context audit: compactions=1 reclaimed=80",
		"Guardian audit: reviews=1 auto_approved=1",
		"Changes:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered inspect missing %q:\n%s", want, rendered)
		}
	}
}

func TestBuildInspectReportThreadsPromptAndCapabilities(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	workspace := t.TempDir()
	store, err := session.NewFileStore(runtimeenv.SessionStoreDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	root := &session.Session{
		Status:    session.StatusPaused,
		CreatedAt: time.Now().Add(-time.Hour),
		Config: session.SessionConfig{
			Goal: "root thread",
			Mode: "interactive",
			ResolvedSessionSpec: &session.ResolvedSessionSpec{
				Intent:  session.ResolvedIntent{CollaborationMode: "plan", PromptPack: session.PromptPackRef{ID: "coding"}},
				Runtime: session.ResolvedRuntime{PermissionProfile: "workspace-write"},
				Origin:  session.ResolvedOrigin{Preset: "code"},
			},
			SystemPrompt: "system prompt",
			Metadata: prompting.AttachComposeDebugMeta(map[string]any{
				"profile":                                "planning",
				session.MetadataTaskMode:                 "planning",
				prompting.MetadataSessionInstructionsKey: "focus on review",
			}, prompting.ComposeDebugMeta{
				BaseSource:         "config",
				DynamicSectionID:   []string{"environment", "profile_mode"},
				EnabledLayers:      []string{"base_config", "environment", "profile_mode"},
				SuppressedLayers:   []string{"skills"},
				SuppressionReasons: map[string]string{"skills": "empty_content"},
				LayerTokenEstimates: map[string]int{
					"base_config": 10,
					"environment": 6,
				},
				SourceChain:        []string{"base:config", "dynamic:environment", "dynamic:profile_mode"},
				InstructionProfile: "planning",
			}),
		},
	}
	session.RefreshThreadMetadata(root, time.Now().Add(-5*time.Minute), "manual")
	if err := store.Save(ctx, root); err != nil {
		t.Fatalf("save root: %v", err)
	}
	child := &session.Session{
		ID:        "sess-child",
		Status:    session.StatusRunning,
		CreatedAt: time.Now().Add(-30 * time.Minute),
		Config: session.SessionConfig{
			Goal: "child thread",
			Mode: "delegated",
		},
	}
	session.SetThreadSource(child, "delegated")
	session.SetThreadParent(child, "sess-root")
	session.SetThreadTaskID(child, "task-1")
	session.RefreshThreadMetadata(child, time.Now(), "delegated")
	if err := store.Save(ctx, child); err != nil {
		t.Fatalf("save child: %v", err)
	}
	cpStore, err := checkpoint.NewFileCheckpointStore(runtimeenv.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	if _, err := cpStore.Create(ctx, checkpoint.CheckpointCreateRequest{SessionID: "sess-root", Note: "before switch"}); err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	catalog, err := rstate.NewStateCatalog(runtimeenv.StateStoreDir(), runtimeenv.StateEventDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	changeStore, err := changes.OpenChangeStore()
	if err != nil {
		t.Fatalf("OpenChangeStore: %v", err)
	}
	if err := changeStore.Save(ctx, &changes.ChangeOperation{
		ID:          "change-1",
		RepoRoot:    workspace,
		SessionID:   "sess-root",
		Summary:     "edit a.txt",
		TargetFiles: []string{"a.txt"},
		Status:      changes.ChangeStatusApplied,
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("Save change operation: %v", err)
	}
	writeAuditLogEntries(t,
		map[string]any{
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
			"type":       "execution_event",
			"session_id": "sess-root",
			"data": map[string]any{
				"type":                             string(observe.ExecutionContextNormalized),
				"phase":                            "prompt",
				"dropped_orphan_tool_results":      1,
				"synthesized_missing_tool_results": 2,
			},
		},
		map[string]any{
			"timestamp":  time.Now().Add(time.Millisecond).UTC().Format(time.RFC3339),
			"type":       "execution_event",
			"session_id": "sess-root",
			"data": map[string]any{
				"type":    string(observe.ExecutionGuardianReviewed),
				"phase":   "approval",
				"outcome": "fallback_error",
			},
		},
	)
	for _, entry := range []rstate.StateEntry{
		{Kind: rstate.StateKindTask, RecordID: "task-1", SessionID: "sess-child", Status: "running", Title: "delegate child", SortTime: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := catalog.Upsert(entry); err != nil {
			t.Fatalf("Upsert(%s): %v", entry.Kind, err)
		}
	}
	capabilitySnapshot := extcapability.CapabilitySnapshot{
		UpdatedAt: time.Now().UTC(),
		Items: []extcapability.CapabilityStatus{
			{Capability: "builtin-tools", Kind: "builtin", Name: "builtin-tools", State: "ready", Critical: true, UpdatedAt: time.Now().UTC()},
			{Capability: "subagent:planner", Kind: "subagent", Name: "planner", State: "ready", UpdatedAt: time.Now().UTC()},
		},
	}
	path := extcapability.CapabilityStatusPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir capability dir: %v", err)
	}
	raw, _ := json.Marshal(capabilitySnapshot)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write capability snapshot: %v", err)
	}

	threadsReport, err := BuildInspectReport(ctx, workspace, []string{"threads", "10"})
	if err != nil {
		t.Fatalf("BuildInspectReport threads: %v", err)
	}
	if len(threadsReport.Threads) < 2 {
		t.Fatalf("unexpected threads report: %+v", threadsReport.Threads)
	}
	foundRoot := false
	foundChild := false
	for _, item := range threadsReport.Threads {
		if item.ID == "sess-root" && item.ChangeCount == 1 {
			foundRoot = true
		}
		if item.ID == "sess-child" && item.TaskCount == 1 {
			foundChild = true
		}
	}
	if !foundRoot || !foundChild {
		t.Fatalf("expected root/child thread summaries, got %+v", threadsReport.Threads)
	}
	threadReport, err := BuildInspectReport(ctx, workspace, []string{"thread", "sess-root"})
	if err != nil {
		t.Fatalf("BuildInspectReport thread: %v", err)
	}
	if threadReport.Thread == nil || len(threadReport.Thread.Children) != 1 || len(threadReport.Thread.Checkpoints) < 1 || threadReport.Thread.Summary.CheckpointCount < 1 {
		t.Fatalf("unexpected thread detail: %+v", threadReport.Thread)
	}
	if threadReport.Thread.Audit == nil || threadReport.Thread.Audit.Context.Normalizations != 1 || threadReport.Thread.Audit.Guardian.Errors != 1 {
		t.Fatalf("unexpected thread audit summary: %+v", threadReport.Thread.Audit)
	}
	promptReport, err := BuildInspectReport(ctx, workspace, []string{"prompt", "sess-root"})
	if err != nil {
		t.Fatalf("BuildInspectReport prompt: %v", err)
	}
	if promptReport.Prompt == nil || promptReport.Prompt.InstructionProfile != "planning" || promptReport.Prompt.CollaborationMode != "plan" || !promptReport.Prompt.SessionInstructions {
		t.Fatalf("unexpected prompt detail: %+v", promptReport.Prompt)
	}
	capReport, err := BuildInspectReport(ctx, workspace, []string{"capabilities"})
	if err != nil {
		t.Fatalf("BuildInspectReport capabilities: %v", err)
	}
	if capReport.Capabilities == nil || len(capReport.Capabilities.Items) == 0 {
		t.Fatalf("unexpected capability detail: %+v", capReport.Capabilities)
	}

	rendered := RenderInspectReport(promptReport) + "\n" + RenderInspectReport(threadReport) + "\n" + RenderInspectReport(capReport)
	for _, want := range []string{"Prompt session: sess-root", "Thread:      sess-root", "Audit: source=audit_log events=2", "Context audit: compactions=0 reclaimed=0 trim_retries=0 trimmed_messages=0 normalizations=1", "Capabilities:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %q in rendered output:\n%s", want, rendered)
		}
	}
}

func TestBuildInspectReportReplayCompareAndGovernance(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	workspace := t.TempDir()
	store, err := session.NewFileStore(runtimeenv.SessionStoreDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	now := time.Now().UTC()
	root := &session.Session{
		ID:        "sess-govern-root",
		Status:    session.StatusPaused,
		CreatedAt: now.Add(-time.Hour),
		Config:    session.SessionConfig{Goal: "root"},
	}
	session.RefreshThreadMetadata(root, now.Add(-2*time.Minute), "manual")
	if err := store.Save(ctx, root); err != nil {
		t.Fatalf("save root: %v", err)
	}
	child := &session.Session{
		ID:        "sess-govern-child",
		Status:    session.StatusRunning,
		CreatedAt: now.Add(-30 * time.Minute),
		Config:    session.SessionConfig{Goal: "child", Mode: "delegated"},
	}
	session.SetThreadSource(child, "delegated")
	session.SetThreadParent(child, root.ID)
	session.SetThreadTaskID(child, "task-govern")
	session.RefreshThreadMetadata(child, now, "delegated")
	if err := store.Save(ctx, child); err != nil {
		t.Fatalf("save child: %v", err)
	}

	cpStore, err := checkpoint.NewFileCheckpointStore(runtimeenv.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("checkpoint store: %v", err)
	}
	checkpoint, err := cpStore.Create(ctx, checkpoint.CheckpointCreateRequest{
		SessionID: child.ID,
		Note:      "before replay",
		PatchIDs:  []string{"patch-1", "patch-2"},
	})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}

	changeStore, err := changes.OpenChangeStore()
	if err != nil {
		t.Fatalf("OpenChangeStore: %v", err)
	}
	if err := changeStore.Save(ctx, &changes.ChangeOperation{
		ID:           "change-govern-1",
		SessionID:    child.ID,
		RepoRoot:     workspace,
		Summary:      "edit tracked.txt",
		Status:       changes.ChangeStatusRolledBack,
		TargetFiles:  []string{"tracked.txt"},
		CreatedAt:    now.Add(-time.Minute),
		RolledBackAt: now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("save change operation: %v", err)
	}
	if err := changeStore.Save(ctx, &changes.ChangeOperation{
		ID:          "change-govern-2",
		SessionID:   child.ID,
		RepoRoot:    workspace,
		Summary:     "edit risk.txt",
		Status:      changes.ChangeStatusApplyInconsistent,
		TargetFiles: []string{"risk.txt"},
		CreatedAt:   now.Add(-20 * time.Second),
	}); err != nil {
		t.Fatalf("save inconsistent change operation: %v", err)
	}

	catalog, err := rstate.NewStateCatalog(runtimeenv.StateStoreDir(), runtimeenv.StateEventDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	for _, event := range []observe.ExecutionEvent{
		{
			EventID:      "evt-gov-model",
			EventVersion: 1,
			SessionID:    child.ID,
			RunID:        "run-gov-1",
			TurnID:       "run-gov-1-turn-001",
			Type:         observe.ExecutionEventType("model.route_planned"),
			Timestamp:    now.Add(-10 * time.Second),
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "model_route",
			Model:        "gpt-5",
			Metadata: map[string]any{
				"lane": "reasoning",
			},
		},
		{
			EventID:      "evt-gov-failover",
			EventVersion: 1,
			SessionID:    child.ID,
			RunID:        "run-gov-1",
			TurnID:       "run-gov-1-turn-001",
			Type:         observe.ExecutionEventType("llm_failover_attempt"),
			Timestamp:    now.Add(-9 * time.Second),
			Phase:        "llm",
			Actor:        "runtime",
			PayloadKind:  "llm_attempt",
			Metadata: map[string]any{
				"candidate_model": "gpt-5",
				"failover_to":     "claude-sonnet",
			},
		},
		{
			EventID:      "evt-gov-approval-request",
			EventVersion: 1,
			SessionID:    child.ID,
			RunID:        "run-gov-1",
			TurnID:       "run-gov-1-turn-001",
			Type:         observe.ExecutionApprovalRequest,
			Timestamp:    now.Add(-8 * time.Second),
			Phase:        "approval",
			Actor:        "runtime",
			PayloadKind:  "approval",
			Metadata: map[string]any{
				"reason_code": "network",
			},
		},
		{
			EventID:      "evt-gov-approval-resolved",
			EventVersion: 1,
			SessionID:    child.ID,
			RunID:        "run-gov-1",
			TurnID:       "run-gov-1-turn-001",
			Type:         observe.ExecutionApprovalResolved,
			Timestamp:    now.Add(-7 * time.Second),
			Phase:        "approval",
			Actor:        "runtime",
			PayloadKind:  "approval",
			Metadata: map[string]any{
				"approved": false,
			},
		},
	} {
		if err := catalog.AppendExecutionEvent(event); err != nil {
			t.Fatalf("AppendExecutionEvent(%s): %v", event.Type, err)
		}
	}
	for _, entry := range []rstate.StateEntry{
		{Kind: rstate.StateKindTask, RecordID: "task-govern", SessionID: child.ID, Status: "running", Title: "delegate child", SortTime: now, CreatedAt: now, UpdatedAt: now},
	} {
		if err := catalog.Upsert(entry); err != nil {
			t.Fatalf("Upsert(%s): %v", entry.Kind, err)
		}
	}

	replayReport, err := BuildInspectReport(ctx, workspace, []string{"replay", checkpoint.ID})
	if err != nil {
		t.Fatalf("BuildInspectReport replay: %v", err)
	}
	if replayReport.Replay == nil || replayReport.Replay.CheckpointID != checkpoint.ID {
		t.Fatalf("unexpected replay report: %+v", replayReport.Replay)
	}
	if !strings.Contains(RenderInspectReport(replayReport), "Suggested replay:") {
		t.Fatalf("expected replay rendering, got:\n%s", RenderInspectReport(replayReport))
	}

	compareReport, err := BuildInspectReport(ctx, workspace, []string{"compare", "thread:" + child.ID, "checkpoint:" + checkpoint.ID})
	if err != nil {
		t.Fatalf("BuildInspectReport compare: %v", err)
	}
	if compareReport.Compare == nil || compareReport.Compare.Left.ID != child.ID || compareReport.Compare.Right.ID != checkpoint.ID {
		t.Fatalf("unexpected compare report: %+v", compareReport.Compare)
	}
	if !strings.Contains(RenderInspectReport(compareReport), "Metrics:") {
		t.Fatalf("expected compare rendering, got:\n%s", RenderInspectReport(compareReport))
	}

	governanceReport, err := BuildInspectReport(ctx, workspace, []string{"governance", "20"})
	if err != nil {
		t.Fatalf("BuildInspectReport governance: %v", err)
	}
	if governanceReport.Governance == nil {
		t.Fatal("expected governance report")
	}
	if governanceReport.Governance.Failover.Attempts != 1 || governanceReport.Governance.Approvals.Denied != 1 {
		t.Fatalf("unexpected governance summary: %+v", governanceReport.Governance)
	}
	rendered := RenderInspectReport(governanceReport)
	for _, want := range []string{
		"Governance window:",
		"Lane stability:",
		"Failover: attempts=1",
		"Approvals: requested=1 resolved=1 approved=0 denied=1",
		"Changes: applied=0 rolled_back=1 inconsistent=1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered governance missing %q:\n%s", want, rendered)
		}
	}
}

func TestBuildInspectReportForTrustSuppressesProjectCapabilities(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	workspace := t.TempDir()
	skillDir := filepath.Join(workspace, ".agents", "skills", "project-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: project-skill\ndescription: demo\n---\nhello"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	projectCfg := &appconfig.Config{
		Skills: []appconfig.SkillConfig{{Name: "project-mcp", Transport: "stdio", Command: "node server.js"}},
	}
	if err := appconfig.SaveConfig(appconfig.DefaultProjectConfigPath(workspace), projectCfg); err != nil {
		t.Fatalf("save project config: %v", err)
	}

	restricted, err := BuildInspectReportForTrust(ctx, workspace, appconfig.TrustRestricted, []string{"capabilities"})
	if err != nil {
		t.Fatalf("BuildInspectReportForTrust restricted: %v", err)
	}
	for _, item := range restricted.Capabilities.Items {
		if item.Name == "project-skill" || item.Name == "project-mcp" {
			t.Fatalf("restricted inspect should suppress project capability, got %+v", item)
		}
	}

	trusted, err := BuildInspectReportForTrust(ctx, workspace, appconfig.TrustTrusted, []string{"capabilities"})
	if err != nil {
		t.Fatalf("BuildInspectReportForTrust trusted: %v", err)
	}
	foundSkill := false
	foundMCP := false
	for _, item := range trusted.Capabilities.Items {
		if item.Name == "project-skill" {
			foundSkill = true
		}
		if item.Name == "project-mcp" {
			foundMCP = true
		}
	}
	if !foundSkill || !foundMCP {
		t.Fatalf("trusted inspect missing project capability: skill=%t mcp=%t items=%+v", foundSkill, foundMCP, trusted.Capabilities.Items)
	}
}

// ── inspectLimitAndText ───────────────────────────────────────────────────────

func TestInspectLimitAndText(t *testing.T) {
	// no args → fallback limit, empty text
	limit, text := inspectLimitAndText(nil, 20)
	if limit != 20 || text != "" {
		t.Fatalf("nil args: limit=%d text=%q, want 20 ''", limit, text)
	}

	// first arg is a valid positive number
	limit, text = inspectLimitAndText([]string{"5", "search terms here"}, 20)
	if limit != 5 || text != "search terms here" {
		t.Fatalf("numeric first arg: limit=%d text=%q", limit, text)
	}

	// first arg is NOT a number → use fallback, join all args as text
	limit, text = inspectLimitAndText([]string{"find", "something"}, 10)
	if limit != 10 || text != "find something" {
		t.Fatalf("non-numeric first arg: limit=%d text=%q", limit, text)
	}

	// first arg is zero → not positive, use fallback
	limit, text = inspectLimitAndText([]string{"0", "term"}, 15)
	if limit != 15 || text != "0 term" {
		t.Fatalf("zero first arg: limit=%d text=%q", limit, text)
	}

	// first arg is negative → use fallback
	limit, text = inspectLimitAndText([]string{"-3"}, 8)
	if limit != 8 {
		t.Fatalf("negative first arg: limit=%d, want 8", limit)
	}
}

func writeAuditLogEntries(t *testing.T, entries ...map[string]any) {
	t.Helper()
	path := AuditLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer f.Close()
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal audit entry: %v", err)
		}
		if _, err := fmt.Fprintln(f, string(data)); err != nil {
			t.Fatalf("write audit entry: %v", err)
		}
	}
}
