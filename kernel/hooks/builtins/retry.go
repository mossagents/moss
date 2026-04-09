package builtins

import (
	"context"
	"math/rand"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/retry"
)

// RetryConfig 复用 retry.Config，避免维护两套重试配置定义。
type RetryConfig = retry.Config

// Retry 构造 LLM 调用重试拦截器，注册在 BeforeLLM pipeline 上。
//
// 使用指数退避 + 抖动策略。当下游 hook 失败时，自动重试直到成功或达到最大重试次数。
func Retry(cfg RetryConfig) hooks.Interceptor[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent, next func(context.Context) error) error {
		var lastErr error
		maxRetries := cfg.MaxRetriesOrDefault()
		delay := cfg.InitialDelayOrDefault()

		for attempt := 0; attempt <= maxRetries; attempt++ {
			lastErr = next(ctx)
			if lastErr == nil {
				return nil
			}

			if !cfg.ShouldRetryOrDefault(ctx, lastErr) {
				return lastErr
			}

			if attempt == maxRetries {
				break
			}

			// 指数退避 + 抖动
			jitter := time.Duration(rand.Int63n(int64(delay) / 2))
			sleepDuration := delay + jitter
			if sleepDuration > cfg.MaxDelayOrDefault() {
				sleepDuration = cfg.MaxDelayOrDefault()
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleepDuration):
			}

			delay = time.Duration(float64(delay) * cfg.MultiplierOrDefault())
			if delay > cfg.MaxDelayOrDefault() {
				delay = cfg.MaxDelayOrDefault()
			}
		}

		return lastErr
	}
}

// DefaultRetry 返回使用默认配置的重试拦截器（3 次重试，指数退避）。
func DefaultRetry() hooks.Interceptor[hooks.LLMEvent] {
	return Retry(RetryConfig{})
}
