package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type captureReporter struct {
	events []string
}

func (c *captureReporter) Report(_ context.Context, capability string, critical bool, state string, err error) {
	suffix := ""
	if err != nil {
		suffix = ":" + err.Error()
	}
	c.events = append(c.events, fmt.Sprintf("%s|%t|%s%s", capability, critical, state, suffix))
}

func TestResolve_ConflictSkillsAndProgressive(t *testing.T) {
	_, err := resolve(WithSkills(false), WithProgressiveSkills(true))
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestResolve_NilSessionStoreRejected(t *testing.T) {
	_, err := resolve(WithSessionStore(nil))
	if err == nil {
		t.Fatal("expected nil session store error")
	}
}

func TestSetup_UsesDefaultsParity(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&intr.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := Setup(context.Background(), k, "."); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if SkillsManager(k) == nil {
		t.Fatal("expected skills manager")
	}
}

func TestSetup_DefaultExecutionPolicyIsRestrictedConfirm(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&intr.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := Setup(context.Background(), k, "."); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	policy := ExecutionPolicyOf(k)
	if policy.Trust != appconfig.TrustRestricted {
		t.Fatalf("policy trust = %q, want %q", policy.Trust, appconfig.TrustRestricted)
	}
	if policy.ApprovalMode != "confirm" {
		t.Fatalf("policy approval = %q, want confirm", policy.ApprovalMode)
	}
	if !policy.Command.ClearEnv {
		t.Fatal("expected command env to be cleared by default")
	}
}

func TestSetup_ManagerReportsValidateReady(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&intr.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	reporter := &captureReporter{}
	if err := Setup(context.Background(), k, ".", WithCapabilityReporter(reporter)); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	foundValidate := false
	foundActivate := false
	for _, ev := range reporter.events {
		if strings.HasPrefix(ev, "runtime-validate|true|ready") {
			foundValidate = true
		}
		if strings.HasPrefix(ev, "runtime-activate|true|ready") {
			foundActivate = true
		}
	}
	if !foundValidate {
		t.Fatalf("expected runtime-validate ready event, got %v", reporter.events)
	}
	if !foundActivate {
		t.Fatalf("expected runtime-activate ready event, got %v", reporter.events)
	}
}

func TestSetup_PersistsCapabilitySnapshot(t *testing.T) {
	appconfig.SetAppName("moss-runtime-test")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&intr.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := Setup(context.Background(), k, "."); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	snapshot, err := LoadCapabilitySnapshot(CapabilityStatusPath())
	if err != nil {
		t.Fatalf("LoadCapabilitySnapshot: %v", err)
	}
	if len(snapshot.Items) == 0 {
		t.Fatal("expected persisted capability items")
	}
	foundBuiltin := false
	for _, item := range snapshot.Items {
		if item.Capability == "builtin-tools" && item.State == "ready" {
			foundBuiltin = true
			break
		}
	}
	if !foundBuiltin {
		t.Fatalf("expected builtin-tools ready in snapshot, got %+v", snapshot.Items)
	}
}

func TestSetup_ReportsBuiltinCriticalFailure(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&intr.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	_ = k.ToolRegistry().Register(toolSpecNoop("read_file"), toolHandlerNoop)
	reporter := &captureReporter{}
	err := Setup(context.Background(), k, ".", WithCapabilityReporter(reporter))
	if err == nil {
		t.Fatal("expected setup error when builtin tools registration conflicts")
	}
	found := false
	for _, ev := range reporter.events {
		if strings.HasPrefix(ev, "builtin-tools|true|failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected builtin-tools critical failure report, got %v", reporter.events)
	}
}

func TestSetup_ReportsDegradedOnOptionalSkillParseFailure(t *testing.T) {
	ws := t.TempDir()
	skillDir := filepath.Join(ws, ".agents", "skills", "broken-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: builtin-tools\n---\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}

	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&intr.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	reporter := &captureReporter{}
	if err := Setup(context.Background(), k, ws, WithCapabilityReporter(reporter), WithWorkspaceTrust(appconfig.TrustTrusted)); err != nil {
		t.Fatalf("setup should not fail on optional skill parse failure: %v", err)
	}
	found := false
	for _, ev := range reporter.events {
		if strings.Contains(ev, "degraded") && strings.Contains(ev, "skill:builtin-tools") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected degraded skill report, got %v", reporter.events)
	}
}

func toolSpecNoop(name string) tool.ToolSpec {
	return tool.ToolSpec{Name: name}
}

func toolHandlerNoop(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

// ---------------------------------------------------------------------------
// collectAgentDirs
// ---------------------------------------------------------------------------

func TestCollectAgentDirs_trusted_includesWorkspaceFirst(t *testing.T) {
	ws := t.TempDir()
	cfg := config{trust: appconfig.TrustTrusted}

	dirs := collectAgentDirs(ws, cfg)

	if len(dirs) == 0 {
		t.Fatal("expected at least one dir")
	}
	want := filepath.Join(ws, ".agents", "agents")
	if dirs[0] != want {
		t.Errorf("dirs[0]: want %q got %q", want, dirs[0])
	}
}

func TestCollectAgentDirs_trusted_alsoIncludesHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	cfg := config{trust: appconfig.TrustTrusted}

	dirs := collectAgentDirs(t.TempDir(), cfg)

	wantSuffix := filepath.Join(".moss", "agents")
	for _, d := range dirs {
		if strings.HasSuffix(d, wantSuffix) {
			return
		}
	}
	t.Errorf("home dir entry (~/.moss/agents) not found in %v; home=%s", dirs, home)
}

func TestCollectAgentDirs_restricted_excludesWorkspace(t *testing.T) {
	ws := t.TempDir()
	cfg := config{trust: appconfig.TrustRestricted}

	dirs := collectAgentDirs(ws, cfg)

	wsPrefix := filepath.Join(ws, ".agents")
	for _, d := range dirs {
		if strings.HasPrefix(d, wsPrefix) {
			t.Errorf("restricted trust should not include workspace agents dir, but got %q", d)
		}
	}
}

func TestCollectAgentDirs_restricted_includesHomeDir(t *testing.T) {
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home dir available")
	}
	cfg := config{trust: appconfig.TrustRestricted}

	dirs := collectAgentDirs(t.TempDir(), cfg)

	wantSuffix := filepath.Join(".moss", "agents")
	for _, d := range dirs {
		if strings.HasSuffix(d, wantSuffix) {
			return
		}
	}
	t.Errorf("~/.moss/agents not found in dirs: %v", dirs)
}

func TestCollectAgentDirs_emptyTrust_treatedAsTrusted(t *testing.T) {
	ws := t.TempDir()
	// empty trust normalises to TrustTrusted per NormalizeTrustLevel
	cfg := config{trust: ""}

	dirs := collectAgentDirs(ws, cfg)

	want := filepath.Join(ws, ".agents", "agents")
	if len(dirs) == 0 || dirs[0] != want {
		t.Errorf("dirs[0]: want %q got %v", want, dirs)
	}
}

func TestCollectAgentDirs_order_workspaceBeforeHome(t *testing.T) {
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home dir available")
	}
	ws := t.TempDir()
	cfg := config{trust: appconfig.TrustTrusted}

	dirs := collectAgentDirs(ws, cfg)

	if len(dirs) < 2 {
		t.Fatalf("expected at least 2 dirs, got %v", dirs)
	}
	wantFirst := filepath.Join(ws, ".agents", "agents")
	if dirs[0] != wantFirst {
		t.Errorf("workspace dir should be first: want %q, got %q", wantFirst, dirs[0])
	}
	wantSuffix := filepath.Join(".moss", "agents")
	if !strings.HasSuffix(dirs[1], wantSuffix) {
		t.Errorf("home dir should be second: want suffix %q, got %q", wantSuffix, dirs[1])
	}
}
