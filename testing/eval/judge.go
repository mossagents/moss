package eval

import (
	"context"
	"fmt"
	"github.com/mossagents/moss/kernel/model"
	"strings"
)

// Judge 对一次 EvalRun 进行评分。
type Judge interface {
	Name() string
	Score(ctx context.Context, run EvalRun, expect EvalExpect) (JudgeScore, error)
}

// ---- RuleJudge ---------------------------------------------------------

// RuleJudge 基于规则进行评分，无需 LLM 调用。
// 检查：Contains / NotContains / ToolCalled / ToolNot / MaxSteps。
type RuleJudge struct{}

// NewRuleJudge 创建基于规则的 Judge。
func NewRuleJudge() Judge { return &RuleJudge{} }

func (j *RuleJudge) Name() string { return "rule" }

func (j *RuleJudge) Score(_ context.Context, run EvalRun, expect EvalExpect) (JudgeScore, error) {
	var failures []string
	checks := 0
	passed := 0

	// MaxSteps
	if expect.MaxSteps > 0 {
		checks++
		if run.Steps <= expect.MaxSteps {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("steps %d > max %d", run.Steps, expect.MaxSteps))
		}
	}

	// Contains
	for _, sub := range expect.Contains {
		checks++
		if strings.Contains(run.Output, sub) {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("output missing %q", sub))
		}
	}

	// NotContains
	for _, sub := range expect.NotContains {
		checks++
		if !strings.Contains(run.Output, sub) {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("output contains forbidden %q", sub))
		}
	}

	// ToolCalled
	calledSet := toolCallSet(run.ToolCalls)
	for _, tool := range expect.ToolCalled {
		checks++
		if calledSet[tool] {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("tool %q not called", tool))
		}
	}

	// ToolNot
	for _, tool := range expect.ToolNot {
		checks++
		if !calledSet[tool] {
			passed++
		} else {
			failures = append(failures, fmt.Sprintf("forbidden tool %q was called", tool))
		}
	}

	score := 1.0
	if checks > 0 {
		score = float64(passed) / float64(checks)
	}

	reasoning := "all checks passed"
	if len(failures) > 0 {
		reasoning = strings.Join(failures, "; ")
	}

	return JudgeScore{
		JudgeName: j.Name(),
		Score:     score,
		Reasoning: reasoning,
		Pass:      score >= 0.8,
	}, nil
}

func toolCallSet(logs []ToolCallLog) map[string]bool {
	s := make(map[string]bool, len(logs))
	for _, l := range logs {
		s[l.Name] = true
	}
	return s
}

// ---- LLMJudge ----------------------------------------------------------

// LLMJudge 使用 LLM-as-judge 对运行结果进行语义评分。
type LLMJudge struct {
	LLM model.LLM
}

// NewLLMJudge 创建基于 LLM 的 Judge。
func NewLLMJudge(llm model.LLM) Judge { return &LLMJudge{LLM: llm} }

func (j *LLMJudge) Name() string { return "llm" }

func (j *LLMJudge) Score(ctx context.Context, run EvalRun, expect EvalExpect) (JudgeScore, error) {
	if expect.Judge == nil {
		return JudgeScore{JudgeName: j.Name(), Score: 1.0, Pass: true, Reasoning: "no criteria defined"}, nil
	}
	if j.LLM == nil {
		return JudgeScore{}, fmt.Errorf("LLMJudge: LLM is nil")
	}

	prompt := buildJudgePrompt(run, *expect.Judge)
	req := model.CompletionRequest{
		Messages: []model.Message{
			{
				Role:         model.RoleSystem,
				ContentParts: []model.ContentPart{model.TextPart(judgeSystemPrompt)},
			},
			{
				Role:         model.RoleUser,
				ContentParts: []model.ContentPart{model.TextPart(prompt)},
			},
		},
		Config: model.ModelConfig{
			Model:     expect.Judge.Model,
			MaxTokens: 512,
		},
		ResponseFormat: &model.ResponseFormat{Type: "json_object"},
	}

	resp, err := j.LLM.Complete(ctx, req)
	if err != nil {
		return JudgeScore{}, fmt.Errorf("LLMJudge: %w", err)
	}

	text := model.ContentPartsToPlainText(resp.Message.ContentParts)
	score, reasoning, err := parseJudgeResponse(text)
	if err != nil {
		return JudgeScore{JudgeName: j.Name(), Score: 0, Reasoning: text, Pass: false}, nil
	}

	return JudgeScore{
		JudgeName: j.Name(),
		Score:     score,
		Reasoning: reasoning,
		Pass:      score >= 0.8,
	}, nil
}

