package runtime

import (
	"context"
	"testing"

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
