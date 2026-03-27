package appkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mossagents/moss/bootstrap"
	"github.com/mossagents/moss/extensions/bootstrapctx"
	"github.com/mossagents/moss/extensions/compactx"
	"github.com/mossagents/moss/extensions/contextx"
	"github.com/mossagents/moss/extensions/knowledgex"
	"github.com/mossagents/moss/extensions/memoryx"
	"github.com/mossagents/moss/extensions/planningx"
	"github.com/mossagents/moss/extensions/scheduling"
	"github.com/mossagents/moss/extensions/sessionstore"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/scheduler"
)

// Installer 在 Kernel 创建后执行扩展安装逻辑。
type Installer func(context.Context, *kernel.Kernel) error

// Extension 描述 appkit 层统一的扩展装配单元。
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

// WithKernelOptions 将原始 kernel.Option 纳入 appkit 统一装配路径。
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

// WithPlanning 装配 write_todos 规划工具。
func WithPlanning() Extension {
	return WithKernelOptions(planningx.WithSessionManager(nil))
}

// WithContextOffload 装配上下文 offload（压缩）工具。
// 依赖可持久化的 SessionStore（建议与 WithSessionStore 配套使用）。
func WithContextOffload(store session.SessionStore) Extension {
	return extensionFunc(func(plan *extensionPlan) {
		plan.options = append(plan.options, compactx.WithSessionStore(store))
		plan.installers = append(plan.installers, func(_ context.Context, k *kernel.Kernel) error {
			return compactx.RegisterTools(k.ToolRegistry(), store, k.SessionManager())
		})
	})
}

// WithContextManagement 装配自动上下文压缩与 compact_conversation 工具。
func WithContextManagement(store session.SessionStore, opts ...contextx.Option) Extension {
	return extensionFunc(func(plan *extensionPlan) {
		ko := []kernel.Option{
			contextx.WithSessionStore(store),
		}
		if len(opts) > 0 {
			ko = append(ko, contextx.Configure(opts...))
		}
		plan.options = append(plan.options, ko...)
	})
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

// WithPersistentMemories 装配 /memories 命名空间的持久化工具。
// memoriesDir 指向持久化目录（建议位于应用数据目录下）。
func WithPersistentMemories(memoriesDir string) Extension {
	return AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
		if memoriesDir == "" {
			return fmt.Errorf("memories dir is empty")
		}
		absDir, err := filepath.Abs(memoriesDir)
		if err != nil {
			return fmt.Errorf("resolve memories dir: %w", err)
		}
		if err := os.MkdirAll(absDir, 0755); err != nil {
			return fmt.Errorf("create memories dir: %w", err)
		}
		sb, err := sandbox.NewLocal(absDir)
		if err != nil {
			return fmt.Errorf("memory sandbox: %w", err)
		}
		ws := sandbox.NewLocalWorkspace(sb)
		memoryx.WithWorkspace(ws)(k)
		return memoryx.RegisterTools(k.ToolRegistry(), ws)
	})
}
