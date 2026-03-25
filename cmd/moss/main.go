package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/userio/tui"
)

const version = "0.3.0"

func main() {
	// 确保 ~/.moss 配置目录存在
	if err := skill.EnsureMossDir(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create config dir: %v\n", err)
	}

	// 无参数默认进入 TUI
	if len(os.Args) < 2 {
		launchTUI(os.Args[1:]) // empty slice
		return
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "version":
		fmt.Printf("moss %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		// 不识别的命令也进入 TUI
		launchTUI(os.Args[1:])
	}
}

func printUsage() {
	fmt.Print(`moss - Agent Runtime Kernel

Usage:
  moss                Launch interactive TUI (default)
  moss run [flags]    Run with a specific goal
  moss version        Show version

Flags:
  --goal        Goal for the agent to accomplish
  --workspace   Workspace directory (default: ".")
  --trust       Trust level: trusted|restricted (default: trusted)
  --provider    LLM provider: claude|openai (default from config or "claude")
  --model       Model name (default from config or provider default)
  --base-url    LLM API base URL (override config)
  --api-key     LLM API key (override config)

Config:
  ~/.moss/config.yaml    Global configuration (provider, model, base_url, api_key, skills)
  ./moss.yaml            Project-level skill configuration

Environment:
  ANTHROPIC_API_KEY  Fallback when provider=claude and no api_key in config.
  OPENAI_API_KEY     Fallback when provider=openai and no api_key in config.
  OPENAI_BASE_URL    Fallback when provider=openai and no base_url in config.
`)
}

// launchTUI 启动 Bubble Tea TUI 界面。
func launchTUI(args []string) {
	fs := flag.NewFlagSet("moss", flag.ExitOnError)
	f := &appkit.CommonFlags{}
	appkit.BindCommonFlags(fs, f)
	_ = fs.Parse(args)
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")

	if err := tui.Run(tui.Config{
		Provider:    f.Provider,
		Model:       f.Model,
		Workspace:   f.Workspace,
		Trust:       f.Trust,
		BaseURL:     f.BaseURL,
		APIKey:      f.APIKey,
		BuildKernel: buildKernelWithIO,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	goal := fs.String("goal", "", "Goal for the agent to accomplish")
	mode := fs.String("mode", "interactive", "Run mode: interactive|autopilot")
	f := &appkit.CommonFlags{}
	appkit.BindCommonFlags(fs, f)

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")

	if *goal == "" {
		fmt.Fprintln(os.Stderr, "error: --goal is required for 'run' command")
		fmt.Fprintln(os.Stderr, "hint: run 'moss' without arguments to enter interactive TUI")
		fs.Usage()
		os.Exit(1)
	}

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	cliIO := &cliUserIO{writer: os.Stdout, reader: os.Stdin}
	k, err := appkit.BuildKernel(ctx, f, cliIO)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing kernel: %v\n", err)
		os.Exit(1)
	}
	applyPolicy(k, f.Trust)

	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error booting kernel: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	fmt.Printf("🌿 moss %s\n", version)
	fmt.Printf("Goal: %s\n", *goal)
	fmt.Printf("Workspace: %s\n", f.Workspace)
	fmt.Printf("Mode: %s | Trust: %s\n", *mode, f.Trust)

	skills := k.SkillManager().List()
	if len(skills) > 0 {
		fmt.Printf("Skills: ")
		for i, s := range skills {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(s.Name)
		}
		fmt.Println()
	}
	fmt.Println()

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:       *goal,
		Mode:       *mode,
		TrustLevel: f.Trust,
		MaxSteps:   50,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
		os.Exit(1)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: *goal})

	result, err := k.Run(ctx, sess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Run failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Session completed (ID: %s)\n", result.SessionID)
	fmt.Printf("Steps: %d | Tokens: %d\n", result.Steps, result.TokensUsed.TotalTokens)
	if result.Output != "" {
		fmt.Printf("\nResult:\n%s\n", result.Output)
	}
}

// buildKernelWithIO 构建 Kernel 实例，供 TUI Config.BuildKernel 回调使用。
func buildKernelWithIO(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
	ctx := context.Background()
	k, err := appkit.BuildKernel(ctx, &appkit.CommonFlags{
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
	applyPolicy(k, trust)
	return k, nil
}

// applyPolicy 根据 trust level 设置策略。
func applyPolicy(k *kernel.Kernel, trust string) {
	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}
}
