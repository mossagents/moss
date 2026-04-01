package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	kt "github.com/mossagents/moss/testing"
)

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
		kernel.WithUserIO(&port.NoOpIO{}),
		kernel.WithSandbox(kt.NewMemorySandbox()),
	)
	if err := Setup(context.Background(), k, "."); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if SkillsManager(k) == nil {
		t.Fatal("expected skills manager")
	}
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
