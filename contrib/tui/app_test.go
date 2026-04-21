package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/harness/appkit/product"
	appconfig "github.com/mossagents/moss/harness/config"
	rpolicy "github.com/mossagents/moss/harness/runtime/policy"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

type markerObserver struct {
	observe.NoOpObserver
	name string
}

func installTestRuntime(t *testing.T, k *kernel.Kernel) {
	t.Helper()
	h, err := harness.NewWithBackendFactory(context.Background(), k, harness.NewLocalBackendFactory("."))
	if err != nil {
		t.Fatalf("NewWithBackendFactory: %v", err)
	}
	if err := h.Install(context.Background(), harness.RuntimeSetup(".", appconfig.TrustRestricted, harness.WithSkills(false), harness.WithMCPServers(false))); err != nil {
		t.Fatalf("Install: %v", err)
	}
}

func TestRenderSkillsSummaryShowsOnlyUserSkills(t *testing.T) {
	k := kernel.New(
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	installTestRuntime(t, k)

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
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(kt.NewMemorySandbox()),
	)
	installTestRuntime(t, k)

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

func TestBuildAgentUsesBootstrappedKernelObserver(t *testing.T) {
	k := kernel.New(
		kernel.WithUserIO(&io.NoOpIO{}),
	)
	kernelObserver := &markerObserver{name: "kernel"}
	configObserver := &markerObserver{name: "config"}
	k.SetObserver(kernelObserver)

	state := &kernelInitState{
		cfg:  Config{BaseObserver: configObserver},
		wCfg: WelcomeConfig{Workspace: ".", Model: "test-model"},
		k:    k,
	}

	agent := state.buildAgent()
	if agent.baseObserver != kernelObserver {
		t.Fatalf("expected agent to keep bootstrapped kernel observer, got %#v", agent.baseObserver)
	}
}

func TestPlanPostureRebuildRequestsRuntimeRebuildOnMismatch(t *testing.T) {
	k := kernel.New()
	if _, err := product.ApplyApprovalModeWithTrust(k, "trusted", "confirm"); err != nil {
		t.Fatalf("ApplyApprovalModeWithTrust: %v", err)
	}
	current := postureFromRuntime(k, "default", "trusted", "confirm")
	toolPolicyMeta, err := rpolicy.EncodeToolPolicyMetadata(rpolicy.ResolveToolPolicyForWorkspace("", "restricted", "read-only"))
	if err != nil {
		t.Fatalf("EncodeToolPolicyMetadata: %v", err)
	}
	targetSession := &session.Session{
		ID: "sess-1",
		Config: session.SessionConfig{
			Profile:    "readonly",
			TrustLevel: "restricted",
			Metadata: map[string]any{
				session.MetadataEffectiveTrust:    "restricted",
				session.MetadataEffectiveApproval: "read-only",
				session.MetadataTaskMode:          "readonly",
				session.MetadataToolPolicy:        toolPolicyMeta,
			},
		},
	}

	plan, err := planPostureRebuild("sess-1", current, targetSession)
	if err != nil {
		t.Fatalf("planPostureRebuild: %v", err)
	}
	if !plan.Rebuild {
		t.Fatal("expected posture mismatch to require runtime rebuild")
	}
	if plan.Profile != "readonly" {
		t.Fatalf("plan.Profile = %q, want readonly", plan.Profile)
	}
	if !strings.Contains(plan.Notice, "auto-rebuilt") {
		t.Fatalf("unexpected rebuild notice: %q", plan.Notice)
	}
}

func TestPlanPostureRebuildDefaultPostureRebuilds(t *testing.T) {
	k := kernel.New()
	if _, err := product.ApplyApprovalModeWithTrust(k, "restricted", "full-auto"); err != nil {
		t.Fatalf("ApplyApprovalModeWithTrust: %v", err)
	}
	current := postureFromRuntime(k, "coding", "restricted", "full-auto")
	targetSession := &session.Session{
		ID: "legacy-1",
		Config: session.SessionConfig{
			TrustLevel: "restricted",
		},
	}

	plan, err := planPostureRebuild("legacy-1", current, targetSession)
	if err != nil {
		t.Fatalf("planPostureRebuild: %v", err)
	}
	if !plan.Rebuild {
		t.Fatal("session without persisted posture should trigger rebuild to canonical defaults")
	}
	if plan.Profile != "default" {
		t.Fatalf("plan.Profile = %q, want default", plan.Profile)
	}
	if !strings.Contains(plan.Notice, "Runtime auto-rebuilt") {
		t.Fatalf("expected rebuild notice, got %q", plan.Notice)
	}
}

func TestSwitchModelMsgPersistsAndClearsOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	appDir := filepath.Join(home, ".moss")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}

	m := appModel{
		state: stateChat,
		config: Config{
			Provider:     appconfig.APITypeOpenAICompletions,
			ProviderName: "OpenAI",
			Model:        "gpt-4o",
			Workspace:    ".",
			Trust:        appconfig.TrustTrusted,
			ApprovalMode: "confirm",
		},
		chat: newChatModel("OpenAI (openai-completions)", "gpt-4o", "."),
	}
	m.chat.setProviderIdentity(appconfig.APITypeOpenAICompletions, "OpenAI")

	handled, model, _ := m.handleControlMessages(switchModelMsg{
		provider:     appconfig.APITypeClaude,
		providerName: "Anthropic",
		model:        "claude-sonnet-4.5",
	})
	if !handled {
		t.Fatal("expected switchModelMsg to be handled")
	}
	updated := model.(appModel)
	cfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.TUI.SelectedProvider != appconfig.APITypeClaude || cfg.TUI.SelectedProviderName != "Anthropic" || cfg.TUI.SelectedModel != "claude-sonnet-4.5" {
		t.Fatalf("unexpected persisted model override: %+v", cfg.TUI)
	}
	if updated.config.Provider != appconfig.APITypeClaude || updated.config.Model != "claude-sonnet-4.5" {
		t.Fatalf("unexpected updated runtime config: %+v", updated.config)
	}

	handled, _, _ = updated.handleControlMessages(switchModelMsg{
		provider:     appconfig.APITypeOpenAICompletions,
		providerName: "OpenAI",
		model:        "gpt-4o",
		auto:         true,
	})
	if !handled {
		t.Fatal("expected auto switchModelMsg to be handled")
	}
	cfg, err = appconfig.LoadGlobalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.TUI.SelectedProvider != "" || cfg.TUI.SelectedProviderName != "" || cfg.TUI.SelectedModel != "" {
		t.Fatalf("expected auto selection to clear override, got %+v", cfg.TUI)
	}
}
