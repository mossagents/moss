package builtins

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

// ─── Tokenizer 注入测试 ──────────────────────────────────────────────────────

// countingTokenizer 用于测试，每次调用后记录调用次数，返回固定 token 数。
type countingTokenizer struct {
	perMsg int
	calls  int64
}

func (t *countingTokenizer) CountMessage(_ mdl.Message) int {
	atomic.AddInt64(&t.calls, 1)
	return t.perMsg
}

func (t *countingTokenizer) CountString(s string) int {
	return len(s) / 4
}

func makeSession(messages ...mdl.Message) *session.Session {
	return &session.Session{ID: "test", Messages: messages}
}

func userMsg(text string) mdl.Message {
	return mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart(text)}}
}

func systemMsg(text string) mdl.Message {
	return mdl.Message{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart(text)}}
}

func assistantMsg(text string) mdl.Message {
	return mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart(text)}}
}

func runBeforeLLM(t *testing.T, mw middleware.Middleware, sess *session.Session) {
	t.Helper()
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}
	if err := mw(context.Background(), mc, func(_ context.Context) error { return nil }); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
}

// ─── AutoTruncate + Tokenizer ────────────────────────────────────────────────

func TestAutoTruncate_WithTokenizer(t *testing.T) {
	tok := &countingTokenizer{perMsg: 10} // 每条消息固定 10 token
	mw := AutoTruncate(TruncateConfig{
		MaxContextTokens: 25, // 只能容纳 2 条消息
		KeepRecent:       2,
		Tokenizer:        tok,
	})

	sess := makeSession(
		systemMsg("system"),
		userMsg("msg1"), assistantMsg("reply1"),
		userMsg("msg2"), assistantMsg("reply2"),
		userMsg("msg3"), assistantMsg("reply3"),
	)
	runBeforeLLM(t, mw, sess)

	// Tokenizer 必须被调用（>0 次）
	if atomic.LoadInt64(&tok.calls) == 0 {
		t.Error("Tokenizer.CountMessage should have been called")
	}
	// 压缩后消息数应 < 原始消息数
	if len(sess.Messages) >= 7 {
		t.Errorf("expected fewer messages after truncation, got %d", len(sess.Messages))
	}
}

func TestAutoTruncate_TokenizerPrecedesTokenCounter(t *testing.T) {
	// Tokenizer 返回 100，TokenCounter 返回 1。应优先使用 Tokenizer（总 token 超阈值触发压缩）
	tok := &countingTokenizer{perMsg: 100}
	mw := AutoTruncate(TruncateConfig{
		MaxContextTokens: 50,
		KeepRecent:       1,
		Tokenizer:        tok,
		TokenCounter:     func(_ mdl.Message) int { return 1 }, // 不应被使用
	})

	sess := makeSession(
		systemMsg("system"),
		userMsg("msg1"), assistantMsg("reply1"),
		userMsg("msg2"), assistantMsg("reply2"),
	)
	originalLen := len(sess.Messages)
	runBeforeLLM(t, mw, sess)

	if len(sess.Messages) == originalLen {
		t.Error("Tokenizer (high count) should have triggered truncation")
	}
}

func TestAutoTruncate_FuncTokenizerAdapter(t *testing.T) {
	callCount := 0
	tok := mdl.FuncTokenizer{Fn: func(msg mdl.Message) int {
		callCount++
		return 50 // always high
	}}

	mw := AutoTruncate(TruncateConfig{
		MaxContextTokens: 30,
		KeepRecent:       1,
		Tokenizer:        tok,
	})

	sess := makeSession(
		systemMsg("sys"),
		userMsg("a"), userMsg("b"), userMsg("c"),
	)
	runBeforeLLM(t, mw, sess)

	if callCount == 0 {
		t.Error("FuncTokenizer should have been called")
	}
}

// ─── AutoSummarize + Tokenizer ───────────────────────────────────────────────

func TestAutoSummarize_WithTokenizer_CallsLLM(t *testing.T) {
	tok := &countingTokenizer{perMsg: 100}
	llmCallCount := 0
	mockLLM := &mockSummaryLLM{fn: func(_ context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
		llmCallCount++
		return &mdl.CompletionResponse{
			Message: mdl.Message{
				Role:         mdl.RoleAssistant,
				ContentParts: []mdl.ContentPart{mdl.TextPart("summary text")},
			},
		}, nil
	}}

	mw := AutoSummarize(SummarizeConfig{
		LLM:              mockLLM,
		MaxContextTokens: 150, // 3 msgs × 100 tokens = 300 > 150
		KeepRecent:       1,
		Tokenizer:        tok,
	})

	sess := makeSession(
		systemMsg("system"),
		userMsg("msg1"), assistantMsg("reply1"),
		userMsg("msg2"),
	)
	runBeforeLLM(t, mw, sess)

	if llmCallCount == 0 {
		t.Error("expected LLM to be called for summary generation")
	}
	// session 消息数应减少（旧历史被摘要替代）
	if len(sess.Messages) >= 4 {
		t.Errorf("expected fewer messages after summarize, got %d", len(sess.Messages))
	}
}

