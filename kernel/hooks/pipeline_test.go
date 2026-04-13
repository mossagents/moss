package hooks

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type testEvent struct {
	Value string
	Log   []string
}

func TestPipelineSimpleHooks(t *testing.T) {
	p := NewPipeline[testEvent]()
	p.On(func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "a")
		return nil
	})
	p.On(func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "b")
		return nil
	})

	ev := &testEvent{}
	if err := p.Run(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ev.Log) != 2 || ev.Log[0] != "a" || ev.Log[1] != "b" {
		t.Fatalf("expected [a b], got %v", ev.Log)
	}
}

func TestPipelineHookError(t *testing.T) {
	p := NewPipeline[testEvent]()
	expectedErr := errors.New("fail")
	p.On(func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "a")
		return expectedErr
	})
	p.On(func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "should not run")
		return nil
	})

	ev := &testEvent{}
	if err := p.Run(context.Background(), ev); err != expectedErr {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
	if len(ev.Log) != 1 {
		t.Fatalf("second hook should not have run, got %v", ev.Log)
	}
}

func TestPipelineInterceptor(t *testing.T) {
	p := NewPipeline[testEvent]()
	p.Intercept(func(ctx context.Context, ev *testEvent, next func(context.Context) error) error {
		ev.Log = append(ev.Log, "before")
		if err := next(ctx); err != nil {
			return err
		}
		ev.Log = append(ev.Log, "after")
		return nil
	})
	p.On(func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "inner")
		return nil
	})

	ev := &testEvent{}
	if err := p.Run(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"before", "inner", "after"}
	if len(ev.Log) != 3 || ev.Log[0] != expected[0] || ev.Log[1] != expected[1] || ev.Log[2] != expected[2] {
		t.Fatalf("expected %v, got %v", expected, ev.Log)
	}
}

func TestPipelineInterceptorShortCircuit(t *testing.T) {
	p := NewPipeline[testEvent]()
	p.Intercept(func(_ context.Context, ev *testEvent, _ func(context.Context) error) error {
		ev.Log = append(ev.Log, "blocked")
		return nil // skip next
	})
	p.On(func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "should not run")
		return nil
	})

	ev := &testEvent{}
	if err := p.Run(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ev.Log) != 1 || ev.Log[0] != "blocked" {
		t.Fatalf("expected [blocked], got %v", ev.Log)
	}
}

func TestPipelineOrderSort(t *testing.T) {
	p := NewPipeline[testEvent]()
	_ = p.OnNamed("c", 30, func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "c")
		return nil
	})
	_ = p.OnNamed("a", 10, func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "a")
		return nil
	})
	_ = p.OnNamed("b", 20, func(_ context.Context, ev *testEvent) error {
		ev.Log = append(ev.Log, "b")
		return nil
	})

	ev := &testEvent{}
	if err := p.Run(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ev.Log) != 3 || ev.Log[0] != "a" || ev.Log[1] != "b" || ev.Log[2] != "c" {
		t.Fatalf("expected [a b c], got %v", ev.Log)
	}
}

func TestPipelineNamedDependency(t *testing.T) {
	p := NewPipeline[testEvent]()
	noop := func(_ context.Context, _ *testEvent) error { return nil }

	if err := p.OnNamed("policy", 0, noop); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := p.OnNamed("audit", 10, noop, "policy"); err != nil {
		t.Fatalf("audit depends on policy which is registered: %v", err)
	}

	// missing dependency
	err := p.OnNamed("orphan", 20, noop, "missing")
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestPipelineEmpty(t *testing.T) {
	p := NewPipeline[testEvent]()
	if !p.Empty() {
		t.Fatal("expected empty")
	}
	p.On(func(_ context.Context, _ *testEvent) error { return nil })
	if p.Empty() {
		t.Fatal("expected not empty")
	}
}

func TestPipelineConcurrentRegistration(t *testing.T) {
	p := NewPipeline[testEvent]()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.On(func(_ context.Context, ev *testEvent) error {
				return nil
			})
		}()
	}
	wg.Wait()
}

func TestRegistryCreation(t *testing.T) {
	r := NewRegistry()
	if r.BeforeLLM == nil || r.AfterLLM == nil || r.BeforeToolCall == nil ||
		r.AfterToolCall == nil || r.OnSessionStart == nil || r.OnSessionEnd == nil ||
		r.OnSessionLifecycle == nil || r.OnToolLifecycle == nil || r.OnError == nil {
		t.Fatal("all pipelines should be initialized")
	}
}

func BenchmarkPipelineRun(b *testing.B) {
	p := NewPipeline[testEvent]()
	for i := 0; i < 10; i++ {
		p.On(func(ctx context.Context, ev *testEvent) error { return nil })
	}
	ev := &testEvent{}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Run(ctx, ev)
	}
}

func BenchmarkPipelineRunInterceptor(b *testing.B) {
	p := NewPipeline[testEvent]()
	for i := 0; i < 10; i++ {
		p.Intercept(func(ctx context.Context, ev *testEvent, next func(context.Context) error) error {
			return next(ctx)
		})
	}
	ev := &testEvent{}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Run(ctx, ev)
	}
}
