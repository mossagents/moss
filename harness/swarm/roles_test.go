package swarm

import (
	"context"
	"testing"

	"github.com/mossagents/moss/harness"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
)

func TestDefaultResearchRolePack(t *testing.T) {
	pack := DefaultResearchRolePack()
	if len(pack) != 5 {
		t.Fatalf("expected 5 roles, got %d", len(pack))
	}
	for _, spec := range pack {
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate(%s): %v", spec.AgentName, err)
		}
		if spec.Protocol.DefaultContract.Role != spec.Protocol.Role {
			t.Fatalf("role mismatch: %+v", spec)
		}
	}
}

func TestInstallRolePackRegistersSubagents(t *testing.T) {
	k := kernel.New()
	if err := InstallRolePack(k, DefaultResearchRolePack()...); err != nil {
		t.Fatalf("InstallRolePack: %v", err)
	}
	catalog := harness.SubagentCatalogOf(k)
	for _, name := range []string{"swarm-planner", "swarm-supervisor", "swarm-worker", "swarm-synthesizer", "swarm-reviewer"} {
		if _, ok := catalog.Get(name); !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}

func TestRolePackFeatureInstallsDelegationSubstrate(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(kt.NewRecorderIO()),
	)
	h := harness.New(k, nil)
	if err := h.Install(context.Background(), RolePackFeature(DefaultResearchRolePack()...)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if _, ok := harness.SubagentCatalogOf(k).Get("swarm-supervisor"); !ok {
		t.Fatal("expected swarm-supervisor to be registered")
	}
	if _, ok := k.ToolRegistry().Get("delegate_agent"); !ok {
		t.Fatal("expected delegation substrate after boot")
	}
}
