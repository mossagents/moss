package product

import (
	"context"
	"github.com/mossagents/moss/harness/appkit"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelectResumeSummaryLatest(t *testing.T) {
	summaries := []session.SessionSummary{
		{ID: "done-1", Status: session.StatusCompleted, Recoverable: false},
		{ID: "run-1", Status: session.StatusRunning, Recoverable: true},
		{ID: "fail-1", Status: session.StatusFailed, Recoverable: true},
	}
	selected, recoverable, err := runtimeenv.SelectResumeSummary(summaries, "", true)
	if err != nil {
		t.Fatalf("select resume: %v", err)
	}
	if selected == nil || selected.ID != "run-1" {
		t.Fatalf("expected latest recoverable session run-1, got %+v", selected)
	}
	if len(recoverable) != 2 {
		t.Fatalf("expected 2 recoverable sessions, got %d", len(recoverable))
	}
}

func TestSelectResumeSummarySpecificRequiresRecoverable(t *testing.T) {
	summaries := []session.SessionSummary{
		{ID: "done-1", Status: session.StatusCompleted, Recoverable: false},
	}
	selected, _, err := runtimeenv.SelectResumeSummary(summaries, "done-1", false)
	if err == nil {
		t.Fatal("expected non-recoverable session error")
	}
	if selected != nil {
		t.Fatalf("expected nil selection, got %+v", selected)
	}
}

func TestSummarizeSnapshot(t *testing.T) {
	now := time.Now().UTC()
	summary := runtimeenv.SummarizeSnapshot(workspace.WorktreeSnapshot{
		ID:        "snap-1",
		SessionID: "sess-1",
		Mode:      workspace.WorktreeSnapshotGhostState,
		Note:      "before review",
		Capture: workspace.RepoState{
			HeadSHA: "abc123",
			Branch:  "main",
		},
		Patches:   []workspace.PatchSnapshotRef{{PatchID: "p1"}},
		CreatedAt: now,
	})
	if summary.ID != "snap-1" || summary.SessionID != "sess-1" {
		t.Fatalf("unexpected snapshot summary %+v", summary)
	}
	if summary.PatchCount != 1 || summary.Head != "abc123" || summary.Branch != "main" {
		t.Fatalf("unexpected snapshot summary fields %+v", summary)
	}
}

func TestSummarizeCheckpoint(t *testing.T) {
	now := time.Now().UTC()
	summary := runtimeenv.SummarizeCheckpoint(checkpoint.CheckpointRecord{
		ID:                 "cp-1",
		SessionID:          "sess-1",
		WorktreeSnapshotID: "snap-1",
		PatchIDs:           []string{"p1", "p2"},
		Lineage:            []checkpoint.CheckpointLineageRef{{Kind: checkpoint.CheckpointLineageSession, ID: "sess-1"}},
		Note:               "before risky change",
		CreatedAt:          now,
	})
	if summary.ID != "cp-1" || summary.SessionID != "sess-1" || summary.SnapshotID != "snap-1" {
		t.Fatalf("unexpected checkpoint summary %+v", summary)
	}
	if summary.PatchCount != 2 || summary.LineageDepth != 1 {
		t.Fatalf("unexpected checkpoint counts %+v", summary)
	}
}

func TestRenderCheckpointSummaries(t *testing.T) {
	out := runtimeenv.RenderCheckpointSummaries([]runtimeenv.CheckpointSummary{{
		ID:           "cp-1",
		SessionID:    "sess-1",
		SnapshotID:   "snap-1",
		PatchCount:   2,
		LineageDepth: 1,
		Note:         "before risky change",
		CreatedAt:    time.Unix(10, 0).UTC(),
	}})
	for _, want := range []string{"Checkpoints:", "cp-1", "sess-1", "snap-1", "patches=2", "lineage=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output %q", want, out)
		}
	}
}

func TestDescribeCheckpointSortsMetadataKeys(t *testing.T) {
	detail := runtimeenv.DescribeCheckpoint(checkpoint.CheckpointRecord{
		ID:                 "cp-1",
		SessionID:          "sess-hidden",
		WorktreeSnapshotID: "snap-1",
		PatchIDs:           []string{"patch-1", "patch-2"},
		Lineage: []checkpoint.CheckpointLineageRef{
			{Kind: checkpoint.CheckpointLineageCheckpoint, ID: "cp-0"},
			{Kind: checkpoint.CheckpointLineageSession, ID: "sess-1"},
		},
		Metadata: map[string]any{
			"zeta":  1,
			"alpha": true,
		},
		CreatedAt: time.Unix(11, 0).UTC(),
	})
	if detail.SessionID != "sess-1" {
		t.Fatalf("session id = %q, want sess-1", detail.SessionID)
	}
	if got, want := strings.Join(detail.MetadataKeys, ","), "alpha,zeta"; got != want {
		t.Fatalf("metadata keys = %q, want %q", got, want)
	}
}

