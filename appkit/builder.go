package appkit

import (
	"context"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/logging"
	providers "github.com/mossagents/moss/providers"
)

// BuildKernel 根据 AppFlags 构建标准 Kernel，并装配官方默认运行时能力。
//
// 这是推荐的快速构建方式，自动完成：
//   - 构建 LLM adapter
//   - 创建本地 Sandbox
//   - 装配内置工具 + MCP servers + Skills
//
// 如果调用者需要自定义 pre-backend kernel ports 或直接控制底层 kernel.Option
// 组装，请改用 kernel.New(...) + harness.NewWithBackendFactory(...)。
// 若要继续走 appkit 的 feature-first 入口，请使用 BuildKernelWithFeatures(...)
// 并通过 harness.KernelOptions(...) 传入 configure-phase kernel.Option。
func BuildKernel(ctx context.Context, flags *AppFlags, io io.UserIO) (*kernel.Kernel, error) {
	return buildKernel(ctx, flags, io, []harness.Feature{
		harness.RuntimeSetup(flags.Workspace, flags.Trust),
	})
}

// BuildKernelWithFeatures 根据 AppFlags 构建 Kernel，并按顺序安装 harness Feature。
//
// 这是官方推荐的 Feature 优先装配入口。官方 Feature 会按 phase/依赖
// 元数据做受控安装；未标注元数据的自定义 Feature 则保持 configure 阶段
// 语义，并在同阶段内按传入顺序安装。如果未包含 RuntimeSetup Feature，
// 则不会自动安装官方 runtime capability surface。需要 pre-backend port
// 注入的调用方应直接组合 kernel.New(...) 与 harness.NewWithBackendFactory(...)。
func BuildKernelWithFeatures(ctx context.Context, flags *AppFlags, io io.UserIO, features ...harness.Feature) (*kernel.Kernel, error) {
	return buildKernel(ctx, flags, io, features)
}

func buildKernel(ctx context.Context, flags *AppFlags, io io.UserIO, features []harness.Feature) (*kernel.Kernel, error) {
	llm, err := providers.BuildLLM(flags.EffectiveAPIType(), flags.Model, flags.APIKey, flags.BaseURL)
	if err != nil {
		return nil, err
	}

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(io),
	)

	installFeatures := append([]harness.Feature(nil), features...)
	if logging.DebugEnabled() {
		installFeatures = append(installFeatures, harness.Plugins(builtins.LoggerPlugin()))
	}

	h, err := harness.NewWithBackendFactory(ctx, k, harness.NewLocalBackendFactory(flags.Workspace))
	if err != nil {
		return nil, err
	}
	if err := h.Install(ctx, installFeatures...); err != nil {
		return nil, err
	}

	if logging.DebugEnabled() {
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
