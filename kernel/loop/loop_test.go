package loop

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"iter"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/hooks"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/kernel/testing"
	"github.com/mossagents/moss/kernel/tool"
)

func TestLoopTextOnly(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("Hello!")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 10},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Hi")}}},
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
		Chunks: [][]model.StreamChunk{{
			{ReasoningDelta: "First inspect the redirect. "},
			{ReasoningDelta: "Then query the weather endpoint."},
			{Delta: "Hangzhou is cloudy.", Done: true, Usage: &model.TokenUsage{TotalTokens: 10}},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("weather?")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	result, err := l.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got := model.ContentPartsToReasoningText(sess.Messages[len(sess.Messages)-1].ContentParts); got != "First inspect the redirect. Then query the weather endpoint." {
		t.Fatalf("session reasoning = %q", got)
	}
	foundReasoning := false
	for _, msg := range io.Sent {
		if msg.Type == kernio.OutputReasoning {
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
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("")},
					ToolCalls:    []model.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
				},
				ToolCalls:  []model.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 15},
			},
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("Done!")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 10},
			},
		},
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "greet", Description: "Greet someone"}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"Hello world"`), nil
	})); err != nil {
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Greet the world")}}},
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
		if msg.Type == kernio.OutputToolStart {
			hasToolStart = true
		}
		if msg.Type == kernio.OutputToolResult {
			hasToolResult = true
		}
	}
	if !hasToolStart || !hasToolResult {
		t.Fatal("expected tool_start and tool_result messages in IO")
	}
}

func TestLoopGreetingTurnDoesNotExposeTools(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("你好！有什么我可以帮你的？")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 8},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "list_files", Description: "List files"}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[]`), nil
	})); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
	}
	sess := &session.Session{
		ID: "test-greeting-no-tools",
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("继续分析项目结构")}},
			{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("我先读取 README")}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("你好")}},
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
	if got := model.ContentPartsToPlainText(mock.Calls[0].Messages[0].ContentParts); got != "你好" {
		t.Fatalf("prompt message=%q, want 你好", got)
	}
}

func TestLoopPlanningTurnBuildsToolRouteAndModelLane(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("Plan first.")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 12},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "read_file", Risk: tool.RiskLow, Capabilities: []string{"filesystem"}}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatalf("register read_file: %v", err)
	}
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "write_file", Risk: tool.RiskHigh, Capabilities: []string{"filesystem"}}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Please plan the refactor")}}},
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
	if !slices.Contains(mock.Calls[0].Config.Requirements.Capabilities, model.CapReasoning) || !slices.Contains(mock.Calls[0].Config.Requirements.Capabilities, model.CapFunctionCalling) {
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
		if event.Type == observe.ExecutionEventType("tool.route_planned") {
			foundToolRouteEvent = true
			if event.EventID == "" || event.RunID != "run-phase2" || event.TurnID == "" {
				t.Fatalf("unexpected route event envelope: %+v", event)
			}
			decisions, ok := event.Metadata["decisions"].([]map[string]any)
			if ok {
				if len(decisions) == 0 {
					t.Fatalf("expected route decisions in event data: %+v", event.Metadata)
				}
			} else if decisionsAny, ok := event.Metadata["decisions"].([]any); !ok || len(decisionsAny) == 0 {
				t.Fatalf("expected route decisions in event data: %+v", event.Metadata)
			}
		}
	}
	if !foundToolRouteEvent {
		t.Fatal("expected tool.route_planned event")
	}
}