const judgeSystemPrompt = `你是一个严格的 AI 评测员。
请基于给定的评分标准，对 Agent 的表现进行客观评分。
返回 JSON：{"score": 0.0-1.0, "reasoning": "..."}`

func buildJudgePrompt(run EvalRun, criteria JudgeCriteria) string {
	var sb strings.Builder
	sb.WriteString("评分标准：\n")
	sb.WriteString(criteria.Rubric)
	sb.WriteString("\n\nAgent 最终输出：\n")
	sb.WriteString(run.Output)
	if len(run.ToolCalls) > 0 {
		sb.WriteString("\n\n工具调用记录：\n")
		for _, tc := range run.ToolCalls {
			sb.WriteString(fmt.Sprintf("- %s(%s)\n", tc.Name, tc.Arguments))
		}
	}
	sb.WriteString(fmt.Sprintf("\n执行步骤数：%d", run.Steps))
	return sb.String()
}

func parseJudgeResponse(text string) (float64, string, error) {
	// 简单 JSON 解析：寻找 "score": <float> 和 "reasoning": "<str>"
	var score float64
	var reasoning string

	text = strings.TrimSpace(text)
	// 尝试从 JSON 中提取 score
	if idx := strings.Index(text, `"score"`); idx >= 0 {
		rest := text[idx+len(`"score"`):]
		rest = strings.TrimLeft(rest, " \t\n\r:")
		if _, err := fmt.Sscanf(rest, "%f", &score); err != nil {
			score = 0
		}
	}
	if idx := strings.Index(text, `"reasoning"`); idx >= 0 {
		rest := text[idx+len(`"reasoning"`):]
		rest = strings.TrimLeft(rest, " \t\n\r:\"")
		if end := strings.Index(rest, `"`); end >= 0 {
			reasoning = rest[:end]
		}
	}
	if score == 0 && reasoning == "" {
		return 0, text, fmt.Errorf("failed to parse judge response")
	}
	return score, reasoning, nil
}

// ---- CompositeJudge ----------------------------------------------------

// CompositeJudge 将多个 Judge 的结果加权聚合。
type CompositeJudge struct {
	judges  []Judge
	weights map[string]float64
}

// NewCompositeJudge 创建聚合 Judge。weights 的 key 是 judge.Name()。
func NewCompositeJudge(judges []Judge, weights map[string]float64) Judge {
	return &CompositeJudge{judges: judges, weights: weights}
}

func (j *CompositeJudge) Name() string { return "composite" }

func (j *CompositeJudge) Score(ctx context.Context, run EvalRun, expect EvalExpect) (JudgeScore, error) {
	var totalWeight, weightedScore float64
	var reasons []string

	for _, judge := range j.judges {
		s, err := judge.Score(ctx, run, expect)
		if err != nil {
			continue
		}
		w := j.weights[judge.Name()]
		if w <= 0 {
			w = 1.0
		}
		weightedScore += s.Score * w
		totalWeight += w
		reasons = append(reasons, fmt.Sprintf("%s(%.2f)", judge.Name(), s.Score))
	}

	score := 0.0
	if totalWeight > 0 {
		score = weightedScore / totalWeight
	}

	return JudgeScore{
		JudgeName: j.Name(),
		Score:     score,
		Reasoning: strings.Join(reasons, ", "),
		Pass:      score >= 0.8,
	}, nil
}
