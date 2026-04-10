package budget

import (
	"fmt"
	"sync/atomic"
)

// GlobalBudget 定义全局预算限制。
type GlobalBudget struct {
	MaxTokens int64   // 0 = 无限制
	MaxSteps  int64   // 0 = 无限制
	WarnAt    float64 // 触发警告的比例，如 0.8；0 = 不警告
}

// BudgetSnapshot 是某时刻的预算使用快照。
type BudgetSnapshot struct {
	UsedTokens   int64
	UsedSteps    int64
	MaxTokens    int64
	MaxSteps     int64
	SessionCount int
}

// Exhausted 返回快照中预算是否耗尽。
func (s BudgetSnapshot) Exhausted() bool {
	return (s.MaxTokens > 0 && s.UsedTokens >= s.MaxTokens) ||
		(s.MaxSteps > 0 && s.UsedSteps >= s.MaxSteps)
}

// TokensPct 返回 token 使用比例（0-1）。
func (s BudgetSnapshot) TokensPct() float64 {
	if s.MaxTokens <= 0 {
		return 0
	}
	return float64(s.UsedTokens) / float64(s.MaxTokens)
}

// StepsPct 返回 step 使用比例（0-1）。
func (s BudgetSnapshot) StepsPct() float64 {
	if s.MaxSteps <= 0 {
		return 0
	}
	return float64(s.UsedSteps) / float64(s.MaxSteps)
}

// Governor 管理全局跨 Agent 预算。
type Governor interface {
	Record(sessionID string, tokens, steps int)
	Check() bool
	// TryReserve 在消耗前预检是否有足够预算。
	// 返回 true 表示预算充足（不会实际扣减）。
	TryReserve(tokens, steps int) bool
	Snapshot() BudgetSnapshot
	Reset()
}

// WarnFunc 是预算警告回调。
type WarnFunc func(snap BudgetSnapshot)

// MemoryGovernor 是 Governor 的内存实现，线程安全。
//
// Governor 提供全局预算控制，管理所有 Session 的 token/step 总消耗。
// 与 BudgetPool 的区别：BudgetPool 管理单个 Session 内各子系统的配额分配，
// Governor 管理跨所有 Session 的全局预算上限。
type MemoryGovernor struct {
	budget GlobalBudget
	warn   WarnFunc

	usedTokens int64
	usedSteps  int64
}

// NewGovernor 创建一个内存 Governor。
func NewGovernor(b GlobalBudget, warn WarnFunc) *MemoryGovernor {
	return &MemoryGovernor{budget: b, warn: warn}
}

// Record 记录一次消耗（线程安全）。
func (g *MemoryGovernor) Record(sessionID string, tokens, steps int) {
	atomic.AddInt64(&g.usedTokens, int64(tokens))
	atomic.AddInt64(&g.usedSteps, int64(steps))

	if g.warn != nil && g.budget.WarnAt > 0 {
		snap := g.Snapshot()
		if snap.TokensPct() >= g.budget.WarnAt || snap.StepsPct() >= g.budget.WarnAt {
			g.warn(snap)
		}
	}
}

// Check 检查是否仍有预算可用（true = 可以继续）。
func (g *MemoryGovernor) Check() bool {
	tokens := atomic.LoadInt64(&g.usedTokens)
	steps := atomic.LoadInt64(&g.usedSteps)
	if g.budget.MaxTokens > 0 && tokens >= g.budget.MaxTokens {
		return false
	}
	if g.budget.MaxSteps > 0 && steps >= g.budget.MaxSteps {
		return false
	}
	return true
}

// TryReserve 在 LLM 调用前预检预算是否充足。
// 返回 true 表示扣减 tokens/steps 后仍不超出限额（不会实际扣减）。
func (g *MemoryGovernor) TryReserve(tokens, steps int) bool {
	usedTokens := atomic.LoadInt64(&g.usedTokens)
	usedSteps := atomic.LoadInt64(&g.usedSteps)
	if g.budget.MaxTokens > 0 && usedTokens+int64(tokens) > g.budget.MaxTokens {
		return false
	}
	if g.budget.MaxSteps > 0 && usedSteps+int64(steps) > g.budget.MaxSteps {
		return false
	}
	return true
}

// Snapshot 返回当前使用状态的快照。
func (g *MemoryGovernor) Snapshot() BudgetSnapshot {
	return BudgetSnapshot{
		UsedTokens:   atomic.LoadInt64(&g.usedTokens),
		UsedSteps:    atomic.LoadInt64(&g.usedSteps),
		MaxTokens:    g.budget.MaxTokens,
		MaxSteps:     g.budget.MaxSteps,
		SessionCount: 0,
	}
}

// Reset 清零所有计数器。
func (g *MemoryGovernor) Reset() {
	atomic.StoreInt64(&g.usedTokens, 0)
	atomic.StoreInt64(&g.usedSteps, 0)
}

// ErrBudgetExhausted 表示全局预算已耗尽。
var ErrBudgetExhausted = fmt.Errorf("global budget exhausted")
