// basic 是 moss kernel 的最简集成示例。
//
// 演示如何用最少代码启动一个可对话的 Agent：
//   - 使用 appkit 解析参数和构建 Kernel
//   - REPL 交互
//   - 8 个内置工具自动注册
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"io"
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

	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "boot error: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	appkit.PrintBanner("basic", map[string]string{
		"Provider":  flags.Provider,
		"Model":     flags.Model,
		"Workspace": flags.Workspace,
	})

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "interactive",
		Mode:         "interactive",
		TrustLevel:   flags.Trust,
		MaxSteps:     100,
		SystemPrompt: "You are a helpful assistant. Answer questions concisely.",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "session error: %v\n", err)
		os.Exit(1)
	}

	if err := runREPL(ctx, "you> ", "basic", 8, k, sess); err != nil {
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
			Agent:       k.BuildLLMAgent("basic"),
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
