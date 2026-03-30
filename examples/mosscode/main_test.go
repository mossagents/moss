package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

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
