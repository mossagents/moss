// custom-tool 演示如何在 moss kernel 中注册自定义工具。
//
// 包含两个示例工具：
//   - calculator：四则运算计算器
//   - random_number：生成指定范围内的随机数
//
// 自定义工具与内置工具共存，Agent 可根据用户意图自动调用。
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"io"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	flags := appkit.ParseAppFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	k, err := appkit.BuildKernel(ctx, flags, kernio.NewConsoleIO())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// ── 注册自定义工具 ──────────────────────────────────
	registerCustomTools(k.ToolRegistry())

	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "boot error: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	tools := k.ToolRegistry().List()
	appkit.PrintBannerWithHint("custom-tool", map[string]string{
		"Provider": flags.Provider,
		"Model":    flags.Model,
		"Tools":    fmt.Sprintf("%d loaded", len(tools)),
	}, "试试说: \"帮我算 123 * 456\"  或  \"给我一个 1 到 100 的随机数\"")

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:       "interactive",
		Mode:       "interactive",
		TrustLevel: flags.Trust,
		MaxSteps:   100,
		SystemPrompt: "You are a helpful assistant with access to a calculator and random number generator. " +
			"Use the calculator tool for math operations and random_number for generating random numbers.",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "session error: %v\n", err)
		os.Exit(1)
	}

	if err := runREPL(ctx, "you> ", "custom-tool", 8, k, sess); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runREPL(ctx context.Context, prompt, appName string, compactKeep int, k *kernel.Kernel, sess *session.Session) error {
	reader := bufio.NewReader(os.Stdin)
	if prompt == "" {
		prompt = "> "
	}
	if appName == "" {
		appName = "agent"
	}
	if compactKeep <= 0 {
		compactKeep = 8
	}

	for {
		fmt.Print(prompt)
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
		if strings.HasPrefix(input, "/") {
			if handleREPLCommand(input, sess, appName, compactKeep) {
				return nil
			}
			continue
		}

		userMsg := model.Message{
			Role:         model.RoleUser,
			ContentParts: []model.ContentPart{model.TextPart(input)},
		}
		sess.AppendMessage(userMsg)
		if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
			Session:     sess,
			Agent:       k.BuildLLMAgent("custom-tool"),
			UserContent: &userMsg,
		}); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\nError: %v\n\n", err)
			continue
		}
		fmt.Println()
	}
}

func handleREPLCommand(input string, sess *session.Session, appName string, compactKeep int) bool {
	cmd := strings.ToLower(strings.Fields(input)[0])
	switch cmd {
	case "/exit", "/quit":
		fmt.Println("Bye!")
		return true
	case "/clear":
		var systemMsgs []model.Message
		for _, msg := range sess.Messages {
			if msg.Role == model.RoleSystem {
				systemMsgs = append(systemMsgs, msg)
			}
		}
		sess.Messages = systemMsgs
		sess.Budget.ResetUsage()
		fmt.Println("Conversation cleared.")
	case "/compact":
		var systemMsgs, dialogMsgs []model.Message
		for _, msg := range sess.Messages {
			if msg.Role == model.RoleSystem {
				systemMsgs = append(systemMsgs, msg)
				continue
			}
			dialogMsgs = append(dialogMsgs, msg)
		}
		if len(dialogMsgs) > compactKeep {
			dialogMsgs = dialogMsgs[len(dialogMsgs)-compactKeep:]
		}
		sess.Messages = append(systemMsgs, dialogMsgs...)
		fmt.Printf("Compacted to %d messages.\n", len(sess.Messages))
	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /help     Show this help")
		fmt.Println("  /clear    Clear conversation history")
		fmt.Println("  /compact  Keep only recent messages")
		fmt.Printf("  /exit     Exit %s\n", appName)
	default:
		fmt.Printf("Unknown command: %s (type /help)\n", cmd)
	}
	return false
}

// ── Calculator Tool ─────────────────────────────────

var calculatorSpec = tool.ToolSpec{
	Name:        "calculator",
	Description: "Perform basic arithmetic operations (+, -, *, /).",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"a":  {"type": "number", "description": "First operand"},
			"b":  {"type": "number", "description": "Second operand"},
			"op": {"type": "string", "enum": ["+", "-", "*", "/"], "description": "Operator"}
		},
		"required": ["a", "b", "op"]
	}`),
	Risk: tool.RiskLow,
}

func calculatorHandler(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		A  float64 `json:"a"`
		B  float64 `json:"b"`
		Op string  `json:"op"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	var result float64
	switch args.Op {
	case "+":
		result = args.A + args.B
	case "-":
		result = args.A - args.B
	case "*":
		result = args.A * args.B
	case "/":
		if args.B == 0 {
			return json.Marshal(map[string]string{"error": "division by zero"})
		}
		result = args.A / args.B
	default:
		return json.Marshal(map[string]string{"error": fmt.Sprintf("unknown operator: %s", args.Op)})
	}

	return json.Marshal(map[string]any{
		"expression": fmt.Sprintf("%g %s %g", args.A, args.Op, args.B),
		"result":     result,
	})
}

// ── Random Number Tool ──────────────────────────────

var randomNumberSpec = tool.ToolSpec{
	Name:        "random_number",
	Description: "Generate a random integer within a specified range [min, max].",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"min": {"type": "integer", "description": "Minimum value (inclusive)"},
			"max": {"type": "integer", "description": "Maximum value (inclusive)"}
		},
		"required": ["min", "max"]
	}`),
	Risk: tool.RiskLow,
}

func randomNumberHandler(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		Min int `json:"min"`
		Max int `json:"max"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	if args.Min > args.Max {
		return json.Marshal(map[string]string{"error": "min must be <= max"})
	}

	n := args.Min + rand.IntN(args.Max-args.Min+1)
	return json.Marshal(map[string]any{"result": n})
}

// ── 注册 ────────────────────────────────────────────

func registerCustomTools(reg tool.Registry) {
	if err := reg.Register(tool.NewRawTool(calculatorSpec, calculatorHandler)); err != nil {
		fmt.Fprintf(os.Stderr, "register calculator: %v\n", err)
	}
	if err := reg.Register(tool.NewRawTool(randomNumberSpec, randomNumberHandler)); err != nil {
		fmt.Fprintf(os.Stderr, "register random_number: %v\n", err)
	}
}
