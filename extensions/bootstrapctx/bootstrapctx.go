package bootstrapctx

import (
	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/kernel"
)

const stateKey kernel.ExtensionStateKey = "bootstrapctx.state"

type state struct {
	ctx *bootstrap.Context
}

// WithContext 将引导上下文作为标准扩展接入 Kernel。
func WithContext(ctx *bootstrap.Context) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).ctx = ctx
	}
}

// WithLoadedContext 根据工作区和应用名加载引导上下文并接入 Kernel。
func WithLoadedContext(workspace, appName string) kernel.Option {
	return WithContext(bootstrap.LoadWithAppName(workspace, appName))
}

// Load 返回指定工作区和应用名对应的引导上下文。
func Load(workspace, appName string) *bootstrap.Context {
	return bootstrap.LoadWithAppName(workspace, appName)
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnSystemPrompt(100, func(_ *kernel.Kernel) string {
		if st.ctx == nil {
			return ""
		}
		return st.ctx.SystemPromptSection()
	})
	return st
}
