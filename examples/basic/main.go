// basic 是 moss kernel 的最简集成示例。
//
// 演示如何用最少代码启动一个可对话的 Agent：
//   - 使用 appkit 解析参数和构建 Kernel
//   - REPL 交互
//   - 6 个内置工具自动注册
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func main() {
	flags := appkit.ParseAppFlags()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	k, err := appkit.BuildKernel(ctx, flags, port.NewConsoleIO())
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

	if err := appkit.REPL(ctx, appkit.REPLConfig{
		Prompt:  "you> ",
		AppName: "basic",
	}, k, sess); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
