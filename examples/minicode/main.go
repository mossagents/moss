// minicode 是一个类 Claude Code 的极简 Code Agent 示例。
//
// 演示如何用 moss kernel 构建一个交互式编程助手：
//   - 交互式 REPL（读取用户输入 → 运行 Agent → 显示输出 → 循环）
//   - 流式输出（逐 token 实时显示）
//   - 6 个内置工具（read_file, write_file, list_files, search_text, run_command, ask_user）
//   - 信任等级（trusted: 自动执行 / restricted: 危险操作需确认）
//   - 斜杠命令（/exit, /clear, /compact, /help）
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider claude
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"bufio"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/mossagi/moss/adapters/claude"
	adaptersopenai "github.com/mossagi/moss/adapters/openai"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 捕获 Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\nBye!")
		cancel()
		os.Exit(0)
	}()

	if err := run(ctx, effectiveProvider, effectiveModel, *workspace, *trust, effectiveAPIKey, effectiveBaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, provider, model, workspace, trust, apiKey, baseURL string) error {
	// 1. 构建 LLM adapter
	llm, err := buildLLM(provider, model, apiKey, baseURL)
	if err != nil {
		return err
	}

	// 2. 构建 Sandbox
	sb, err := sandbox.NewLocal(workspace)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}

	// 3. 构建 UserIO（终端交互）
	userIO := &consoleIO{
		writer: os.Stdout,
		reader: os.Stdin,
	}

	// 4. 构建 Kernel
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(userIO),
	)

	// 5. 注册标准技能
	if err := k.SetupWithDefaults(ctx, workspace, kernel.WithWarningWriter(os.Stderr)); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// 6. 设置策略（restricted 模式下危险操作需确认）
	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	// 7. Boot
	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

	// 8. 创建 Session（system prompt 由 Kernel 自动注入）
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "interactive coding assistant",
		Mode:         "interactive",
		TrustLevel:   trust,
		SystemPrompt: buildSystemPrompt(workspace),
	})
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}

	// 9. 打印欢迎信息
	modelName := model
	if modelName == "" {
		modelName = "(default)"
	}
	fmt.Printf("╭──────────────────────────────────────╮\n")
	fmt.Printf("│         minicode — Code Agent         │\n")
	fmt.Printf("╰──────────────────────────────────────╯\n")
	fmt.Printf("  Provider:  %s\n", provider)
	fmt.Printf("  Model:     %s\n", modelName)
	fmt.Printf("  Workspace: %s\n", workspace)
	fmt.Printf("  Trust:     %s\n", trust)
	fmt.Printf("  Tools:     %d loaded\n", len(k.ToolRegistry().List()))
	fmt.Println()
	fmt.Println("  Type /help for commands, /exit to quit.")
	fmt.Println()

	// 10. REPL 循环
	return repl(ctx, k, sess)
}

// repl 实现交互式读取-执行-打印循环。
func repl(ctx context.Context, k *kernel.Kernel, sess *session.Session) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		// 斜杠命令
		if strings.HasPrefix(input, "/") {
			done := handleCommand(input, sess)
			if done {
				return nil
			}
			continue
		}

		// 追加用户消息并运行 Agent Loop
		sess.AppendMessage(port.Message{Role: port.RoleUser, Content: input})

		result, err := k.Run(ctx, sess)
		if err != nil {
			if ctx.Err() != nil {
				return nil // 用户中断
			}
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n\n", err)
			continue
		}

		_ = result // 输出已通过 UserIO 实时显示
		fmt.Println()
	}
}

// handleCommand 处理斜杠命令，返回 true 表示应退出。
func handleCommand(input string, sess *session.Session) bool {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/exit", "/quit":
		fmt.Println("Bye!")
		return true

	case "/clear":
		// 保留 system 消息，清除对话历史
		var systemMsgs []port.Message
		for _, m := range sess.Messages {
			if m.Role == port.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			}
		}
		sess.Messages = systemMsgs
		sess.Budget.UsedSteps = 0
		sess.Budget.UsedTokens = 0
		fmt.Println("✓ Conversation cleared.")

	case "/compact":
		// 保留 system 消息和最后 6 条对话消息
		var systemMsgs []port.Message
		var dialogMsgs []port.Message
		for _, m := range sess.Messages {
			if m.Role == port.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			} else {
				dialogMsgs = append(dialogMsgs, m)
			}
		}
		keep := 6
		if len(dialogMsgs) > keep {
			dialogMsgs = dialogMsgs[len(dialogMsgs)-keep:]
		}
		sess.Messages = append(systemMsgs, dialogMsgs...)
		fmt.Printf("✓ Compacted to %d messages (system + %d dialog).\n", len(sess.Messages), len(dialogMsgs))

	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /help     Show this help")
		fmt.Println("  /clear    Clear conversation history")
		fmt.Println("  /compact  Keep only recent messages (save tokens)")
		fmt.Println("  /exit     Exit minicode")

	default:
		fmt.Printf("Unknown command: %s (type /help)\n", cmd)
	}

	return false
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

// ─── Console UserIO ─────────────────────────────────

// consoleIO 实现 port.UserIO，提供终端交互能力。
type consoleIO struct {
	writer io.Writer
	reader *os.File
}

func (c *consoleIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(c.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(c.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(c.writer)
	case port.OutputProgress:
		fmt.Fprintf(c.writer, "⏳ %s\n", msg.Content)
	case port.OutputToolStart:
		fmt.Fprintf(c.writer, "🔧 %s\n", msg.Content)
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(c.writer, "❌ %s\n", msg.Content)
		} else {
			// 截断过长的工具输出
			content := msg.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Fprintf(c.writer, "✅ %s\n", content)
		}
	}
	return nil
}

func (c *consoleIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	reader := bufio.NewReader(c.reader)

	switch req.Type {
	case port.InputConfirm:
		fmt.Fprintf(c.writer, "%s [y/N]: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		answer := strings.TrimSpace(strings.ToLower(line))
		return port.InputResponse{Approved: answer == "y" || answer == "yes"}, nil

	case port.InputSelect:
		for i, opt := range req.Options {
			fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt)
		}
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		var sel int
		fmt.Fscan(c.reader, &sel)
		return port.InputResponse{Selected: sel - 1}, nil

	default: // FreeText
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		return port.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
