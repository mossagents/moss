package loop

import (
	"context"
	"encoding/json"
	stderrors "errors"
	kerrors "github.com/mossagents/moss/kernel/errors"
	intr "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoopTextOnly(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("Hello!")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 10},
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
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Hi")}}},
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

func TestLoopStreamingReasoning(t *testing.T) {
	mock := &kt.MockStreamingLLM{
		Chunks: [][]mdl.StreamChunk{{
			{ReasoningDelta: "First inspect the redirect. "},
			{ReasoningDelta: "Then query the weather endpoint."},
			{Delta: "Hangzhou is cloudy.", Done: true, Usage: &mdl.TokenUsage{TotalTokens: 10}},
		}},
	}
	io := kt.NewRecorderIO()

	l := &AgentLoop{
		LLM:   mock,
		Tools: tool.NewRegistry(),
		IO:    io,
	}
	sess := &session.Session{
		ID:       "test-reasoning-stream",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("weather?")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got := mdl.ContentPartsToReasoningText(sess.Messages[len(sess.Messages)-1].ContentParts); got != "First inspect the redirect. Then query the weather endpoint." {
		t.Fatalf("session reasoning = %q", got)
	}
	foundReasoning := false
	for _, msg := range io.Sent {
		if msg.Type == intr.OutputReasoning {
			foundReasoning = true
			break
		}
	}
	if !foundReasoning {
		t.Fatal("expected reasoning output to be sent to IO")
	}
}

func TestLoopToolCall(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message: mdl.Message{
					Role:         mdl.RoleAssistant,
					ContentParts: []mdl.ContentPart{mdl.TextPart("")},
					ToolCalls:    []mdl.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
				},
				ToolCalls:  []mdl.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
				StopReason: "tool_use",
				Usage:      mdl.TokenUsage{TotalTokens: 15},
			},
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("Done!")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 10},
			},
		},
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "greet", Description: "Greet someone"}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"Hello world"`), nil
	}); err != nil {
		t.Fatalf("register greet: %v", err)
	}

	io := kt.NewRecorderIO()
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
		IO:    io,
	}

	sess := &session.Session{
		ID:       "test-2",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Greet the world")}}},
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
		if msg.Type == intr.OutputToolStart {
			hasToolStart = true
		}
		if msg.Type == intr.OutputToolResult {
			hasToolResult = true
		}
	}
	if !hasToolStart || !hasToolResult {
		t.Fatal("expected tool_start and tool_result messages in IO")
	}
}

func TestLoopGreetingTurnDoesNotExposeTools(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("你好！有什么我可以帮你的？")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 8},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "list_files", Description: "List files"}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[]`), nil
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
	}
	sess := &session.Session{
		ID: "test-greeting-no-tools",
		Messages: []mdl.Message{
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("继续分析项目结构")}},
			{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("我先读取 README")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("你好")}},
		},
		Budget: session.Budget{MaxSteps: 4},
	}
	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("calls=%d, want 1", len(mock.Calls))
	}
	if got := len(mock.Calls[0].Tools); got != 0 {
		t.Fatalf("tools exposed for lightweight chat: %d", got)
	}
	if len(mock.Calls[0].Messages) != 1 {
		t.Fatalf("messages=%d, want 1", len(mock.Calls[0].Messages))
	}
	if got := mdl.ContentPartsToPlainText(mock.Calls[0].Messages[0].ContentParts); got != "你好" {
		t.Fatalf("prompt message=%q, want 你好", got)
	}
}

