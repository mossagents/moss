package product

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/sandbox"
)

func TestApplyChangeAndReviewAndRollback(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	repo := initTestRepo(t)
	patch := makeTrackedPatch(t, repo, "tracked.txt", "one\n", "two\n")

	rt := ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
	}

	applied, err := ApplyChange(ctx, rt, ApplyChangeRequest{
		Patch:   patch,
		Summary: "update tracked.txt",
	})
	if err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if applied.Status != ChangeStatusApplied {
		t.Fatalf("unexpected apply status %q", applied.Status)
	}
	if applied.PatchID == "" {
		t.Fatal("expected patch id")
	}
	if got := readTestFile(t, filepath.Join(repo, "tracked.txt")); got != "two\n" {
		t.Fatalf("unexpected applied file content %q", got)
	}

	report, err := BuildReviewReport(ctx, repo, []string{"changes"})
	if err != nil {
		t.Fatalf("BuildReviewReport changes: %v", err)
	}
	if len(report.Changes) != 1 || report.Changes[0].ID != applied.ID {
		t.Fatalf("unexpected review changes output %+v", report.Changes)
	}
	detail, err := BuildReviewReport(ctx, repo, []string{"change", applied.ID})
	if err != nil {
		t.Fatalf("BuildReviewReport change: %v", err)
	}
	if detail.Change == nil || detail.Change.ID != applied.ID {
		t.Fatalf("unexpected review change detail %+v", detail.Change)
	}

	rolledBack, err := RollbackChange(ctx, rt, RollbackChangeRequest{ChangeID: applied.ID})
	if err != nil {
		t.Fatalf("RollbackChange: %v", err)
	}
	if rolledBack.Status != ChangeStatusRolledBack {
		t.Fatalf("unexpected rollback status %q", rolledBack.Status)
	}
	if rolledBack.RollbackMode != RollbackModeExact {
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

	rt := ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
	}

	_, err := ApplyChange(ctx, rt, ApplyChangeRequest{
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

	rt := ChangeRuntime{
		Workspace:        repo,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(repo),
		PatchApply:       sandbox.NewGitPatchApply(repo),
		PatchRevert:      sandbox.NewGitPatchRevert(repo),
	}

	applied, err := ApplyChange(ctx, rt, ApplyChangeRequest{
		Patch:   patch,
		Summary: "update tracked.txt",
	})
	if err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}

	journalPath := filepath.Join(repo, ".git", "moss-patches.json")
	if err := os.Remove(journalPath); err != nil {
		t.Fatalf("remove patch journal: %v", err)
	}

	item, err := RollbackChange(ctx, rt, RollbackChangeRequest{ChangeID: applied.ID})
	if err == nil {
		t.Fatal("expected rollback failure")
	}
	var opErr *ChangeOperationError
	if !strings.Contains(err.Error(), "exact rollback") {
		t.Fatalf("unexpected rollback error %v", err)
	}
	if !strings.Contains(err.Error(), "capture_head=") {
		t.Fatalf("expected manual recovery details in error, got %v", err)
	}
	if !asChangeOperationError(err, &opErr) {
		t.Fatalf("expected ChangeOperationError, got %T", err)
	}
	if item == nil || item.Status != ChangeStatusApplied {
		t.Fatalf("expected original applied operation, got %+v", item)
	}
}

func configureProductTestApp(t *testing.T) {
	t.Helper()
	appconfig.SetAppName("moss-product-test")
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

func asChangeOperationError(err error, target **ChangeOperationError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(*ChangeOperationError)
	if !ok {
		return false
	}
	*target = value
	return true
}

func TestSummarizeChange(t *testing.T) {
	summary := SummarizeChange(ChangeOperation{
		ID:           "change-1",
		RepoRoot:     "repo",
		BaseHeadSHA:  "abc123",
		SessionID:    "sess-1",
		PatchID:      "patch-1",
		CheckpointID: "cp-1",
		Summary:      "hello",
		TargetFiles:  []string{"tracked.txt"},
		Status:       ChangeStatusApplied,
		CreatedAt:    timeNowTest(),
	})
	if summary.ID != "change-1" || summary.PatchID != "patch-1" {
		t.Fatalf("unexpected summary %+v", summary)
	}
}

func timeNowTest() (out time.Time) {
	return time.Unix(10, 0).UTC()
}
