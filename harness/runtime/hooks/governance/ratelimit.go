package governance

import (
	"context"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/hooks"
)

// RateLimiter 构造速率限制 hook。
// 在 BeforeLLM 阶段按 Session 维度限流。
// rps 为每秒允许的请求数，burst 为突发容量。
func RateLimiter(rps int, burst int) hooks.Hook[hooks.LLMEvent] {
	limiters := &sync.Map{} // session_id → *tokenBucket

	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		sessID := ev.Session.ID
		v, _ := limiters.LoadOrStore(sessID, newTokenBucket(rps, burst))
		bucket := v.(*tokenBucket)

		if !bucket.allow() {
			return errors.New(errors.ErrRateLimit, "rate limit exceeded for session "+sessID)
		}

		return nil
	}
}

// tokenBucket 令牌桶限流器。
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func newTokenBucket(rps, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(burst),
		max:      float64(burst),
		rate:     float64(rps),
		lastTime: time.Now(),
	}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.lastTime = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
