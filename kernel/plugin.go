package kernel

import (
	"github.com/mossagents/moss/kernel/hooks"
	kplugin "github.com/mossagents/moss/kernel/plugin"
)

// Plugin re-exports the shared lifecycle extension model as the canonical kernel name.
type Plugin = kplugin.Plugin

// installPlugin 将 Plugin 中所有非 nil 的 hook / interceptor 注册到 Registry。
func installPlugin(reg *hooks.Registry, p Plugin) {
	kplugin.Install(reg, p)
}

func validatePlugin(p Plugin) error {
	return kplugin.Validate(p)
}
