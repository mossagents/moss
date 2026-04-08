// Package eval 提供 Agent 行为的声明式评测框架。
//
// 核心概念：
//   - EvalCase：一个评测用例（输入 + 期望行为）
//   - Judge：对运行结果进行评分（RuleJudge / LLMJudge）
//   - EvalRunner：批量执行用例并收集结果
//   - EvalResult：单次运行的综合评分
//
// 用法示例：
//
//	runner := eval.NewRunner(eval.RunnerConfig{
//	    RunCase: myRunFunc,
//	    Judges:  []eval.Judge{eval.NewRuleJudge()},
//	})
//	results, err := runner.RunDir(ctx, "cases/")
package eval

import (
	"time"

	mdl "github.com/mossagents/moss/kernel/model"
)

// EvalCase 声明一个评测用例。
type EvalCase struct {
	ID          string        `yaml:"id"          json:"id"`
	Description string        `yaml:"description" json:"description,omitempty"`
	Tags        []string      `yaml:"tags"        json:"tags,omitempty"`
	Input       EvalInput     `yaml:"input"       json:"input"`
	Expect      EvalExpect    `yaml:"expect"      json:"expect"`
	Scoring     ScoringConfig `yaml:"scoring"     json:"scoring,omitempty"`
}

// EvalInput 定义评测的输入条件。
type EvalInput struct {
	// Messages 是发给 Agent 的初始消息序列。
	Messages []mdl.Message `yaml:"-" json:"messages,omitempty"`
	// RawMessages 是 YAML 友好的消息定义，加载时转换为 Messages。
	RawMessages []RawMessage `yaml:"messages" json:"-"`
	// Tools 限制可用工具集（空 = 使用 runner 默认工具集）。
	Tools []string `yaml:"tools" json:"tools,omitempty"`
	// State 设置 session 初始 KV 状态。
	State map[string]any `yaml:"state" json:"state,omitempty"`
	// Fixtures 描述 mock 环境（文件内容、命令输出等）。
	Fixtures []Fixture `yaml:"fixtures" json:"fixtures,omitempty"`
}

// RawMessage 是 YAML 可表达的消息定义。
type RawMessage struct {
	Role    string `yaml:"role"    json:"role"`
	Content string `yaml:"content" json:"content"`
}

// Fixture 描述测试环境中的一条 mock 数据。
type Fixture struct {
	Kind  FixtureKind `yaml:"kind"  json:"kind"`
	Key   string      `yaml:"key"   json:"key"`
	Value string      `yaml:"value" json:"value"`
}

// FixtureKind 枚举 Fixture 类型。
type FixtureKind string

const (
	FixtureFile    FixtureKind = "file"
	FixtureCommand FixtureKind = "command"
	FixtureEnv     FixtureKind = "env"
)

// EvalExpect 描述期望的 Agent 行为。
type EvalExpect struct {
	// Contains/NotContains：最终答复文本必须/不得包含这些子串。
	Contains    []string `yaml:"contains"     json:"contains,omitempty"`
	NotContains []string `yaml:"not_contains" json:"not_contains,omitempty"`
	// ToolCalled/ToolNot：必须/不得调用的工具名称。
	ToolCalled []string `yaml:"tool_called" json:"tool_called,omitempty"`
	ToolNot    []string `yaml:"tool_not"    json:"tool_not,omitempty"`
	// MaxSteps：允许的最大执行步骤数（0 = 不限制）。
	MaxSteps int `yaml:"max_steps" json:"max_steps,omitempty"`
	// Judge：LLM-as-judge 评分标准（可选）。
	Judge *JudgeCriteria `yaml:"judge" json:"judge,omitempty"`
}

// JudgeCriteria 配置 LLM-as-judge 评分。
type JudgeCriteria struct {
	Rubric  string   `yaml:"rubric"   json:"rubric"`
	Aspects []string `yaml:"aspects"  json:"aspects,omitempty"`
	Model   string   `yaml:"model"    json:"model,omitempty"`
}

// ScoringConfig 配置最终得分的计算方式。
type ScoringConfig struct {
	// PassThreshold：通过阈值，默认 0.8。
	PassThreshold float64 `yaml:"pass_threshold" json:"pass_threshold,omitempty"`
	// Weights：各 judge 的权重（key = judge name）。
	Weights map[string]float64 `yaml:"weights" json:"weights,omitempty"`
}

func (c ScoringConfig) passThreshold() float64 {
	if c.PassThreshold <= 0 {
		return 0.8
	}
	return c.PassThreshold
}

// ToolCallLog 记录一次工具调用日志。
type ToolCallLog struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// EvalRun 记录一次评测执行的完整状态。
type EvalRun struct {
	CaseID    string        `json:"case_id"`
	RunID     string        `json:"run_id"`
	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"duration"`
	Steps     int           `json:"steps"`
	Messages  []mdl.Message `json:"messages"`
	ToolCalls []ToolCallLog `json:"tool_calls,omitempty"`
	Output    string        `json:"output"`
	Error     string        `json:"error,omitempty"`
}

// JudgeScore 是单个 judge 的评分结果。
type JudgeScore struct {
	JudgeName string             `json:"judge_name"`
	Score     float64            `json:"score"`
	Reasoning string             `json:"reasoning,omitempty"`
	Breakdown map[string]float64 `json:"breakdown,omitempty"`
	Pass      bool               `json:"pass"`
}

// EvalResult 是单个 EvalCase 的综合评测结果。
type EvalResult struct {
	Run        EvalRun      `json:"run"`
	Scores     []JudgeScore `json:"scores"`
	FinalScore float64      `json:"final_score"`
	Pass       bool         `json:"pass"`
	Dimensions ReportDimensions `json:"dimensions,omitempty"`
}

// ReportDimensions captures prompt/budget dimensions used in grouped eval reports.
type ReportDimensions struct {
	PromptVersion string `json:"prompt_version,omitempty"`
	BudgetPolicy  string `json:"budget_policy,omitempty"`
}

// BaselineSnapshot 定义基线分数快照文件结构。
type BaselineSnapshot struct {
	Version string              `json:"version,omitempty"`
	Cases   []BaselineCaseScore `json:"cases"`
}

// BaselineCaseScore 是单个 case 的基线记录。
type BaselineCaseScore struct {
	CaseID     string  `json:"case_id"`
	FinalScore float64 `json:"final_score"`
	Pass       bool    `json:"pass"`
}

// GateDecision 表示基线回归判定结果。
type GateDecision struct {
	Blocked    bool       `json:"blocked"`
	ReportOnly bool       `json:"report_only"`
	Threshold  float64    `json:"threshold"`
	Regressed  []string   `json:"regressed,omitempty"`
	Reasons    []string   `json:"reasons,omitempty"`
	Current    []EvalResult `json:"current,omitempty"`
}

