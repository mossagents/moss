package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	kt "github.com/mossagents/moss/testing"
)

func TestRenderSkillsSummaryIncludesRuntimeBuiltinTools(t *testing.T) {
	k := kernel.New(
		kernel.WithUserIO(&port.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := runtime.Setup(context.Background(), k, ".", runtime.WithSkills(false), runtime.WithMCPServers(false)); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	out := renderSkillsSummary(&agentState{k: k}, ".")
	if !strings.Contains(out, "Runtime builtin tools:") {
		t.Fatalf("expected runtime builtin tools section, got %q", out)
	}
	if !strings.Contains(out, "read_file") || !strings.Contains(out, "http_request") {
		t.Fatalf("expected builtin tools in summary, got %q", out)
	}
}
