package retry

import (
	"sync"
	"time"
)

// BreakerState 表示熔断器状态。
type BreakerState int

const (
	StateClosed   BreakerState = iota // 正常放行
	StateOpen                         // 熔断拒绝
	StateHalfOpen                     // 半开试探
)

// Breaker 实现简单的熔断器模式。
// 连续 MaxFailures 次失败 → Open（拒绝请求）→ 等待 ResetAfter → HalfOpen（放行一个试探）→ 成功则 Closed。
// 线程安全。
type Breaker struct {
	mu              sync.Mutex
	maxFailures     int
	resetAfter      time.Duration
	failures        int
	state           BreakerState
	lastFailedAt    time.Time
	halfOpenAllowed bool
}

// BreakerConfig 配置熔断器。
type BreakerConfig struct {
	MaxFailures int           // 连续失败多少次后触发熔断（默认 5）
	ResetAfter  time.Duration // 熔断后等待多久进入半开状态（默认 30s）
}

// NewBreaker 创建熔断器。
func NewBreaker(cfg BreakerConfig) *Breaker {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.ResetAfter <= 0 {
		cfg.ResetAfter = 30 * time.Second
	}
	return &Breaker{
		maxFailures: cfg.MaxFailures,
		resetAfter:  cfg.ResetAfter,
		state:       StateClosed,
	}
}

// Allow 检查是否允许请求通过。
// 返回 true 表示允许，false 表示熔断拒绝。
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.lastFailedAt) >= b.resetAfter {
			b.state = StateHalfOpen
			b.halfOpenAllowed = true
			return true
		}
		return false
	case StateHalfOpen:
		// 半开状态只允许一个试探请求
		if b.halfOpenAllowed {
			b.halfOpenAllowed = false
			return true
		}
		return false
	}
	return true
}

// RecordSuccess 记录一次成功，重置熔断器。
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = StateClosed
}

// RecordFailure 记录一次失败。
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	b.lastFailedAt = time.Now()
	if b.failures >= b.maxFailures {
		b.state = StateOpen
	}
}

// ForceReset 强制将熔断器重置为 Closed 状态，无需等待 ResetAfter。
func (b *Breaker) ForceReset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = StateClosed
	b.halfOpenAllowed = false
}

// State 返回当前状态。
func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	// 检查 Open 状态是否应该转为 HalfOpen
	if b.state == StateOpen && time.Since(b.lastFailedAt) >= b.resetAfter {
		b.state = StateHalfOpen
		b.halfOpenAllowed = true
	}
	return b.state
}
