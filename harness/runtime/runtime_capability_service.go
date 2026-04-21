package runtime

import (
	"context"

	capability "github.com/mossagents/moss/harness/extensions/capability"
	"github.com/mossagents/moss/harness/extensions/skill"
	"github.com/mossagents/moss/harness/runtime/capstate"
	"github.com/mossagents/moss/kernel"
)

// LookupCapabilityManager returns the current capability manager if runtime
// capability state has already been assembled on the kernel.
func LookupCapabilityManager(k *kernel.Kernel) (*capability.Manager, bool) {
	return capability.LookupManager(k)
}

// LookupSkillManifests returns the currently remembered discovered skill
// manifests without creating capability state on first access.
func LookupSkillManifests(k *kernel.Kernel) []skill.Manifest {
	return capstate.LookupSkillManifests(k)
}

// ActivateSkill loads the named skill into the capability manager.
func ActivateSkill(ctx context.Context, k *kernel.Kernel, name string) error {
	return capstate.ActivateSkill(ctx, k, name)
}

// DeactivateSkill unregisters the named skill from the capability manager.
func DeactivateSkill(ctx context.Context, k *kernel.Kernel, name string) error {
	return capstate.DeactivateSkill(ctx, k, name)
}
