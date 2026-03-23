package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/workspace"
)

func TestCollectRunRequestUsesDefaults(t *testing.T) {
	// goal, workspace (default), mode (default), trust (default), confirm
	input := strings.NewReader("analyze repository\n\n\n\ny\n")
	var output bytes.Buffer
	ui := newTerminalUI(input, &output)

	req, err := ui.collectRunRequest("/repo", domain.RunModeInteractive, workspace.TrustLevelTrusted)
	if err != nil {
		t.Fatalf("collectRunRequest returned error: %v", err)
	}

	if req.Goal != "analyze repository" {
		t.Fatalf("expected goal to be collected, got %q", req.Goal)
	}
	if req.Workspace != "/repo" {
		t.Fatalf("expected default workspace, got %q", req.Workspace)
	}
	if req.Mode != domain.RunModeInteractive {
		t.Fatalf("expected default mode, got %q", req.Mode)
	}
	if req.Trust != workspace.TrustLevelTrusted {
		t.Fatalf("expected default trust, got %q", req.Trust)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "moss interactive TUI") {
		t.Fatalf("expected TUI title in output, got %q", rendered)
	}
}

func TestCollectRunRequestCanCancel(t *testing.T) {
	input := strings.NewReader("q\n")
	ui := newTerminalUI(input, &bytes.Buffer{})

	_, err := ui.collectRunRequest("/repo", domain.RunModeInteractive, workspace.TrustLevelTrusted)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if err != errCancelled {
		t.Fatalf("expected errCancelled, got %v", err)
	}
}
