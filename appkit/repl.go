package appkit

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

// REPLConfig 配置 REPL 引擎。
type REPLConfig struct {
	// Prompt 是每轮输入前的提示符，如 "🕷 > "。
	Prompt string

	// AppName 用于 /help 中显示退出命令的名称。
	AppName string

	// CompactKeep 是 /compact 保留的最近消息数。默认 8。
	CompactKeep int
}

// REPL 在终端中运行交互式读取-执行-打印循环。
//
// 内置命令：/help、/clear、/compact、/exit。
func REPL(ctx context.Context, cfg REPLConfig, k *kernel.Kernel, sess *session.Session) error {
	reader := bufio.NewReader(os.Stdin)
	prompt := cfg.Prompt
	if prompt == "" {
		prompt = "> "
	}
	appName := cfg.AppName
	if appName == "" {
		appName = "agent"
	}
	compactKeep := cfg.CompactKeep
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
			done := handleREPLCommand(input, sess, appName, compactKeep)
			if done {
				return nil
			}
			continue
		}

		sess.AppendMessage(port.Message{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart(input)}})

		result, err := k.Run(ctx, sess)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n\n", err)
			continue
		}
		_ = result
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
		var systemMsgs, dialogMsgs []port.Message
		for _, m := range sess.Messages {
			if m.Role == port.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			} else {
				dialogMsgs = append(dialogMsgs, m)
			}
		}
		if len(dialogMsgs) > compactKeep {
			dialogMsgs = dialogMsgs[len(dialogMsgs)-compactKeep:]
		}
		sess.Messages = append(systemMsgs, dialogMsgs...)
		fmt.Printf("✓ Compacted to %d messages.\n", len(sess.Messages))
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
