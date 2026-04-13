package loop

import (
	"sync"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// ContextCompressionStrategy 枚举上下文压缩策略。
type ContextCompressionStrategy string

const (
	// CompressionTruncate 直接截断（默认），保留最近消息，丢弃旧消息。
	CompressionTruncate ContextCompressionStrategy = "truncate"
	// CompressionSummary 使用 LLM 生成摘要替代被压缩的历史。
	CompressionSummary ContextCompressionStrategy = "summary"
	// CompressionSliding 滑动窗口，窗口外内容一次性生成静态摘要。
	CompressionSliding ContextCompressionStrategy = "sliding"
	// CompressionPriority 基于重要性评分保留高价值消息。
	CompressionPriority ContextCompressionStrategy = "priority"
)

// ContextCompressionConfig 配置 AgentLoop 的上下文压缩行为。
// 当设置了 Strategy 时，AgentLoop 会自动注册对应的压缩 hook。
// 若已手动通过 kernel.WithPlugin() 注册压缩 hook，无需设置此字段。
type ContextCompressionConfig struct {
	// Strategy 压缩策略，默认空（不自动注入，依赖已注册的 hook）。
	// 显式设置后，AgentLoop 会自动注入压缩 hook。
	Strategy ContextCompressionStrategy

	// MaxContextTokens 整个 context window 的 token 上限，0 = 不自动触发压缩。
	MaxContextTokens int

	// KeepRecent 压缩时保留的最新消息数，默认 20。
	KeepRecent int

	// SummaryPrompt 摘要指令（仅 summary 策略使用）。
	SummaryPrompt string

	// MaxSummaryTokens 单次摘要最大 token 数（仅 summary 策略），默认 800。
	MaxSummaryTokens int

	// WindowSize 滑动窗口大小（仅 sliding 策略），默认 30。
	WindowSize int

	// MinScore 最低保留分数（仅 priority 策略），默认 0.0。
	MinScore float64

	// Tokenizer 用于精确 token 计数，nil 时使用字符/4 估算。
	// 设置此字段后，所有压缩中间件将统一使用该 Tokenizer。
	Tokenizer model.Tokenizer
}

// LoopConfig 配置 Agent Loop 的行为。
type LoopConfig struct {
	MaxIterations      int                      // 最大循环次数（默认 50）
	StopWhen           func(model.Message) bool // 自定义停止条件
	ParallelToolCall   bool                     // 启用并行工具调用（默认 false，串行执行）
	MaxConcurrentTools int                      // 并行工具调用的最大并发数（默认 8，0 表示使用默认值）
	LLMRetry           RetryConfig              // LLM 调用重试配置
	LLMBreaker         *retry.Breaker           // LLM 调用熔断器（可选）
	// ContextCompression 配置自动上下文压缩（可选）。
	// 设置后 AgentLoop 会在启动时自动将压缩 hook 添加到 BeforeLLM pipeline。
	ContextCompression ContextCompressionConfig
}

// RetryConfig 复用 retry.Config，避免 loop 与其他组件维护多套重试配置定义。
type RetryConfig = retry.Config

type callAttemptResult struct {
	resp      *model.CompletionResponse
	retryable bool
	err       error
}

func (c LoopConfig) maxIter() int {
	if c.MaxIterations <= 0 {
		return 50
	}
	return c.MaxIterations
}

// AgentLoop 组合所有子系统，驱动 Agent 的 think→act→observe 循环。
type AgentLoop struct {
	LLM                 model.LLM
	Tools               tool.Registry
	Hooks               *hooks.Registry
	IO                  io.UserIO
	Config              LoopConfig
	Observer            observe.Observer // 可观测性观察者（可选，默认 NoOpObserver）
	RunID               string
	AgentName           string // name of the agent driving this loop (used in yielded events)
	sidefxMu            sync.Mutex
	eventSeq            uint64
	currentTurn         TurnPlan
	compressionInjected bool                             // 防止 Run() 重复注入压缩 hook
	eventYield          func(*session.Event, error) bool // internal: set by RunYield
	yieldStopped        bool                             // internal: set when eventYield returns false
}

// emitAgentEvent yields an agent-level event (LLM response, tool result) to the EventYield callback.
// Returns true if the loop should continue, false if the consumer requested stop.
func (l *AgentLoop) emitAgentEvent(event *session.Event) bool {
	if l.eventYield == nil {
		return true
	}
	if l.yieldStopped {
		return false
	}
	if event.ID == "" {
		event.ID = l.nextEventID("agent")
	}
	if !l.eventYield(event, nil) {
		l.yieldStopped = true
		return false
	}
	return true
}

// SessionResult 是一次 Session 执行的结果。
type SessionResult struct {
	SessionID  string           `json:"session_id"`
	Success    bool             `json:"success"`
	Output     string           `json:"output"`
	Steps      int              `json:"steps"`
	TokensUsed model.TokenUsage `json:"tokens_used"`
	Error      string           `json:"error,omitempty"`
}

func (l *AgentLoop) observer() observe.Observer {
	if l.Observer != nil {
		return l.Observer
	}
	return observe.NoOpObserver{}
}