func TestLoopPlanningTurnBuildsToolRouteAndModelLane(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("Plan first.")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 12},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "read_file", Risk: tool.RiskLow, Capabilities: []string{"filesystem"}}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}); err != nil {
		t.Fatalf("register read_file: %v", err)
	}
	if err := reg.Register(tool.ToolSpec{Name: "write_file", Risk: tool.RiskHigh, Capabilities: []string{"filesystem"}}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}); err != nil {
		t.Fatalf("register write_file: %v", err)
	}
	observer := &recordingObserver{}
	l := &AgentLoop{
		LLM:      mock,
		Tools:    reg,
		Observer: observer,
		RunID:    "run-phase2",
	}
	sess := &session.Session{
		ID: "planning-turn",
		Config: session.SessionConfig{
			Profile: "planner",
			Metadata: map[string]any{
				session.MetadataTaskMode:          "planning",
				session.MetadataEffectiveTrust:    "trusted",
				session.MetadataEffectiveApproval: "confirm",
			},
		},
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Please plan the refactor")}}},
		Budget:   session.Budget{MaxSteps: 4},
	}
	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected one llm call, got %d", len(mock.Calls))
	}
	if len(mock.Calls[0].Tools) != 1 || mock.Calls[0].Tools[0].Name != "read_file" {
		t.Fatalf("unexpected tool exposure: %+v", mock.Calls[0].Tools)
	}
	if mock.Calls[0].Config.Requirements == nil || mock.Calls[0].Config.Requirements.Lane != "reasoning" {
		t.Fatalf("unexpected model lane: %+v", mock.Calls[0].Config.Requirements)
	}
	if !slices.Contains(mock.Calls[0].Config.Requirements.Capabilities, mdl.CapReasoning) || !slices.Contains(mock.Calls[0].Config.Requirements.Capabilities, mdl.CapFunctionCalling) {
		t.Fatalf("unexpected capabilities: %+v", mock.Calls[0].Config.Requirements.Capabilities)
	}
	if got := sess.Config.Metadata[session.MetadataModelLane]; got != "reasoning" {
		t.Fatalf("model lane metadata = %#v", got)
	}
	if got := sess.Config.Metadata[session.MetadataVisibleTools]; !reflect.DeepEqual(got, []string{"read_file"}) {
		t.Fatalf("visible tools metadata = %#v", got)
	}
	foundToolRouteEvent := false
	for _, event := range observer.execution {
		if event.Type == kobs.ExecutionEventType("tool.route_planned") {
			foundToolRouteEvent = true
			if event.EventID == "" || event.RunID != "run-phase2" || event.TurnID == "" {
				t.Fatalf("unexpected route event envelope: %+v", event)
			}
			decisions, ok := event.Data["decisions"].([]map[string]any)
			if ok {
				if len(decisions) == 0 {
					t.Fatalf("expected route decisions in event data: %+v", event.Data)
				}
			} else if decisionsAny, ok := event.Data["decisions"].([]any); !ok || len(decisionsAny) == 0 {
				t.Fatalf("expected route decisions in event data: %+v", event.Data)
			}
		}
	}
	if !foundToolRouteEvent {
		t.Fatal("expected tool.route_planned event")
	}
}

