package retry

import (
	"context"
	"time"
)

// Config 描述 LLM/中间件重试策略。
type Config struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	// TotalTimeout 限制所有重试的总时间预算。0 = 无限制。
	TotalTimeout time.Duration
	ShouldRetry  func(error) bool
}

func (c Config) Enabled() bool {
	return c.MaxRetries > 0 || c.InitialDelay > 0 || c.MaxDelay > 0 || c.Multiplier > 0 || c.ShouldRetry != nil
}

func (c Config) MaxRetriesOrDefault() int {
	if c.MaxRetries <= 0 {
		return 3
	}
	return c.MaxRetries
}

func (c Config) InitialDelayOrDefault() time.Duration {
	if c.InitialDelay <= 0 {
		return time.Second
	}
	return c.InitialDelay
}

func (c Config) MaxDelayOrDefault() time.Duration {
	if c.MaxDelay <= 0 {
		return 30 * time.Second
	}
	return c.MaxDelay
}

func (c Config) MultiplierOrDefault() float64 {
	if c.Multiplier <= 0 {
		return 2.0
	}
	return c.Multiplier
}

func (c Config) ShouldRetryOrDefault(ctx context.Context, err error) bool {
	// If the caller's context is already done, retrying is pointless.
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if c.ShouldRetry != nil {
		return c.ShouldRetry(err)
	}
	return true
}

// TotalTimeoutOrDefault 返回总超时时间，0 = 无限制。
func (c Config) TotalTimeoutOrDefault() time.Duration {
	return c.TotalTimeout
}
