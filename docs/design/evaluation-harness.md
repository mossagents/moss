# Evaluation Harness 设计

> 状态：**草稿** · 优先级：P0 · 关联待办：P0-D2 / P0-I4

---

## 1. 问题陈述

Moss 目前**完全缺失**评测基础设施：
- 无法对 agent 行为进行系统性评分
- 无回归测试保障，任何 prompt/logic 改动都是"赌博"
- 无 judge 机制（LLM-as-judge 或 rule-based）
- 无 eval dataset 管理
- 现有 `testing/` 目录仅有极少量示例，无框架

这是整个项目**质量保障的最大盲区**。

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 可声明式定义 eval case | YAML/JSON 格式，可 git 管理 |
| 支持多种 judge | rule-based / LLM-as-judge / human |
| CI 可集成 | `go test ./testing/eval/...` 即可运行 |
| 支持批量并行 | 大批量 eval 可并行执行 |
| 结果可对比 | 同一 case 在不同版本/配置下的得分变化 |
| 可扩展 Metric | 支持自定义评分维度 |

---

## 3. 核心概念

### 3.1 EvalCase — 评测用例

```go
// testing/eval/types.go
type EvalCase struct {
    ID          string            `yaml:"id"`
    Description string            `yaml:"description,omitempty"`
    Tags        []string          `yaml:"tags,omitempty"`

    // 输入
    Input       EvalInput         `yaml:"input"`

    // 期望行为（至少一个）
    Expect      EvalExpect        `yaml:"expect"`

    // 评分配置
    Scoring     ScoringConfig     `yaml:"scoring,omitempty"`
}

type EvalInput struct {
    // 用户消息
    Messages    []Message         `yaml:"messages"`
    // 可用工具（空=继承默认工具集）
    Tools       []string          `yaml:"tools,omitempty"`
    // Session 初始状态
    State       map[string]any    `yaml:"state,omitempty"`
    // 环境 mock（文件系统快照、命令输出 stub）
    Fixtures    []Fixture         `yaml:"fixtures,omitempty"`
}

type EvalExpect struct {
    // 最终答复必须包含的关键词/正则
    Contains    []string          `yaml:"contains,omitempty"`
    NotContains []string          `yaml:"not_contains,omitempty"`
    // 必须/不得调用的工具
    ToolCalled  []string          `yaml:"tool_called,omitempty"`
    ToolNot     []string          `yaml:"tool_not,omitempty"`
    // 最大步骤数
    MaxSteps    int               `yaml:"max_steps,omitempty"`
    // LLM Judge 评分
    Judge       *JudgeCriteria    `yaml:"judge,omitempty"`
    // 自定义 Go asserter（用于编程式断言）
    Asserter    string            `yaml:"asserter,omitempty"` // 注册名
}
```

### 3.2 Fixture — 环境桩

```go
type Fixture struct {
    Kind  FixtureKind `yaml:"kind"`  // file / command / env
    Key   string      `yaml:"key"`   // 文件路径 / 命令模式 / 环境变量名
    Value string      `yaml:"value"` // 内容 / 输出 / 值
}

type FixtureKind string
const (
    FixtureFile    FixtureKind = "file"
    FixtureCommand FixtureKind = "command"
    FixtureEnv     FixtureKind = "env"
)
```

### 3.3 Judge — 评判器

```go
// testing/eval/judge.go
type Judge interface {
    Name() string
    Score(ctx context.Context, run EvalRun, criteria JudgeCriteria) (JudgeScore, error)
}

type JudgeCriteria struct {
    Rubric   string   `yaml:"rubric"`   // 评分标准自然语言描述
    Aspects  []string `yaml:"aspects,omitempty"` // 评分维度，如 ["accuracy", "conciseness"]
    Model    string   `yaml:"model,omitempty"`   // 指定用于 judge 的 LLM 模型
}

type JudgeScore struct {
    Score      float64            `json:"score"`      // 0.0-1.0
    Reasoning  string             `json:"reasoning"`
    Breakdown  map[string]float64 `json:"breakdown,omitempty"` // 各维度得分
    Pass       bool               `json:"pass"`               // score >= threshold
}
```

**内置 Judge 实现**：

| Judge | 说明 |
|-------|------|
| `RuleJudge` | 基于 Contains/ToolCalled 等规则，无 LLM 开销 |
| `LLMJudge` | 将完整对话 + 期望 + rubric 发给 LLM 评分 |
| `CompositeJudge` | 多个 Judge 加权聚合 |

### 3.4 EvalRun — 运行结果

```go
type EvalRun struct {
    CaseID     string          `json:"case_id"`
    RunID      string          `json:"run_id"`
    StartedAt  time.Time       `json:"started_at"`
    Duration   time.Duration   `json:"duration"`
    Steps      int             `json:"steps"`
    Messages   []Message       `json:"messages"`   // 完整对话历史
    ToolCalls  []ToolCallLog   `json:"tool_calls"`
    Error      string          `json:"error,omitempty"`
}

type EvalResult struct {
    Run        EvalRun         `json:"run"`
    Scores     []JudgeScore    `json:"scores"`
    FinalScore float64         `json:"final_score"` // 加权平均
    Pass       bool            `json:"pass"`
    Metadata   map[string]any  `json:"metadata,omitempty"`
}
```

---

## 4. Runner 设计

### 4.1 EvalRunner

