package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/testing"
)

func TestRenderSkillsSummaryShowsOnlyUserSkills(t *testing.T) {
	k := kernel.New(
		kernel.WithUserIO(&port.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := runtime.Setup(context.Background(), k, ".", runtime.WithSkills(false), runtime.WithMCPServers(false)); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	out := renderSkillsSummary(&agentState{k: k}, ".")
	if strings.Contains(out, "Runtime builtin tools:") {
		t.Fatalf("expected no runtime builtin tools section, got %q", out)
	}
	if strings.Contains(out, "```") {
		t.Fatalf("summary should be plain text without markdown fences, got %q", out)
	}
}

func TestRenderSkillsSummaryUsesStatusIcons(t *testing.T) {
	k := kernel.New(
		kernel.WithUserIO(&port.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := runtime.Setup(context.Background(), k, ".", runtime.WithSkills(false), runtime.WithMCPServers(false)); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	out := renderSkillsSummary(&agentState{k: k}, ".")
	if strings.Contains(out, "[active]") || strings.Contains(out, "[inactive]") {
		t.Fatalf("expected icon-based statuses instead of bracket labels, got %q", out)
	}
	if !strings.Contains(out, "Direct slash usage:") || !strings.Contains(out, "/skill <name> <task...>") || !strings.Contains(out, "/<skill_or_tool_name> <task...>") {
		t.Fatalf("expected direct slash usage guidance in skills summary, got %q", out)
	}
}

func TestWelcomeViewIncludesConfiguredBanner(t *testing.T) {
	m := newWelcomeModel("openai-completions", "deepseek", "gpt-4o", ".", "MOSSCODE BANNER")
	m.width = 120

	out := m.View()
	if !strings.Contains(out, "MOSSCODE BANNER") {
		t.Fatalf("expected configured banner in welcome view, got %q", out)
	}
	if !strings.Contains(out, "Session setup") || !strings.Contains(out, "Ready when you are") {
		t.Fatalf("expected redesigned welcome shell, got %q", out)
	}
}

func TestSwitchProfileRejectsActiveRun(t *testing.T) {
	m := appModel{
		state: stateChat,
		config: Config{
			Workspace:    ".",
			Profile:      "default",
			Trust:        "trusted",
			ApprovalMode: "confirm",
		},
		chat:  newChatModel("openai", "gpt-4o", "."),
		agent: &agentState{running: true},
	}

	updated, cmd := m.Update(switchProfileMsg{profile: "research"})
	if cmd != nil {
		t.Fatal("expected no async command when switch is rejected")
	}
	next := updated.(appModel)
	if len(next.chat.messages) == 0 {
		t.Fatal("expected error message")
	}
	last := next.chat.messages[len(next.chat.messages)-1]
	if !strings.Contains(last.content, "cannot switch profile while a run is active") {
		t.Fatalf("unexpected message: %q", last.content)
	}
}

func TestPlanPostureRebuildRequestsRuntimeRebuildOnMismatch(t *testing.T) {
	current := postureFromRuntime("default", "trusted", "confirm", runtime.ResolveExecutionPolicyForWorkspace("", "trusted", "confirm"))
	target := runtime.SessionPostureFromSession(&session.Session{
		ID: "sess-1",
		Config: session.SessionConfig{
			Profile:    "readonly",
			TrustLevel: "restricted",
			Metadata: map[string]any{
				session.MetadataEffectiveTrust:    "restricted",
				session.MetadataEffectiveApproval: "read-only",
				session.MetadataTaskMode:          "readonly",
				session.MetadataExecutionPolicy:   runtime.ResolveExecutionPolicyForWorkspace("", "restricted", "read-only"),
			},
		},
	})

	plan, err := planPostureRebuild("sess-1", current, target)
	if err != nil {
		t.Fatalf("planPostureRebuild: %v", err)
	}
	if !plan.Rebuild {
		t.Fatal("expected posture mismatch to require runtime rebuild")
	}
	if !strings.Contains(plan.Notice, "auto-rebuilt") {
		t.Fatalf("unexpected rebuild notice: %q", plan.Notice)
	}
}

func TestPlanPostureRebuildLegacyWarns(t *testing.T) {
	current := postureFromRuntime("coding", "restricted", "full-auto", runtime.ResolveExecutionPolicyForWorkspace("", "restricted", "full-auto"))
	target := runtime.SessionPostureFromSession(&session.Session{
		ID: "legacy-1",
		Config: session.SessionConfig{
			TrustLevel: "restricted",
		},
	})

	plan, err := planPostureRebuild("legacy-1", current, target)
	if err != nil {
		t.Fatalf("planPostureRebuild: %v", err)
	}
	if plan.Rebuild {
		t.Fatal("legacy session should not trigger rebuild")
	}
	if !strings.Contains(plan.Notice, "predates profile persistence") {
		t.Fatalf("expected legacy warning, got %q", plan.Notice)
	}
}
