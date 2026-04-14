package runtime

import (
	"github.com/mossagents/moss/capability"
	"github.com/mossagents/moss/extensions/skill"
	"github.com/mossagents/moss/internal/runtime/capability"
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
	return runtimecapability.LookupSkillManifests(k)
}