func TestLoopHiddenToolCallReturnsNotAllowedError(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message: mdl.Message{
					Role:      mdl.RoleAssistant,
					ToolCalls: []mdl.ToolCall{{ID: "c1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x","content":"y"}`)}},
				},
				ToolCalls:  []mdl.ToolCall{{ID: "c1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x","content":"y"}`)}},
				StopReason: "tool_use",
				Usage:      mdl.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 8},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "write_file", Risk: tool.RiskHigh, Capabilities: []string{"filesystem"}}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"should-not-run"`), nil
	}); err != nil {
		t.Fatalf("register write_file: %v", err)
	}
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
	}
	sess := &session.Session{
		ID: "planning-hidden-tool",
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataTaskMode: "planning",
			},
		},
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Plan the change")}}},
		Budget:   session.Budget{MaxSteps: 4},
	}
	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sess.Messages) < 3 {
		t.Fatalf("expected tool result message, got %+v", sess.Messages)
	}
	toolMsg := sess.Messages[2]
	if toolMsg.Role != mdl.RoleTool || len(toolMsg.ToolResults) != 1 {
		t.Fatalf("unexpected tool message: %+v", toolMsg)
	}
	if got := mdl.ContentPartsToPlainText(toolMsg.ToolResults[0].ContentParts); !strings.Contains(got, "not allowed in current turn") {
		t.Fatalf("unexpected tool error: %q", got)
	}
}

func TestLoopExecutionProgressEvents(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 9},
			},
		},
	}
	observer := &recordingObserver{}
	l := &AgentLoop{
		LLM:      mock,
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{
		ID:       "test-progress-events",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}
	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	types := make([]kobs.ExecutionEventType, 0, len(observer.execution))
	for _, event := range observer.execution {
		types = append(types, event.Type)
	}
	wantOrder := []kobs.ExecutionEventType{
		kobs.ExecutionRunStarted,
		kobs.ExecutionIterationStarted,
		kobs.ExecutionLLMStarted,
		kobs.ExecutionLLMCompleted,
		kobs.ExecutionIterationProgress,
		kobs.ExecutionRunCompleted,
	}
	lastIndex := -1
	for _, want := range wantOrder {
		found := -1
		for i := lastIndex + 1; i < len(types); i++ {
			if types[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("execution events missing %s in order: %v", want, types)
		}
		lastIndex = found
	}
	var progress kobs.ExecutionEvent
	found := false
	for _, event := range observer.execution {
		if event.Type == kobs.ExecutionIterationProgress {
			progress = event
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected iteration.progress event")
	}
	if got := progress.Data["iteration"]; got != 1 {
		t.Fatalf("progress iteration = %v, want 1", got)
	}
	if got := progress.Data["stop_reason"]; got != "end_turn" {
		t.Fatalf("progress stop_reason = %v, want end_turn", got)
	}
	if progress.EventID == "" || progress.EventVersion != 1 || progress.Phase != "iteration" || progress.PayloadKind != "iteration" {
		t.Fatalf("unexpected progress envelope: %+v", progress)
	}
	var completed kobs.ExecutionEvent
	found = false
	for _, event := range observer.execution {
		if event.Type == kobs.ExecutionRunCompleted {
			completed = event
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected run.completed event")
	}
	if completed.EventID == "" || completed.EventVersion != 1 || completed.Phase != "run" || completed.PayloadKind != "run" {
		t.Fatalf("unexpected completed envelope: %+v", completed)
	}
}

func TestLoopPolicyDeny(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message: mdl.Message{
					Role:      mdl.RoleAssistant,
					ToolCalls: []mdl.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
				},
				ToolCalls:  []mdl.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
				StopReason: "tool_use",
				Usage:      mdl.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("Ok")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 5},
			},
		},
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		t.Fatal("should not be called")
		return nil, nil
	}); err != nil {
		t.Fatalf("register dangerous_tool: %v", err)
	}

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
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Do something dangerous")}}},
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
	var toolResultMsg *intr.OutputMessage
	for _, msg := range sess.Messages {
		for _, tr := range msg.ToolResults {
			if tr.IsError && mdl.ContentPartsToPlainText(tr.ContentParts) == builtins.ErrDenied.Error() {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected denied tool result in session messages")
	}
	for i := range io.Sent {
		msg := &io.Sent[i]
		if msg.Type == intr.OutputToolResult {
			toolResultMsg = msg
			break
		}
	}
	if toolResultMsg == nil {
		t.Fatal("expected tool_result message")
	}
	if got := toolResultMsg.Meta["error_code"]; got != string(kerrors.ErrPolicyDenied) {
		t.Fatalf("expected error_code %s, got %v", kerrors.ErrPolicyDenied, got)
	}
	if got := toolResultMsg.Meta["reason_code"]; got != "tool.denied" {
		t.Fatalf("expected reason_code tool.denied, got %v", got)
	}
}

func TestLoopBudgetExhausted(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("step 1")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 100},
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
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("test")}}},
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

func TestLoopBudgetStopsWhenTokenConsumeWouldExceedLimit(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("large token response")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 11},
			},
		},
	}

	l := &AgentLoop{
		LLM:   mock,
		Tools: tool.NewRegistry(),
		IO:    kt.NewRecorderIO(),
	}

	sess := &session.Session{
		ID:       "budget-tokens",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("test")}}},
		Budget:   session.Budget{MaxTokens: 10, MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success stop, got error %s", result.Error)
	}
	if result.Steps != 0 {
		t.Fatalf("Steps = %d, want 0 because consume should be rejected", result.Steps)
	}
	if sess.Budget.UsedTokensValue() != 0 {
		t.Fatalf("UsedTokens = %d, want 0", sess.Budget.UsedTokensValue())
	}
}

func TestLoopParallelToolCalls(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []mdl.CompletionResponse{
			{
				Message: mdl.Message{
					Role: mdl.RoleAssistant,
					ToolCalls: []mdl.ToolCall{
						{ID: "c1", Name: "slow_one", Arguments: json.RawMessage(`{}`)},
						{ID: "c2", Name: "slow_two", Arguments: json.RawMessage(`{}`)},
					},
				},
				ToolCalls: []mdl.ToolCall{
					{ID: "c1", Name: "slow_one", Arguments: json.RawMessage(`{}`)},
					{ID: "c2", Name: "slow_two", Arguments: json.RawMessage(`{}`)},
				},
				StopReason: "tool_use",
				Usage:      mdl.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 5},
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
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("run both tools")}}},
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

func TestLoopCancellationMarksSessionCancelledAndEnded(t *testing.T) {
	bl := &blockingLLM{}
	l := &AgentLoop{
		LLM:   bl,
		Tools: tool.NewRegistry(),
		IO:    kt.NewRecorderIO(),
	}
	sess := &session.Session{
		ID:       "cancelled-loop",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("wait")}}},
		Budget:   session.Budget{MaxSteps: 5},
	}
	ctx, cancel := context.WithCancel(context.Background())

	runErrCh := make(chan error, 1)
	go func() {
		_, err := l.Run(ctx, sess)
		runErrCh <- err
	}()

	deadline := time.After(500 * time.Millisecond)
	for atomic.LoadInt32(&bl.calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("LLM was not called before timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()

	select {
	case err := <-runErrCh:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
		if !stderrors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after cancellation")
	}

	if sess.Status != session.StatusCancelled {
		t.Fatalf("status = %q, want %q", sess.Status, session.StatusCancelled)
	}
	if sess.EndedAt.IsZero() {
		t.Fatal("ended_at should be set on cancellation")
	}
}

func TestExecuteSingleToolCall_RepairsTruncatedArguments(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "echo_json"}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in map[string]any
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return json.Marshal(in)
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	l := &AgentLoop{Tools: reg, IO: kt.NewRecorderIO()}
	sess := &session.Session{ID: "sess-json-repair", Status: session.StatusCreated, Budget: session.Budget{MaxSteps: 5}}

	call := mdl.ToolCall{
		ID:        "c1",
		Name:      "echo_json",
		Arguments: json.RawMessage(`{"a":1`),
	}
	result := l.executeSingleToolCall(context.Background(), sess, call)
	if result.IsError {
		t.Fatalf("expected repaired args to succeed, got error: %s", mdl.ContentPartsToPlainText(result.ContentParts))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(mdl.ContentPartsToPlainText(result.ContentParts)), &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if out["a"] != float64(1) {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestExecuteSingleToolCall_PolicyDeniedAddsStructuredExecutionMetadata(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("tool should not execute")
		return nil, nil
	}); err != nil {
		t.Fatalf("register dangerous_tool: %v", err)
	}
	chain := middleware.NewChain()
	chain.Use(builtins.PolicyCheck(builtins.DenyTool("dangerous_tool")))
	observer := &recordingObserver{}
	l := &AgentLoop{
		Tools:    reg,
		Chain:    chain,
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{ID: "sess-policy-meta", Status: session.StatusCreated, Budget: session.Budget{MaxSteps: 5}}
	call := mdl.ToolCall{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}

	result := l.executeSingleToolCall(context.Background(), sess, call)
	if !result.IsError {
		t.Fatalf("expected denied tool result, got %+v", result)
	}

	var toolCompleted *kobs.ExecutionEvent
	for i := range observer.execution {
		ev := &observer.execution[i]
		if ev.Type == kobs.ExecutionToolCompleted {
			toolCompleted = ev
			break
		}
	}
	if toolCompleted == nil {
		t.Fatal("expected tool.completed event")
	}
	if got := toolCompleted.Data["error_code"]; got != string(kerrors.ErrPolicyDenied) {
		t.Fatalf("expected error_code %s, got %v", kerrors.ErrPolicyDenied, got)
	}
	if got := toolCompleted.Data["reason_code"]; got != "tool.denied" {
		t.Fatalf("expected reason_code tool.denied, got %v", got)
	}
}

func TestLoopLLMErrorEventIncludesErrorCode(t *testing.T) {
	observer := &recordingObserver{}
	loopErr := kerrors.New(kerrors.ErrLLMCall, "llm failed")
	l := &AgentLoop{
		LLM:      &errorLLM{err: loopErr},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{
		ID:       "test-llm-error-code",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 5},
	}
	if _, err := l.Run(context.Background(), sess); err == nil {
		t.Fatal("expected run to fail")
	}
	var llmCompleted *kobs.ExecutionEvent
	for i := range observer.execution {
		ev := &observer.execution[i]
		if ev.Type == kobs.ExecutionLLMCompleted && ev.Error != "" {
			llmCompleted = ev
			break
		}
	}
	if llmCompleted == nil {
		t.Fatal("expected llm.completed error event")
	}
	if got := llmCompleted.Data["error_code"]; got != string(kerrors.ErrLLMCall) {
		t.Fatalf("expected error_code %s, got %v", kerrors.ErrLLMCall, got)
	}
}

func TestLoopRunFailedEventIncludesErrorCode(t *testing.T) {
	observer := &recordingObserver{}
	loopErr := kerrors.New(kerrors.ErrLLMCall, "llm failed")
	l := &AgentLoop{
		LLM:      &errorLLM{err: loopErr},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{
		ID:       "test-run-failed-error-code",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 5},
	}
	if _, err := l.Run(context.Background(), sess); err == nil {
		t.Fatal("expected run to fail")
	}
	var runFailed *kobs.ExecutionEvent
	for i := range observer.execution {
		ev := &observer.execution[i]
		if ev.Type == kobs.ExecutionRunFailed {
			runFailed = ev
			break
		}
	}
	if runFailed == nil {
		t.Fatal("expected run.failed event")
	}
	if got := runFailed.Data["error_code"]; got != string(kerrors.ErrLLMCall) {
		t.Fatalf("expected error_code %s, got %v", kerrors.ErrLLMCall, got)
	}
}

type blockingLLM struct {
	calls int32
}

func (b *blockingLLM) Complete(ctx context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	atomic.AddInt32(&b.calls, 1)
	<-ctx.Done()
	return nil, ctx.Err()
}

type flakyLLM struct {
	failures int32
	calls    int32
	resp     mdl.CompletionResponse
}

type errorLLM struct {
	err error
}

func (e *errorLLM) Complete(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	return nil, e.err
}

func (f *flakyLLM) Complete(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
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
	chunks   []mdl.StreamChunk
	resp     *mdl.CompletionResponse
}

func (f *flakyStreamingLLM) Complete(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	if f.resp != nil {
		return f.resp, nil
	}
	return nil, context.DeadlineExceeded
}

func (f *flakyStreamingLLM) Stream(_ context.Context, _ mdl.CompletionRequest) (mdl.StreamIterator, error) {
	call := atomic.AddInt32(&f.calls, 1)
	if call <= f.failures {
		return &errIterator{err: context.DeadlineExceeded}, nil
	}
	return &sliceIterator{chunks: f.chunks}, nil
}

type sliceIterator struct {
	chunks []mdl.StreamChunk
	index  int
}

func (it *sliceIterator) Next() (mdl.StreamChunk, error) {
	if it.index >= len(it.chunks) {
		return mdl.StreamChunk{}, io.EOF
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

func (it *errIterator) Next() (mdl.StreamChunk, error) {
	if it.called {
		return mdl.StreamChunk{}, io.EOF
	}
	it.called = true
	return mdl.StreamChunk{}, it.err
}

func (it *errIterator) Close() error { return nil }

type metadataStreamingLLM struct {
	chunks   []mdl.StreamChunk
	metadata mdl.LLMCallMetadata
}

func (m *metadataStreamingLLM) Complete(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	return nil, context.DeadlineExceeded
}

func (m *metadataStreamingLLM) Stream(_ context.Context, _ mdl.CompletionRequest) (mdl.StreamIterator, error) {
	return &metadataIterator{chunks: m.chunks, metadata: m.metadata}, nil
}

type metadataIterator struct {
	chunks   []mdl.StreamChunk
	index    int
	metadata mdl.LLMCallMetadata
}

func (it *metadataIterator) Next() (mdl.StreamChunk, error) {
	if it.index >= len(it.chunks) {
		return mdl.StreamChunk{}, io.EOF
	}
	chunk := it.chunks[it.index]
	it.index++
	return chunk, nil
}

func (it *metadataIterator) Close() error { return nil }

func (it *metadataIterator) Metadata() mdl.LLMCallMetadata { return it.metadata }

type postEmissionErrorLLM struct {
	completeCalls int32
}

func (p *postEmissionErrorLLM) Complete(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	atomic.AddInt32(&p.completeCalls, 1)
	return &mdl.CompletionResponse{
		Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("fallback")}},
		StopReason: "end_turn",
	}, nil
}

func (p *postEmissionErrorLLM) Stream(_ context.Context, _ mdl.CompletionRequest) (mdl.StreamIterator, error) {
	return &postEmissionIterator{}, nil
}

type postEmissionIterator struct {
	calls int
}

func (it *postEmissionIterator) Next() (mdl.StreamChunk, error) {
	it.calls++
	switch it.calls {
	case 1:
		return mdl.StreamChunk{Delta: "partial"}, nil
	default:
		return mdl.StreamChunk{}, context.DeadlineExceeded
	}
}

func (it *postEmissionIterator) Close() error { return nil }

type toolThenErrIterator struct {
	calls int
}

func (it *toolThenErrIterator) Next() (mdl.StreamChunk, error) {
	it.calls++
	switch it.calls {
	case 1:
		tc := mdl.ToolCall{ID: "call_t", Name: "noop", Arguments: json.RawMessage(`{"x":1}`)}
		return mdl.StreamChunk{ToolCall: &tc}, nil
	case 2:
		return mdl.StreamChunk{}, io.ErrUnexpectedEOF
	default:
		return mdl.StreamChunk{}, io.EOF
	}
}

func (it *toolThenErrIterator) Close() error { return nil }

type recordingObserver struct {
	llmCalls  []kobs.LLMCallEvent
	execution []kobs.ExecutionEvent
	errors    []kobs.ErrorEvent
}

func (o *recordingObserver) OnLLMCall(_ context.Context, e kobs.LLMCallEvent) {
	o.llmCalls = append(o.llmCalls, e)
}

func (o *recordingObserver) OnToolCall(context.Context, kobs.ToolCallEvent) {}

func (o *recordingObserver) OnExecutionEvent(_ context.Context, e kobs.ExecutionEvent) {
	o.execution = append(o.execution, e)
}

func (o *recordingObserver) OnApproval(context.Context, intr.ApprovalEvent)   {}
func (o *recordingObserver) OnSessionEvent(context.Context, kobs.SessionEvent) {}
func (o *recordingObserver) OnError(_ context.Context, e kobs.ErrorEvent) {
	o.errors = append(o.errors, e)
}

func (o *recordingObserver) lastCompletedModel() string {
	for i := len(o.execution) - 1; i >= 0; i-- {
		if o.execution[i].Type == kobs.ExecutionLLMCompleted {
			return o.execution[i].Model
		}
	}
	return ""
}

type toolThenErrLLM struct{}

func (t *toolThenErrLLM) Complete(_ context.Context, _ mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	return nil, nil
}

func (t *toolThenErrLLM) Stream(_ context.Context, _ mdl.CompletionRequest) (mdl.StreamIterator, error) {
	return &toolThenErrIterator{}, nil
}

func TestLoopLLMRetry_Sync(t *testing.T) {
	l := &AgentLoop{
		LLM: &flakyLLM{
			failures: 2,
			resp: mdl.CompletionResponse{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("retried")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 7},
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
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
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

func TestLoopLifecycleHookPanicEmitsErrorAndContinues(t *testing.T) {
	observer := &recordingObserver{}
	var stages []session.LifecycleStage
	l := &AgentLoop{
		LLM: &kt.MockLLM{
			Responses: []mdl.CompletionResponse{{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("ok")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 3},
			}},
		},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
		LifecycleHook: func(_ context.Context, event session.LifecycleEvent) {
			stages = append(stages, event.Stage)
			if event.Stage == session.LifecycleStarted {
				panic("boom")
			}
		},
	}

	sess := &session.Session{
		ID:       "test-lifecycle-panic",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}
	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got, want := len(observer.errors), 1; got != want {
		t.Fatalf("error events = %d, want %d", got, want)
	}
	if got := observer.errors[0].Phase; got != "session_lifecycle_hook" {
		t.Fatalf("error phase = %q, want session_lifecycle_hook", got)
	}
	wantStages := []session.LifecycleStage{session.LifecycleStarted, session.LifecycleCompleted}
	if len(stages) != len(wantStages) {
		t.Fatalf("stages len = %d, want %d (%v)", len(stages), len(wantStages), stages)
	}
	for i := range wantStages {
		if stages[i] != wantStages[i] {
			t.Fatalf("stages[%d] = %q, want %q", i, stages[i], wantStages[i])
		}
	}
}

func TestLoopToolLifecycleHooksCaptureDeniedToolCall(t *testing.T) {
	observer := &recordingObserver{}
	chain := middleware.NewChain()
	chain.Use(builtins.PolicyCheck(builtins.DenyTool("dangerous_tool")))
	var events []session.ToolLifecycleEvent
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("tool should not be executed")
		return nil, nil
	}); err != nil {
		t.Fatalf("register dangerous_tool: %v", err)
	}
	l := &AgentLoop{
		LLM: &kt.MockLLM{
			Responses: []mdl.CompletionResponse{
				{
					Message: mdl.Message{
						Role:      mdl.RoleAssistant,
						ToolCalls: []mdl.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
					},
					ToolCalls:  []mdl.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
					StopReason: "tool_use",
					Usage:      mdl.TokenUsage{TotalTokens: 5},
				},
				{
					Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("done")}},
					StopReason: "end_turn",
					Usage:      mdl.TokenUsage{TotalTokens: 3},
				},
			},
		},
		Tools:    reg,
		Chain:    chain,
		IO:       kt.NewRecorderIO(),
		Observer: observer,
		ToolLifecycleHook: func(_ context.Context, event session.ToolLifecycleEvent) {
			events = append(events, event)
		},
	}

	sess := &session.Session{
		ID:       "test-tool-lifecycle-denied",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("do the dangerous thing")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}
	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success after denied tool handling, got error: %s", result.Error)
	}
	if got, want := len(events), 2; got != want {
		t.Fatalf("tool lifecycle events = %d, want %d", got, want)
	}
	if events[0].Stage != session.ToolLifecycleBefore {
		t.Fatalf("first tool lifecycle stage = %q, want before", events[0].Stage)
	}
	if events[1].Stage != session.ToolLifecycleAfter {
		t.Fatalf("second tool lifecycle stage = %q, want after", events[1].Stage)
	}
	if events[1].Error == nil {
		t.Fatal("expected denied tool call to surface error in after hook")
	}
	if events[1].Result == nil || !events[1].Result.IsError {
		t.Fatal("expected denied tool call to surface error result in after hook")
	}
}

func TestLoopLLMRetry_StreamingBeforeEmission(t *testing.T) {
	streamLLM := &flakyStreamingLLM{
		failures: 1,
		chunks: []mdl.StreamChunk{
			{Delta: "ok"},
			{Done: true, Usage: &mdl.TokenUsage{TotalTokens: 3}},
		},
		resp: &mdl.CompletionResponse{
			Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("fallback")}},
			StopReason: "end_turn",
			Usage:      mdl.TokenUsage{TotalTokens: 2},
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
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Output != "fallback" && result.Output != "ok" {
		t.Fatalf("Output = %q, want fallback|ok", result.Output)
	}
	if got := atomic.LoadInt32(&streamLLM.calls); got != 1 {
		t.Fatalf("expected 1 stream attempt with sync fallback, got %d", got)
	}
}

func TestLoopLLMCallUsesActualModelMetadata(t *testing.T) {
	observer := &recordingObserver{}
	l := &AgentLoop{
		LLM: &kt.MockLLM{
			Responses: []mdl.CompletionResponse{{
				Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("ok")}},
				StopReason: "end_turn",
				Usage:      mdl.TokenUsage{TotalTokens: 3},
				Metadata:   &mdl.LLMCallMetadata{ActualModel: "router-picked"},
			}},
		},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}

	sess := &session.Session{
		ID:       "test-actual-model",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(observer.llmCalls) == 0 {
		t.Fatal("expected LLM observer event")
	}
	if observer.llmCalls[0].Model != "router-picked" {
		t.Fatalf("observer model = %q, want router-picked", observer.llmCalls[0].Model)
	}
	if got := observer.lastCompletedModel(); got != "router-picked" {
		t.Fatalf("completed event model = %q, want router-picked", got)
	}
}

func TestLoopStreamingUsesIteratorMetadataActualModel(t *testing.T) {
	observer := &recordingObserver{}
	l := &AgentLoop{
		LLM: &metadataStreamingLLM{
			chunks: []mdl.StreamChunk{
				{Delta: "streamed"},
				{Done: true, Usage: &mdl.TokenUsage{TotalTokens: 2}},
			},
			metadata: mdl.LLMCallMetadata{ActualModel: "stream-router"},
		},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}

	sess := &session.Session{
		ID:       "test-stream-model",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(observer.llmCalls) == 0 {
		t.Fatal("expected LLM observer event")
	}
	if observer.llmCalls[0].Model != "stream-router" {
		t.Fatalf("observer model = %q, want stream-router", observer.llmCalls[0].Model)
	}
}

func TestLoopStreamingAfterVisibleOutputDoesNotSyncFallback(t *testing.T) {
	streamLLM := &postEmissionErrorLLM{}
	l := &AgentLoop{
		LLM:   streamLLM,
		Tools: tool.NewRegistry(),
		IO:    kt.NewRecorderIO(),
		Config: LoopConfig{
			LLMRetry: RetryConfig{
				MaxRetries:   1,
				InitialDelay: time.Millisecond,
				MaxDelay:     5 * time.Millisecond,
			},
		},
	}

	sess := &session.Session{
		ID:       "test-post-emission-no-fallback",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	if _, err := l.Run(context.Background(), sess); err == nil {
		t.Fatal("expected streaming error")
	}
	if got := atomic.LoadInt32(&streamLLM.completeCalls); got != 0 {
		t.Fatalf("expected no sync fallback after visible output, got %d complete calls", got)
	}
}

func TestLoopStreamingTailJSONErrorWithToolCall_ShouldProceed(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.ToolSpec{Name: "noop"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}); err != nil {
		t.Fatalf("register noop: %v", err)
	}
	l := &AgentLoop{
		LLM:   &toolThenErrLLM{},
		Tools: reg,
		IO:    kt.NewRecorderIO(),
		Config: LoopConfig{
			LLMRetry: RetryConfig{
				MaxRetries:   1,
				InitialDelay: time.Millisecond,
				MaxDelay:     5 * time.Millisecond,
			},
		},
	}
	sess := &session.Session{
		ID:       "test-tail-json-error",
		Status:   session.StatusCreated,
		Messages: []mdl.Message{{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("run")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}
	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	foundToolResult := false
	for _, m := range sess.Messages {
		if len(m.ToolResults) > 0 {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatal("expected tool result appended despite stream tail json error")
	}
}