func TestAutoSummarize_Cache_AvoidsDuplicateLLMCall(t *testing.T) {
	tok := &countingTokenizer{perMsg: 100}
	llmCallCount := int32(0)
	mockLLM := &mockSummaryLLM{fn: func(_ context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
		atomic.AddInt32(&llmCallCount, 1)
		return &mdl.CompletionResponse{
			Message: mdl.Message{
				Role:         mdl.RoleAssistant,
				ContentParts: []mdl.ContentPart{mdl.TextPart("cached summary")},
			},
		}, nil
	}}

	mw := AutoSummarize(SummarizeConfig{
		LLM:              mockLLM,
		MaxContextTokens: 50,
		KeepRecent:       1,
		Tokenizer:        tok,
	})

	// 第一次调用：LLM 被调用
	sess1 := makeSession(
		userMsg("a"), userMsg("b"), userMsg("c"),
	)
	runBeforeLLM(t, mw, sess1)
	firstCount := atomic.LoadInt32(&llmCallCount)
	if firstCount != 1 {
		t.Errorf("expected 1 LLM call, got %d", firstCount)
	}

	// 第二次调用：相同消息集，应命中缓存，LLM 不再被调用
	sess2 := makeSession(
		userMsg("a"), userMsg("b"), userMsg("c"),
	)
	runBeforeLLM(t, mw, sess2)
	if atomic.LoadInt32(&llmCallCount) != firstCount {
		t.Error("second call with same messages should use cache, LLM should NOT be called again")
	}
}

func TestAutoSummarize_Cache_DifferentMessages_CallsLLMAgain(t *testing.T) {
	tok := &countingTokenizer{perMsg: 100}
	llmCallCount := int32(0)
	mockLLM := &mockSummaryLLM{fn: func(_ context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
		atomic.AddInt32(&llmCallCount, 1)
		return &mdl.CompletionResponse{
			Message: mdl.Message{
				Role:         mdl.RoleAssistant,
				ContentParts: []mdl.ContentPart{mdl.TextPart("summary")},
			},
		}, nil
	}}

	mw := AutoSummarize(SummarizeConfig{
		LLM:              mockLLM,
		MaxContextTokens: 50,
		KeepRecent:       1,
		Tokenizer:        tok,
	})

	sess1 := makeSession(userMsg("a"), userMsg("b"), userMsg("c"))
	runBeforeLLM(t, mw, sess1)

	sess2 := makeSession(userMsg("x"), userMsg("y"), userMsg("z")) // different content
	runBeforeLLM(t, mw, sess2)

	if atomic.LoadInt32(&llmCallCount) < 2 {
		t.Error("different message sets should trigger separate LLM calls")
	}
}

// ─── SlidingWindow + Tokenizer ───────────────────────────────────────────────

func TestSlidingWindow_WithTokenizer(t *testing.T) {
	tok := &countingTokenizer{perMsg: 50}
	mw := SlidingWindow(SlidingWindowConfig{
		WindowSize:       2,
		MaxContextTokens: 100, // 3 × 50 = 150 > 100, triggers
		Tokenizer:        tok,
	})

	sess := makeSession(
		systemMsg("sys"),
		userMsg("a"), userMsg("b"), userMsg("c"),
	)
	runBeforeLLM(t, mw, sess)

	if atomic.LoadInt64(&tok.calls) == 0 {
		t.Error("Tokenizer should have been called")
	}
	// 应保留 system + summary + 2 recent = 4
	if len(sess.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(sess.Messages))
	}
}

// ─── PriorityCompress + Tokenizer ───────────────────────────────────────────

func TestPriorityCompress_WithTokenizer(t *testing.T) {
	tok := &countingTokenizer{perMsg: 30}
	mw := PriorityCompress(PriorityConfig{
		MaxContextTokens: 60, // 3 × 30 = 90 > 60, triggers
		KeepRecent:       1,
		Tokenizer:        tok,
	})

	sess := makeSession(
		systemMsg("sys"),
		userMsg("low priority"), assistantMsg("medium"),
		userMsg("recent"),
	)
	runBeforeLLM(t, mw, sess)

	if atomic.LoadInt64(&tok.calls) == 0 {
		t.Error("Tokenizer should have been called")
	}
	if len(sess.Messages) >= 5 {
		t.Errorf("expected compression to reduce messages, got %d", len(sess.Messages))
	}
}

// ─── 重复注入防护测试（通过 loop.AgentLoop 直接测试）──────────────────────────

