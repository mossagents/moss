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

func TestRunCheckpointShowJSON(t *testing.T) {
	appconfig.SetAppName(appName)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	store, err := port.NewFileCheckpointStore(product.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	record, err := store.Create(context.Background(), port.CheckpointCreateRequest{
		SessionID: "sess-1",
		PatchIDs:  []string{"patch-1", "patch-2"},
		Metadata:  map[string]any{"source": "test"},
		Note:      "before inspect",
	})
	if err != nil {
		t.Fatalf("Create checkpoint: %v", err)
	}

	cfg := &config{
		flags:          &appkit.AppFlags{},
		governance:     product.DefaultGovernanceConfig(),
		checkpointArgs: []string{"show", record.ID, "--json"},
	}
	out, err := captureStdout(func() error {
		return runCheckpoint(context.Background(), cfg)
	})
	if err != nil {
		t.Fatalf("runCheckpoint show: %v", err)
	}
	if !strings.Contains(out, "\"mode\": \"show\"") || !strings.Contains(out, "\"checkpoint_detail\"") || !strings.Contains(out, "\"metadata_keys\": [") {
		t.Fatalf("unexpected checkpoint show json: %s", out)
	}
}

func TestRunCheckpointShowLatestJSON(t *testing.T) {
	appconfig.SetAppName(appName)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	store, err := port.NewFileCheckpointStore(product.CheckpointStoreDir())
	if err != nil {
		t.Fatalf("NewFileCheckpointStore: %v", err)
	}
	first, err := store.Create(context.Background(), port.CheckpointCreateRequest{SessionID: "sess-1", Note: "first"})
	if err != nil {
		t.Fatalf("Create first checkpoint: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := store.Create(context.Background(), port.CheckpointCreateRequest{SessionID: "sess-2", Note: "second"})
	if err != nil {
		t.Fatalf("Create second checkpoint: %v", err)
	}

	cfg := &config{
		flags:          &appkit.AppFlags{},
		governance:     product.DefaultGovernanceConfig(),
		checkpointArgs: []string{"show", "latest", "--json"},
	}
	out, err := captureStdout(func() error {
		return runCheckpoint(context.Background(), cfg)
	})
	if err != nil {
		t.Fatalf("runCheckpoint show latest: %v", err)
	}
	if !strings.Contains(out, second.ID) || strings.Contains(out, first.ID) {
		t.Fatalf("expected latest checkpoint %s in output, got %s", second.ID, out)
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

func TestApplyGovernanceEnvReadsFailoverSettings(t *testing.T) {
	t.Setenv("MOSSCODE_LLM_FAILOVER", "true")
	t.Setenv("MOSSCODE_LLM_FAILOVER_MAX_CANDIDATES", "3")
	t.Setenv("MOSSCODE_LLM_FAILOVER_RETRIES", "2")
	t.Setenv("MOSSCODE_LLM_FAILOVER_ON_BREAKER_OPEN", "false")

	cfg := product.DefaultGovernanceConfig()
	applyGovernanceEnv(&cfg, nil)

	if !cfg.LLMFailoverEnabled {
		t.Fatal("expected failover enabled from env")
	}
	if cfg.LLMFailoverMaxCandidates != 3 {
		t.Fatalf("max candidates = %d, want 3", cfg.LLMFailoverMaxCandidates)
	}
	if cfg.LLMFailoverPerCandidateRetries != 2 {
		t.Fatalf("per-candidate retries = %d, want 2", cfg.LLMFailoverPerCandidateRetries)
	}
	if cfg.LLMFailoverOnBreakerOpen {
		t.Fatal("expected breaker-open failover override to false")
	}
}

func TestRunDoctorJSONIncludesFailoverFields(t *testing.T) {
	cfg := &config{
		flags:      &appkit.AppFlags{Workspace: t.TempDir()},
		governance: product.DefaultGovernanceConfig(),
		doctorJSON: true,
	}
	cfg.governance.LLMFailoverEnabled = true

	out, err := captureStdout(func() error {
		return runDoctor(context.Background(), cfg)
	})
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if !strings.Contains(out, "\"failover_enabled\": true") {
		t.Fatalf("expected failover_enabled in doctor json, got %s", out)
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
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		_ = r.Close()
		done <- buf.String()
	}()
	runErr := run()
	_ = w.Close()
	os.Stdout = original
	return <-done, runErr
}
