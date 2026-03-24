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
//	go run . --provider openai --model gpt-4o --goal "分析 main.go 并编写测试"
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/mossagi/moss/adapters/claude"
	adaptersopenai "github.com/mossagi/moss/adapters/openai"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
)

func main() {
	skill.SetAppName("miniwork")
	_ = skill.EnsureMossDir()

	cfg := parseFlags()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\nInterrupted, cancelling...")
		cancel()
	}()

	if err := run(ctx, cfg); err != nil {
		if ctx.Err() != nil {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// ─── Config & Flags ─────────────────────────────────

type config struct {
	provider  string
	model     string
	workspace string
	trust     string
	apiKey    string
	baseURL   string
	goal      string
	workers   int
}

func parseFlags() config {
	c := config{
		provider:  envOrDefault("MINIWORK_PROVIDER", "openai"),
		workspace: ".",
		trust:     "trusted",
		workers:   3,
	}

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			i++
			c.provider = args[i]
		case "--model":
			i++
			c.model = args[i]
		case "--workspace":
			i++
			c.workspace = args[i]
		case "--trust":
			i++
			c.trust = args[i]
		case "--api-key":
			i++
			c.apiKey = args[i]
		case "--base-url":
			i++
			c.baseURL = args[i]
		case "--goal":
			i++
			c.goal = args[i]
		case "--workers":
			i++
			fmt.Sscanf(args[i], "%d", &c.workers)
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		}
	}

	if c.goal == "" {
		fmt.Fprintln(os.Stderr, "error: --goal is required")
		printUsage()
		os.Exit(1)
	}
	return c
}

func printUsage() {
	fmt.Print(`miniwork — Multi-Agent Workflow Orchestrator

Usage:
  miniwork --goal "your task description" [flags]

Flags:
  --goal        (required) Goal for the workflow
  --provider    LLM provider: claude|openai (default: openai)
  --model       Model name
  --workspace   Workspace directory (default: ".")
  --trust       Trust level: trusted|restricted (default: trusted)
  --workers     Max parallel workers (default: 3)
  --api-key     API key
  --base-url    API base URL
`)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Main Run ───────────────────────────────────────

func run(ctx context.Context, cfg config) error {
	llm, err := buildLLM(cfg.provider, cfg.model, cfg.apiKey, cfg.baseURL)
	if err != nil {
		return err
	}

	sb, err := sandbox.NewLocal(cfg.workspace)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}

	io := &logIO{writer: os.Stdout}

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(io),
	)

	if err := k.SetupWithDefaults(ctx, cfg.workspace, kernel.WithWarningWriter(os.Stderr)); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// 注册编排工具：delegate_tasks
	if err := registerOrchestrationTools(k, ctx, cfg); err != nil {
		return fmt.Errorf("register orchestration tools: %w", err)
	}

	if cfg.trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
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
	fmt.Println("╭──────────────────────────────────────╮")
	fmt.Println("│       miniwork — Orchestrator         │")
	fmt.Println("╰──────────────────────────────────────╯")
	fmt.Printf("  Provider:  %s\n", cfg.provider)
	fmt.Printf("  Model:     %s\n", modelName)
	fmt.Printf("  Workspace: %s\n", cfg.workspace)
	fmt.Printf("  Workers:   max %d parallel\n", cfg.workers)
	fmt.Printf("  Tools:     %d loaded\n", len(k.ToolRegistry().List()))
	fmt.Printf("  Goal:      %s\n", cfg.goal)
	fmt.Println()

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

		return json.Marshal(results)
	}
}

// runWorker 创建一个独立的 Worker Session 并执行子任务。
func runWorker(ctx context.Context, k *kernel.Kernel, cfg config, task taskInput) taskOutput {
	fmt.Fprintf(os.Stdout, "  🚀 [%s] worker started\n", task.ID)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         task.Description,
		Mode:         "worker",
		TrustLevel:   cfg.trust,
		SystemPrompt: buildWorkerPrompt(cfg.workspace),
		MaxSteps:     30,
	})
	if err != nil {
		return taskOutput{ID: task.ID, Success: false, Error: err.Error()}
	}

	sess.AppendMessage(port.Message{
		Role:    port.RoleUser,
		Content: task.Description,
	})

	result, err := k.Run(ctx, sess)
	if err != nil {
		return taskOutput{ID: task.ID, Success: false, Error: err.Error()}
	}

	return taskOutput{
		ID:      task.ID,
		Success: result.Success,
		Output:  result.Output,
		Steps:   result.Steps,
		Error:   result.Error,
	}
}

// ─── System Prompts ─────────────────────────────────

func buildManagerPrompt(workspace string, maxWorkers int) string {
	osName := runtime.GOOS
	return fmt.Sprintf(`You are a workflow manager agent. Your job is to break down complex goals into sub-tasks and delegate them to worker agents.

## Environment
- OS: %s
- Workspace: %s
- Max parallel workers: %d

## Available Tools
- **delegate_tasks**: Delegate sub-tasks to worker agents for parallel execution.
  Each worker is an independent agent with its own session that can read/write files, run commands, and search code.
- **read_file**, **list_files**, **search_text**: Use these to understand the codebase BEFORE delegating.
- **ask_user**: Ask the user for clarification if the goal is ambiguous.

## Workflow
1. **Analyze**: First, explore the codebase to understand its structure (use list_files, read_file, search_text).
2. **Plan**: Break the goal into independent, well-defined sub-tasks. Each task should be self-contained.
3. **Delegate**: Use delegate_tasks to send tasks to workers. Be specific in task descriptions — include file paths, expected behavior, and constraints.
4. **Synthesize**: After workers complete, review their results and provide a summary to the user.

## Rules
- Always explore the codebase before delegating to gather necessary context.
- Each task description must include ALL context the worker needs (file paths, function names, requirements). Workers cannot ask you follow-up questions.
- Prefer fewer, well-scoped tasks over many tiny ones.
- If a task fails, analyze the error and decide whether to retry with adjusted instructions or report the failure.
- Use Markdown formatting in your final summary.
`, osName, workspace, maxWorkers)
}

func buildWorkerPrompt(workspace string) string {
	osName := runtime.GOOS
	shell := "bash"
	if osName == "windows" {
		shell = "powershell"
	}

	return fmt.Sprintf(`You are a worker agent executing a specific task. Focus on completing the assigned task efficiently.

## Environment
- OS: %s
- Shell: %s
- Workspace: %s

## Tools
- **read_file**: Read file contents
- **write_file**: Create or overwrite files
- **list_files**: List files matching a glob pattern
- **search_text**: Search for text patterns across files
- **run_command**: Execute shell commands

## Rules
- Focus solely on the task given to you. Do not explore unrelated areas.
- Read relevant files before making changes.
- After writing files, verify your changes compile/work (use run_command).
- Be concise in your output — summarize what you did and any issues encountered.
- Use OS-appropriate commands (%s on %s).
`, osName, shell, workspace, shell, osName)
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