// TestCompressionInjection_Idempotent 在 builtins 层验证 Chain 幂等性。
// loop 层的测试在 kernel/loop 包中，此处验证 middleware Chain 多次 Use 的行为。
func TestMultipleUse_SameMiddlewareRunsMultipleTimes(t *testing.T) {
	// 这个测试验证不加 guard 时确实会有重复问题，
	// 从而证明 loop.go 中 compressionInjected guard 的必要性。
	runCount := 0
	mw := func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		runCount++
		return next(ctx)
	}

	chain := middleware.NewChain()
	chain.Use(mw)
	chain.Use(mw) // 同一 middleware 注册两次

	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: &session.Session{ID: "test"}}
	_ = chain.Run(context.Background(), middleware.BeforeLLM, mc)

	if runCount != 2 {
		t.Errorf("expected 2 runs (confirming double registration problem), got %d", runCount)
	}
}

// ─── Backward Compatibility — TokenCounter 仍然有效 ────────────────────────

func TestAutoTruncate_BackwardCompat_TokenCounterStillWorks(t *testing.T) {
	called := false
	mw := AutoTruncate(TruncateConfig{
		MaxContextTokens: 5,
		KeepRecent:       1,
		// 旧式 TokenCounter，不设置 Tokenizer
		TokenCounter: func(msg mdl.Message) int {
			called = true
			return 10
		},
	})

	sess := makeSession(userMsg("a"), userMsg("b"), userMsg("c"))
	runBeforeLLM(t, mw, sess)

	if !called {
		t.Error("TokenCounter should still be called when Tokenizer is not set")
	}
}

func TestAutoSummarize_BackwardCompat_TokenCounterStillWorks(t *testing.T) {
	called := false
	mockLLM := &mockSummaryLLM{fn: func(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
		return &mdl.CompletionResponse{
			Message: mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("ok")}},
		}, nil
	}}

	mw := AutoSummarize(SummarizeConfig{
		LLM:              mockLLM,
		MaxContextTokens: 5,
		KeepRecent:       1,
		TokenCounter: func(msg mdl.Message) int {
			called = true
			return 10
		},
	})

	sess := makeSession(userMsg("a"), userMsg("b"), userMsg("c"))
	runBeforeLLM(t, mw, sess)

	if !called {
		t.Error("TokenCounter should still be called when Tokenizer is not set")
	}
}

// ─── 工具类型 ────────────────────────────────────────────────────────────────

// mockSummaryLLM 实现 mdl.LLM 供摘要测试使用。
type mockSummaryLLM struct {
	fn func(ctx context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error)
}

func (m *mockSummaryLLM) Complete(ctx context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	return m.fn(ctx, req)
}

// 兼容性检查（编译时确认 mockSummaryLLM 实现了 mdl.LLM）
var _ mdl.LLM = (*mockSummaryLLM)(nil)

// ─── estimateTokens 函数本身回归测试 ─────────────────────────────────────────

func TestEstimateTokens_ToolCalls(t *testing.T) {
	msg := mdl.Message{
		Role:      mdl.RoleAssistant,
		ToolCalls: []mdl.ToolCall{{Name: "read_file", Arguments: []byte(`{"path":"foo.txt"}`)}},
	}
	got := estimateTokens(msg)
	if got < 1 {
		t.Errorf("tool call message should have > 0 tokens, got %d", got)
	}
}

func TestEstimateTokens_SystemMessage(t *testing.T) {
	msg := systemMsg("You are a helpful assistant with a very long system prompt that should count many tokens.")
	got := estimateTokens(msg)
	if got < 5 {
		t.Errorf("long system message should have many tokens, got %d", got)
	}
}

// ─── SimpleTokenizer 与 estimateTokens 一致性 ────────────────────────────────

func TestSimpleTokenizer_MatchesEstimateTokens(t *testing.T) {
	tok := mdl.SimpleTokenizer{}
	msgs := []mdl.Message{
		systemMsg("system prompt"),
		userMsg("hello world"),
		assistantMsg("hi there"),
	}
	for i, msg := range msgs {
		simple := tok.CountMessage(msg)
		estimate := estimateTokens(msg)
		if simple != estimate {
			t.Errorf("msg[%d]: SimpleTokenizer.CountMessage=%d, estimateTokens=%d; should match",
				i, simple, estimate)
		}
	}
}

// ─── 错误场景 ─────────────────────────────────────────────────────────────────

func TestAutoSummarize_LLMError_FallsBackToTruncate(t *testing.T) {
	tok := &countingTokenizer{perMsg: 100}
	mockLLM := &mockSummaryLLM{fn: func(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
		return nil, fmt.Errorf("LLM timeout")
	}}

	mw := AutoSummarize(SummarizeConfig{
		LLM:              mockLLM,
		MaxContextTokens: 50,
		KeepRecent:       1,
		Tokenizer:        tok,
	})

	sess := makeSession(userMsg("a"), userMsg("b"), userMsg("c"))
	// 即使 LLM 报错，middleware 也不应返回 error（降级处理）
	runBeforeLLM(t, mw, sess)

	// 应有摘要失败的通知消息
	found := false
	for _, m := range sess.Messages {
		if m.Role == mdl.RoleSystem {
			text := mdl.ContentPartsToPlainText(m.ContentParts)
			if len(text) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected fallback notice message after LLM error")
	}
}
