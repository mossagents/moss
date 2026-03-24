package middleware

import (
	"context"
	"testing"

	"github.com/mossagi/moss/kernel/session"
)

func TestChainOrdering(t *testing.T) {
	c := NewChain()
	var order []int

	for i := 0; i < 3; i++ {
		i := i
		c.Use(func(ctx context.Context, mc *Context, next Next) error {
			order = append(order, i)
			return next(ctx)
		})
	}

	mc := &Context{Session: &session.Session{ID: "test"}}
	if err := c.Run(context.Background(), BeforeLLM, mc); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("order len = %d, want 3", len(order))
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("order[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestChainPhaseFiltering(t *testing.T) {
	c := NewChain()
	var called bool

	c.Use(func(ctx context.Context, mc *Context, next Next) error {
		if mc.Phase == BeforeToolCall {
			called = true
		}
		return next(ctx)
	})

	mc := &Context{Session: &session.Session{ID: "test"}}
	c.Run(context.Background(), BeforeLLM, mc)
	if called {
		t.Fatal("should not have matched BeforeToolCall phase")
	}

	c.Run(context.Background(), BeforeToolCall, mc)
	if !called {
		t.Fatal("should have matched BeforeToolCall phase")
	}
}

func TestChainErrorPropagation(t *testing.T) {
	c := NewChain()
	expectedErr := context.DeadlineExceeded

	c.Use(func(ctx context.Context, mc *Context, next Next) error {
		return expectedErr
	})
	c.Use(func(ctx context.Context, mc *Context, next Next) error {
		t.Fatal("should not reach second middleware")
		return next(ctx)
	})

	mc := &Context{Session: &session.Session{ID: "test"}}
	if err := c.Run(context.Background(), BeforeLLM, mc); err != expectedErr {
		t.Fatalf("err = %v, want %v", err, expectedErr)
	}
}

func TestChainEmpty(t *testing.T) {
	c := NewChain()
	mc := &Context{Session: &session.Session{ID: "test"}}
	if err := c.Run(context.Background(), BeforeLLM, mc); err != nil {
		t.Fatalf("empty chain: %v", err)
	}
}
