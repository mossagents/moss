package runtime

import (
	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/kernel"
)

const bootstrapStateKey kernel.ExtensionStateKey = "bootstrap.state"

type bootstrapState struct {
	ctx *bootstrap.Context
}

func WithBootstrapContext(ctx *bootstrap.Context) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureBootstrapState(k).ctx = ctx
	}
}

func WithLoadedBootstrapContext(workspace, appName string) kernel.Option {
	return WithBootstrapContext(bootstrap.LoadWithAppName(workspace, appName))
}

func WithLoadedBootstrapContextWithTrust(workspace, appName, trust string) kernel.Option {
	return WithBootstrapContext(bootstrap.LoadWithAppNameAndTrust(workspace, appName, trust))
}

func LoadBootstrapContext(workspace, appName string) *bootstrap.Context {
	return bootstrap.LoadWithAppName(workspace, appName)
}

func LoadBootstrapContextWithTrust(workspace, appName, trust string) *bootstrap.Context {
	return bootstrap.LoadWithAppNameAndTrust(workspace, appName, trust)
}

func ensureBootstrapState(k *kernel.Kernel) *bootstrapState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(bootstrapStateKey, &bootstrapState{})
	st := actual.(*bootstrapState)
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
