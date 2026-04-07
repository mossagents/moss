# Context 压缩策略设计

> 状态：**草稿** · 优先级：P0 · 关联待办：P0-D3 / P0-I5

---

## 1. 问题陈述

`kernel/session/session.go` 中 `TruncateMessages()` 是唯一的 context 管理策略：
- **简单截断**：直接丢弃最旧的消息，导致信息丢失
- **无摘要**：无法保留历史关键决策和错误修正
- **无区分**：system message、tool call、普通对话一视同仁
- **无 token 感知**：按条数截断而非 token 数截断
- **无策略切换**：无法根据场景选择不同压缩方式

在长任务（>20轮）中，这个问题会导致 agent 忘记早期的关键上下文。

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 多策略支持 | 截断 / 摘要 / 滑动窗口 / 优先级保留 |
| Token 感知 | 基于实际 token 数决策，而非消息条数 |
| 重要性感知 | system prompt / error / decision 优先保留 |
| 可配置 | 每个 agent/session 可指定不同策略 |
| 可扩展 | 策略接口标准化，易于添加新策略 |
| 向后兼容 | 默认行为不变（截断），需显式开启新策略 |

---

## 3. 接口设计

### 3.1 Compressor 接口

```go
// kernel/session/compressor.go
type Compressor interface {
    // Compress 在 messages 超出限制时压缩历史
    // 返回压缩后的 messages，以及是否执行了压缩
    Compress(ctx context.Context, messages []Message, limit TokenLimit) ([]Message, bool, error)
}

type TokenLimit struct {
    MaxTokens    int     // 总 token 上限
    ReserveRatio float64 // 预留给响应的比例，默认 0.3
}

// EffectiveLimit 返回实际可用于历史的 token 数
func (l TokenLimit) EffectiveLimit() int {
    return int(float64(l.MaxTokens) * (1 - l.ReserveRatio))
}
```

### 3.2 Session 集成

```go
// kernel/session/session.go（扩展）
type Session struct {
    // ... 现有字段 ...
    
    // 压缩策略（nil = TruncateCompressor 默认行为）
    Compressor Compressor
    
    // token 计数器（由 port.Tokenizer 提供）
    tokenizer  port.Tokenizer
}

// PrepareMessages 返回压缩后的 messages，用于 LLM 调用前
func (s *Session) PrepareMessages(ctx context.Context, limit TokenLimit) ([]Message, error) {
    if s.Compressor == nil {
        return s.TruncateMessages(limit.EffectiveLimit()), nil
    }
    compressed, _, err := s.Compressor.Compress(ctx, s.Messages, limit)
    return compressed, err
}
```

---

## 4. 内置压缩策略

### 4.1 TruncateCompressor（默认，兼容现有行为）

```go
type TruncateCompressor struct {
    // KeepSystem 始终保留 system messages
    KeepSystem bool
    // KeepRecent N 条最新消息不压缩
    KeepRecent int
}
```

**行为**：
- 始终保留所有 system messages
- 始终保留最新 `KeepRecent` 条消息
- 删除其余最旧的消息，直到满足 token 限制

---

### 4.2 SummaryCompressor（推荐用于长任务）

```go
type SummaryCompressor struct {
    LLM         port.LLM
    // 触发摘要的 token 比例阈值（默认 0.8）
    TriggerRatio float64
    // 每次摘要时压缩的消息比例（默认 0.5，即压缩前一半）
    CompressRatio float64
    // 摘要指令（发给 LLM 的 prompt）
    SummaryPrompt string
    // 最大摘要长度（token）
    MaxSummaryTokens int
    // 摘要缓存（避免重复摘要相同内容）
    cache summaryCache
}
```

**执行流程**：
```
1. 检查当前 token 数是否超过 TriggerRatio × MaxTokens
2. 若未超过 → 直接返回原 messages
3. 取前 CompressRatio 比例的历史消息（保留 system + recent N 条）
4. 调用 LLM 生成摘要（缓存结果避免重复调用）
5. 将摘要作为一条特殊 summary message 替换被压缩的消息
6. 返回：system messages + [summary message] + recent messages
```

**摘要消息格式**：
```go
Message{
    Role: "system",
    Content: "[对话历史摘要]\n" + summaryText,
    Metadata: map[string]any{
        "type":              "summary",
        "compressed_count":  42,
        "compressed_tokens": 8500,
        "generated_at":      time.Now(),
    },
}
```

**默认摘要 Prompt**：
```
请将以下对话历史压缩为简洁的摘要，重点保留：
1. 用户的核心目标和约束
2. 已做出的重要决策
3. 已执行的关键操作及其结果
4. 遇到的错误及解决方法
5. 当前进度状态

对话历史：
{messages}

输出格式：自然语言段落，不超过500词。
```

---

### 4.3 SlidingWindowCompressor

```go
type SlidingWindowCompressor struct {
    // 始终保留最近 N 条消息
    WindowSize int
    // 对窗口外的消息生成固定摘要（一次性，不重新摘要）
    Summarizer func(ctx context.Context, msgs []Message) (string, error)
}
```

