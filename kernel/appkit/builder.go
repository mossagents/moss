package appkit

import (
	"context"
	"fmt"
	"os"

	"github.com/mossagi/moss/adapters"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/retry"
	"github.com/mossagi/moss/kernel/sandbox"
)

// BuildConfig 描述 appkit.BuildKernel 的可选默认装配行为。
type BuildConfig struct {
	// DefaultLLMRetry 会在未显式禁用时注入 kernel.WithLLMRetry。
	DefaultLLMRetry *retry.Config
}

// BuildKernel 根据 AppFlags 构建标准 Kernel，注册默认工具和 MCP servers。
//
// 这是推荐的快速构建方式，自动完成：
//   - 构建 LLM adapter
//   - 创建本地 Sandbox
//   - 注册内置工具 + MCP servers + Skills
//
// 调用者仍可通过 extraOpts 追加自定义配置（如 WithScheduler、WithSessionStore）。
func BuildKernel(ctx context.Context, flags *AppFlags, io port.UserIO, extraOpts ...kernel.Option) (*kernel.Kernel, error) {
	return BuildKernelWithConfig(ctx, flags, io, BuildConfig{}, extraOpts...)
}

// BuildKernelWithConfig 在标准装配基础上，允许附加 appkit 级默认行为。
//
// 这用于把常见运行时默认值收敛在 appkit，而不是散落在各个示例应用中。
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

	k := kernel.New(opts...)

	if err := k.SetupWithDefaults(ctx, flags.Workspace, kernel.WithWarningWriter(os.Stderr)); err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}

	return k, nil
}
