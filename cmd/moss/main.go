package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mossagi/moss/adapters/claude"
	adaptersopenai "github.com/mossagi/moss/adapters/openai"
	"github.com/mossagi/moss/cmd/moss/tui"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"
)

const version = "0.3.0"

func main() {
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
	wsDir := fs.String("workspace", "", "Workspace directory")
	trust := fs.String("trust", "trusted", "Trust level: trusted|restricted")
	provider := fs.String("provider", "", "LLM provider: claude|openai")
	model := fs.String("model", "", "Model name (default depends on provider)")
	baseURL := fs.String("base-url", "", "LLM API base URL")
	apiKey := fs.String("api-key", "", "LLM API key")

	_ = fs.Parse(args)

	// 加载 ~/.moss/config.yaml
	mossCfg := loadMossConfig()

	// CLI flags > config file defaults
	effectiveProvider := firstNonEmpty(*provider, mossCfg.Provider, "claude")
	effectiveModel := firstNonEmpty(*model, mossCfg.Model)
	effectiveBaseURL := firstNonEmpty(*baseURL, mossCfg.BaseURL)
	effectiveAPIKey := firstNonEmpty(*apiKey, mossCfg.APIKey)
	effectiveWorkspace := firstNonEmpty(*wsDir, ".")

	if err := tui.Run(tui.Config{
		Provider:    effectiveProvider,
		Model:       effectiveModel,
		Workspace:   effectiveWorkspace,
		Trust:       *trust,
		BaseURL:     effectiveBaseURL,
		APIKey:      effectiveAPIKey,
		BuildKernel: BuildKernelWithIO,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	goal := fs.String("goal", "", "Goal for the agent to accomplish")
	wsDir := fs.String("workspace", "", "Workspace directory")
	mode := fs.String("mode", "interactive", "Run mode: interactive|autopilot")
	trust := fs.String("trust", "trusted", "Trust level: trusted|restricted")
	provider := fs.String("provider", "", "LLM provider: claude|openai")
	model := fs.String("model", "", "Model name (default depends on provider)")
	baseURL := fs.String("base-url", "", "LLM API base URL")
	apiKey := fs.String("api-key", "", "LLM API key")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *goal == "" {
		fmt.Fprintln(os.Stderr, "error: --goal is required for 'run' command")
		fmt.Fprintln(os.Stderr, "hint: run 'moss' without arguments to enter interactive TUI")
		fs.Usage()
		os.Exit(1)
	}

	// 加载 ~/.moss/config.yaml
	mossCfg := loadMossConfig()

	effectiveProvider := firstNonEmpty(*provider, mossCfg.Provider, "claude")
	effectiveModel := firstNonEmpty(*model, mossCfg.Model)
	effectiveBaseURL := firstNonEmpty(*baseURL, mossCfg.BaseURL)
	effectiveAPIKey := firstNonEmpty(*apiKey, mossCfg.APIKey)
	effectiveWorkspace := firstNonEmpty(*wsDir, ".")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cancelling run...")
		cancel()
	}()

	k, err := buildKernel(effectiveWorkspace, *trust, effectiveProvider, effectiveModel, effectiveAPIKey, effectiveBaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing kernel: %v\n", err)
		os.Exit(1)
	}
	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error booting kernel: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	fmt.Printf("🌿 moss %s\n", version)
	fmt.Printf("Goal: %s\n", *goal)
	fmt.Printf("Workspace: %s\n", effectiveWorkspace)
	fmt.Printf("Mode: %s | Trust: %s\n", *mode, *trust)

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
		TrustLevel: *trust,
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

// buildLLM 根据 provider 创建 LLM 适配器。
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

// buildKernel 构建 Kernel 实例（CLI 模式，使用 cliUserIO）。
func buildKernel(wsDir, trust, provider, model, apiKey, baseURL string) (*kernel.Kernel, error) {
	cliIO := &cliUserIO{writer: os.Stdout, reader: os.Stdin}
	return buildKernelWithIO(wsDir, trust, provider, model, apiKey, baseURL, cliIO)
}

// BuildKernelWithIO 构建 Kernel 实例，允许注入自定义 UserIO（供 TUI 使用）。
func BuildKernelWithIO(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
	return buildKernelWithIO(wsDir, trust, provider, model, apiKey, baseURL, io)
}

func buildKernelWithIO(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
	sb, err := sandbox.NewLocal(wsDir)
	if err != nil {
		return nil, err
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
	deps := k.SkillDeps()

	// 注册内置工具 skill
	if err := k.SkillManager().Register(ctx, &toolbuiltins.CoreSkill{}, deps); err != nil {
		return nil, fmt.Errorf("register core skill: %w", err)
	}

	// 加载配置文件中的 MCP skills
	globalCfg, _ := skill.LoadConfig(skill.DefaultGlobalConfigPath())
	projectCfg, _ := skill.LoadConfig(skill.DefaultProjectConfigPath(wsDir))
	merged := skill.MergeConfigs(globalCfg, projectCfg)

	for _, sc := range merged.Skills {
		if !sc.IsEnabled() || !sc.IsMCP() {
			continue
		}
		mcpSkill := skill.NewMCPSkill(sc)
		if err := k.SkillManager().Register(ctx, mcpSkill, deps); err != nil {
			// MCP skill 加载失败不中断启动，仅打印警告
			fmt.Fprintf(os.Stderr, "warning: failed to load MCP skill %q: %v\n", sc.Name, err)
		}
	}

	// 发现并加载 skills.sh 兼容的 SKILL.md 文件
	promptSkills := skill.DiscoverPromptSkills(wsDir)
	for _, ps := range promptSkills {
		if err := k.SkillManager().Register(ctx, ps, deps); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load prompt skill %q: %v\n", ps.Metadata().Name, err)
		}
	}

	// 根据 trust level 设置策略
	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

// loadMossConfig 加载 ~/.moss/config.yaml 全局配置。
func loadMossConfig() *skill.Config {
	cfg, err := skill.LoadConfig(skill.DefaultGlobalConfigPath())
	if err != nil {
		return &skill.Config{}
	}
	return cfg
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
