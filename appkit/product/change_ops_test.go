package product

import (
	"context"
	"github.com/mossagents/moss/appkit/product/changes"
	runtimeenv "github.com/mossagents/moss/appkit/product/runtimeenv"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyChangeAndReviewAndRollback(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	repo := initTestRepo(t)
	patch := makeTrackedPatch(t, repo, "tracked.txt", "one\n", "two\n")

	rt := changes.ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
	}

	applied, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
		Patch:   patch,
		Summary: "update tracked.txt",
	})
	if err != nil {
		t.Fatalf("changes.ApplyChange: %v", err)
	}
	if applied.Status != changes.ChangeStatusApplied {
		t.Fatalf("unexpected apply status %q", applied.Status)
	}
	if applied.PatchID == "" {
		t.Fatal("expected patch id")
	}
	if got := readTestFile(t, filepath.Join(repo, "tracked.txt")); got != "two\n" {
		t.Fatalf("unexpected applied file content %q", got)
	}

	report, err := runtimeenv.BuildReviewReport(ctx, repo, []string{"changes"})
	if err != nil {
		t.Fatalf("runtimeenv.BuildReviewReport changes: %v", err)
	}
	if len(report.Changes) != 1 || report.Changes[0].ID != applied.ID {
		t.Fatalf("unexpected review changes output %+v", report.Changes)
	}
	detail, err := runtimeenv.BuildReviewReport(ctx, repo, []string{"change", applied.ID})
	if err != nil {
		t.Fatalf("runtimeenv.BuildReviewReport change: %v", err)
	}
	if detail.Change == nil || detail.Change.ID != applied.ID {
		t.Fatalf("unexpected review change detail %+v", detail.Change)
	}

	rolledBack, err := changes.RollbackChange(ctx, rt, changes.RollbackChangeRequest{ChangeID: applied.ID})
	if err != nil {
		t.Fatalf("changes.RollbackChange: %v", err)
	}
	if rolledBack.Status != changes.ChangeStatusRolledBack {
		t.Fatalf("unexpected rollback status %q", rolledBack.Status)
	}
	if rolledBack.RollbackMode != changes.RollbackModeExact {
		t.Fatalf("unexpected rollback mode %q", rolledBack.RollbackMode)
	}
	if got := readTestFile(t, filepath.Join(repo, "tracked.txt")); got != "one\n" {
		t.Fatalf("unexpected rolled back file content %q", got)
	}
}

func TestApplyChangeRejectsDirtyRepo(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	repo := initTestRepo(t)
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "dirty\n")

	rt := changes.ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
	}

	_, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
		Patch:   "diff --git a/tracked.txt b/tracked.txt\n",
		Summary: "bad",
	})
	if err == nil || !strings.Contains(err.Error(), "clean repository") {
		t.Fatalf("expected clean repository error, got %v", err)
	}
}

func TestRollbackChangeManualRecoveryWhenExactUnavailable(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	repo := initTestRepo(t)
	patch := makeTrackedPatch(t, repo, "tracked.txt", "one\n", "two\n")

	rt := changes.ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
	}

	applied, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
		Patch:   patch,
		Summary: "update tracked.txt",
	})
	if err != nil {
		t.Fatalf("changes.ApplyChange: %v", err)
	}

	journalPath := filepath.Join(repo, ".git", "moss-patches.json")
	if err := os.Remove(journalPath); err != nil {
		t.Fatalf("remove patch journal: %v", err)
	}

	item, err := changes.RollbackChange(ctx, rt, changes.RollbackChangeRequest{ChangeID: applied.ID})
	if err == nil {
		t.Fatal("expected rollback failure")
	}
	var opErr *changes.ChangeOperationError
	if !strings.Contains(err.Error(), "exact rollback") {
		t.Fatalf("unexpected rollback error %v", err)
	}
	if !strings.Contains(err.Error(), "capture_head=") {
		t.Fatalf("expected manual recovery details in error, got %v", err)
	}
	if !asChangeOperationError(err, &opErr) {
		t.Fatalf("expected changes.ChangeOperationError, got %T", err)
	}
	if item == nil || item.Status != changes.ChangeStatusApplied {
		t.Fatalf("expected original applied operation, got %+v", item)
	}
}

