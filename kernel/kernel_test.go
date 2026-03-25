package kernel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	kt "github.com/mossagi/moss/kernel/testing"
	"github.com/mossagi/moss/kernel/tool"
)

func TestKernelIntegration(t *testing.T) {
	// MockLLM: 先请求 tool call，然后 text 回复
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{
			{
				Message: port.Message{
					Role:      port.RoleAssistant,
					Content:   "Let me read the file.",
					ToolCalls: []port.ToolCall{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}},
				},
				ToolCalls:  []port.ToolCall{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}},
				StopReason: "tool_use",
				Usage:      port.TokenUsage{TotalTokens: 20},
			},
			{
				Message: port.Message{
					Role:      port.RoleAssistant,
					Content:   "Now let me write a fix.",
					ToolCalls: []port.ToolCall{{ID: "c2", Name: "write_file", Arguments: json.RawMessage(`{"path":"main.go","content":"fixed"}`)}},
				},
				ToolCalls:  []port.ToolCall{{ID: "c2", Name: "write_file", Arguments: json.RawMessage(`{"path":"main.go","content":"fixed"}`)}},
				StopReason: "tool_use",
				Usage:      port.TokenUsage{TotalTokens: 25},
			},
			{
				Message:    port.Message{Role: port.RoleAssistant, Content: "I've fixed the null pointer bug in main.go."},
				StopReason: "end_turn",
				Usage:      port.TokenUsage{TotalTokens: 15},
			},
		},
	}

	io := kt.NewRecorderIO()
	// 当被要求审批时，批准
	io.AskFunc = func(req port.InputRequest) (port.InputResponse, error) {
		return port.InputResponse{Approved: true}, nil
	}

	k := New(
		WithLLM(mock),
		WithUserIO(io),
	)

	// 注册工具
	k.ToolRegistry().Register(tool.ToolSpec{
		Name: "read_file", Description: "Read file contents", Risk: tool.RiskLow,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"package main\nfunc main() {}"`), nil
	})
	k.ToolRegistry().Register(tool.ToolSpec{
		Name: "write_file", Description: "Write file contents", Risk: tool.RiskHigh,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})

	// 设置策略：write_file 需要审批
	k.WithPolicy(
		builtins.RequireApprovalFor("write_file"),
		builtins.DefaultAllow(),
	)

	// 收集事件
	var events []builtins.Event
	k.OnEvent("tool.*", func(e builtins.Event) {
		events = append(events, e)
	})

	// Boot
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	// 创建 Session
	sess, err := k.NewSession(context.Background(), session.SessionConfig{
		Goal:     "Fix the null pointer in main.go",
		MaxSteps: 10,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// 注入初始用户消息
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "Fix the null pointer in main.go"})

	// 运行
	result, err := k.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 验证结果
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Steps != 3 {
		t.Fatalf("Steps = %d, want 3", result.Steps)
	}
	if result.Output != "I've fixed the null pointer bug in main.go." {
		t.Fatalf("Output = %q", result.Output)
	}

	// 验证 3 次 LLM 调用
	if len(mock.Calls) != 3 {
		t.Fatalf("LLM calls = %d, want 3", len(mock.Calls))
	}

	// 验证 write_file 审批被触发
	if len(io.Asked) != 1 {
		t.Fatalf("Ask calls = %d, want 1 (write_file approval)", len(io.Asked))
	}

	// 验证事件被触发
	if len(events) == 0 {
		t.Fatal("expected tool events")
	}
}

func TestKernelBootRequiresLLM(t *testing.T) {
	k := New()
	if err := k.Boot(context.Background()); err == nil {
		t.Fatal("expected error when LLM not configured")
	}
}

func TestKernelRunWithUserIO_OverridesDefaultIO(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []port.CompletionResponse{{
			Message:    port.Message{Role: port.RoleAssistant, Content: "hello from override"},
			StopReason: "end_turn",
		}},
	}

	defaultIO := kt.NewRecorderIO()
	overrideIO := kt.NewRecorderIO()
	k := New(
		WithLLM(mock),
		WithUserIO(defaultIO),
	)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "test", MaxSteps: 5})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "hi"})

	result, err := k.RunWithUserIO(context.Background(), sess, overrideIO)
	if err != nil {
		t.Fatalf("RunWithUserIO: %v", err)
	}
	if result.Output != "hello from override" {
		t.Fatalf("Output = %q, want hello from override", result.Output)
	}
	if len(defaultIO.Sent) != 0 {
		t.Fatalf("default IO should be unused, got %d messages", len(defaultIO.Sent))
	}
	if len(overrideIO.Sent) != 1 {
		t.Fatalf("override IO messages = %d, want 1", len(overrideIO.Sent))
	}
	if overrideIO.Sent[0].Content != "hello from override" {
		t.Fatalf("override IO content = %q", overrideIO.Sent[0].Content)
	}
}
