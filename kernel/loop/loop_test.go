package loop

import (
	"context"
	"encoding/json"
	"testing"

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
