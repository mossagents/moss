package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/model"
	"os"
	"sort"
	"strings"
	"time"
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

	// BaselinePath 指向 baseline JSON 文件。
	BaselinePath string

	// GateScoreDrop 为判定分数回归的阈值，默认 0.03。
	GateScoreDrop float64

	// GateReportOnly 为 true 时仅报告回归，不阻断。
	GateReportOnly bool

	// ReportMode 评测报告模式：grouped|flat。
	ReportMode string

	// PromptVersion 默认 prompt 维度值。
	PromptVersion string

	// BudgetPolicy 默认 budget 维度值。
	BudgetPolicy string
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

func (c RunnerConfig) gateScoreDrop() float64 {
	if c.GateScoreDrop <= 0 {
		return 0.03
	}
	return c.GateScoreDrop
}

func (c RunnerConfig) reportMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.ReportMode))
	if mode == "" {
		return "grouped"
	}
	if mode != "grouped" && mode != "flat" {
		return "grouped"
	}
	return mode
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
		Dimensions: ReportDimensions{
			PromptVersion: strings.TrimSpace(r.cfg.PromptVersion),
			BudgetPolicy:  strings.TrimSpace(r.cfg.BudgetPolicy),
		},
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

// PrintGroupedSummary 按 prompt_version + budget_policy 维度分组输出摘要。
func PrintGroupedSummary(results []EvalResult) string {
	if len(results) == 0 {
		return "\nTotal: 0  Pass: 0  Fail: 0\n"
	}
	groups := GroupResultsByDimensions(results)
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var sb strings.Builder
	totalPass, totalFail := 0, 0
	for _, key := range keys {
		items := groups[key]
		pass, fail := 0, 0
		for _, r := range items {
			if r.Pass {
				pass++
				totalPass++
			} else {
				fail++
				totalFail++
			}
		}
		sb.WriteString(fmt.Sprintf("[%s] total=%d pass=%d fail=%d\n", key, len(items), pass, fail))
	}
	sb.WriteString(fmt.Sprintf("\nTotal: %d  Pass: %d  Fail: %d\n", len(results), totalPass, totalFail))
	return sb.String()
}

// GroupResultsByDimensions 按 prompt_version + budget_policy 维度分组。
func GroupResultsByDimensions(results []EvalResult) map[string][]EvalResult {
	groups := make(map[string][]EvalResult)
	for _, r := range results {
		pv := strings.TrimSpace(r.Dimensions.PromptVersion)
		if pv == "" {
			pv = "unknown"
		}
		bp := strings.TrimSpace(r.Dimensions.BudgetPolicy)
		if bp == "" {
			bp = "unknown"
		}
		key := fmt.Sprintf("prompt_version=%s | budget_policy=%s", pv, bp)
		groups[key] = append(groups[key], r)
	}
	return groups
}

// RenderSummary 根据 RunnerConfig 的报告模式输出摘要文本。
func (r *EvalRunner) RenderSummary(results []EvalResult) string {
	if r.cfg.reportMode() == "flat" {
		return PrintSummary(results)
	}
	return PrintGroupedSummary(results)
}

// ---- RunCase helper for kernel integration -----------------------------

// KernelRunFunc 封装一个简单的 kernel-style 运行函数，以便与 EvalRunner 集成。
// outputExtractor 从最终消息列表中提取输出文本。
func KernelRunFunc(
	run func(ctx context.Context, messages []model.Message) ([]model.Message, []ToolCallLog, int, error),
) CaseRunner {
	return func(ctx context.Context, c EvalCase) (EvalRun, error) {
		msgs := resolveMessages(c)
		start := time.Now()
		outMsgs, toolCalls, steps, err := run(ctx, msgs)

		output := ""
		if len(outMsgs) > 0 {
			last := outMsgs[len(outMsgs)-1]
			output = model.ContentPartsToPlainText(last.ContentParts)
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

func resolveMessages(c EvalCase) []model.Message {
	if len(c.Input.Messages) > 0 {
		return c.Input.Messages
	}
	msgs := make([]model.Message, 0, len(c.Input.RawMessages))
	for _, rm := range c.Input.RawMessages {
		msgs = append(msgs, model.Message{
			Role:         model.Role(rm.Role),
			ContentParts: []model.ContentPart{model.TextPart(rm.Content)},
		})
	}
	return msgs
}

// LoadBaseline 读取 baseline JSON。
func LoadBaseline(path string) (BaselineSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BaselineSnapshot{}, err
	}
	var snapshot BaselineSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return BaselineSnapshot{}, fmt.Errorf("eval: parse baseline %s: %w", path, err)
	}
	return snapshot, nil
}

// WriteBaseline 将当前结果保存为 baseline JSON。
func WriteBaseline(path string, results []EvalResult) error {
	snapshot := BaselineSnapshot{Version: "v1", Cases: make([]BaselineCaseScore, 0, len(results))}
	for _, r := range results {
		snapshot.Cases = append(snapshot.Cases, BaselineCaseScore{
			CaseID:     r.Run.CaseID,
			FinalScore: r.FinalScore,
			Pass:       r.Pass,
		})
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: encode baseline: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("eval: write baseline %s: %w", path, err)
	}
	return nil
}

// CompareBaseline 按阈值比较当前结果与 baseline，返回 gate 判定。
func (r *EvalRunner) CompareBaseline(results []EvalResult) (GateDecision, error) {
	decision := GateDecision{
		ReportOnly: r.cfg.GateReportOnly,
		Threshold:  r.cfg.gateScoreDrop(),
		Current:    results,
	}
	if r.cfg.BaselinePath == "" {
		decision.ReportOnly = true
		decision.Reasons = append(decision.Reasons, "baseline path is empty")
		return decision, nil
	}

	baseline, err := LoadBaseline(r.cfg.BaselinePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			decision.ReportOnly = true
			decision.Reasons = append(decision.Reasons, "baseline file missing; fallback to report-only")
			return decision, nil
		}
		if decision.ReportOnly {
			decision.Reasons = append(decision.Reasons, fmt.Sprintf("baseline load failed: %v", err))
			return decision, nil
		}
		return decision, err
	}

	baseMap := make(map[string]BaselineCaseScore, len(baseline.Cases))
	for _, c := range baseline.Cases {
		baseMap[c.CaseID] = c
	}

	regressedSet := make(map[string]bool)
	for _, cur := range results {
		base, ok := baseMap[cur.Run.CaseID]
		if !ok {
			continue
		}
		drop := base.FinalScore - cur.FinalScore
		if drop > decision.Threshold {
			if !regressedSet[cur.Run.CaseID] {
				decision.Regressed = append(decision.Regressed, cur.Run.CaseID)
				regressedSet[cur.Run.CaseID] = true
			}
			decision.Reasons = append(decision.Reasons,
				fmt.Sprintf("%s score drop %.3f > %.3f", cur.Run.CaseID, drop, decision.Threshold))
		}
		if base.Pass && !cur.Pass {
			if !regressedSet[cur.Run.CaseID] {
				decision.Regressed = append(decision.Regressed, cur.Run.CaseID)
				regressedSet[cur.Run.CaseID] = true
			}
			decision.Reasons = append(decision.Reasons,
				fmt.Sprintf("%s pass->fail", cur.Run.CaseID))
		}
	}

	decision.Blocked = len(decision.Regressed) > 0 && !decision.ReportOnly
	return decision, nil
}

