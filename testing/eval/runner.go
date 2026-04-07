package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/port"
)

// CaseRunner 是执行单个 EvalCase 并返回 EvalRun 的函数。
// 调用方注入具体的 Agent 执行逻辑（通常是 kernel.Run）。
type CaseRunner func(ctx context.Context, c EvalCase) (EvalRun, error)

// RunnerConfig 配置 EvalRunner。
type RunnerConfig struct {
	// RunCase 执行单个用例（必须提供）。
	RunCase CaseRunner

	// Judges 评分器列表（至少一个）。
	Judges []Judge

	// Parallelism 并发执行数，默认 1（串行）。
	Parallelism int

	// Timeout 单个用例的超时时间，默认 60s。
	Timeout time.Duration
}

func (c RunnerConfig) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 60 * time.Second
	}
	return c.Timeout
}

func (c RunnerConfig) parallelism() int {
	if c.Parallelism <= 0 {
		return 1
	}
	return c.Parallelism
}

// EvalRunner 批量执行评测用例并聚合结果。
type EvalRunner struct {
	cfg RunnerConfig
}

// NewRunner 创建 EvalRunner。
func NewRunner(cfg RunnerConfig) *EvalRunner {
	return &EvalRunner{cfg: cfg}
}

// Run 执行给定的用例列表，返回所有结果。
func (r *EvalRunner) Run(ctx context.Context, cases []EvalCase) ([]EvalResult, error) {
	if len(cases) == 0 {
		return nil, nil
	}

	type resultOrErr struct {
		result EvalResult
		err    error
		idx    int
	}

	sem := make(chan struct{}, r.cfg.parallelism())
	resultCh := make(chan resultOrErr, len(cases))

	for i, c := range cases {
		sem <- struct{}{}
		go func(idx int, c EvalCase) {
			defer func() { <-sem }()
			result, err := r.runOne(ctx, c)
			resultCh <- resultOrErr{result: result, err: err, idx: idx}
		}(i, c)
	}

	// 等待所有 goroutine 完成
	for range cases {
		sem <- struct{}{}
	}
	close(resultCh)

	// 收集结果并按原始顺序排列
	results := make([]EvalResult, len(cases))
	var firstErr error
	for ro := range resultCh {
		if ro.err != nil && firstErr == nil {
			firstErr = ro.err
		}
		results[ro.idx] = ro.result
	}

	return results, firstErr
}

// runOne 执行单个用例。
func (r *EvalRunner) runOne(ctx context.Context, c EvalCase) (EvalResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, r.cfg.timeout())
	defer cancel()

	runID := uuid.New().String()
	start := time.Now()

	var run EvalRun
	var runErr error

	if r.cfg.RunCase != nil {
		run, runErr = r.cfg.RunCase(runCtx, c)
	} else {
		run = buildMockRun(c, runID, start)
	}

	if run.CaseID == "" {
		run.CaseID = c.ID
	}
	if run.RunID == "" {
		run.RunID = runID
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = start
	}
	if run.Duration == 0 {
		run.Duration = time.Since(start)
	}

	if runErr != nil {
		run.Error = runErr.Error()
	}

	// 评分
	scores, finalScore := r.scoreRun(ctx, run, c)

	return EvalResult{
		Run:        run,
		Scores:     scores,
		FinalScore: finalScore,
		Pass:       finalScore >= c.Scoring.passThreshold(),
	}, nil
}

func (r *EvalRunner) scoreRun(ctx context.Context, run EvalRun, c EvalCase) ([]JudgeScore, float64) {
	if len(r.cfg.Judges) == 0 {
		return nil, 1.0
	}

	scores := make([]JudgeScore, 0, len(r.cfg.Judges))
	var totalWeight, weightedSum float64

	for _, judge := range r.cfg.Judges {
		s, err := judge.Score(ctx, run, c.Expect)
		if err != nil {
			s = JudgeScore{JudgeName: judge.Name(), Score: 0, Reasoning: err.Error(), Pass: false}
		}
		scores = append(scores, s)

		w := c.Scoring.Weights[judge.Name()]
		if w <= 0 {
			w = 1.0
		}
		weightedSum += s.Score * w
		totalWeight += w
	}

	final := 0.0
	if totalWeight > 0 {
		final = weightedSum / totalWeight
	}
	return scores, final
}

// buildMockRun 当 RunCase 未配置时，生成空的 EvalRun（仅用于测试框架自身）。
func buildMockRun(c EvalCase, runID string, start time.Time) EvalRun {
	return EvalRun{
		CaseID:    c.ID,
		RunID:     runID,
		StartedAt: start,
		Duration:  0,
	}
}

// ---- 简单 text/JSON 报告 ------------------------------------------------

// PrintSummary 向标准输出打印评测结果摘要。
func PrintSummary(results []EvalResult) string {
	var sb strings.Builder
	pass, fail := 0, 0
	for _, r := range results {
		status := "✅ PASS"
		if !r.Pass {
			status = "❌ FAIL"
			fail++
		} else {
			pass++
		}
		sb.WriteString(fmt.Sprintf("%s  %-40s score=%.2f  steps=%d",
			status, r.Run.CaseID, r.FinalScore, r.Run.Steps))
		if r.Run.Error != "" {
			sb.WriteString("  error=" + r.Run.Error)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(fmt.Sprintf("\nTotal: %d  Pass: %d  Fail: %d\n", len(results), pass, fail))
	return sb.String()
}

// ---- RunCase helper for kernel integration -----------------------------

// KernelRunFunc 封装一个简单的 kernel-style 运行函数，以便与 EvalRunner 集成。
// outputExtractor 从最终消息列表中提取输出文本。
func KernelRunFunc(
	run func(ctx context.Context, messages []port.Message) ([]port.Message, []ToolCallLog, int, error),
) CaseRunner {
	return func(ctx context.Context, c EvalCase) (EvalRun, error) {
		msgs := resolveMessages(c)
		start := time.Now()
		outMsgs, toolCalls, steps, err := run(ctx, msgs)

		output := ""
		if len(outMsgs) > 0 {
			last := outMsgs[len(outMsgs)-1]
			output = port.ContentPartsToPlainText(last.ContentParts)
		}

		run_ := EvalRun{
			CaseID:    c.ID,
			StartedAt: start,
			Duration:  time.Since(start),
			Steps:     steps,
			Messages:  outMsgs,
			ToolCalls: toolCalls,
			Output:    output,
		}
		if err != nil {
			run_.Error = err.Error()
		}
		return run_, err
	}
}

func resolveMessages(c EvalCase) []port.Message {
	if len(c.Input.Messages) > 0 {
		return c.Input.Messages
	}
	msgs := make([]port.Message, 0, len(c.Input.RawMessages))
	for _, rm := range c.Input.RawMessages {
		msgs = append(msgs, port.Message{
			Role:         port.Role(rm.Role),
			ContentParts: []port.ContentPart{port.TextPart(rm.Content)},
		})
	}
	return msgs
}
