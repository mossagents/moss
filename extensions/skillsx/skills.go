package skillsx

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/skill"
)

// WithManager 替换当前 Skill Manager。
func WithManager(m *skill.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		kernel.Extensions(k).SetSkillManager(m)
	}
}

// Manager 返回当前 Kernel 绑定的 Skill Manager。
func Manager(k *kernel.Kernel) *skill.Manager {
	return kernel.Extensions(k).SkillManager()
}

// Deps 返回 Skill 注册所需的依赖集合。
func Deps(k *kernel.Kernel) skill.Deps {
	return kernel.Extensions(k).SkillDeps()
}