func TestBuildDoctorReportIncludesMCPServerStatus(t *testing.T) {
	tempHome := t.TempDir()
	workspace := filepath.Join(tempHome, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	appconfig.SetAppName("moss-product-test")
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)

	globalCfg := &appconfig.Config{
		Skills: []appconfig.SkillConfig{
			{Name: "global-mcp", Transport: "stdio", Command: "node global.js"},
		},
	}
	if err := appconfig.SaveConfig(appconfig.DefaultGlobalConfigPath(), globalCfg); err != nil {
		t.Fatalf("SaveConfig global: %v", err)
	}
	projectCfg := &appconfig.Config{
		Skills: []appconfig.SkillConfig{
			{Name: "project-mcp", Transport: "sse", URL: "https://example.test/mcp"},
		},
	}
	if err := appconfig.SaveConfig(appconfig.DefaultProjectConfigPath(workspace), projectCfg); err != nil {
		t.Fatalf("SaveConfig project: %v", err)
	}

	report := BuildDoctorReport(context.Background(), "mosscode", workspace, &appkit.AppFlags{
		Workspace: workspace,
		Trust:     appconfig.TrustRestricted,
	}, nil, "confirm", DefaultGovernanceConfig())
	if got, want := len(report.Health.Extensions.MCPServerStatus), 1; got != want {
		t.Fatalf("mcp server status count = %d, want %d", got, want)
	}
	for _, server := range report.Health.Extensions.MCPServerStatus {
		if server.Name == "project-mcp" {
			t.Fatalf("restricted doctor report should not read project MCP config: %+v", report.Health.Extensions.MCPServerStatus)
		}
	}
	rendered := RenderDoctorReport(report)
	if strings.Contains(rendered, "MCP project-mcp [project]") {
		t.Fatalf("doctor output should not include project MCP under restricted trust: %q", rendered)
	}
	for _, want := range []string{"Adaptive governance:", "Capability workspace [execution]: state=ready"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("doctor output missing %q: %q", want, rendered)
		}
	}
}

func TestRenderCheckpointDetail(t *testing.T) {
	out := runtimeenv.RenderCheckpointDetail(&runtimeenv.CheckpointDetail{
		ID:           "cp-1",
		SessionID:    "sess-1",
		SnapshotID:   "snap-1",
		Note:         "before risky change",
		PatchIDs:     []string{"patch-1", "patch-2"},
		PatchCount:   2,
		Lineage:      []checkpoint.CheckpointLineageRef{{Kind: checkpoint.CheckpointLineageSession, ID: "sess-1"}},
		LineageDepth: 1,
		MetadataKeys: []string{"source", "trigger"},
		CreatedAt:    time.Unix(12, 0).UTC(),
	})
	for _, want := range []string{"Checkpoint: cp-1", "snapshot: snap-1", "patches:  2 (patch-1, patch-2)", "metadata: source, trigger", "lineage refs:", "mosscode checkpoint replay --checkpoint cp-1 --mode resume"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in detail output %q", want, out)
		}
	}
}

func TestListCheckpoints(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	store, err := checkpoint.NewFileCheckpointStore(runtimeenv.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	if _, err := store.Create(context.Background(), checkpoint.CheckpointCreateRequest{
		SessionID: "sess-1",
		Note:      "a",
	}); err != nil {
		t.Fatalf("Create first checkpoint: %v", err)
	}
	if _, err := store.Create(context.Background(), checkpoint.CheckpointCreateRequest{
		SessionID: "sess-2",
		Note:      "b",
	}); err != nil {
		t.Fatalf("Create second checkpoint: %v", err)
	}
	items, err := runtimeenv.ListCheckpoints(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("checkpoint summaries = %d, want 1", len(items))
	}
}

func TestLoadCheckpoint(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	store, err := checkpoint.NewFileCheckpointStore(runtimeenv.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	record, err := store.Create(context.Background(), checkpoint.CheckpointCreateRequest{
		SessionID: "sess-1",
		PatchIDs:  []string{"patch-1"},
		Metadata:  map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("Create checkpoint: %v", err)
	}
	detail, err := runtimeenv.LoadCheckpoint(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if detail == nil || detail.ID != record.ID || detail.PatchCount != 1 {
		t.Fatalf("unexpected checkpoint detail %+v", detail)
	}
}

func TestResolveCheckpointRecordLatest(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	store, err := checkpoint.NewFileCheckpointStore(runtimeenv.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	first, err := store.Create(context.Background(), checkpoint.CheckpointCreateRequest{SessionID: "sess-1", Note: "first"})
	if err != nil {
		t.Fatalf("Create first checkpoint: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := store.Create(context.Background(), checkpoint.CheckpointCreateRequest{SessionID: "sess-2", Note: "second"})
	if err != nil {
		t.Fatalf("Create second checkpoint: %v", err)
	}
	latest, err := runtimeenv.ResolveCheckpointRecord(context.Background(), store, "latest")
	if err != nil {
		t.Fatalf("ResolveCheckpointRecord latest: %v", err)
	}
	if latest == nil || latest.ID != second.ID {
		t.Fatalf("latest checkpoint = %+v, want %s", latest, second.ID)
	}
	implicit, err := runtimeenv.ResolveCheckpointRecord(context.Background(), store, "")
	if err != nil {
		t.Fatalf("ResolveCheckpointRecord empty selector: %v", err)
	}
	if implicit == nil || implicit.ID != second.ID || implicit.ID == first.ID {
		t.Fatalf("implicit latest checkpoint = %+v, want %s", implicit, second.ID)
	}
}

// ── MarshalJSON (runtime_paths.go) ───────────────────────────────────────────

func TestMarshalJSON_Simple(t *testing.T) {
	got, err := MarshalJSON(map[string]int{"count": 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `"count"`) || !strings.Contains(got, "42") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestMarshalJSON_Indented(t *testing.T) {
	got, err := MarshalJSON([]string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("expected indented output, got: %q", got)
	}
}
