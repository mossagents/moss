package skillsx

import (
	"context"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/skill"
)

const stateKey kernel.ExtensionStateKey = "skillsx.state"

type state struct {
	manager *skill.Manager
}

// WithManager 替换当前 Skill Manager。
func WithManager(m *skill.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).manager = m
	}
}

// Manager 返回当前 Kernel 绑定的 Skill Manager。
func Manager(k *kernel.Kernel) *skill.Manager {
	return ensureState(k).manager
}

// Deps 返回 Skill 注册所需的依赖集合。
func Deps(k *kernel.Kernel) skill.Deps {
	return skill.Deps{
		ToolRegistry: k.ToolRegistry(),
		Middleware:   k.Middleware(),
		Sandbox:      k.Sandbox(),
		UserIO:       k.UserIO(),
		Workspace:    k.Workspace(),
		Executor:     k.Executor(),
	}
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{
		manager: skill.NewManager(),
	})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnShutdown(300, func(ctx context.Context, _ *kernel.Kernel) error {
		if st.manager == nil {
			return nil
		}
		return st.manager.ShutdownAll(ctx)
	})
	bridge.OnSystemPrompt(200, func(_ *kernel.Kernel) string {
		if st.manager == nil {
			return ""
		}
		return st.manager.SystemPromptAdditions()
	})
	return st
}
