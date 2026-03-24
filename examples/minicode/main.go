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
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/mossagi/moss/adapters/claude"
	adaptersopenai "github.com/mossagi/moss/adapters/openai"
	mossTUI "github.com/mossagi/moss/cmd/moss/tui"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/skill"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

func main() {
	// 配置目录使用 ~/.minicode
	skill.SetAppName("minicode")
	_ = skill.EnsureMossDir()

	provider := flag.String("provider", "", "LLM provider: claude|openai")
	model := flag.String("model", "", "Model name")
	workspace := flag.String("workspace", ".", "Workspace directory")
	trust := flag.String("trust", "trusted", "Trust level: trusted|restricted")
	apiKey := flag.String("api-key", "", "API key (overrides env)")
	baseURL := flag.String("base-url", "", "API base URL")
	flag.Parse()

	cfg, err := skill.LoadGlobalConfig()
	if err != nil || cfg == nil {
		cfg = &skill.Config{}
	}
	effectiveProvider := firstNonEmpty(*provider, cfg.Provider, "openai")
	effectiveModel := firstNonEmpty(*model, cfg.Model)
	effectiveAPIKey := firstNonEmpty(*apiKey, cfg.APIKey)
	effectiveBaseURL := firstNonEmpty(*baseURL, cfg.BaseURL)

	if err := launchTUI(effectiveProvider, effectiveModel, *workspace, *trust, effectiveAPIKey, effectiveBaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func launchTUI(provider, model, workspace, trust, apiKey, baseURL string) error {
	return mossTUI.Run(mossTUI.Config{
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
	sb, err := sandbox.NewLocal(wsDir)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}

	llm, err := buildLLM(provider, model, apiKey, baseURL)
	if err != nil {
		return nil, err
	}

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(io),
	)

	ctx := context.Background()
	if err := k.SetupWithDefaults(ctx, wsDir, kernel.WithWarningWriter(os.Stderr)); err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}

	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

// ─── LLM Construction ───────────────────────────────

func buildLLM(provider, model, apiKey, baseURL string) (port.LLM, error) {
	switch strings.ToLower(provider) {
	case "claude", "anthropic":
		var opts []claude.Option
		if model != "" {
			opts = append(opts, claude.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return claude.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return claude.New("", opts...), nil

	case "openai":
		var opts []adaptersopenai.Option
		if model != "" {
			opts = append(opts, adaptersopenai.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return adaptersopenai.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return adaptersopenai.New("", opts...), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: claude, openai)", provider)
	}
}

// ─── System Prompt ──────────────────────────────────

func buildSystemPrompt(workspace string) string {
	osName := runtime.GOOS
	shell := "bash"
	if osName == "windows" {
		shell = "powershell"
	}

	return skill.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, map[string]any{
		"OS":        osName,
		"Shell":     shell,
		"Workspace": workspace,
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
