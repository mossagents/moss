// miniwork 是一个多 Agent 工作流编排示例。
//
// 演示如何用 moss kernel 构建 Manager → Worker 的委派模式：
//   - Manager Agent 接收用户目标，拆分为子任务
//   - 每个子任务由独立的 Worker Session 执行
//   - 支持并行执行多个 Worker（通过 delegate_tasks 工具）
//   - Manager 汇总 Worker 结果后回复用户
//
// 用法:
//
//	go run .
//	go run . --provider openai --model gpt-4o --goal "分析 main.go 并编写测试"
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
	mossTUI "github.com/mossagi/moss/userio/tui"
)

//go:embed templates/manager_system_prompt.tmpl
var defaultManagerPromptTemplate string

//go:embed templates/worker_system_prompt.tmpl
var defaultWorkerPromptTemplate string

func main() {
	skill.SetAppName("miniwork")
	_ = appkit.EnsureAppDir()

	cfg := parseFlags()
	if cfg.goal == "" {
		if err := launchTUI(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		if ctx.Err() != nil {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// ─── Config & AppFlags ─────────────────────────────────

type config struct {
	provider  string
	model     string
	workspace string
	trust     string
	apiKey    string
	baseURL   string
	goal      string
	workers   int
	tracker   *orchestrationTracker
}

func parseFlags() config {
	common := &appkit.AppFlags{}
	c := config{
		workspace: ".",
		trust:     "trusted",
		workers:   3,
	}
	fs := flag.NewFlagSet("miniwork", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, common)
	fs.StringVar(&c.goal, "goal", "", "Goal for one-shot workflow execution; omit to launch TUI")
	fs.IntVar(&c.workers, "workers", 3, "Max parallel workers")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	common.MergeGlobalConfig()
	common.MergeEnv("MINIWORK", "MOSS")

	c.provider = common.Provider
	c.model = common.Model
	c.workspace = common.Workspace
	c.trust = common.Trust
	c.apiKey = common.APIKey
	c.baseURL = common.BaseURL

	return c
}

func printUsage() {
	fmt.Print(`miniwork — Multi-Agent Workflow Orchestrator

Usage:
	miniwork [flags]

AppFlags:
	--goal        Goal for one-shot workflow execution; omit to launch TUI
  --provider    LLM provider: claude|openai (default: openai)
  --model       Model name
  --workspace   Workspace directory (default: ".")
  --trust       Trust level: trusted|restricted (default: trusted)
  --workers     Max parallel workers (default: 3)
  --api-key     API key
  --base-url    API base URL
`)
}

func launchTUI(cfg config) error {
	tracker := newOrchestrationTracker(nil)
	cfg.tracker = tracker

	return mossTUI.Run(mossTUI.Config{
		Provider:  cfg.provider,
		Model:     cfg.model,
		Workspace: cfg.workspace,
		Trust:     cfg.trust,
		BaseURL:   cfg.baseURL,
		APIKey:    cfg.apiKey,
		BuildKernel: func(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
			tuiCfg := cfg
			tuiCfg.workspace = wsDir
			tuiCfg.trust = trust
			tuiCfg.provider = provider
			tuiCfg.model = model
			tuiCfg.apiKey = apiKey
			tuiCfg.baseURL = baseURL
			if tuiBridge, ok := io.(interface{ Refresh() }); ok {
				tuiCfg.tracker = newOrchestrationTracker(tuiBridge.Refresh)
				tracker = tuiCfg.tracker
			}
			return buildKernelForConfig(tuiCfg, io)
		},
		BuildSystemPrompt: func(workspace string) string {
			return buildManagerPrompt(workspace, cfg.workers)
		},
		BuildSessionConfig: func(workspace, trust, systemPrompt string) session.SessionConfig {
			return session.SessionConfig{
				Goal:         "interactive orchestration",
				Mode:         "orchestrator",
				TrustLevel:   trust,
				MaxSteps:     100,
				SystemPrompt: systemPrompt,
			}
		},
		SidebarTitle: "Workers",
		RenderSidebar: func() string {
			return tracker.Summary()
		},
	})
}

// ─── Main Run ───────────────────────────────────────

func run(ctx context.Context, cfg config) error {
	io := &logIO{writer: os.Stdout}
	k, err := buildKernelForConfig(cfg, io)
	if err != nil {
		return err
	}

	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

	// 创建 Manager Session
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         cfg.goal,
		Mode:         "orchestrator",
		TrustLevel:   cfg.trust,
		SystemPrompt: buildManagerPrompt(cfg.workspace, cfg.workers),
		MaxSteps:     100,
	})
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}

	// 打印启动信息
	modelName := cfg.model
	if modelName == "" {
		modelName = "(default)"
	}
	appkit.PrintBanner("miniwork — Orchestrator", map[string]string{
		"Provider":  cfg.provider,
		"Model":     modelName,
		"Workspace": cfg.workspace,
		"Workers":   fmt.Sprintf("max %d parallel", cfg.workers),
		"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
		"Goal":      cfg.goal,
	})

	// 注入目标并运行
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: cfg.goal})

	result, err := k.Run(ctx, sess)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════")
	fmt.Printf("✅ Workflow completed (session: %s)\n", result.SessionID)
	fmt.Printf("   Steps: %d | Tokens: %d\n", result.Steps, result.TokensUsed.TotalTokens)
	if result.Output != "" {
		fmt.Printf("\n%s\n", result.Output)
	}
	return nil
}

