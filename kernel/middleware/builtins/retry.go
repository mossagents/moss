package builtins

import (
	"context"
	"math/rand"
	"time"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/retry"
)

// RetryConfig 复用 retry.Config，避免维护两套重试配置定义。
type RetryConfig = retry.Config

// Retry 构造 LLM 调用重试 middleware，仅在 BeforeLLM 阶段拦截。
//
// 使用指数退避 + 抖动策略。当 LLM 调用失败时，自动重试直到成功或达到最大重试次数。
//
// 用法：
//
//	k := kernel.New(kernel.Use(builtins.Retry(builtins.RetryConfig{MaxRetries: 3})))
func Retry(cfg RetryConfig) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeLLM {
			return next(ctx)
		}

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

// DefaultRetry 返回使用默认配置的重试 middleware（3 次重试，指数退避）。
func DefaultRetry() middleware.Middleware {
	return Retry(RetryConfig{})
}
