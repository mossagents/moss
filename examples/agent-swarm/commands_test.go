package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/harness/config"
)

func TestDemoRunInspectExportAndResume(t *testing.T) {
	setupTestAppHome(t)
	ctx := context.Background()
	runID := "swarm-test-demo"

	runCfg, err := parseRunCommand([]string{"--demo", "--run-id", runID})
	if err != nil {
		t.Fatalf("parse run command: %v", err)
	}
	if err := runRunCommand(runCfg); err != nil {
		t.Fatalf("run demo command: %v", err)
	}

	env, err := openSnapshotEnv()
	if err != nil {
		t.Fatalf("open snapshot env: %v", err)
	}
	defer env.Close(ctx)

	target, err := env.Targets.ResolveForInspect(ctx, "", runID, false)
	if err != nil {
		t.Fatalf("resolve inspect target: %v", err)
	}
	snapshot, err := env.Recovery.Load(ctx, target)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != "completed" {
		t.Fatalf("expected completed snapshot, got %q", snapshot.Status)
	}
	if snapshot.ExecutionMode != "demo" {
		t.Fatalf("expected demo execution mode, got %q", snapshot.ExecutionMode)
	}
	if snapshot.Recoverable {
		t.Fatal("completed demo run must not remain recoverable")
	}
	if snapshot.FinalArtifactName != "final-report.md" {
		t.Fatalf("expected final-report.md, got %q", snapshot.FinalArtifactName)
	}

	report, err := buildInspectReport(ctx, env, snapshot, "threads", "")
	if err != nil {
		t.Fatalf("build threads report: %v", err)
	}
	if len(report.Threads) != 7 {
		t.Fatalf("expected 7 threads, got %d", len(report.Threads))
	}

	var workerThreadID string
	for _, thread := range snapshot.Snapshot.Threads {
		if thread.ThreadRole == "worker" {
			workerThreadID = thread.SessionID
			break
		}
	}
	if workerThreadID == "" {
		t.Fatal("expected at least one worker thread")
	}
	threadReport, err := buildInspectReport(ctx, env, snapshot, "thread", workerThreadID)
	if err != nil {
		t.Fatalf("build thread report: %v", err)
	}
	if len(threadReport.Artifacts) < 3 {
		t.Fatalf("expected worker thread to publish at least 3 artifacts, got %d", len(threadReport.Artifacts))
	}

	eventsReport, err := buildInspectReport(ctx, env, snapshot, "events", "")
	if err != nil {
		t.Fatalf("build events report: %v", err)
	}
	if len(eventsReport.Events) == 0 {
		t.Fatal("expected persisted runtime events")
	}

	bundleDir := filepath.Join(config.AppDir(), "bundle-export")
	bundleCfg, err := parseExportCommand([]string{"--run-id", runID, "--format", "bundle", "--output", bundleDir, "--include-payloads"})
	if err != nil {
		t.Fatalf("parse bundle export: %v", err)
	}
	if err := runExportCommand(bundleCfg); err != nil {
		t.Fatalf("run bundle export: %v", err)
	}
	assertFileExists(t, filepath.Join(bundleDir, "summary.json"))
	assertFileExists(t, filepath.Join(bundleDir, "artifacts.json"))
	assertFileExists(t, filepath.Join(bundleDir, "events.jsonl"))
	assertFileExists(t, filepath.Join(bundleDir, "final-report.md"))
	assertDirHasEntries(t, filepath.Join(bundleDir, "payloads"))

	jsonDir := filepath.Join(config.AppDir(), "json-export")
	jsonCfg, err := parseExportCommand([]string{"--run-id", runID, "--format", "json", "--output", jsonDir})
	if err != nil {
		t.Fatalf("parse json export: %v", err)
	}
	if err := runExportCommand(jsonCfg); err != nil {
		t.Fatalf("run json export: %v", err)
	}
	assertFileExists(t, filepath.Join(jsonDir, "summary.json"))
	assertFileExists(t, filepath.Join(jsonDir, "artifacts.json"))

	jsonlDir := filepath.Join(config.AppDir(), "jsonl-export")
	jsonlCfg, err := parseExportCommand([]string{"--run-id", runID, "--format", "jsonl", "--output", jsonlDir})
	if err != nil {
		t.Fatalf("parse jsonl export: %v", err)
	}
	if err := runExportCommand(jsonlCfg); err != nil {
		t.Fatalf("run jsonl export: %v", err)
	}
	assertFileExists(t, filepath.Join(jsonlDir, "events.jsonl"))

	resumeCfg, err := parseResumeCommand([]string{"--run-id", runID})
	if err != nil {
		t.Fatalf("parse resume command: %v", err)
	}
	if err := runResumeCommand(resumeCfg); err == nil {
		t.Fatal("expected resume of completed run to fail")
	}
}

func setupTestAppHome(t *testing.T) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create test home: %v", err)
	}
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file", path)
	}
}

func assertDirHasEntries(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read dir %s: %v", path, err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected %s to contain exported payloads", path)
	}
}