func TestLoopHiddenToolCallReturnsNotAllowedError(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role:      model.RoleAssistant,
					ToolCalls: []model.ToolCall{{ID: "c1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x","content":"y"}`)}},
				},
				ToolCalls:  []model.ToolCall{{ID: "c1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x","content":"y"}`)}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 8},
			},
		},
	}
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "write_file", Risk: tool.RiskHigh, Capabilities: []string{"filesystem"}}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"should-not-run"`), nil
	})); err != nil {
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Plan the change")}}},
		Budget:   session.Budget{MaxSteps: 4},
	}
	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sess.Messages) < 3 {
		t.Fatalf("expected tool result message, got %+v", sess.Messages)
	}
	toolMsg := sess.Messages[2]
	if toolMsg.Role != model.RoleTool || len(toolMsg.ToolResults) != 1 {
		t.Fatalf("unexpected tool message: %+v", toolMsg)
	}
	if got := model.ContentPartsToPlainText(toolMsg.ToolResults[0].ContentParts); !strings.Contains(got, "not allowed in current turn") {
		t.Fatalf("unexpected tool error: %q", got)
	}
}

func TestLoopExecutionProgressEvents(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 9},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}
	if _, err := l.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	types := make([]observe.ExecutionEventType, 0, len(observer.execution))
	for _, event := range observer.execution {
		types = append(types, event.Type)
	}
	wantOrder := []observe.ExecutionEventType{
		observe.ExecutionRunStarted,
		observe.ExecutionIterationStarted,
		observe.ExecutionLLMStarted,
		observe.ExecutionLLMCompleted,
		observe.ExecutionIterationProgress,
		observe.ExecutionRunCompleted,
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
	var progress observe.ExecutionEvent
	found := false
	for _, event := range observer.execution {
		if event.Type == observe.ExecutionIterationProgress {
			progress = event
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected iteration.progress event")
	}
	if got := progress.Metadata["iteration"]; got != 1 {
		t.Fatalf("progress iteration = %v, want 1", got)
	}
	if got := progress.Metadata["stop_reason"]; got != "end_turn" {
		t.Fatalf("progress stop_reason = %v, want end_turn", got)
	}
	if progress.EventID == "" || progress.EventVersion != 1 || progress.Phase != "iteration" || progress.PayloadKind != "iteration" {
		t.Fatalf("unexpected progress envelope: %+v", progress)
	}
	var completed observe.ExecutionEvent
	found = false
	for _, event := range observer.execution {
		if event.Type == observe.ExecutionRunCompleted {
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
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role:      model.RoleAssistant,
					ToolCalls: []model.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
				},
				ToolCalls:  []model.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("Ok")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 5},
			},
		},
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		t.Fatal("should not be called")
		return nil, nil
	})); err != nil {
		t.Fatalf("register dangerous_tool: %v", err)
	}

	chain := hooks.NewRegistry()
	chain.OnToolLifecycle.AddHook("", func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev.Stage == hooks.ToolLifecycleBefore && ev.Tool != nil && ev.Tool.Name == "dangerous_tool" {
			e := errors.New(errors.ErrPolicyDenied, "tool call denied by policy")
			e.Meta = map[string]any{
				"reason_code": "tool.denied",
				"tool":        ev.Tool.Name,
			}
			return e
		}
		return nil
	}, 0)

	io := kt.NewRecorderIO()
	l := &AgentLoop{
		LLM:   mock,
		Tools: reg,
		Hooks: chain,
		IO:    io,
	}

	sess := &session.Session{
		ID:       "test-3",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Do something dangerous")}}},
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
	var toolResultMsg *kernio.OutputMessage
	for _, msg := range sess.Messages {
		for _, tr := range msg.ToolResults {
			if tr.IsError && strings.Contains(model.ContentPartsToPlainText(tr.ContentParts), "tool call denied by policy") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected denied tool result in session messages")
	}
	for i := range io.Sent {
		msg := &io.Sent[i]
		if msg.Type == kernio.OutputToolResult {
			toolResultMsg = msg
			break
		}
	}
	if toolResultMsg == nil {
		t.Fatal("expected tool_result message")
	}
	if got := toolResultMsg.Meta["error_code"]; got != string(errors.ErrPolicyDenied) {
		t.Fatalf("expected error_code %s, got %v", errors.ErrPolicyDenied, got)
	}
	if got := toolResultMsg.Meta["reason_code"]; got != "tool.denied" {
		t.Fatalf("expected reason_code tool.denied, got %v", got)
	}
}

func TestLoopBudgetExhausted(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("step 1")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 100},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("test")}}},
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
		Responses: []model.CompletionResponse{
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("large token response")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 11},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("test")}}},
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
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{
						{ID: "c1", Name: "slow_one", Arguments: json.RawMessage(`{}`)},
						{ID: "c2", Name: "slow_two", Arguments: json.RawMessage(`{}`)},
					},
				},
				ToolCalls: []model.ToolCall{
					{ID: "c1", Name: "slow_one", Arguments: json.RawMessage(`{}`)},
					{ID: "c2", Name: "slow_two", Arguments: json.RawMessage(`{}`)},
				},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 10},
			},
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 5},
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
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "slow_one", Effects: []tool.Effect{tool.EffectReadOnly}}, handler("one"))); err != nil {
		t.Fatalf("register slow_one: %v", err)
	}
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "slow_two", Effects: []tool.Effect{tool.EffectReadOnly}}, handler("two"))); err != nil {
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("run both tools")}}},
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

func TestAdmitToolCallBatches_SerializesConflictingWorkspaceWrites(t *testing.T) {
	reg := tool.NewRegistry()
	handler := func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	}
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:            "write_one",
		Risk:            tool.RiskHigh,
		Effects:         []tool.Effect{tool.EffectWritesWorkspace},
		SideEffectClass: tool.SideEffectWorkspace,
		ResourceScope:   []string{"workspace:docs/spec.md"},
	}, handler)); err != nil {
		t.Fatalf("register write_one: %v", err)
	}
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:            "write_two",
		Risk:            tool.RiskHigh,
		Effects:         []tool.Effect{tool.EffectWritesWorkspace},
		SideEffectClass: tool.SideEffectWorkspace,
		ResourceScope:   []string{"workspace:docs/spec.md"},
	}, handler)); err != nil {
		t.Fatalf("register write_two: %v", err)
	}

	l := &AgentLoop{
		Tools: reg,
		Config: LoopConfig{
			ParallelToolCall: true,
		},
	}

	batches := l.admitToolCallBatches([]model.ToolCall{
		{ID: "c1", Name: "write_one", Arguments: json.RawMessage(`{}`)},
		{ID: "c2", Name: "write_two", Arguments: json.RawMessage(`{}`)},
	})
	if len(batches) != 2 {
		t.Fatalf("batch len = %d, want 2", len(batches))
	}
	if len(batches[0]) != 1 || len(batches[1]) != 1 {
		t.Fatalf("unexpected batch sizes: %d, %d", len(batches[0]), len(batches[1]))
	}
	if batches[0][0].Name != "write_one" || batches[1][0].Name != "write_two" {
		t.Fatalf("unexpected batch order: %#v", batches)
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("wait")}}},
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
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "echo_json"}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in map[string]any
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, err
		}
		return json.Marshal(in)
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	l := &AgentLoop{Tools: reg, IO: kt.NewRecorderIO()}
	sess := &session.Session{ID: "sess-json-repair", Status: session.StatusCreated, Budget: session.Budget{MaxSteps: 5}}

	call := model.ToolCall{
		ID:        "c1",
		Name:      "echo_json",
		Arguments: json.RawMessage(`{"a":1`),
	}
	result := l.executeSingleToolCall(context.Background(), sess, call)
	if result.IsError {
		t.Fatalf("expected repaired args to succeed, got error: %s", model.ContentPartsToPlainText(result.ContentParts))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(model.ContentPartsToPlainText(result.ContentParts)), &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if out["a"] != float64(1) {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestExecuteSingleToolCall_PolicyDeniedAddsStructuredExecutionMetadata(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("tool should not execute")
		return nil, nil
	})); err != nil {
		t.Fatalf("register dangerous_tool: %v", err)
	}
	chain := hooks.NewRegistry()
	chain.OnToolLifecycle.AddHook("", func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev.Stage == hooks.ToolLifecycleBefore && ev.Tool != nil && ev.Tool.Name == "dangerous_tool" {
			e := errors.New(errors.ErrPolicyDenied, "tool call denied by policy")
			e.Meta = map[string]any{
				"reason_code": "tool.denied",
				"tool":        ev.Tool.Name,
			}
			return e
		}
		return nil
	}, 0)
	observer := &recordingObserver{}
	l := &AgentLoop{
		Tools:    reg,
		Hooks:    chain,
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{ID: "sess-policy-meta", Status: session.StatusCreated, Budget: session.Budget{MaxSteps: 5}}
	call := model.ToolCall{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}

	result := l.executeSingleToolCall(context.Background(), sess, call)
	if !result.IsError {
		t.Fatalf("expected denied tool result, got %+v", result)
	}

	var toolCompleted *observe.ExecutionEvent
	for i := range observer.execution {
		ev := &observer.execution[i]
		if ev.Type == observe.ExecutionToolCompleted {
			toolCompleted = ev
			break
		}
	}
	if toolCompleted == nil {
		t.Fatal("expected tool.completed event")
	}
	if got := toolCompleted.Metadata["error_code"]; got != string(errors.ErrPolicyDenied) {
		t.Fatalf("expected error_code %s, got %v", errors.ErrPolicyDenied, got)
	}
	if got := toolCompleted.Metadata["reason_code"]; got != "tool.denied" {
		t.Fatalf("expected reason_code tool.denied, got %v", got)
	}
}

func TestLoopLLMErrorEventIncludesErrorCode(t *testing.T) {
	observer := &recordingObserver{}
	loopErr := errors.New(errors.ErrLLMCall, "llm failed")
	l := &AgentLoop{
		LLM:      &errorLLM{err: loopErr},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{
		ID:       "test-llm-error-code",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 5},
	}
	if _, err := l.Run(context.Background(), sess); err == nil {
		t.Fatal("expected run to fail")
	}
	var llmCompleted *observe.ExecutionEvent
	for i := range observer.execution {
		ev := &observer.execution[i]
		if ev.Type == observe.ExecutionLLMCompleted && ev.Error != "" {
			llmCompleted = ev
			break
		}
	}
	if llmCompleted == nil {
		t.Fatal("expected llm.completed error event")
	}
	if got := llmCompleted.Metadata["error_code"]; got != string(errors.ErrLLMCall) {
		t.Fatalf("expected error_code %s, got %v", errors.ErrLLMCall, got)
	}
}

func TestLoopRunFailedEventIncludesErrorCode(t *testing.T) {
	observer := &recordingObserver{}
	loopErr := errors.New(errors.ErrLLMCall, "llm failed")
	l := &AgentLoop{
		LLM:      &errorLLM{err: loopErr},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	sess := &session.Session{
		ID:       "test-run-failed-error-code",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 5},
	}
	if _, err := l.Run(context.Background(), sess); err == nil {
		t.Fatal("expected run to fail")
	}
	var runFailed *observe.ExecutionEvent
	for i := range observer.execution {
		ev := &observer.execution[i]
		if ev.Type == observe.ExecutionRunFailed {
			runFailed = ev
			break
		}
	}
	if runFailed == nil {
		t.Fatal("expected run.failed event")
	}
	if got := runFailed.Metadata["error_code"]; got != string(errors.ErrLLMCall) {
		t.Fatalf("expected error_code %s, got %v", errors.ErrLLMCall, got)
	}
}

type blockingLLM struct {
	calls int32
}

func (b *blockingLLM) GenerateContent(ctx context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		atomic.AddInt32(&b.calls, 1)
		<-ctx.Done()
		yield(model.StreamChunk{}, ctx.Err())
	}
}

type flakyLLM struct {
	failures int32
	calls    int32
	resp     model.CompletionResponse
}

type errorLLM struct {
	err error
}

func (e *errorLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		yield(model.StreamChunk{}, e.err)
	}
}

func (f *flakyLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		call := atomic.AddInt32(&f.calls, 1)
		if call <= f.failures {
			yield(model.StreamChunk{}, context.DeadlineExceeded)
			return
		}
		resp := f.resp
		for chunk, err := range model.ResponseToSeq(&resp) {
			if !yield(chunk, err) {
				return
			}
		}
	}
}

type flakyStreamingLLM struct {
	failures int32
	calls    int32
	chunks   []model.StreamChunk
}

func (f *flakyStreamingLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		call := atomic.AddInt32(&f.calls, 1)
		if call <= f.failures {
			yield(model.StreamChunk{}, context.DeadlineExceeded)
			return
		}
		for _, chunk := range f.chunks {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

type metadataStreamingLLM struct {
	chunks   []model.StreamChunk
	metadata model.LLMCallMetadata
}

func (m *metadataStreamingLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		for i, chunk := range m.chunks {
			// Attach metadata to the last (Done) chunk.
			if i == len(m.chunks)-1 && chunk.Done {
				meta := m.metadata
				chunk.Metadata = &meta
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

type postEmissionErrorLLM struct{}

func (p *postEmissionErrorLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		if !yield(model.StreamChunk{Delta: "partial"}, nil) {
			return
		}
		yield(model.StreamChunk{}, context.DeadlineExceeded)
	}
}

type toolThenErrLLM struct{}

func (t *toolThenErrLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		tc := model.ToolCall{ID: "call_t", Name: "noop", Arguments: json.RawMessage(`{"x":1}`)}
		if !yield(model.StreamChunk{ToolCall: &tc}, nil) {
			return
		}
		yield(model.StreamChunk{}, io.ErrUnexpectedEOF)
	}
}

type recordingObserver struct {
	llmCalls  []observe.LLMCallEvent
	execution []observe.ExecutionEvent
	errors    []observe.ErrorEvent
}

func (o *recordingObserver) OnLLMCall(_ context.Context, e observe.LLMCallEvent) {
	o.llmCalls = append(o.llmCalls, e)
}

func (o *recordingObserver) OnToolCall(context.Context, observe.ToolCallEvent) {}

func (o *recordingObserver) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	o.execution = append(o.execution, e)
}

func (o *recordingObserver) OnApproval(context.Context, kernio.ApprovalEvent)     {}
func (o *recordingObserver) OnSessionEvent(context.Context, observe.SessionEvent) {}
func (o *recordingObserver) OnError(_ context.Context, e observe.ErrorEvent) {
	o.errors = append(o.errors, e)
}

func (o *recordingObserver) lastCompletedModel() string {
	for i := len(o.execution) - 1; i >= 0; i-- {
		if o.execution[i].Type == observe.ExecutionLLMCompleted {
			return o.execution[i].Model
		}
	}
	return ""
}

func TestLoopLLMRetry_Sync(t *testing.T) {
	l := &AgentLoop{
		LLM: &flakyLLM{
			failures: 2,
			resp: model.CompletionResponse{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("retried")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 7},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
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
			Responses: []model.CompletionResponse{{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("ok")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 3},
			}},
		},
		Tools:    tool.NewRegistry(),
		Hooks:    hooks.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}
	l.Hooks.OnSessionLifecycle.AddHook("", func(_ context.Context, event *session.LifecycleEvent) error {
		if event == nil {
			return nil
		}
		stages = append(stages, event.Stage)
		if event.Stage == session.LifecycleStarted {
			panic("boom")
		}
		return nil
	}, 0)

	sess := &session.Session{
		ID:       "test-lifecycle-panic",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
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
	chain := hooks.NewRegistry()
	var events []hooks.ToolEvent
	chain.OnToolLifecycle.AddHook("", func(_ context.Context, event *hooks.ToolEvent) error {
		if event != nil {
			events = append(events, *event)
		}
		return nil
	}, -10)
	chain.OnToolLifecycle.AddHook("", func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev.Stage == hooks.ToolLifecycleBefore && ev.Tool != nil && ev.Tool.Name == "dangerous_tool" {
			e := errors.New(errors.ErrPolicyDenied, "tool call denied by policy")
			e.Meta = map[string]any{
				"reason_code": "tool.denied",
				"tool":        ev.Tool.Name,
			}
			return e
		}
		return nil
	}, 0)
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "dangerous_tool", Risk: tool.RiskHigh}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("tool should not be executed")
		return nil, nil
	})); err != nil {
		t.Fatalf("register dangerous_tool: %v", err)
	}
	l := &AgentLoop{
		LLM: &kt.MockLLM{
			Responses: []model.CompletionResponse{
				{
					Message: model.Message{
						Role:      model.RoleAssistant,
						ToolCalls: []model.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
					},
					ToolCalls:  []model.ToolCall{{ID: "c1", Name: "dangerous_tool", Arguments: json.RawMessage(`{}`)}},
					StopReason: "tool_use",
					Usage:      model.TokenUsage{TotalTokens: 5},
				},
				{
					Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
					StopReason: "end_turn",
					Usage:      model.TokenUsage{TotalTokens: 3},
				},
			},
		},
		Tools:    reg,
		Hooks:    chain,
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}

	sess := &session.Session{
		ID:       "test-tool-lifecycle-denied",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("do the dangerous thing")}}},
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
	if events[0].Stage != hooks.ToolLifecycleBefore {
		t.Fatalf("first tool lifecycle stage = %q, want before", events[0].Stage)
	}
	if events[1].Stage != hooks.ToolLifecycleAfter {
		t.Fatalf("second tool lifecycle stage = %q, want after", events[1].Stage)
	}
	if events[1].Error == nil {
		t.Fatal("expected denied tool call to surface error in after hook")
	}
	if events[1].ToolResult == nil || !events[1].ToolResult.IsError {
		t.Fatal("expected denied tool call to surface error result in after hook")
	}
}

func TestLoopLLMRetry_StreamingBeforeEmission(t *testing.T) {
	streamLLM := &flakyStreamingLLM{
		failures: 1,
		chunks: []model.StreamChunk{
			{Delta: "ok"},
			{Done: true, Usage: &model.TokenUsage{TotalTokens: 3}},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
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
		t.Fatalf("expected 2 stream attempts (1 fail + 1 success), got %d", got)
	}
}

func TestLoopLLMCallUsesActualModelMetadata(t *testing.T) {
	observer := &recordingObserver{}
	l := &AgentLoop{
		LLM: &kt.MockLLM{
			Responses: []model.CompletionResponse{{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("ok")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 3},
				Metadata:   &model.LLMCallMetadata{ActualModel: "router-picked"},
			}},
		},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}

	sess := &session.Session{
		ID:       "test-actual-model",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
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
			chunks: []model.StreamChunk{
				{Delta: "streamed"},
				{Done: true, Usage: &model.TokenUsage{TotalTokens: 2}},
			},
			metadata: model.LLMCallMetadata{ActualModel: "stream-router"},
		},
		Tools:    tool.NewRegistry(),
		IO:       kt.NewRecorderIO(),
		Observer: observer,
	}

	sess := &session.Session{
		ID:       "test-stream-model",
		Status:   session.StatusCreated,
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}},
		Budget:   session.Budget{MaxSteps: 10},
	}

	if _, err := l.Run(context.Background(), sess); err == nil {
		t.Fatal("expected streaming error")
	}
}

func TestLoopStreamingTailJSONErrorWithToolCall_ShouldProceed(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{Name: "noop"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
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
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("run")}}},
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
