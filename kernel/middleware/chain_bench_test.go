package middleware

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/session"
)

func BenchmarkChainRun(b *testing.B) {
	c := NewChain()
	for range 5 {
		c.Use(func(ctx context.Context, mc *Context, next Next) error {
			return next(ctx)
		})
	}

	mc := &Context{Session: &session.Session{ID: "bench"}}
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.Run(ctx, BeforeLLM, mc)
	}
}

func BenchmarkChainRunEmpty(b *testing.B) {
	c := NewChain()
	mc := &Context{Session: &session.Session{ID: "bench-empty"}}
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.Run(ctx, BeforeLLM, mc)
	}
}
