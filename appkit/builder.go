package appkit

import (
	"context"
	"fmt"
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/interaction"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/logging"
	providers "github.com/mossagents/moss/providers"
	"github.com/mossagents/moss/sandbox"
)

// BuildKernel 根据 AppFlags 构建标准 Kernel，并装配官方默认扩展。
//
// 这是推荐的快速构建方式，自动完成：
//   - 构建 LLM adapter
//   - 创建本地 Sandbox
//   - 装配内置工具 + MCP servers + Skills
//
// 调用者仍可通过 extraOpts 追加底层 kernel.Option。若要安装 appkit
// 层扩展，请使用 BuildKernelWithExtensions。
func BuildKernel(ctx context.Context, flags *AppFlags, io intr.UserIO, extraOpts ...kernel.Option) (*kernel.Kernel, error) {
	return buildKernel(ctx, flags, io, nil, extraOpts...)
}

// BuildKernelWithExtensions 根据 AppFlags 构建 Kernel，并按顺序装配 appkit 扩展。
//
// 这是官方推荐的扩展优先装配入口：优先通过 Extension 追加能力，而不是
// 暴露额外的构建分支给应用层。
func BuildKernelWithExtensions(ctx context.Context, flags *AppFlags, io intr.UserIO, exts ...Extension) (*kernel.Kernel, error) {
	return buildKernel(ctx, flags, io, exts)
}

func buildKernel(ctx context.Context, flags *AppFlags, io intr.UserIO, exts []Extension, extraOpts ...kernel.Option) (*kernel.Kernel, error) {
	llm, err := providers.BuildLLM(flags.EffectiveAPIType(), flags.Model, flags.APIKey, flags.BaseURL)
	if err != nil {
		return nil, err
	}

	sb, err := sandbox.NewLocal(flags.Workspace)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}

	opts := []kernel.Option{
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(io),
	}
	opts = append(opts, extraOpts...)
	plan := extensionPlan{}
	for _, ext := range exts {
		if ext != nil {
			ext.apply(&plan)
		}
	}
	opts = append(opts, plan.options...)

	k := kernel.New(opts...)

	setupOpts := append([]runtime.Option{runtime.WithWorkspaceTrust(flags.Trust)}, plan.runtimeOptions...)
	if err := runtime.Setup(ctx, k, flags.Workspace, setupOpts...); err != nil {
		return nil, err
	}
	for _, installer := range plan.installers {
		if installer == nil {
			continue
		}
		if err := installer(ctx, k); err != nil {
			return nil, err
		}
	}
	if logging.DebugEnabled() {
		k.Middleware().Use(builtins.Logger())
		logging.GetLogger().DebugContext(ctx, "kernel built",
			"workspace", flags.Workspace,
			"trust", flags.Trust,
			"profile", flags.Profile,
			"provider", flags.Provider,
			"model", flags.Model,
		)
	}

	return k, nil
}
