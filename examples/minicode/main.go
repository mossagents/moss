// minicode 是一个类 Claude Code 的极简 Code Agent 示例（TUI）。
//
// 演示如何用 moss kernel 构建一个交互式编程助手：
//   - Bubble Tea TUI 交互界面
//   - 6 个内置工具（read_file, write_file, list_files, search_text, run_command, ask_user）
//   - 信任等级（trusted: 自动执行 / restricted: 危险操作需确认）
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider claude
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/mossagi/moss/agentkit"
	"github.com/mossagi/moss/kernel"
	appconfig "github.com/mossagi/moss/kernel/config"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/userio/tui"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

func main() {
	// 配置目录使用 ~/.minicode
	appconfig.SetAppName("minicode")
	_ = appconfig.EnsureAppDir()

	flags := agentkit.ParseAppFlags()

	if err := launchTUI(flags.Provider, flags.Model, flags.Workspace, flags.Trust, flags.APIKey, flags.BaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func launchTUI(provider, model, workspace, trust, apiKey, baseURL string) error {
	return tui.Run(tui.Config{
		Provider:          provider,
		Model:             model,
		Workspace:         workspace,
		Trust:             trust,
		BaseURL:           baseURL,
		APIKey:            apiKey,
		BuildKernel:       buildKernelWithIO,
		BuildSystemPrompt: buildSystemPrompt,
	})
}

func buildKernelWithIO(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
	ctx := context.Background()
	k, err := agentkit.BuildKernel(ctx, &agentkit.AppFlags{
		Provider:  provider,
		Model:     model,
		Workspace: wsDir,
		Trust:     trust,
		APIKey:    apiKey,
		BaseURL:   baseURL,
	}, io)
	if err != nil {
		return nil, err
	}

	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

// ─── System Prompt ──────────────────────────────────

func buildSystemPrompt(workspace string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, ctx)
}
