package agentkit

import (
	"context"

	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/extensions/bootstrapctx"
	"github.com/mossagents/moss/extensions/knowledgex"
	"github.com/mossagents/moss/extensions/scheduling"
	"github.com/mossagents/moss/extensions/sessionstore"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
	"github.com/mossagents/moss/scheduler"
)

// Installer 在 Kernel 创建后执行扩展安装逻辑。
type Installer func(context.Context, *kernel.Kernel) error

// Extension 描述 agentkit 层统一的扩展装配单元。
// 它可以同时提供 Kernel 选项和 build 后安装动作。
type Extension interface {
	apply(*extensionPlan)
}

type extensionPlan struct {
	options    []kernel.Option
	installers []Installer
}

type extensionFunc func(*extensionPlan)

func (f extensionFunc) apply(plan *extensionPlan) {
	f(plan)
}

// WithKernelOptions 将原始 kernel.Option 纳入 agentkit 统一装配路径。
func WithKernelOptions(opts ...kernel.Option) Extension {
	return extensionFunc(func(plan *extensionPlan) {
		plan.options = append(plan.options, opts...)
	})
}

// AfterBuild 在 Kernel 创建并完成默认扩展装配后执行自定义安装逻辑。
func AfterBuild(installer Installer) Extension {
	return extensionFunc(func(plan *extensionPlan) {
		plan.installers = append(plan.installers, installer)
	})
}

// WithSessionStore 按官方推荐方式装配 SessionStore 扩展。
func WithSessionStore(store session.SessionStore) Extension {
	return WithKernelOptions(sessionstore.WithStore(store))
}

// WithBootstrapContext 按官方推荐方式装配 bootstrap 上下文扩展。
func WithBootstrapContext(ctx *bootstrap.Context) Extension {
	return WithKernelOptions(bootstrapctx.WithContext(ctx))
}

// WithLoadedBootstrapContext 根据工作区和应用名加载 bootstrap 上下文并装配。
func WithLoadedBootstrapContext(workspace, appName string) Extension {
	return WithKernelOptions(bootstrapctx.WithLoadedContext(workspace, appName))
}

// WithScheduling 按官方推荐方式装配调度器扩展，并注册标准调度工具。
func WithScheduling(s *scheduler.Scheduler) Extension {
	return extensionFunc(func(plan *extensionPlan) {
		plan.options = append(plan.options, scheduling.WithScheduler(s))
		plan.installers = append(plan.installers, func(_ context.Context, k *kernel.Kernel) error {
			return scheduling.RegisterTools(k, s)
		})
	})
}

// WithKnowledge 按官方推荐方式注册知识库工具集。
func WithKnowledge(store knowledge.Store, embedder port.Embedder) Extension {
	return AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
		return knowledgex.RegisterTools(k, store, embedder)
	})
}
