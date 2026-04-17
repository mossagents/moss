package kernel

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/session"
)

// noopAgent 返回一个空事件序列的测试 Agent。
func noopAgent(name string) *CustomAgent {
	return NewCustomAgent(CustomAgentConfig{
		Name: name,
		Run: func(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
}

// TestForkDepthGuard 验证 RunChild 在 fork 深度达到 maxForkDepth 时返回错误。
func TestForkDepthGuard(t *testing.T) {
	// maxForkDepth == 5，depth 5 的 branch 应被阻止
	// forkDepth("a.b.c.d.e") = 1 + 4 = 5 → >= maxForkDepth → blocked
	deepBranch := "a.b.c.d.e"

	sess := &session.Session{ID: "test-fork-depth"}
	ctx := NewInvocationContext(context.Background(), InvocationContextParams{
		Branch:  deepBranch,
		Session: sess,
	})

	var gotErr error
	for _, err := range ctx.RunChild(noopAgent("child"), ChildRunConfig{}) {
		if err != nil {
			gotErr = err
			break
		}
	}

	if gotErr == nil {
		t.Fatal("expected fork depth error, got nil")
	}
	if !strings.Contains(gotErr.Error(), "fork depth limit") {
		t.Fatalf("expected fork depth error message, got %q", gotErr.Error())
	}
}

// TestForkDepthAllowed 验证正常深度（<5）下 RunChild 能正常执行。
func TestForkDepthAllowed(t *testing.T) {
	// forkDepth("a.b.c") = 1 + 2 = 3 → < maxForkDepth(5) → allowed
	shallowBranch := "a.b.c"

	sess := &session.Session{ID: "test-fork-allowed"}
	ctx := NewInvocationContext(context.Background(), InvocationContextParams{
		Branch:  shallowBranch,
		Session: sess,
	})

	var ran bool
	for _, err := range ctx.RunChild(noopAgent("child"), ChildRunConfig{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ran = true
	}
	// noopAgent yields no events, so ran stays false — that's expected
	_ = ran
}

// TestForkDepthAtLimit 验证正好等于 maxForkDepth 的 branch 会被阻止。
func TestForkDepthAtLimit(t *testing.T) {
	// forkDepth("a.b.c.d.e") = 5 → exactly at limit → blocked
	limitBranch := "a.b.c.d.e"

	sess := &session.Session{ID: "test-fork-at-limit"}
	ctx := NewInvocationContext(context.Background(), InvocationContextParams{
		Branch:  limitBranch,
		Session: sess,
	})

	var gotErr error
	for _, err := range ctx.RunChild(noopAgent("child"), ChildRunConfig{}) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected error at fork depth limit, got nil")
	}
}

// TestForkDepthEmptyBranch 验证空 branch（根节点）可以正常派生子任务。
func TestForkDepthEmptyBranch(t *testing.T) {
	sess := &session.Session{ID: "test-fork-root"}
	ctx := NewInvocationContext(context.Background(), InvocationContextParams{
		Branch:  "",
		Session: sess,
	})

	var gotErr error
	for _, err := range ctx.RunChild(noopAgent("child"), ChildRunConfig{}) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr != nil {
		t.Fatalf("root branch should allow RunChild, got %v", gotErr)
	}
}

func TestRunChildActiveAgentLimit(t *testing.T) {
	t.Cleanup(func() {
		activeChildAgentCount.Store(0)
	})
	activeChildAgentCount.Store(maxActiveAgents)

	sess := &session.Session{ID: "test-active-agent-limit"}
	ctx := NewInvocationContext(context.Background(), InvocationContextParams{
		Branch:  "root",
		Session: sess,
	})

	var gotErr error
	for _, err := range ctx.RunChild(noopAgent("child"), ChildRunConfig{}) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected active child agent limit error, got nil")
	}
	if !strings.Contains(gotErr.Error(), "active child agent limit") {
		t.Fatalf("expected active child agent limit error, got %q", gotErr.Error())
	}
}
