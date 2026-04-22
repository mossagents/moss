// agent-swarm 演示如何使用 moss kernel 协调智能 Agent Swarm 进行协作研究。
//
// 本示例通过真实 LLM 展示多 Agent 协作研究流程：
//   1. DecomposerAgent 将研究主题自主拆分为 N 个子问题
//   2. N 个具有不同人设的 PersonaWorkerAgent 并行研究各自子问题
//   3. 多轮迭代：后续轮次中每个 Agent 阅读所有人的发现并进行思维碰撞
//   4. SynthesisAgent 汇总所有发现生成结构化研究报告
//
// 约束说明：
//   - kernel.maxActiveAgents = 16：全局并发子 Agent 上限
//   - 批次大小（-batch）须 ≤ 10，以保证 ParallelAgent(1) + workers(batch) ≤ 16
//   - kernel.maxForkDepth = 5：分支嵌套层数上限（本示例最深 2 层，安全）
//
// 用法：
//
//	go run . --provider openai --model gpt-4o --topic "量子计算的产业化路径" --agents 8 --rounds 2
//	go run . --provider claude --model claude-3-5-sonnet-20241022 --agents 10 --rounds 3 --batch 5
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func main() {
	// Register custom flags before appkit.ParseAppFlags() calls flag.Parse().
	topic := flag.String("topic", "大规模语言模型在教育领域的应用前景", "研究主题")
	agentsN := flag.Int("agents", 8, "研究 Agent 数量（2–30）")
	rounds := flag.Int("rounds", 2, "研究轮次（1–5）；轮次 ≥2 时 Agent 间会相互评论发现")
	batch := flag.Int("batch", 5, "每批并行 Agent 数（≤10，受 kernel maxActiveAgents=16 约束）")

	// ParseAppFlags binds standard LLM flags (--provider, --model, --api-key, …)
	// and calls flag.Parse(), so our custom flags above are also parsed.
	flags := appkit.ParseAppFlags()

	// Validate and clamp custom flags.
	if *agentsN < 2 {
		*agentsN = 2
	}
	if *agentsN > 30 {
		*agentsN = 30
	}
	if *rounds < 1 {
		*rounds = 1
	}
	if *rounds > 5 {
		*rounds = 5
	}
	if *batch < 1 {
		*batch = 1
	}
	if *batch > 10 {
		fmt.Fprintln(os.Stderr, "警告: -batch 超过 10，已自动截断（kernel maxActiveAgents 约束）")
		*batch = 10
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Build kernel (sets up LLM adapter, tools, etc.).
	k, err := appkit.BuildKernel(ctx, flags, &kernio.NoOpIO{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "构建 kernel 失败: %v\n", err)
		os.Exit(1)
	}
	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "kernel boot 失败: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	printBanner(*topic, *agentsN, *rounds, *batch, flags.Provider, flags.Model)

	// Create a minimal session for the orchestrator.
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     "research-swarm",
		Mode:     "batch",
		MaxSteps: 1,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建 session 失败: %v\n", err)
		os.Exit(1)
	}

	swarm := &ResearchSwarm{
		topic:  *topic,
		agents: *agentsN,
		rounds: *rounds,
		batch:  *batch,
		llm:    k.LLM(),
	}

	start := time.Now()
	var finalReport string
	var findingCount int

	for event, err := range k.RunAgent(ctx, kernel.RunAgentRequest{
		Agent:   swarm,
		Session: sess,
	}) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n错误: %v\n", err)
			os.Exit(1)
		}
		if event == nil || event.Content == nil {
			continue
		}
		text := model.ContentPartsToPlainText(event.Content.ContentParts)
		if text == "" {
			continue
		}
		switch event.Type {
		case session.EventTypeLLMResponse:
			// Worker findings: stream as they arrive.
			if event.Author == "synthesis" {
				finalReport = text
			} else {
				findingCount++
				printFinding(findingCount, event.Author, text)
			}
		case session.EventTypeCustom:
			if event.Author == "synthesis" {
				finalReport = text
			} else {
				findingCount++
				printFinding(findingCount, event.Author, text)
			}
		}
	}

	elapsed := time.Since(start)

	sep := strings.Repeat("═", 70)
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  📄  最终研究报告\n")
	fmt.Printf("%s\n\n", sep)
	if finalReport != "" {
		fmt.Print(finalReport)
	} else {
		fmt.Println("（未生成报告）")
	}
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  ⏱  总耗时 %v  ｜  %d 条研究发现\n",
		elapsed.Round(time.Millisecond), findingCount)
	fmt.Printf("%s\n", sep)
}

func printFinding(n int, author, text string) {
	// Trim to first 3 lines for readability during streaming.
	lines := strings.SplitN(text, "\n", 5)
	preview := strings.Join(lines[:min(3, len(lines))], " ")
	if len(preview) > 120 {
		preview = preview[:120] + "…"
	}
	fmt.Printf("  [%3d] %-22s %s\n", n, author+":", preview)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func printBanner(topic string, agentsN, rounds, batch int, provider, model string) {
	sep := strings.Repeat("─", 70)
	fmt.Println(sep)
	fmt.Println("  🐝  智能 Agent Swarm 研究系统  (moss kernel)")
	fmt.Println(sep)
	fmt.Printf("  研究主题  : %s\n", topic)
	fmt.Printf("  Agent 数量: %d（%d 种人设循环分配）\n", agentsN, len(personas))
	fmt.Printf("  研究轮次  : %d\n", rounds)
	fmt.Printf("  批次大小  : %d（每批并行，批次间串行）\n", batch)
	fmt.Printf("  LLM       : %s / %s\n", provider, model)
	fmt.Printf("  总调用次数: ≈%d（分解1 + 研究%d + 综合1）\n",
		1+agentsN*rounds+1, agentsN*rounds)
	fmt.Println(sep)
}