**行为**：维护一个固定大小的滑动窗口，窗口外的内容只保留一条静态摘要（首次计算后不再更新）。适合工具调用密集型场景。

---

### 4.4 PriorityCompressor（高级场景）

```go
type PriorityCompressor struct {
    // 消息重要性评分函数
    Scorer     MessageScorer
    // 最低保留分数
    MinScore   float64
    // 始终保留最近 N 条（不参与评分淘汰）
    KeepRecent int
}

type MessageScorer interface {
    Score(msg Message) float64
}
```

**内置 Scorer**：
```go
// RuleScorer 基于规则评分
type RuleScorer struct{}
func (s RuleScorer) Score(msg Message) float64 {
    score := 0.5
    // system messages 最重要
    if msg.Role == "system" { return 1.0 }
    // 包含 error 的消息较重要
    if strings.Contains(msg.Content, "error") || strings.Contains(msg.Content, "failed") {
        score += 0.3
    }
    // tool call 结果适中
    if msg.Role == "tool" { score += 0.1 }
    return min(score, 1.0)
}
```

---

## 5. Tokenizer 接口

为实现 token 感知压缩，需要在 `port` 中定义 Tokenizer：

```go
// kernel/port/tokenizer.go
type Tokenizer interface {
    // CountTokens 返回消息序列的 token 数估算
    CountTokens(messages []session.Message) (int, error)
    // CountString 返回字符串的 token 数估算
    CountString(s string) (int, error)
}

// SimpleTokenizer 基于字符数估算（1 token ≈ 4 chars）
// 用于无法获取精确 tokenizer 的场景
type SimpleTokenizer struct{}
```

---

## 6. 配置集成

### 6.1 LoopConfig 扩展

```go
// kernel/loop/loop.go（扩展）
type LoopConfig struct {
    // ... 现有字段 ...
    
    // Context 压缩配置
    ContextCompression ContextCompressionConfig
}

type ContextCompressionConfig struct {
    // 策略：truncate（默认）/ summary / sliding / priority
    Strategy   string
    // MaxContextTokens：整个 context window 的 token 上限
    // 0 = 不限制（依赖 LLM 自身报错）
    MaxContextTokens int
    // ReserveForResponse：为响应预留的 token 比例，默认 0.3
    ReserveForResponse float64
    // SummaryCompressor 专属配置
    SummaryPrompt    string
    MaxSummaryTokens int
}
```

### 6.2 使用示例

```go
// 使用摘要压缩（推荐用于长任务）
kernel.Run(ctx, session, input,
    kernel.WithLoopConfig(loop.LoopConfig{
        ContextCompression: loop.ContextCompressionConfig{
            Strategy:         "summary",
            MaxContextTokens: 128000,
        },
    }),
)
```

---

## 7. 压缩策略对比

| 策略 | LLM 调用 | 信息保留 | 适用场景 |
|------|----------|----------|----------|
| `truncate` | 否 | 低（直接丢弃）| 短对话、兼容模式 |
| `summary` | 是（压缩时）| 高（摘要保留要点）| 长任务、复杂 agent |
| `sliding` | 可选（一次）| 中（窗口外仅有静态摘要）| 工具密集型、流式任务 |
| `priority` | 否 | 中高（保留高分消息）| 有明确优先级的场景 |

---

## 8. 文件结构规划

```
kernel/session/
├── session.go          # 扩展 PrepareMessages()
├── compressor.go       # Compressor 接口
├── truncate.go         # TruncateCompressor（现有逻辑迁移）
├── summary.go          # SummaryCompressor
├── sliding.go          # SlidingWindowCompressor
├── priority.go         # PriorityCompressor + MessageScorer
└── compressor_test.go  # 各策略单元测试

kernel/port/
└── tokenizer.go        # Tokenizer 接口 + SimpleTokenizer
```

---

## 9. 实现顺序

1. 定义 `port.Tokenizer` 接口 + `SimpleTokenizer` 实现
2. 定义 `session.Compressor` 接口
3. 将现有 `TruncateMessages()` 重构为 `TruncateCompressor`
4. 扩展 `LoopConfig` 支持 `ContextCompressionConfig`
5. 在 `AgentLoop.Run()` 中替换为 `PrepareMessages()` 调用
6. 实现 `SummaryCompressor`（最高价值）
7. 实现 `SlidingWindowCompressor` 和 `PriorityCompressor`
8. 添加全套单元测试

---

## 10. 边界条件

- **摘要失败**：`SummaryCompressor` 摘要调用失败时，降级为 `TruncateCompressor` 行为
- **首轮消息**：若 session 只有 1-2 条消息但已超 token 限制，直接截断（不摘要）
- **纯 tool call 历史**：摘要时需包含 tool call 的 input/output，确保 agent 知道做过什么
- **多轮摘要**：摘要消息本身可以被再次摘要（递归摘要），但需限制最大递归深度

---

*文档状态：草稿 · 待评审*
