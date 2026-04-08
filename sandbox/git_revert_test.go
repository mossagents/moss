package sandbox

import (
	"context"
	kws "github.com/mossagents/moss/kernel/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestGitPatchRevert_RevertByPatchID(t *testing.T) {
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
	applied, err := applier.Apply(context.Background(), kws.PatchApplyRequest{
		Patch:    patch,
		Source:   kws.PatchSourceLLM,
		ThreeWay: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	reverter := NewGitPatchRevert(repo)
	reverted, err := reverter.Revert(context.Background(), kws.PatchRevertRequest{
		PatchID: applied.PatchID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reverted.Reverted || reverted.Mode != "patch" {
		t.Fatalf("unexpected revert result: %+v", reverted)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeTestNewlines(string(data)) != "one\n" {
		t.Fatalf("unexpected reverted content %q", string(data))
	}
}

func TestGitPatchRevert_RevertToCapture(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	file := filepath.Join(repo, "tracked.txt")
	writeFile(t, file, "one\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	capture, err := NewGitRepoStateCapture(repo).Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, file, "two\n")
	untracked := filepath.Join(repo, "new.txt")
	writeFile(t, untracked, "new\n")

	reverter := NewGitPatchRevert(repo)
	reverted, err := reverter.Revert(context.Background(), kws.PatchRevertRequest{
		Capture: capture,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reverted.Reverted || reverted.Mode != "capture" {
		t.Fatalf("unexpected capture revert result: %+v", reverted)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeTestNewlines(string(data)) != "one\n" {
		t.Fatalf("unexpected capture reverted content %q", string(data))
	}
	if _, err := os.Stat(untracked); !os.IsNotExist(err) {
		t.Fatalf("expected untracked file removed, stat err=%v", err)
	}
}

func TestGitPatchRevert_Unavailable(t *testing.T) {
	reverter := NewGitPatchRevert(t.TempDir())
	_, err := reverter.Revert(context.Background(), kws.PatchRevertRequest{
		PatchID: "missing",
	})
	if err != kws.ErrPatchRevertUnavailable {
		t.Fatalf("expected ErrPatchRevertUnavailable, got %v", err)
	}
}

func TestGitPatchRevert_PatchNotFound(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	reverter := NewGitPatchRevert(repo)
	_, err := reverter.Revert(context.Background(), kws.PatchRevertRequest{
		PatchID: "missing",
	})
	if err != kws.ErrPatchNotFound {
		t.Fatalf("expected ErrPatchNotFound, got %v", err)
	}
}
