// minitrade 是一个量化交易 AI Agent POC。
//
// 基于 moss kernel 构建，集成模拟市场（10 种资产、5 秒 tick）、
// 交易工具（下单/查询/投资组合）、技术分析、定时调度等能力。
// LLM 驱动决策，Agent 自主进行分析→规划→执行→监控的交易循环。
//
// 用法:
//
//	go run . --capital 100000
//	go run . --provider openai --model gpt-4o --capital 50000
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"
)

//go:embed templates/trading_prompt.tmpl
var tradingPromptTemplate string

type config struct {
	flags   *appkit.CommonFlags
	capital float64
}

func main() {
	skill.SetAppName("minitrade")
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
	flag.Float64Var(&cfg.capital, "capital", 100000, "Starting capital ($)")
	cfg.flags = appkit.ParseCommonFlags()
	return cfg
}

func run(ctx context.Context, cfg *config) error {
	capital := cfg.capital
	if capital <= 0 {
		capital = 100000
	}
	mkt := newMarket(capital)

	storeDir := filepath.Join(skill.MossDir(), "sessions")
	store, err := session.NewFileStore(storeDir)
	if err != nil {
		return fmt.Errorf("session store: %w", err)
	}

	sched := scheduler.New()
	userIO := port.NewConsoleIO()

	k, err := appkit.BuildKernel(ctx, cfg.flags, userIO,
		kernel.WithSessionStore(store),
		kernel.WithScheduler(sched),
	)
	if err != nil {
		return err
	}

	// 注册交易工具
	if err := registerTradeTools(k.ToolRegistry(), mkt); err != nil {
		return fmt.Errorf("register trade tools: %w", err)
	}

	// 注册技术分析工具
	if err := registerAnalysisTools(k.ToolRegistry(), mkt); err != nil {
		return fmt.Errorf("register analysis tools: %w", err)
	}

	// 注册调度工具
	if err := toolbuiltins.RegisterScheduleTools(k.ToolRegistry(), sched); err != nil {
		return fmt.Errorf("register schedule tools: %w", err)
	}

	// 策略：下单需要审批
	k.WithPolicy(
		builtins.RequireApprovalFor("place_order"),
		builtins.DefaultAllow(),
	)

	// 事件：交易执行日志
	k.OnEvent("tool.completed", func(e builtins.Event) {
		if data, ok := e.Data.(map[string]any); ok {
			if name, _ := data["tool"].(string); name == "place_order" {
				fmt.Printf("  📊 [event] Trade executed at %s\n", e.Timestamp.Format("15:04:05"))
			}
		}
	})

	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

	// 启动定时调度器
	sysPrompt := buildSystemPrompt(cfg.flags.Workspace, capital)
	sched.Start(ctx, func(jobCtx context.Context, job scheduler.Job) {
		fmt.Fprintf(os.Stdout, "\n⏰ Scheduled [%s]: %s\n", job.ID, job.Goal)
		jobSess, err := k.NewSession(jobCtx, session.SessionConfig{
			Goal:         job.Goal,
			Mode:         "scheduled",
			TrustLevel:   "restricted",
			SystemPrompt: sysPrompt,
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

	// 启动市场行情（每 5 秒 tick）
	mktDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-mktDone:
				return
			case <-ticker.C:
				mkt.tick()
			}
		}
	}()
	defer close(mktDone)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "quantitative trading assistant",
		Mode:         "interactive",
		TrustLevel:   "restricted",
		SystemPrompt: sysPrompt,
	})
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}

	modelName := cfg.flags.Model
	if modelName == "" {
		modelName = "(default)"
	}
	appkit.PrintBannerWithHint("minitrade — Quantitative Trading Agent",
		map[string]string{
			"Provider": cfg.flags.Provider,
			"Model":    modelName,
			"Capital":  fmt.Sprintf("$%.2f", capital),
			"Symbols":  fmt.Sprintf("%d available", len(mkt.prices)),
			"Tools":    fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
		},
		"Market is live! Prices update every 5 seconds.",
		"Type /help for commands, /exit to quit.",
	)

	return appkit.REPL(ctx, appkit.REPLConfig{
		Prompt:      "💰 > ",
		AppName:     "minitrade",
		CompactKeep: 8,
	}, k, sess)
}

func buildSystemPrompt(workspace string, capital float64) string {
	return appkit.RenderSystemPrompt(workspace, tradingPromptTemplate, map[string]any{
		"Capital": capital,
	})
}