func TestApplyChangeCapturesTurnMetadata(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	repo := initTestRepo(t)
	patch := makeTrackedPatch(t, repo, "tracked.txt", "one\n", "two\n")
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	sess := &session.Session{
		ID: "sess-change-turn",
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataRunID:              "run-123",
				session.MetadataTurnID:             "run-123-turn-001",
				session.MetadataInstructionProfile: "planning",
				session.MetadataModelLane:          "reasoning",
				session.MetadataVisibleTools:       []string{"read_file"},
				session.MetadataHiddenTools:        []string{"write_file"},
			},
		},
	}
	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	rt := changes.ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
		SessionStore:     store,
	}

	applied, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
		Patch:     patch,
		Summary:   "update tracked.txt",
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatalf("changes.ApplyChange: %v", err)
	}
	if applied.RunID != "run-123" || applied.TurnID != "run-123-turn-001" {
		t.Fatalf("unexpected run/turn metadata: %+v", applied)
	}
	if applied.InstructionProfile != "planning" || applied.ModelLane != "reasoning" {
		t.Fatalf("unexpected plan provenance: %+v", applied)
	}
	if len(applied.VisibleTools) != 1 || applied.VisibleTools[0] != "read_file" {
		t.Fatalf("unexpected visible tools: %+v", applied.VisibleTools)
	}
}

func TestApplyChangePrefersLiveSessionMetadata(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	repo := initTestRepo(t)
	patch := makeTrackedPatch(t, repo, "tracked.txt", "one\n", "two\n")
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	stale := &session.Session{
		ID: "sess-live-turn",
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataRunID:        "run-stale",
				session.MetadataTurnID:       "run-stale-turn-001",
				session.MetadataModelLane:    "default",
				session.MetadataVisibleTools: []string{"old_tool"},
			},
		},
	}
	if err := store.Save(ctx, stale); err != nil {
		t.Fatalf("save stale session: %v", err)
	}
	live := &session.Session{
		ID: "sess-live-turn",
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataRunID:        "run-live",
				session.MetadataTurnID:       "run-live-turn-002",
				session.MetadataModelLane:    "reasoning",
				session.MetadataVisibleTools: []string{"read_file"},
			},
		},
	}
	rt := changes.ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
		SessionStore:     store,
		SessionLookup: func(id string) (*session.Session, bool) {
			if id == live.ID {
				return live, true
			}
			return nil, false
		},
	}
	applied, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
		Patch:     patch,
		Summary:   "update tracked.txt",
		SessionID: live.ID,
	})
	if err != nil {
		t.Fatalf("changes.ApplyChange: %v", err)
	}
	if applied.RunID != "run-live" || applied.TurnID != "run-live-turn-002" {
		t.Fatalf("expected live metadata, got %+v", applied)
	}
	if len(applied.VisibleTools) != 1 || applied.VisibleTools[0] != "read_file" {
		t.Fatalf("expected live visible tools, got %+v", applied.VisibleTools)
	}
}

func configureProductTestApp(t *testing.T) {
	t.Helper()
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-", "(", "-", ")", "-", "[", "-", "]", "-")
	appconfig.SetAppName("moss-product-test-" + replacer.Replace(strings.ToLower(t.Name())))
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runProductGit(t, repo, "init")
	runProductGit(t, repo, "config", "user.email", "test@example.com")
	runProductGit(t, repo, "config", "user.name", "Test User")
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "one\n")
	runProductGit(t, repo, "add", "tracked.txt")
	runProductGit(t, repo, "commit", "-m", "initial")
	return repo
}

func makeTrackedPatch(t *testing.T, repo, rel, before, after string) string {
	t.Helper()
	path := filepath.Join(repo, rel)
	writeTestFile(t, path, after)
	patch := gitOutputProduct(t, repo, "diff")
	runProductGit(t, repo, "checkout", "--", rel)
	if got := readTestFile(t, path); got != before {
		t.Fatalf("expected %q after checkout, got %q", before, got)
	}
	return patch
}

