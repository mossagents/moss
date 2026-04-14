package builtins_test

// Extended tests covering previously-uncovered builtins:
// events, retry, patch_tool_calls, priority, sliding, truncate,
// policy_command, rbac/AuthMiddleware, rag helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	kernio "github.com/mossagents/moss/kernel/io"
	kernelmemory "github.com/mossagents/moss/kernel/memory"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func sessWithMessages(msgs ...model.Message) *session.Session {
	s := &session.Session{ID: "s1", State: map[string]any{}}
	for _, m := range msgs {
		s.AppendMessage(m)
	}
	return s
}

func textMsg(role model.Role, text string) model.Message {
	return model.Message{Role: role, ContentParts: []model.ContentPart{model.TextPart(text)}}
}

type stubContextInjector struct {
	injected   string
	err        error
	lastConfig kernelmemory.ContextInjectConfig
}

func (s *stubContextInjector) InjectContext(_ context.Context, cfg kernelmemory.ContextInjectConfig) (string, error) {
	s.lastConfig = cfg
	return s.injected, s.err
}

// ─── EventEmitterPlugin ───────────────────────────────────────────────────────

func TestEventEmitter_BeforeLLM(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("llm.*", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.LLMEvent{Session: &session.Session{ID: "s1", State: map[string]any{}}}
	if err := p.BeforeLLM(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "llm.started" {
		t.Fatalf("expected llm.started, got %v", got)
	}
}

func TestEventEmitter_AfterLLM(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("llm.*", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.LLMEvent{Session: &session.Session{ID: "s1", State: map[string]any{}}}
	if err := p.AfterLLM(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "llm.completed" {
		t.Fatalf("expected llm.completed, got %v", got)
	}
}

func TestEventEmitter_PatternNoMatch(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("session.*", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.LLMEvent{Session: &session.Session{ID: "s1", State: map[string]any{}}}
	_ = p.BeforeLLM(context.Background(), ev)
	if len(got) != 0 {
		t.Fatalf("expected no events for non-matching pattern, got %v", got)
	}
}

func TestEventEmitter_WildcardPattern(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("*", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.LLMEvent{Session: &session.Session{ID: "s1", State: map[string]any{}}}
	_ = p.BeforeLLM(context.Background(), ev)
	if len(got) != 1 {
		t.Fatalf("wildcard should match llm.started")
	}
}

func TestEventEmitter_ToolLifecycle(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("tool.*", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.ToolEvent{
		Stage:     hooks.ToolLifecycleBefore,
		Tool:      &tool.ToolSpec{Name: "read_file"},
		Session:   &session.Session{ID: "s2", State: map[string]any{}},
		CallID:    "c1",
		Risk:      "low",
		Timestamp: time.Now(),
	}
	if err := p.OnToolLifecycle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "tool.started" {
		t.Fatalf("expected tool.started, got %v", got)
	}
	data := got[0].Data.(map[string]any)
	if data["tool"] != "read_file" {
		t.Fatalf("expected tool=read_file in data")
	}
}

func TestEventEmitter_ToolAfterStage(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("tool.*", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.ToolEvent{
		Stage:      hooks.ToolLifecycleAfter,
		Tool:       &tool.ToolSpec{Name: "write_file"},
		ToolResult: &model.ToolResult{IsError: false},
		Duration:   5 * time.Millisecond,
	}
	_ = p.OnToolLifecycle(context.Background(), ev)
	if len(got) != 1 || got[0].Type != "tool.completed" {
		t.Fatalf("expected tool.completed")
	}
}

func TestEventEmitter_SessionLifecycle(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("session.*", func(e builtins.Event) { got = append(got, e) })

	ev := &session.LifecycleEvent{
		Stage:   session.LifecycleCreated,
		Session: &session.Session{ID: "s3", State: map[string]any{}},
	}
	if err := p.OnSessionLifecycle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "session.created" {
		t.Fatalf("expected session.created, got %v", got)
	}
}

func TestEventEmitter_SessionLifecycleVariants(t *testing.T) {
	cases := []struct {
		stage session.LifecycleStage
		want  string
	}{
		{session.LifecycleStarted, "session.started"},
		{session.LifecycleFailed, "session.failed"},
		{session.LifecycleCancelled, "session.cancelled"},
		{session.LifecycleCompleted, "session.completed"},
	}
	for _, c := range cases {
		var got []builtins.Event
		p := builtins.EventEmitterPlugin("*", func(e builtins.Event) { got = append(got, e) })
		ev := &session.LifecycleEvent{Stage: c.stage, Session: &session.Session{ID: "s", State: map[string]any{}}}
		_ = p.OnSessionLifecycle(context.Background(), ev)
		if len(got) == 0 || got[0].Type != c.want {
			t.Errorf("stage %v: expected %q, got %v", c.stage, c.want, got)
		}
	}
}

func TestEventEmitter_OnError(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("error", func(e builtins.Event) { got = append(got, e) })

	ev := &hooks.ErrorEvent{
		Session: &session.Session{ID: "s4", State: map[string]any{}},
		Error:   errors.New("something failed"),
	}
	if err := p.OnError(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "error" {
		t.Fatalf("expected error event, got %v", got)
	}
	data := got[0].Data.(map[string]any)
	if data["error"] != "something failed" {
		t.Fatalf("expected error message in data")
	}
}

func TestEventEmitter_NilSessionFields(t *testing.T) {
	var got []builtins.Event
	p := builtins.EventEmitterPlugin("*", func(e builtins.Event) { got = append(got, e) })

	// nil session on LLM event
	_ = p.BeforeLLM(context.Background(), &hooks.LLMEvent{Session: nil})
	if len(got) == 0 {
		t.Fatal("should still emit when session is nil")
	}
}

// ─── Retry ────────────────────────────────────────────────────────────────────

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	interceptor := builtins.DefaultRetry()
	ev := &hooks.LLMEvent{}
	called := 0
	err := interceptor(context.Background(), ev, func(ctx context.Context) error {
		called++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if called != 1 {
		t.Fatalf("next should be called exactly once, got %d", called)
	}
}

func TestRetry_RetriesOnFailure(t *testing.T) {
	cfg := builtins.RetryConfig{
		MaxRetries:   2,
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		Multiplier:   1.5,
	}
	interceptor := builtins.Retry(cfg)
	ev := &hooks.LLMEvent{}
	callCount := 0
	sentinelErr := errors.New("transient")
	err := interceptor(context.Background(), ev, func(ctx context.Context) error {
		callCount++
		return sentinelErr
	})
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("expected sentinelErr after retries, got %v", err)
	}
	if callCount != 3 { // 1 attempt + 2 retries
		t.Fatalf("expected 3 calls, got %d", callCount)
	}
}

func TestRetry_SucceedsOnRetry(t *testing.T) {
	cfg := builtins.RetryConfig{
		MaxRetries:   3,
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	}
	interceptor := builtins.Retry(cfg)
	ev := &hooks.LLMEvent{}
	callCount := 0
	err := interceptor(context.Background(), ev, func(ctx context.Context) error {
		callCount++
		if callCount < 2 {
			return errors.New("not yet")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after successful retry, got %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	cfg := builtins.RetryConfig{
		MaxRetries:   5,
		InitialDelay: 100 * time.Millisecond,
	}
	interceptor := builtins.Retry(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel after first attempt begins sleeping (20ms << 100ms sleep)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := interceptor(ctx, &hooks.LLMEvent{}, func(_ context.Context) error {
		return errors.New("transient")
	})
	elapsed := time.Since(start)

	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 80*time.Millisecond {
		t.Fatalf("should cancel early, took %v", elapsed)
	}
}

func TestRetry_ShouldRetryFalse(t *testing.T) {
	sentinel := errors.New("non-retryable")
	cfg := builtins.RetryConfig{
		MaxRetries:   5,
		InitialDelay: time.Millisecond,
		ShouldRetry:  func(err error) bool { return false },
	}
	interceptor := builtins.Retry(cfg)
	callCount := 0
	err := interceptor(context.Background(), &hooks.LLMEvent{}, func(_ context.Context) error {
		callCount++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	if callCount != 1 {
		t.Fatalf("should not retry when ShouldRetry=false, got %d calls", callCount)
	}
}

// ─── PatchToolCalls ───────────────────────────────────────────────────────────

func TestPatchToolCalls_NilSession(t *testing.T) {
	hook := builtins.PatchToolCalls()
	err := hook(context.Background(), &hooks.LLMEvent{Session: nil})
	if err != nil {
		t.Fatalf("nil session should return nil, got %v", err)
	}
}

func TestPatchToolCalls_NoMessages(t *testing.T) {
	hook := builtins.PatchToolCalls()
	sess := &session.Session{ID: "s", State: map[string]any{}}
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatalf("empty messages should return nil, got %v", err)
	}
}

func TestPatchToolCalls_BalancedCalls(t *testing.T) {
	hook := builtins.PatchToolCalls()
	sess := &session.Session{ID: "s", State: map[string]any{}}
	// tool call + matching result = balanced
	sess.AppendMessage(model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "c1", Name: "tool1"}},
	})
	sess.AppendMessage(model.Message{
		Role:        model.RoleTool,
		ToolResults: []model.ToolResult{{CallID: "c1"}},
	})
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("balanced calls should not add patch messages")
	}
}

func TestPatchToolCalls_UnbalancedCalls(t *testing.T) {
	hook := builtins.PatchToolCalls()
	sess := &session.Session{ID: "s", State: map[string]any{}}
	// two tool calls, only one result
	sess.AppendMessage(model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []model.ToolCall{
			{ID: "c1", Name: "tool1"},
			{ID: "c2", Name: "tool2"},
		},
	})
	sess.AppendMessage(model.Message{
		Role:        model.RoleTool,
		ToolResults: []model.ToolResult{{CallID: "c1"}},
	})
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	msgs := sess.CopyMessages()
	if len(msgs) != 3 { // 2 original + 1 patch
		t.Fatalf("expected 3 messages after patch, got %d", len(msgs))
	}
	// verify the patch message is a tool result with IsError=true
	last := msgs[2]
	if last.Role != model.RoleTool {
		t.Fatalf("patch message should be RoleTool, got %v", last.Role)
	}
	if !last.ToolResults[0].IsError {
		t.Fatal("patch result should be marked as error")
	}
}

func TestPatchToolCalls_EmptyCallIDs(t *testing.T) {
	hook := builtins.PatchToolCalls()
	sess := &session.Session{ID: "s", State: map[string]any{}}
	// tool calls with empty IDs are ignored
	sess.AppendMessage(model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "", Name: "anon"}},
	})
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("empty-ID calls should not trigger patching")
	}
}

// ─── PriorityCompress ─────────────────────────────────────────────────────────

func mkMessages(n int) []model.Message {
	msgs := make([]model.Message, n)
	for i := 0; i < n; i++ {
		msgs[i] = textMsg(model.RoleUser, strings.Repeat("w", 100)) // ~25 tokens each
	}
	return msgs
}

func TestPriorityCompress_Disabled(t *testing.T) {
	disabled := false
	hook := builtins.PriorityCompress(builtins.PriorityConfig{
		Enabled:          &disabled,
		MaxContextTokens: 10,
		TokenCounter:     func(m model.Message) int { return 100 },
	})
	sess := sessWithMessages(mkMessages(5)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("disabled hook should not modify messages")
	}
}

func TestPriorityCompress_NilSession(t *testing.T) {
	hook := builtins.PriorityCompress(builtins.PriorityConfig{})
	err := hook(context.Background(), &hooks.LLMEvent{Session: nil})
	if err != nil {
		t.Fatalf("nil session: expected nil, got %v", err)
	}
}

func TestPriorityCompress_UnderLimit(t *testing.T) {
	hook := builtins.PriorityCompress(builtins.PriorityConfig{
		MaxContextTokens: 10000,
		TokenCounter:     func(m model.Message) int { return 1 },
	})
	sess := sessWithMessages(mkMessages(3)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("under limit should not compress")
	}
}

func TestPriorityCompress_DropsByScore(t *testing.T) {
	// 5 messages all 10 tokens each = 50 tokens; limit 30 tokens
	// keep recent 1 (10 tokens); budget for history = 20 tokens → keep 2 of 4 history messages
	hook := builtins.PriorityCompress(builtins.PriorityConfig{
		MaxContextTokens: 30,
		KeepRecent:       1,
		TokenCounter:     func(m model.Message) int { return 10 },
		Scorer:           builtins.MessageScorerFunc(func(m model.Message) float64 { return 0.5 }),
	})
	sess := sessWithMessages(mkMessages(5)...)
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	msgs := sess.CopyMessages()
	// system notice injected + kept messages; total must be ≤ 30 tokens
	totalTokens := 0
	for _, m := range msgs {
		if m.Role == model.RoleSystem {
			continue // notice message has near-zero "token" cost in our counter
		}
		totalTokens += 10
	}
	if totalTokens > 30 {
		t.Fatalf("compressed session still exceeds token limit: %d tokens in %d messages", totalTokens, len(msgs))
	}
}

func TestPriorityCompress_NoCompressWhenAllRecent(t *testing.T) {
	// keepRecent >= total dialog messages → no candidate → skip
	hook := builtins.PriorityCompress(builtins.PriorityConfig{
		MaxContextTokens: 10,
		KeepRecent:       100,
		TokenCounter:     func(m model.Message) int { return 5 },
	})
	sess := sessWithMessages(mkMessages(3)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("no candidates → messages should not change")
	}
}

// ─── RuleScorer ───────────────────────────────────────────────────────────────

func TestRuleScorer_SystemAlwaysMax(t *testing.T) {
	s := builtins.RuleScorer{}
	score := s.Score(textMsg(model.RoleSystem, "system instructions"))
	if score != 1.0 {
		t.Fatalf("system message should score 1.0, got %v", score)
	}
}

func TestRuleScorer_ErrorKeywordBoosted(t *testing.T) {
	s := builtins.RuleScorer{}
	normal := s.Score(textMsg(model.RoleUser, "all good"))
	boosted := s.Score(textMsg(model.RoleUser, "there was an error here"))
	if boosted <= normal {
		t.Fatalf("error keyword should boost score: normal=%v boosted=%v", normal, boosted)
	}
}

func TestRuleScorer_ToolResultsBoosted(t *testing.T) {
	s := builtins.RuleScorer{}
	without := s.Score(textMsg(model.RoleUser, "hello"))
	with := s.Score(model.Message{
		Role:        model.RoleTool,
		ToolResults: []model.ToolResult{{CallID: "x"}},
	})
	if with <= without {
		t.Fatalf("tool results should boost score: without=%v with=%v", without, with)
	}
}

// ─── SlidingWindow ────────────────────────────────────────────────────────────

func TestSlidingWindow_Disabled(t *testing.T) {
	disabled := false
	hook := builtins.SlidingWindow(builtins.SlidingWindowConfig{
		Enabled:          &disabled,
		MaxContextTokens: 5,
		TokenCounter:     func(m model.Message) int { return 10 },
	})
	sess := sessWithMessages(mkMessages(4)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("disabled should not modify")
	}
}

func TestSlidingWindow_NilSession(t *testing.T) {
	hook := builtins.SlidingWindow(builtins.SlidingWindowConfig{})
	err := hook(context.Background(), &hooks.LLMEvent{Session: nil})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSlidingWindow_UnderLimit(t *testing.T) {
	hook := builtins.SlidingWindow(builtins.SlidingWindowConfig{
		MaxContextTokens: 10000,
		TokenCounter:     func(m model.Message) int { return 1 },
	})
	sess := sessWithMessages(mkMessages(3)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("under limit: should not modify")
	}
}

func TestSlidingWindow_EvictsOldMessages(t *testing.T) {
	// 10 dialog messages, windowSize=3, token limit exceeded
	hook := builtins.SlidingWindow(builtins.SlidingWindowConfig{
		MaxContextTokens: 5,
		WindowSize:       3,
		TokenCounter:     func(m model.Message) int { return 1 },
	})
	msgs := mkMessages(10)
	sess := sessWithMessages(msgs...)
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	result := sess.CopyMessages()
	// 1 system notice + 3 recent = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages (1 notice + 3 recent), got %d", len(result))
	}
	if result[0].Role != model.RoleSystem {
		t.Fatal("first message should be system summary notice")
	}
}

func TestSlidingWindow_CustomSummarizer(t *testing.T) {
	called := false
	hook := builtins.SlidingWindow(builtins.SlidingWindowConfig{
		MaxContextTokens: 4, // 5 messages × 1 token = 5 > 4 → triggers compression
		WindowSize:       2,
		TokenCounter:     func(m model.Message) int { return 1 },
		Summarizer: func(ctx context.Context, msgs []model.Message) (string, error) {
			called = true
			return "custom summary", nil
		},
	})
	sess := sessWithMessages(mkMessages(5)...)
	_ = hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if !called {
		t.Fatal("summarizer should be called when evicting messages")
	}
	result := sess.CopyMessages()
	// check that the summary is the custom one
	if result[0].Role != model.RoleSystem {
		t.Fatal("first message should be the system summary")
	}
	text := model.ContentPartsToPlainText(result[0].ContentParts)
	if text != "custom summary" {
		t.Fatalf("expected custom summary, got %q", text)
	}
}

func TestSlidingWindow_WindowLargerThanDialog(t *testing.T) {
	// windowSize > dialog messages → skip
	hook := builtins.SlidingWindow(builtins.SlidingWindowConfig{
		MaxContextTokens: 1,
		WindowSize:       100,
		TokenCounter:     func(m model.Message) int { return 10 },
	})
	sess := sessWithMessages(mkMessages(3)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("window >= dialog size should not modify")
	}
}

// ─── AutoTruncate ─────────────────────────────────────────────────────────────

func TestAutoTruncate_NilSession(t *testing.T) {
	hook := builtins.AutoTruncate(builtins.TruncateConfig{})
	err := hook(context.Background(), &hooks.LLMEvent{Session: nil})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAutoTruncate_UnderLimit(t *testing.T) {
	hook := builtins.AutoTruncate(builtins.TruncateConfig{
		MaxContextTokens: 10000,
		TokenCounter:     func(m model.Message) int { return 1 },
	})
	sess := sessWithMessages(mkMessages(5)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("under limit: no truncation expected")
	}
}

func TestAutoTruncate_TruncatesAndAddsNotice(t *testing.T) {
	hook := builtins.AutoTruncate(builtins.TruncateConfig{
		MaxContextTokens: 5,
		KeepRecent:       2,
		TokenCounter:     func(m model.Message) int { return 2 },
	})
	sess := sessWithMessages(mkMessages(6)...)
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	msgs := sess.CopyMessages()
	// system notice + 2 recent = 3
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleSystem {
		t.Fatal("first message should be system notice")
	}
	text := model.ContentPartsToPlainText(msgs[0].ContentParts)
	if !strings.Contains(text, "Context truncated") {
		t.Fatalf("notice should contain 'Context truncated', got %q", text)
	}
}

func TestAutoTruncate_Default(t *testing.T) {
	hook := builtins.DefaultAutoTruncate()
	// default limit is 80000 tokens; our messages are tiny, so no truncation
	sess := sessWithMessages(mkMessages(3)...)
	origLen := len(sess.CopyMessages())
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.CopyMessages()) != origLen {
		t.Fatal("default truncation should not affect short sessions")
	}
}

func TestAutoTruncate_SystemMessagesPreserved(t *testing.T) {
	hook := builtins.AutoTruncate(builtins.TruncateConfig{
		MaxContextTokens: 5,
		KeepRecent:       1,
		TokenCounter:     func(m model.Message) int { return 2 },
	})
	sys := textMsg(model.RoleSystem, "system prompt")
	sess := sessWithMessages(sys)
	for _, m := range mkMessages(4) {
		sess.AppendMessage(m)
	}
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	msgs := sess.CopyMessages()
	// system prompt + truncation notice + 1 recent = 3
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleSystem {
		t.Fatal("original system message should be first")
	}
}

// ─── CommandRules ─────────────────────────────────────────────────────────────

func makePolicyCtx(toolName string, input any) kernio.PolicyContext {
	b, _ := json.Marshal(input)
	return kernio.PolicyContext{
		Tool:  tool.ToolSpec{Name: toolName},
		Input: b,
	}
}

func TestCommandRules_AllowedCommand(t *testing.T) {
	rule := builtins.CommandRules(
		builtins.CommandPatternRule{Name: "git", Match: "git", Access: builtins.Allow},
	)
	ctx := makePolicyCtx("run_command", map[string]any{"command": "git", "args": []string{"status"}})
	result := rule(ctx)
	if result.Decision != kernio.PolicyAllow {
		t.Fatalf("git should be allowed, got %v", result.Decision)
	}
}

func TestCommandRules_DeniedCommand(t *testing.T) {
	rule := builtins.CommandRules(
		builtins.CommandPatternRule{Name: "rm", Match: "rm", Access: builtins.Deny},
	)
	ctx := makePolicyCtx("run_command", map[string]any{"command": "rm", "args": []string{"-rf", "/"}})
	result := rule(ctx)
	if result.Decision != kernio.PolicyDeny {
		t.Fatalf("rm should be denied, got %v", result.Decision)
	}
}

func TestCommandRules_RequireApproval(t *testing.T) {
	rule := builtins.CommandRules(
		builtins.CommandPatternRule{Name: "sudo", Match: "sudo*", Access: builtins.RequireApproval},
	)
	ctx := makePolicyCtx("run_command", map[string]any{"command": "sudo", "args": []string{"rm", "/etc/file"}})
	result := rule(ctx)
	if result.Decision != kernio.PolicyRequireApproval {
		t.Fatalf("sudo should require approval, got %v", result.Decision)
	}
}

func TestCommandRules_DefaultWithNoMatch(t *testing.T) {
	rule := builtins.CommandRulesWithDefault(builtins.Deny,
		builtins.CommandPatternRule{Name: "git", Match: "git", Access: builtins.Allow},
	)
	// unknown command → default = Deny
	ctx := makePolicyCtx("run_command", map[string]any{"command": "curl"})
	result := rule(ctx)
	if result.Decision != kernio.PolicyDeny {
		t.Fatalf("unknown command with Deny default should be denied, got %v", result.Decision)
	}
}

func TestCommandRules_NonCommandToolPassthrough(t *testing.T) {
	rule := builtins.CommandRules(
		builtins.CommandPatternRule{Name: "rm", Match: "rm", Access: builtins.Deny},
	)
	ctx := makePolicyCtx("read_file", map[string]any{"command": "rm"})
	result := rule(ctx)
	if result.Decision != kernio.PolicyAllow {
		t.Fatalf("non-run_command tool should pass through, got %v", result.Decision)
	}
}

func TestCommandRules_WildcardGlob(t *testing.T) {
	rule := builtins.CommandRules(
		builtins.CommandPatternRule{Name: "delete", Match: "rm*", Access: builtins.Deny},
	)
	ctx := makePolicyCtx("run_command", map[string]any{"command": "rmdir", "args": []string{"/tmp/work"}})
	result := rule(ctx)
	if result.Decision != kernio.PolicyDeny {
		t.Fatalf("rmdir should match rm* pattern, got %v", result.Decision)
	}
}

func TestCommandRules_EmptyInput(t *testing.T) {
	rule := builtins.CommandRulesWithDefault(builtins.RequireApproval)
	ctx := kernio.PolicyContext{
		Tool:  tool.ToolSpec{Name: "run_command"},
		Input: nil,
	}
	result := rule(ctx)
	if result.Decision != kernio.PolicyRequireApproval {
		t.Fatalf("empty input with RequireApproval default, got %v", result.Decision)
	}
}

// ─── HTTPRules ────────────────────────────────────────────────────────────────

func TestHTTPRules_AllowedURL(t *testing.T) {
	rule := builtins.HTTPRules(
		builtins.HTTPPatternRule{Name: "github", Match: "*github.com*", Access: builtins.Allow},
	)
	ctx := makePolicyCtx("http_request", map[string]any{"url": "https://api.github.com/repos", "method": "GET"})
	result := rule(ctx)
	if result.Decision != kernio.PolicyAllow {
		t.Fatalf("github should be allowed, got %v", result.Decision)
	}
}

func TestHTTPRules_DeniedURL(t *testing.T) {
	rule := builtins.HTTPRules(
		builtins.HTTPPatternRule{Name: "evil", Match: "*evil.com*", Access: builtins.Deny},
	)
	ctx := makePolicyCtx("http_request", map[string]any{"url": "https://evil.com/api", "method": "GET"})
	result := rule(ctx)
	if result.Decision != kernio.PolicyDeny {
		t.Fatalf("evil.com should be denied, got %v", result.Decision)
	}
}

func TestHTTPRules_MethodFilter(t *testing.T) {
	rule := builtins.HTTPRules(
		builtins.HTTPPatternRule{
			Name:    "post-only-deny",
			Match:   "*api.example.com*",
			Methods: []string{"DELETE"},
			Access:  builtins.Deny,
		},
	)
	// DELETE should be denied
	ctxDel := makePolicyCtx("http_request", map[string]any{"url": "https://api.example.com/res", "method": "DELETE"})
	if result := rule(ctxDel); result.Decision != kernio.PolicyDeny {
		t.Fatalf("DELETE to api.example.com should be denied, got %v", result.Decision)
	}
	// GET should pass through (method doesn't match rule)
	ctxGet := makePolicyCtx("http_request", map[string]any{"url": "https://api.example.com/res", "method": "GET"})
	if result := rule(ctxGet); result.Decision != kernio.PolicyAllow {
		t.Fatalf("GET should not be denied by DELETE-only rule, got %v", result.Decision)
	}
}

func TestHTTPRules_DefaultDeny(t *testing.T) {
	rule := builtins.HTTPRulesWithDefault(builtins.Deny,
		builtins.HTTPPatternRule{Name: "gh", Match: "*github.com*", Access: builtins.Allow},
	)
	ctx := makePolicyCtx("http_request", map[string]any{"url": "https://unknown.io/api", "method": "GET"})
	result := rule(ctx)
	if result.Decision != kernio.PolicyDeny {
		t.Fatalf("unknown URL with Deny default should be denied, got %v", result.Decision)
	}
}

func TestHTTPRules_NonHTTPTool(t *testing.T) {
	rule := builtins.HTTPRules(
		builtins.HTTPPatternRule{Name: "block", Match: "*", Access: builtins.Deny},
	)
	ctx := makePolicyCtx("run_command", map[string]any{"url": "https://evil.com"})
	result := rule(ctx)
	if result.Decision != kernio.PolicyAllow {
		t.Fatalf("non-http_request tool should pass through, got %v", result.Decision)
	}
}

// ─── AuthMiddleware ───────────────────────────────────────────────────────────

type mockAuthenticator struct {
	token    string
	identity *kernio.Identity
	err      error
}

func (m *mockAuthenticator) Authenticate(_ context.Context, token string) (*kernio.Identity, error) {
	if token == m.token {
		return m.identity, m.err
	}
	return nil, errors.New("invalid token")
}

func sessionWithMetadata(id, key, value string) *session.Session {
	s := &session.Session{ID: id, State: map[string]any{}}
	s.SetMetadata(key, value)
	return s
}

func TestAuthMiddleware_NilEvent(t *testing.T) {
	auth := &mockAuthenticator{}
	hook := builtins.AuthMiddleware(auth)
	err := hook(context.Background(), nil)
	if err != nil {
		t.Fatalf("nil event should return nil, got %v", err)
	}
}

func TestAuthMiddleware_NonStartedStage(t *testing.T) {
	auth := &mockAuthenticator{}
	hook := builtins.AuthMiddleware(auth)
	ev := &session.LifecycleEvent{
		Stage:   session.LifecycleCreated,
		Session: &session.Session{ID: "s", State: map[string]any{}},
	}
	err := hook(context.Background(), ev)
	if err != nil {
		t.Fatalf("non-started stage should be skipped, got %v", err)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	auth := &mockAuthenticator{}
	hook := builtins.AuthMiddleware(auth)
	ev := &session.LifecycleEvent{
		Stage:   session.LifecycleStarted,
		Session: &session.Session{ID: "s", State: map[string]any{}},
	}
	err := hook(context.Background(), ev)
	if err == nil || !strings.Contains(err.Error(), "auth token is required") {
		t.Fatalf("missing token should return auth error, got %v", err)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	identity := &kernio.Identity{UserID: "user1", Roles: []string{"admin"}}
	auth := &mockAuthenticator{token: "valid-token", identity: identity}
	hook := builtins.AuthMiddleware(auth)
	sess := sessionWithMetadata("s", "auth_token", "valid-token")
	ev := &session.LifecycleEvent{
		Stage:   session.LifecycleStarted,
		Session: sess,
	}
	err := hook(context.Background(), ev)
	if err != nil {
		t.Fatalf("valid token should succeed, got %v", err)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	auth := &mockAuthenticator{token: "good", identity: &kernio.Identity{UserID: "u1"}}
	hook := builtins.AuthMiddleware(auth)
	sess := sessionWithMetadata("s", "auth_token", "bad-token")
	ev := &session.LifecycleEvent{
		Stage:   session.LifecycleStarted,
		Session: sess,
	}
	err := hook(context.Background(), ev)
	if err == nil {
		t.Fatal("invalid token should return error")
	}
}

func TestAuthMiddleware_AuthError(t *testing.T) {
	authErr := errors.New("server down")
	auth := &mockAuthenticator{token: "tok", err: authErr}
	hook := builtins.AuthMiddleware(auth)
	sess := sessionWithMetadata("s", "auth_token", "tok")
	ev := &session.LifecycleEvent{
		Stage:   session.LifecycleStarted,
		Session: sess,
	}
	err := hook(context.Background(), ev)
	if !errors.Is(err, authErr) {
		t.Fatalf("expected authErr, got %v", err)
	}
}

// ─── RAG helpers ──────────────────────────────────────────────────────────────

// TestRAG_DisabledSkips verifies RAG hook returns nil immediately when disabled.
func TestRAG_DisabledSkips(t *testing.T) {
	disabled := false
	hook := builtins.RAG(builtins.RAGConfig{Enabled: &disabled})
	sess := sessWithMessages(textMsg(model.RoleUser, "query"))
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRAG_NilManagerSkips verifies RAG hook is a no-op when Manager is nil.
func TestRAG_NilManagerSkips(t *testing.T) {
	hook := builtins.RAG(builtins.RAGConfig{Manager: nil})
	sess := sessWithMessages(textMsg(model.RoleUser, "what is moss?"))
	err := hook(context.Background(), &hooks.LLMEvent{Session: sess})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRAG_NilSessionSkips verifies RAG hook is a no-op when session is nil.
func TestRAG_NilSessionSkips(t *testing.T) {
	hook := builtins.RAG(builtins.RAGConfig{})
	err := hook(context.Background(), &hooks.LLMEvent{Session: nil})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRAG_TypedNilManagerSkips(t *testing.T) {
	var injector *stubContextInjector
	hook := builtins.RAG(builtins.RAGConfig{Manager: injector})
	sess := sessWithMessages(textMsg(model.RoleUser, "what is moss?"))
	if err := hook(context.Background(), &hooks.LLMEvent{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 1 {
		t.Fatalf("expected session messages to remain unchanged, got %d", len(sess.Messages))
	}
}

func TestRAG_AppendsInjectedContext(t *testing.T) {
	injector := &stubContextInjector{injected: "<memory_context>\nremember this\n</memory_context>"}
	hook := builtins.RAG(builtins.RAGConfig{
		Manager:   injector,
		MaxChars:  1200,
		EpisodicN: 7,
		SemanticK: 3,
		Threshold: 0.8,
	})
	sess := sessWithMessages(
		textMsg(model.RoleSystem, "base system prompt"),
		textMsg(model.RoleUser, "what is moss?"),
	)
	if err := hook(context.Background(), &hooks.LLMEvent{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if injector.lastConfig.SessionID != "s1" {
		t.Fatalf("expected SessionID s1, got %q", injector.lastConfig.SessionID)
	}
	if injector.lastConfig.Query != "what is moss?" {
		t.Fatalf("expected query from latest user turn, got %q", injector.lastConfig.Query)
	}
	if injector.lastConfig.EpisodicN != 7 || injector.lastConfig.SemanticK != 3 || injector.lastConfig.Threshold != 0.8 || injector.lastConfig.MaxChars != 1200 {
		t.Fatalf("unexpected injector config: %+v", injector.lastConfig)
	}
	got := model.ContentPartsToPlainText(sess.Messages[0].ContentParts)
	if !strings.Contains(got, "base system prompt") || !strings.Contains(got, "remember this") {
		t.Fatalf("expected merged system prompt, got %q", got)
	}
}
