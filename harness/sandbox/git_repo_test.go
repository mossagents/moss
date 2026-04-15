package sandbox

import (
	"context"
	"github.com/mossagents/moss/kernel/workspace"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitRepoStateCapture_Capture(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(repo, "tracked.txt"), "one\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	writeFile(t, filepath.Join(repo, ".gitignore"), "ignored.txt\n")
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-m", "ignore")

	writeFile(t, filepath.Join(repo, "tracked.txt"), "two\n")
	runGit(t, repo, "add", "tracked.txt")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "three\n")
	writeFile(t, filepath.Join(repo, "new.txt"), "new\n")
	writeFile(t, filepath.Join(repo, "ignored.txt"), "ignore me\n")

	capture := NewGitRepoStateCapture(repo)
	state, err := capture.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !state.IsDirty {
		t.Fatal("expected dirty repo")
	}
	if state.RepoRoot == "" || state.HeadSHA == "" {
		t.Fatalf("expected repo root and head sha: %+v", state)
	}
	if !containsFileState(state.Staged, "tracked.txt", "M") {
		t.Fatalf("expected staged tracked.txt, got %+v", state.Staged)
	}
	if !containsFileState(state.Unstaged, "tracked.txt", "M") {
		t.Fatalf("expected unstaged tracked.txt, got %+v", state.Unstaged)
	}
	if !containsString(state.Untracked, "new.txt") {
		t.Fatalf("expected untracked new.txt, got %+v", state.Untracked)
	}
	if !containsString(state.Ignored, "ignored.txt") {
		t.Fatalf("expected ignored.txt in ignored list, got %+v", state.Ignored)
	}
}

func TestGitRepoStateCapture_RepoUnavailable(t *testing.T) {
	capture := NewGitRepoStateCapture(t.TempDir())
	_, err := capture.Capture(context.Background())
	if err != workspace.ErrRepoUnavailable {
		t.Fatalf("expected ErrRepoUnavailable, got %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func containsFileState(items []workspace.RepoFileState, path, status string) bool {
	for _, item := range items {
		if item.Path == path && item.Status == status {
			return true
		}
	}
	return false
}
