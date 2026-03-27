package bootstrapctx

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/kernel"
)

// WithContext 将引导上下文作为标准扩展接入 Kernel。
// Deprecated: use runtime.WithBootstrapContext.
func WithContext(ctx *bootstrap.Context) kernel.Option {
	return runtime.WithBootstrapContext(ctx)
}

// WithLoadedContext 根据工作区和应用名加载引导上下文并接入 Kernel。
// Deprecated: use runtime.WithLoadedBootstrapContext.
func WithLoadedContext(workspace, appName string) kernel.Option {
	return runtime.WithLoadedBootstrapContext(workspace, appName)
}

// Load 返回指定工作区和应用名对应的引导上下文。
// Deprecated: use runtime.LoadBootstrapContext.
func Load(workspace, appName string) *bootstrap.Context {
	return runtime.LoadBootstrapContext(workspace, appName)
}
