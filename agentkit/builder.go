package agentkit

import (
	"context"
	"fmt"

	"github.com/mossagi/moss/adapters"
	"github.com/mossagi/moss/extensions/defaults"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/retry"
	"github.com/mossagi/moss/kernel/sandbox"
)

// BuildConfig 描述 agentkit.BuildKernel 的可选默认装配行为。
type BuildConfig struct {
	// DefaultLLMRetry 会在未显式禁用时注入 kernel.WithLLMRetry。
	DefaultLLMRetry *retry.Config

	// DefaultSetupOptions 控制默认扩展装配行为。
	// 这些选项会传递给 extensions/defaults.Setup。
	DefaultSetupOptions []defaults.Option

	// Extensions 描述 agentkit 层统一的推荐扩展装配单元。
	// 它们可同时携带 kernel.Option 与 build 后安装动作。
	Extensions []Extension
}

// BuildKernel 根据 AppFlags 构建标准 Kernel，并装配官方默认扩展。
//
// 这是推荐的快速构建方式，自动完成：
//   - 构建 LLM adapter
//   - 创建本地 Sandbox
//   - 装配内置工具 + MCP servers + Skills
//
// 调用者仍可通过 extraOpts 追加底层 kernel.Option。
// 若要使用统一的官方扩展装配路径，请优先使用 BuildKernelWithExtensions。
func BuildKernel(ctx context.Context, flags *AppFlags, io port.UserIO, extraOpts ...kernel.Option) (*kernel.Kernel, error) {
	return BuildKernelWithConfig(ctx, flags, io, BuildConfig{}, extraOpts...)
}

// BuildKernelWithExtensions 根据 AppFlags 构建 Kernel，并按顺序装配 agentkit 扩展。
func BuildKernelWithExtensions(ctx context.Context, flags *AppFlags, io port.UserIO, exts ...Extension) (*kernel.Kernel, error) {
	return BuildKernelWithConfig(ctx, flags, io, BuildConfig{
		Extensions: exts,
	})
}

// BuildKernelWithConfig 在标准装配基础上，允许附加 agentkit 级默认行为。
//
// 这用于把常见运行时默认值收敛在 agentkit，而不是散落在各个示例应用中。
func BuildKernelWithConfig(ctx context.Context, flags *AppFlags, io port.UserIO, cfg BuildConfig, extraOpts ...kernel.Option) (*kernel.Kernel, error) {
	llm, err := adapters.BuildLLM(flags.Provider, flags.Model, flags.APIKey, flags.BaseURL)
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
	if cfg.DefaultLLMRetry != nil {
		opts = append(opts, kernel.WithLLMRetry(*cfg.DefaultLLMRetry))
	}
	opts = append(opts, extraOpts...)
	plan := extensionPlan{}
	for _, ext := range cfg.Extensions {
		if ext != nil {
			ext.apply(&plan)
		}
	}
	opts = append(opts, plan.options...)

	k := kernel.New(opts...)

	if err := defaults.Setup(ctx, k, flags.Workspace, cfg.DefaultSetupOptions...); err != nil {
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

	return k, nil
}
