package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

func TestGitPatchApply_Apply(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "tracked.txt")
	writeFile(t, file, "one\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	writeFile(t, file, "two\n")
	patch := gitOutput(t, repo, "diff")
	runGit(t, repo, "checkout", "--", "tracked.txt")

	applier := NewGitPatchApply(repo)
	result, err := applier.Apply(context.Background(), port.PatchApplyRequest{
		Patch:    patch,
		Source:   port.PatchSourceLLM,
		ThreeWay: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatalf("expected applied result, got %+v", result)
	}
	if len(result.TargetFiles) != 1 || result.TargetFiles[0] != "tracked.txt" {
		t.Fatalf("unexpected target files: %+v", result.TargetFiles)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeTestNewlines(string(data)) != "two\n" {
		t.Fatalf("unexpected file content %q", string(data))
	}
}

func TestGitPatchApply_Failure(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "tracked.txt")
	writeFile(t, file, "one\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	writeFile(t, file, "two\n")
	patch := gitOutput(t, repo, "diff")
	runGit(t, repo, "checkout", "--", "tracked.txt")

	applier := NewGitPatchApply(repo)
	if _, err := applier.Apply(context.Background(), port.PatchApplyRequest{Patch: patch}); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	result, err := applier.Apply(context.Background(), port.PatchApplyRequest{Patch: patch})
	if err == nil {
		t.Fatal("expected second apply to fail")
	}
	if result == nil || result.Applied {
		t.Fatalf("expected failed result, got %+v", result)
	}
	if !strings.Contains(result.Error, "git apply") {
		t.Fatalf("expected git apply error, got %+v", result)
	}
}

func TestGitPatchApply_Unavailable(t *testing.T) {
	applier := NewGitPatchApply(t.TempDir())
	_, err := applier.Apply(context.Background(), port.PatchApplyRequest{Patch: "diff --git a/a b/a"})
	if err != port.ErrPatchApplyUnavailable {
		t.Fatalf("expected ErrPatchApplyUnavailable, got %v", err)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func normalizeTestNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