// ─── Orchestration Tools ────────────────────────────

// registerOrchestrationTools 注册 Manager 专用的委派工具。
func registerOrchestrationTools(k *kernel.Kernel, ctx context.Context, cfg config) error {
	delegateSpec := tool.ToolSpec{
		Name: "delegate_tasks",
		Description: `Delegate multiple sub-tasks to worker agents for parallel execution.
Each task gets its own independent agent session with full tool access.
Workers can read/write files, run commands, and search code.
Returns the result of each worker as an array.`,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tasks": {
					"type": "array",
					"description": "Array of task descriptions for workers to execute",
					"items": {
						"type": "object",
						"properties": {
							"id":          {"type": "string", "description": "Unique task identifier"},
							"description": {"type": "string", "description": "Detailed description of what the worker should do"}
						},
						"required": ["id", "description"]
					}
				}
			},
			"required": ["tasks"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"orchestration"},
	}

	handler := makeDelegateHandler(k, ctx, cfg)
	return k.ToolRegistry().Register(delegateSpec, handler)
}

type taskInput struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type taskOutput struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Steps   int    `json:"steps"`
	Error   string `json:"error,omitempty"`
}

func makeDelegateHandler(k *kernel.Kernel, _ context.Context, cfg config) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var req struct {
			Tasks []taskInput `json:"tasks"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		if len(req.Tasks) == 0 {
			return json.Marshal([]taskOutput{})
		}

		fmt.Fprintf(os.Stdout, "\n📋 Delegating %d task(s)...\n", len(req.Tasks))
		for _, t := range req.Tasks {
			fmt.Fprintf(os.Stdout, "  • [%s] %s\n", t.ID, truncate(t.Description, 80))
		}
		fmt.Println()
		if cfg.tracker != nil {
			cfg.tracker.StartBatch(req.Tasks)
		}

		// 并行执行，限制并发数
		results := make([]taskOutput, len(req.Tasks))
		sem := make(chan struct{}, cfg.workers)
		var wg sync.WaitGroup

		for i, task := range req.Tasks {
			wg.Add(1)
			go func(idx int, t taskInput) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				results[idx] = runWorker(ctx, k, cfg, t)
			}(i, task)
		}
		wg.Wait()

		// 输出摘要
		fmt.Println()
		succeeded := 0
		for _, r := range results {
			if r.Success {
				succeeded++
				fmt.Fprintf(os.Stdout, "  ✅ [%s] done (%d steps)\n", r.ID, r.Steps)
			} else {
				fmt.Fprintf(os.Stdout, "  ❌ [%s] failed: %s\n", r.ID, r.Error)
			}
		}
		fmt.Fprintf(os.Stdout, "\n  %d/%d tasks succeeded\n\n", succeeded, len(results))
		if cfg.tracker != nil {
			cfg.tracker.FinishBatch(results)
		}

		return json.Marshal(results)
	}
}

// runWorker 创建一个独立的 Worker Session 并执行子任务。
func runWorker(ctx context.Context, k *kernel.Kernel, cfg config, task taskInput) taskOutput {
	fmt.Fprintf(os.Stdout, "  🚀 [%s] worker started\n", task.ID)
	if cfg.tracker != nil {
		cfg.tracker.StartWorker(task)
	}

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         task.Description,
		Mode:         "worker",
		TrustLevel:   cfg.trust,
		SystemPrompt: buildWorkerPrompt(cfg.workspace),
		MaxSteps:     30,
	})
	if err != nil {
		result := taskOutput{ID: task.ID, Success: false, Error: err.Error()}
		if cfg.tracker != nil {
			cfg.tracker.FinishWorker(result)
		}
		return result
	}

	sess.AppendMessage(port.Message{
		Role:    port.RoleUser,
		Content: task.Description,
	})

	result, err := k.Run(ctx, sess)
	if err != nil {
		workerResult := taskOutput{ID: task.ID, Success: false, Error: err.Error()}
		if cfg.tracker != nil {
			cfg.tracker.FinishWorker(workerResult)
		}
		return workerResult
	}

	workerResult := taskOutput{
		ID:      task.ID,
		Success: result.Success,
		Output:  result.Output,
		Steps:   result.Steps,
		Error:   result.Error,
	}
	if cfg.tracker != nil {
		cfg.tracker.FinishWorker(workerResult)
	}
	return workerResult
}

// ─── System Prompts ─────────────────────────────────

func buildManagerPrompt(workspace string, maxWorkers int) string {
	return appkit.RenderSystemPrompt(workspace, defaultManagerPromptTemplate, map[string]any{
		"MaxWorkers": maxWorkers,
	})
}

func buildWorkerPrompt(workspace string) string {
	return appkit.RenderSystemPrompt(workspace, defaultWorkerPromptTemplate, nil)
}

func buildKernelForConfig(cfg config, io port.UserIO) (*kernel.Kernel, error) {
	ctx := context.Background()
	k, err := appkit.BuildKernel(ctx, &appkit.AppFlags{
		Provider:  cfg.provider,
		Model:     cfg.model,
		Workspace: cfg.workspace,
		Trust:     cfg.trust,
		APIKey:    cfg.apiKey,
		BaseURL:   cfg.baseURL,
	}, io)
	if err != nil {
		return nil, err
	}

	if err := registerOrchestrationTools(k, ctx, cfg); err != nil {
		return nil, fmt.Errorf("register orchestration tools: %w", err)
	}

	if cfg.trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}

// ─── Output IO ──────────────────────────────────────

// logIO 是一个只输出不交互的 UserIO 实现（工作流模式无需用户输入）。
type logIO struct {
	writer *os.File
}

func (l *logIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(l.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(l.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(l.writer)
	case port.OutputToolStart:
		fmt.Fprintf(l.writer, "  🔧 %s\n", msg.Content)
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(l.writer, "  ❌ %s\n", msg.Content)
		} else {
			content := msg.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Fprintf(l.writer, "  ✅ %s\n", content)
		}
	}
	return nil
}

func (l *logIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	// 工作流模式默认自动批准
	if req.Type == port.InputConfirm {
		return port.InputResponse{Approved: true}, nil
	}
	return port.InputResponse{Value: ""}, nil
}

// ─── Helpers ────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
