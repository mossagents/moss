package skillsx

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/skill"
)

// Deprecated: use runtime.SkillsManager.
func Manager(k *kernel.Kernel) *skill.Manager { return runtime.SkillsManager(k) }

// Deprecated: use runtime.SetSkillManifests.
func SetManifests(k *kernel.Kernel, manifests []skill.Manifest) { runtime.SetSkillManifests(k, manifests) }

// Deprecated: use runtime.SkillManifests.
func Manifests(k *kernel.Kernel) []skill.Manifest { return runtime.SkillManifests(k) }

// Deprecated: use runtime.EnableProgressiveSkills.
func EnableProgressive(k *kernel.Kernel) { runtime.EnableProgressiveSkills(k) }

// Deprecated: use runtime.RegisterProgressiveSkillTools.
func RegisterProgressiveTools(k *kernel.Kernel) error { return runtime.RegisterProgressiveSkillTools(k) }

// Deprecated: use runtime.Deps.
func Deps(k *kernel.Kernel) skill.Deps { return runtime.Deps(k) }

// Deprecated: use runtime.WithSkillManager.
func WithManager(m *skill.Manager) kernel.Option {
	return runtime.WithSkillManager(m)
}
