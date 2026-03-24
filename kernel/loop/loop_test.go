package loop

import (
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	kt "github.com/mossagi/moss/kernel/testing"
	"github.com/mossagi/moss/kernel/tool"
)

func TestLoopTextOnly(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "Hello!"},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 10},
			},
		},
	}
	io := kt.NewRecorderIO()

	l := &AgentLoop{
		LLM:   mock,
		Tools: tool.NewRegistry(),
		IO:    io,
	}

	sess := &session.Session{
		ID:       "test-1",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "Hi"}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "Hello!" {
		t.Fatalf("Output = %q, want %q", result.Output, "Hello!")
	}
	if len(io.Sent) == 0 {
		t.Fatal("expected at least one Send call")
	}
}

func TestLoopToolCall(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message: port.Message{
					Role:      port.RoleAssistant,
					Content:   "",
					ToolCalls: []port.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
				},
				ToolCalls:  []port.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
				StopReason: "tool_use",
				Usage:      port.TokenUsage{TotalTokens: 15},
			},
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "Done!"},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 10},
			},
		},
	}

	reg := tool.NewRegistry()
	reg.Register(tool.ToolSpec{Name: "greet", Description: "Greet someone"}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"Hello world"`), nil
	})

	io := kt.NewRecorderIO()
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
		IO:    io,
	}

	sess := &session.Session{
		ID:       "test-2",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "Greet the world"}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Steps != 2 {
		t.Fatalf("Steps = %d, want 2", result.Steps)
	}

	// 应该有 tool_start 和 tool_result 消息
	hasToolStart := false
	hasToolResult := false
	for _, msg := range io.Sent {
		if msg.Type == port.OutputToolStart {
			hasToolStart = true
		}
		if msg.Type == port.OutputToolResult {
			hasToolResult = true
		}
	}
	if !hasToolStart || !hasToolResult {
		t.Fatal("expected tool_start and tool_result messages in IO")
	}
}

func TestLoopPolicyDeny(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message: port.Message{
					Role:      port.RoleAssistant,
					ToolCalls: []port.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
				},
				ToolCalls:  []port.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
				StopReason: "tool_use",
				Usage:      port.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "Ok"},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 5},
			},
		},
	}

	reg := tool.NewRegistry()
	reg.Register(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		t.Fatal("should not be called")
		return nil, nil
	})

	chain := middleware.NewChain()
	chain.Use(builtins.PolicyCheck(builtins.DenyTool("dangerous_tool")))

	io := kt.NewRecorderIO()
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
		Chain: chain,
		IO:    io,
	}

	sess := &session.Session{
		ID:       "test-3",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "Do something dangerous"}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 工具被拒绝后，结果被记为错误 ToolResult 追加到消息中，loop 继续
	if !result.Success {
		t.Fatalf("expected success (tool denied but loop continues), got error: %s", result.Error)
	}

	// 验证 tool result 包含 denied 错误
	found := false
	for _, msg := range sess.Messages {
		for _, tr := range msg.ToolResults {
			if tr.IsError && tr.Content == builtins.ErrDenied.Error() {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected denied tool result in session messages")
	}
}

func TestLoopBudgetExhausted(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "step 1"},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 100},
			},
		},
	}

	l := &AgentLoop{
		LLM:   mock,
		Tools: tool.NewRegistry(),
		IO:    kt.NewRecorderIO(),
	}

	sess := &session.Session{
		ID:       "test-4",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "test"}},
		Budget:   session.Budget{MaxSteps: 1},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Steps != 1 {
		t.Fatalf("Steps = %d, want 1", result.Steps)
	}
}

func TestLoopParallelToolCalls(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message: port.Message{
					Role: port.RoleAssistant,
					ToolCalls: []port.ToolCall{
						{ID: "c1", Name: "slow_one", Arguments: json.RawMessage(`{}`)},
						{ID: "c2", Name: "slow_two", Arguments: json.RawMessage(`{}`)},
					},
				},
				ToolCalls: []port.ToolCall{
					{ID: "c1", Name: "slow_one", Arguments: json.RawMessage(`{}`)},
					{ID: "c2", Name: "slow_two", Arguments: json.RawMessage(`{}`)},
				},
				StopReason: "tool_use",
				Usage:      port.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "done"},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 5},
			},
		},
	}

	reg := tool.NewRegistry()
	var running int32
	var sawParallel int32
	handler := func(name string) tool.ToolHandler {
		return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			if atomic.AddInt32(&running, 1) > 1 {
				atomic.StoreInt32(&sawParallel, 1)
			}
			defer atomic.AddInt32(&running, -1)
			time.Sleep(30 * time.Millisecond)
			return json.RawMessage(`"` + name + `"`), nil
		}
	}
	if err := reg.Register(tool.ToolSpec{Name: "slow_one"}, handler("one")); err != nil {
		t.Fatalf("register slow_one: %v", err)
	}
	if err := reg.Register(tool.ToolSpec{Name: "slow_two"}, handler("two")); err != nil {
		t.Fatalf("register slow_two: %v", err)
	}

	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
		IO:    kt.NewRecorderIO(),
		Config: LoopConfig{
			ParallelToolCall: true,
		},
	}

	sess := &session.Session{
		ID:       "test-parallel",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "run both tools"}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if atomic.LoadInt32(&sawParallel) != 1 {
		t.Fatal("expected tool calls to run in parallel")
	}

	if len(sess.Messages) < 4 {
		t.Fatalf("expected tool results appended to session, got %d messages", len(sess.Messages))
	}
	toolResults := 0
	for _, msg := range sess.Messages {
		toolResults += len(msg.ToolResults)
	}
	if toolResults != 2 {
		t.Fatalf("expected 2 tool results, got %d", toolResults)
	}
}

type flakyLLM struct {
	failures int32
	calls    int32
	resp     port.CompletionResponse
}

func (f *flakyLLM) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	call := atomic.AddInt32(&f.calls, 1)
	if call <= f.failures {
		return nil, context.DeadlineExceeded
	}
	resp := f.resp
	return &resp, nil
}

type flakyStreamingLLM struct {
	failures int32
	calls    int32
	chunks   []port.StreamChunk
}

func (f *flakyStreamingLLM) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	return nil, nil
}

func (f *flakyStreamingLLM) Stream(_ context.Context, _ port.CompletionRequest) (port.StreamIterator, error) {
	call := atomic.AddInt32(&f.calls, 1)
	if call <= f.failures {
		return &errIterator{err: context.DeadlineExceeded}, nil
	}
	return &sliceIterator{chunks: f.chunks}, nil
}

type sliceIterator struct {
	chunks []port.StreamChunk
	index  int
}

func (it *sliceIterator) Next() (port.StreamChunk, error) {
	if it.index >= len(it.chunks) {
		return port.StreamChunk{}, io.EOF
	}
	chunk := it.chunks[it.index]
	it.index++
	return chunk, nil
}

func (it *sliceIterator) Close() error { return nil }

type errIterator struct {
	err    error
	called bool
}

func (it *errIterator) Next() (port.StreamChunk, error) {
	if it.called {
		return port.StreamChunk{}, io.EOF
	}
	it.called = true
	return port.StreamChunk{}, it.err
}

func (it *errIterator) Close() error { return nil }

func TestLoopLLMRetry_Sync(t *testing.T) {
	l := &AgentLoop{
		LLM: &flakyLLM{
			failures: 2,
			resp: port.CompletionResponse{
				Message:    port.Message{Role: port.RoleAssistant, Content: "retried"},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 7},
			},
		},
		Tools: tool.NewRegistry(),
		IO:    kt.NewRecorderIO(),
		Config: LoopConfig{
			LLMRetry: RetryConfig{
				MaxRetries:   3,
				InitialDelay: time.Millisecond,
				MaxDelay:     5 * time.Millisecond,
			},
		},
	}

	sess := &session.Session{
		ID:       "test-retry-sync",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "hi"}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "retried" {
		t.Fatalf("Output = %q, want retried", result.Output)
	}
	if got := atomic.LoadInt32(&l.LLM.(*flakyLLM).calls); got != 3 {
		t.Fatalf("expected 3 LLM calls, got %d", got)
	}
}

func TestLoopLLMRetry_StreamingBeforeEmission(t *testing.T) {
	streamLLM := &flakyStreamingLLM{
		failures: 1,
		chunks: []port.StreamChunk{
			{Delta: "ok"},
			{Done: true, Usage: &port.TokenUsage{TotalTokens: 3}},
		},
	}
	l := &AgentLoop{
		LLM:   streamLLM,
		Tools: tool.NewRegistry(),
		IO:    kt.NewRecorderIO(),
		Config: LoopConfig{
			LLMRetry: RetryConfig{
				MaxRetries:   2,
				InitialDelay: time.Millisecond,
				MaxDelay:     5 * time.Millisecond,
			},
		},
	}

	sess := &session.Session{
		ID:       "test-retry-stream",
		Status:   session.StatusCreated,
		Messages: []port.Message{{Role: port.RoleUser, Content: "hi"}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "ok" {
		t.Fatalf("Output = %q, want ok", result.Output)
	}
	if got := atomic.LoadInt32(&streamLLM.calls); got != 2 {
		t.Fatalf("expected 2 stream attempts, got %d", got)
	}
}
