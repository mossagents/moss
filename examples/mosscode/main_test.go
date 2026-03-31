package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/product"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
)

func TestRunCheckpointListJSON(t *testing.T) {
	appconfig.SetAppName(appName)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	store, err := port.NewFileCheckpointStore(product.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	if _, err := store.Create(context.Background(), port.CheckpointCreateRequest{
		SessionID: "sess-1",
		Note:      "before risky change",
	}); err != nil {
		t.Fatalf("Create checkpoint: %v", err)
	}

	cfg := &config{
		flags:          &appkit.AppFlags{},
		governance:     product.DefaultGovernanceConfig(),
		checkpointArgs: []string{"list", "--json"},
	}
	out, err := captureStdout(func() error {
		return runCheckpoint(context.Background(), cfg)
	})
	if err != nil {
		t.Fatalf("runCheckpoint list: %v", err)
	}
	if !strings.Contains(out, "\"mode\": \"list\"") || !strings.Contains(out, "\"checkpoints\"") {
		t.Fatalf("unexpected checkpoint list json: %s", out)
	}
}

func TestRunCheckpointCreateRequiresSession(t *testing.T) {
	cfg := &config{
		flags:          &appkit.AppFlags{},
		governance:     product.DefaultGovernanceConfig(),
		checkpointArgs: []string{"create"},
	}
	err := runCheckpoint(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "usage: mosscode checkpoint create") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunApplyRequiresPatchFile(t *testing.T) {
	cfg := &config{
		flags:      &appkit.AppFlags{},
		governance: product.DefaultGovernanceConfig(),
		applyArgs:  []string{},
	}
	err := runApply(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "usage: mosscode apply") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunChangesListJSON(t *testing.T) {
	appconfig.SetAppName(appName)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	repo := initRepoForCLIChangeTests(t)

	store, err := product.OpenChangeStore()
	if err != nil {
		t.Fatalf("OpenChangeStore: %v", err)
	}
	err = store.Save(context.Background(), &product.ChangeOperation{
		ID:           "change-1",
		RepoRoot:     repo,
		BaseHeadSHA:  "abc123",
		PatchID:      "patch-1",
		Summary:      "update tracked.txt",
		Status:       product.ChangeStatusApplied,
		TargetFiles:  []string{"tracked.txt"},
		RecoveryMode: "patch+capture",
		CreatedAt:    time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Save change operation: %v", err)
	}

	cfg := &config{
		flags:       &appkit.AppFlags{},
		governance:  product.DefaultGovernanceConfig(),
		changesArgs: []string{"list", "--json", "--workspace", repo},
	}
	out, err := captureStdout(func() error {
		return runChanges(context.Background(), cfg)
	})
	if err != nil {
		t.Fatalf("runChanges list: %v", err)
	}
	if !strings.Contains(out, "\"mode\": \"list\"") || !strings.Contains(out, "\"change-1\"") {
		t.Fatalf("unexpected changes list json: %s", out)
	}
}

func TestRunChangesShowJSON(t *testing.T) {
	appconfig.SetAppName(appName)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	repo := initRepoForCLIChangeTests(t)

	store, err := product.OpenChangeStore()
	if err != nil {
		t.Fatalf("OpenChangeStore: %v", err)
	}
	err = store.Save(context.Background(), &product.ChangeOperation{
		ID:           "change-1",
		RepoRoot:     repo,
		BaseHeadSHA:  "abc123",
		PatchID:      "patch-1",
		Summary:      "update tracked.txt",
		Status:       product.ChangeStatusApplied,
		TargetFiles:  []string{"tracked.txt"},
		RecoveryMode: "patch+capture",
		CreatedAt:    time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Save change operation: %v", err)
	}

	cfg := &config{
		flags:       &appkit.AppFlags{},
		governance:  product.DefaultGovernanceConfig(),
		changesArgs: []string{"show", "--json", "--workspace", repo, "change-1"},
	}
	out, err := captureStdout(func() error {
		return runChanges(context.Background(), cfg)
	})
	if err != nil {
		t.Fatalf("runChanges show: %v", err)
	}
	if !strings.Contains(out, "\"mode\": \"show\"") || !strings.Contains(out, "\"patch_id\": \"patch-1\"") {
		t.Fatalf("unexpected changes show json: %s", out)
	}
}

func TestRunRollbackRequiresChange(t *testing.T) {
	cfg := &config{
		flags:        &appkit.AppFlags{},
		governance:   product.DefaultGovernanceConfig(),
		rollbackArgs: []string{},
	}
	err := runRollback(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "usage: mosscode rollback") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func initRepoForCLIChangeTests(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitMainTest(t, repo, "init")
	runGitMainTest(t, repo, "config", "user.email", "test@example.com")
	runGitMainTest(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(repo+"\\tracked.txt", []byte("one\n"), 0600); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitMainTest(t, repo, "add", "tracked.txt")
	runGitMainTest(t, repo, "commit", "-m", "initial")
	return repo
}

func runGitMainTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func captureStdout(run func() error) (string, error) {
	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	runErr := run()
	_ = w.Close()
	os.Stdout = original
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), runErr
}
