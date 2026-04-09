package middleware

import (
	"context"
	"github.com/mossagents/moss/kernel/session"
	"testing"
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
	if err := c.Run(context.Background(), BeforeLLM, mc); err != nil {
		t.Fatalf("Run BeforeLLM: %v", err)
	}
	if called {
		t.Fatal("should not have matched BeforeToolCall phase")
	}

	if err := c.Run(context.Background(), BeforeToolCall, mc); err != nil {
		t.Fatalf("Run BeforeToolCall: %v", err)
	}
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

func TestUseNamedDependencySatisfied(t *testing.T) {
	c := NewChain()
	noop := func(ctx context.Context, mc *Context, next Next) error { return next(ctx) }

	if err := c.UseNamed(NamedMiddleware{Name: "policy", Middleware: noop}); err != nil {
		t.Fatalf("UseNamed(policy): %v", err)
	}
	if err := c.UseNamed(NamedMiddleware{Name: "audit", Middleware: noop, DependsOn: []string{"policy"}}); err != nil {
		t.Fatalf("UseNamed(audit): %v", err)
	}

	mc := &Context{Session: &session.Session{ID: "test"}}
	if err := c.Run(context.Background(), BeforeLLM, mc); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestUseNamedDependencyMissing(t *testing.T) {
	c := NewChain()
	noop := func(ctx context.Context, mc *Context, next Next) error { return next(ctx) }

	err := c.UseNamed(NamedMiddleware{Name: "audit", Middleware: noop, DependsOn: []string{"policy"}})
	if err == nil {
		t.Fatal("expected error when dependency is missing")
	}
}

func TestUseNamedNoDeps(t *testing.T) {
	c := NewChain()
	var called bool
	err := c.UseNamed(NamedMiddleware{
		Name: "simple",
		Middleware: func(ctx context.Context, mc *Context, next Next) error {
			called = true
			return next(ctx)
		},
	})
	if err != nil {
		t.Fatalf("UseNamed: %v", err)
	}
	mc := &Context{Session: &session.Session{ID: "test"}}
	if err := c.Run(context.Background(), BeforeLLM, mc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("named middleware was not called")
	}
}

func TestMixedUseAndUseNamed(t *testing.T) {
	c := NewChain()
	var order []string

	c.Use(func(ctx context.Context, mc *Context, next Next) error {
		order = append(order, "anonymous")
		return next(ctx)
	})
	if err := c.UseNamed(NamedMiddleware{
		Name: "named",
		Middleware: func(ctx context.Context, mc *Context, next Next) error {
			order = append(order, "named")
			return next(ctx)
		},
	}); err != nil {
		t.Fatalf("UseNamed: %v", err)
	}

	mc := &Context{Session: &session.Session{ID: "test"}}
	if err := c.Run(context.Background(), BeforeLLM, mc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(order) != 2 || order[0] != "anonymous" || order[1] != "named" {
		t.Fatalf("unexpected order: %v", order)
	}
}