```go
// testing/eval/runner.go
type RunnerConfig struct {
    Kernel      *kernel.Kernel
    Judges      []Judge
    Parallelism int           // 并发数，默认 4
    Timeout     time.Duration // 单 case 超时，默认 60s
    Reporter    Reporter
}

type EvalRunner struct {
    cfg RunnerConfig
}

func (r *EvalRunner) Run(ctx context.Context, cases []EvalCase) ([]EvalResult, error)
func (r *EvalRunner) RunFile(ctx context.Context, path string) ([]EvalResult, error)
func (r *EvalRunner) RunDir(ctx context.Context, dir string) ([]EvalResult, error)
```

### 4.2 执行流程

```
for each EvalCase (并发):
  1. 构建 Fixture 环境（mock workspace / commands）
  2. 创建隔离 Session（不污染真实状态）
  3. kernel.Run(input.Messages) 驱动 agent
  4. 收集 EvalRun（对话历史 + tool calls + steps）
  5. 依次调用所有 Judge.Score()
  6. 聚合 EvalResult
  7. 报告结果
```

### 4.3 Fixture 沙箱

Eval 执行时使用 in-memory workspace mock，不触碰真实文件系统：

```go
type MockWorkspace struct {
    files    map[string]string  // 路径 → 内容
    commands map[string]string  // 命令模式 → 输出
}
```

---

## 5. Reporter

```go
type Reporter interface {
    Report(results []EvalResult) error
}
```

**内置 Reporter**：

| Reporter | 说明 |
|----------|------|
| `TextReporter` | 命令行表格输出 |
| `JSONReporter` | 输出到文件，供 CI artifact |
| `MarkdownReporter` | 生成 markdown 报告，供 PR comment |

---

## 6. 声明式 Eval Case 格式（YAML）

```yaml
# testing/eval/cases/coding/fix_bug.yaml
id: fix-jwt-expiry-bug
description: Agent 应能定位并修复 JWT 过期时间硬编码问题
tags: [coding, bug-fix, auth]

input:
  messages:
    - role: user
      content: "请修复 auth/jwt.go 中 token 过期时间硬编码的问题，改为从配置文件读取"
  tools: [read_file, write_file, search_code]
  fixtures:
    - kind: file
      key: auth/jwt.go
      value: |
        func GenerateToken(userID string) (string, error) {
            expiry := time.Now().Add(1 * time.Hour)  // TODO: 从配置读取
            ...
        }
    - kind: file
      key: config/config.go
      value: |
        type Config struct {
            TokenExpiry time.Duration `yaml:"token_expiry"`
        }

expect:
  tool_called: [read_file, write_file]
  contains: ["TokenExpiry", "config"]
  max_steps: 8
  judge:
    rubric: "Agent 是否正确地从配置读取 token 过期时间，而不是硬编码？修改是否语法正确？"
    aspects: [correctness, code_quality]

scoring:
  pass_threshold: 0.8
  weights:
    rule: 0.4
    judge: 0.6
```

---

## 7. Go Test 集成

```go
// testing/eval/eval_test.go
func TestEvalSuite(t *testing.T) {
    k := testutil.NewKernel(t)
    runner := eval.NewRunner(eval.RunnerConfig{
        Kernel:      k,
        Judges:      []eval.Judge{eval.NewRuleJudge(), eval.NewLLMJudge(k.LLM)},
        Parallelism: 4,
        Reporter:    eval.NewTextReporter(os.Stdout),
    })
    
    results, err := runner.RunDir(t.Context(), "cases/")
    require.NoError(t, err)
    
    for _, r := range results {
        t.Run(r.Run.CaseID, func(t *testing.T) {
            assert.True(t, r.Pass, "case %s failed with score %.2f\n%s",
                r.Run.CaseID, r.FinalScore, r.Scores[0].Reasoning)
        })
    }
}
```

---

## 8. 文件结构规划

```
testing/
├── eval/
│   ├── types.go          # EvalCase, EvalRun, EvalResult
│   ├── judge.go          # Judge 接口 + RuleJudge
│   ├── judge_llm.go      # LLMJudge 实现
│   ├── runner.go         # EvalRunner
│   ├── reporter.go       # Reporter 接口 + 内置实现
│   ├── fixtures.go       # MockWorkspace + Fixture 处理
│   ├── loader.go         # YAML/JSON case 加载器
│   ├── eval_test.go      # 框架自身的测试
│   └── cases/
│       ├── coding/
│       │   ├── fix_bug.yaml
│       │   └── write_test.yaml
│       ├── planning/
│       │   └── multi_step_task.yaml
│       └── tool_use/
│           └── filesystem_ops.yaml
└── testutil/
    ├── kernel.go         # NewKernel() 测试辅助
    └── mock_workspace.go # MockWorkspace 共用
```

---

## 9. CI 集成建议

```yaml
# .github/workflows/eval.yml
name: Eval Suite
on: [push, pull_request]
jobs:
  eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Run eval suite
        run: go test ./testing/eval/... -v -timeout 300s
        env:
          MOSS_EVAL_MODEL: gpt-4o-mini  # 低成本 judge 模型
      - name: Upload eval report
        uses: actions/upload-artifact@v4
        with:
          name: eval-report
          path: eval-report.json
```

---

## 10. 实现顺序

1. 定义核心类型（`types.go`）
2. 实现 `RuleJudge`（无 LLM 依赖，先跑通框架）
3. 实现 `MockWorkspace` + `EvalRunner`（串行版本）
4. 添加 3-5 个 seed eval cases（YAML 格式）
5. 接入 `go test`，CI 跑通
6. 实现 `LLMJudge`（依赖 LLM 配置）
7. 实现并发 Runner + 多种 Reporter

---

*文档状态：草稿 · 待评审*
