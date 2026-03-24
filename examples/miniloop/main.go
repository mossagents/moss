// miniloop 是一个通用的有状态自主循环 Agent 框架。
//
// 它通过 Domain 接口将领域逻辑（工具、提示词、策略、后台进程）
// 与 Agent 运行时（Kernel、REPL、LLM 适配）彻底解耦。
//
// 添加新领域只需实现 Domain 接口并在 init() 中注册：
//
//	func init() { registerDomain("mydom", newMyDomain) }
//
// 内置领域：
//   - trading: 模拟市场交易（随机游走、组合管理、交易审批）
//
// 用法:
//
//	go run . --domain trading --capital 100000
//	go run . --provider openai --model gpt-4o --domain trading
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagi/moss/adapters"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"
)

type config struct {
	provider  string
	model     string
	workspace string
	apiKey    string
	baseURL   string
	domain    string
	capital   float64 // trading domain
}

func main() {
	skill.SetAppName("miniloop")
	_ = skill.EnsureMossDir()

	cfg := parseFlags()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *config {
	cfg := &config{}

	// Domain list for help text
	var domainNames []string
	for name := range domains {
		domainNames = append(domainNames, name)
	}

	flag.StringVar(&cfg.provider, "provider", "openai", "LLM provider: claude|openai")
	flag.StringVar(&cfg.model, "model", "", "Model name")
	flag.StringVar(&cfg.workspace, "workspace", ".", "Workspace directory")
	flag.StringVar(&cfg.apiKey, "api-key", "", "API key")
	flag.StringVar(&cfg.baseURL, "base-url", "", "API base URL")
	flag.StringVar(&cfg.domain, "domain", "trading", "Domain adapter: "+strings.Join(domainNames, "|"))
	flag.Float64Var(&cfg.capital, "capital", 100000, "Starting capital (trading domain)")
	flag.Parse()

	// Merge with global config (~/.miniloop/config.yaml)
	if globalCfg, err := skill.LoadGlobalConfig(); err == nil {
		cfg.provider = appkit.FirstNonEmpty(cfg.provider, globalCfg.Provider, "openai")
		cfg.model = appkit.FirstNonEmpty(cfg.model, globalCfg.Model)
		cfg.apiKey = appkit.FirstNonEmpty(cfg.apiKey, globalCfg.APIKey)
		cfg.baseURL = appkit.FirstNonEmpty(cfg.baseURL, globalCfg.BaseURL)
	}

	return cfg
}

func run(ctx context.Context, cfg *config) error {
	// Resolve domain adapter
	factory, ok := domains[cfg.domain]
	if !ok {
		var names []string
		for k := range domains {
			names = append(names, k)
		}
		return fmt.Errorf("unknown domain: %s (available: %s)", cfg.domain, strings.Join(names, ", "))
	}
	dom := factory(cfg)

	// Build kernel
	llm, err := adapters.BuildLLM(cfg.provider, cfg.model, cfg.apiKey, cfg.baseURL)
	if err != nil {
		return err
	}

	sb, err := sandbox.NewLocal(cfg.workspace)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}

	// Session 持久化存储
	storeDir := filepath.Join(skill.MossDir(), "sessions")
	store, err := session.NewFileStore(storeDir)
	if err != nil {
		return fmt.Errorf("session store: %w", err)
	}

	// 定时调度器
	sched := scheduler.New()

	userIO := port.NewConsoleIO()

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(userIO),
		kernel.WithSessionStore(store),
		kernel.WithScheduler(sched),
	)

	if err := k.SetupWithDefaults(ctx, cfg.workspace,
		kernel.WithWarningWriter(os.Stderr),
	); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// Domain-specific setup: tools, policies, events
	if err := dom.Setup(k); err != nil {
		return fmt.Errorf("domain setup: %w", err)
	}

	// 注册调度工具
	if err := toolbuiltins.RegisterScheduleTools(k.ToolRegistry(), sched); err != nil {
		return fmt.Errorf("register schedule tools: %w", err)
	}

	if rules := dom.Policies(); len(rules) > 0 {
		k.WithPolicy(rules...)
	}

	for pattern, handler := range dom.EventHooks() {
		k.OnEvent(pattern, handler)
	}

	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

	// 启动调度器
	sched.Start(ctx, func(jobCtx context.Context, job scheduler.Job) {
		fmt.Fprintf(os.Stdout, "\n⏰ Scheduled [%s]: %s\n", job.ID, job.Goal)
		jobSess, err := k.NewSession(jobCtx, session.SessionConfig{
			Goal:         job.Goal,
			Mode:         "scheduled",
			TrustLevel:   "restricted",
			SystemPrompt: dom.SystemPrompt(cfg.workspace),
			MaxSteps:     30,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ schedule session: %v\n", err)
			return
		}
		jobSess.AppendMessage(port.Message{Role: port.RoleUser, Content: job.Goal})
		result, err := k.Run(jobCtx, jobSess)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ schedule run: %v\n", err)
			return
		}
		_ = store.Save(jobCtx, jobSess)
		fmt.Fprintf(os.Stdout, "  ✅ [%s] done (%d steps)\n\n", job.ID, result.Steps)
	})
	defer sched.Stop()

	// Start domain background processes
	stop := dom.Start(ctx)
	defer stop()

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         dom.Name() + " assistant",
		Mode:         "interactive",
		TrustLevel:   "restricted",
		SystemPrompt: dom.SystemPrompt(cfg.workspace),
	})
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}

	// Print banner
	modelName := cfg.model
	if modelName == "" {
		modelName = "(default)"
	}
	fmt.Println("╭─────────────────────────────────────────╮")
	fmt.Printf("│  miniloop — %-27s │\n", dom.Name())
	fmt.Println("╰─────────────────────────────────────────╯")
	fmt.Printf("  Provider:  %s\n", cfg.provider)
	fmt.Printf("  Model:     %s\n", modelName)
	fmt.Printf("  Domain:    %s\n", dom.Description())
	for _, line := range dom.Banner() {
		fmt.Println(line)
	}
	fmt.Printf("  Tools:     %d loaded\n", len(k.ToolRegistry().List()))
	fmt.Println()
	fmt.Println("  Type /help for commands, /exit to quit.")
	fmt.Println()

	return appkit.REPL(ctx, appkit.REPLConfig{
		Prompt:      dom.Prompt(),
		AppName:     "miniloop",
		CompactKeep: 8,
	}, k, sess)
}