func gitOutputProduct(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func runProductGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func asChangeOperationError(err error, target **changes.ChangeOperationError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(*changes.ChangeOperationError)
	if !ok {
		return false
	}
	*target = value
	return true
}

func TestSummarizeChange(t *testing.T) {
	summary := changes.SummarizeChange(changes.ChangeOperation{
		ID:           "change-1",
		RepoRoot:     "repo",
		BaseHeadSHA:  "abc123",
		SessionID:    "sess-1",
		PatchID:      "patch-1",
		CheckpointID: "cp-1",
		Summary:      "hello",
		TargetFiles:  []string{"tracked.txt"},
		Status:       changes.ChangeStatusApplied,
		CreatedAt:    timeNowTest(),
	})
	if summary.ID != "change-1" || summary.PatchID != "patch-1" {
		t.Fatalf("unexpected summary %+v", summary)
	}
}

func timeNowTest() (out time.Time) {
	return time.Unix(10, 0).UTC()
}

// ── pure rendering functions ──────────────────────────────────────────────────

func TestRenderChangeSummaries_Empty(t *testing.T) {
	got := changes.RenderChangeSummaries(nil)
	if got != "Changes: none" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRenderChangeSummaries_Items(t *testing.T) {
	items := []changes.ChangeSummary{
		{
			ID:          "chg-1",
			Status:      changes.ChangeStatusApplied,
			PatchID:     "p1",
			TargetFiles: []string{"a.go", "b.go"},
			Summary:     "fix bug",
			CreatedAt:   timeNowTest(),
		},
		{
			ID:        "chg-2",
			Status:    changes.ChangeStatusPreparing,
			CreatedAt: timeNowTest(),
		},
	}
	got := changes.RenderChangeSummaries(items)
	if !strings.Contains(got, "chg-1") {
		t.Error("missing chg-1")
	}
	if !strings.Contains(got, "files=2") {
		t.Error("missing files count")
	}
	if !strings.Contains(got, "fix bug") {
		t.Error("missing summary")
	}
	if !strings.Contains(got, "chg-2") {
		t.Error("missing chg-2")
	}
	if !strings.Contains(got, "(none)") {
		t.Error("missing (none) fallback for missing summary")
	}
	if !strings.Contains(got, "(pending)") {
		t.Error("missing (pending) fallback for missing patch")
	}
}

func TestRenderChangeDetail_Nil(t *testing.T) {
	got := changes.RenderChangeDetail(nil)
	if got != "Change: not found" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRenderChangeDetail_Full(t *testing.T) {
	item := &changes.ChangeOperation{
		ID:                 "chg-full",
		RepoRoot:           "/repo",
		BaseHeadSHA:        "abc123",
		SessionID:          "sess-1",
		RunID:              "run-1",
		TurnID:             "turn-1",
		InstructionProfile: "default",
		ModelLane:          "fast",
		PatchID:            "patch-1",
		CheckpointID:       "cp-1",
		Summary:            "test change",
		TargetFiles:        []string{"main.go", "go.mod"},
		VisibleTools:       []string{"read_file", "write_file"},
		Status:             changes.ChangeStatusApplied,
		RecoveryMode:       "",
		RollbackMode:       changes.RollbackModeExact,
		RollbackDetails:    "rollback detail",
		CreatedAt:          timeNowTest(),
	}
	got := changes.RenderChangeDetail(item)
	for _, want := range []string{
		"chg-full", "/repo", "abc123", "sess-1", "run-1", "turn-1", "default",
		"fast", "patch-1", "cp-1", "main.go", "go.mod", "read_file", "write_file",
		string(changes.RollbackModeExact), "rollback detail", "applied",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestRenderChangeDetail_NoFiles(t *testing.T) {
	item := &changes.ChangeOperation{ID: "chg-empty", Status: changes.ChangeStatusPreparing, CreatedAt: timeNowTest()}
	got := changes.RenderChangeDetail(item)
	if !strings.Contains(got, "files:     (none)") {
		t.Errorf("expected no-files message, got:\n%s", got)
	}
	// No rollback details → should not appear
	if strings.Contains(got, "rollback details") {
		t.Error("should not include rollback details when empty")
	}
}

func TestRenderChangeDetail_WithCapture(t *testing.T) {
	item := &changes.ChangeOperation{
		ID:        "chg-cap",
		Status:    changes.ChangeStatusApplied,
		CreatedAt: timeNowTest(),
	}
	// No capture → no capture line
	got := changes.RenderChangeDetail(item)
	if strings.Contains(got, "capture:") {
		t.Error("should not include capture line when nil")
	}
}

func TestRenderChangeDetail_RecoveryDetails(t *testing.T) {
	item := &changes.ChangeOperation{
		ID:              "chg-rec",
		Status:          changes.ChangeStatusApplyInconsistent,
		RecoveryDetails: "manual step needed",
		CreatedAt:       timeNowTest(),
	}
	got := changes.RenderChangeDetail(item)
	if !strings.Contains(got, "manual step needed") {
		t.Errorf("missing recovery details in output:\n%s", got)
	}
}

func TestPadField(t *testing.T) {
	if got := changes.PadField(""); got != "" {
		t.Fatalf("empty string: want empty, got %q", got)
	}
	if got := changes.PadField("val"); got != " val" {
		t.Fatalf("non-empty: want \" val\", got %q", got)
	}
}
