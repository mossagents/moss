package session

import (
	"context"
	"time"

	"github.com/mossagents/moss/kernel/model"
)

// LifecycleStage 表示 Session 生命周期 hook 的阶段。
type LifecycleStage string

const (
	LifecycleCreated   LifecycleStage = "created"
	LifecycleStarted   LifecycleStage = "started"
	LifecycleCompleted LifecycleStage = "completed"
	LifecycleFailed    LifecycleStage = "failed"
	LifecycleCancelled LifecycleStage = "cancelled"
	// LifecycleBudgetExhausted 预算耗尽（§14.4）。
	LifecycleBudgetExhausted LifecycleStage = "budget_exhausted"
)

// LifecycleResult 描述一次 Session 运行的最终结果摘要。
type LifecycleResult struct {
	Success         bool                  `json:"success"`
	Status          LifecycleStage        `json:"status,omitempty"`
	Output          string                `json:"output,omitempty"`
	Steps           int                   `json:"steps"`
	TokensUsed      model.TokenUsage      `json:"tokens_used"`
	Error           string                `json:"error,omitempty"`
	BudgetExhausted *BudgetExhaustedDetail `json:"budget_exhausted,omitempty"`
}

// BudgetExhaustedDetail 预算耗尽的详细信息（§14.4）。
type BudgetExhaustedDetail struct {
	BudgetKind    string `json:"budget_kind"` // token / step / time / thinking_token
	ConsumedValue int    `json:"consumed_value"`
	LimitValue    int    `json:"limit_value"`
}

// LifecycleEvent 描述一次 Session 生命周期事件。
type LifecycleEvent struct {
	Stage           LifecycleStage         `json:"stage"`
	Session         *Session               `json:"-"`
	Result          *LifecycleResult       `json:"result,omitempty"`
	BudgetExhausted *BudgetExhaustedDetail `json:"budget_exhausted,omitempty"`
	Error           error                  `json:"-"`
	Timestamp       time.Time              `json:"timestamp"`
}

// LifecycleHook 在 Session 生命周期阶段被调用。
type LifecycleHook func(context.Context, LifecycleEvent)
