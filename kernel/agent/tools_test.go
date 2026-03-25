package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mossagi/moss/kernel/loop"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
)

// mockDelegator 模拟 Kernel 的委派能力。
type mockDelegator struct {
	registry tool.Registry
	runFn    func(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error)
}

func (m *mockDelegator) NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	mgr := session.NewManager()
	return mgr.Create(ctx, cfg)
}

func (m *mockDelegator) RunWithTools(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error) {
	if m.runFn != nil {
		return m.runFn(ctx, sess, tools)
	}
	return &loop.SessionResult{
		SessionID: sess.ID,
		Success:   true,
		Output:    "mock result for: " + sess.Config.Goal,
	}, nil
}

func (m *mockDelegator) ToolRegistry() tool.Registry {
	return m.registry
}

func TestDelegateAgent(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{
		Name:         "researcher",
		SystemPrompt: "You research.",
		Tools:        []string{"search_text"},
		MaxSteps:     10,
	})

	tracker := NewTaskTracker()
	parentReg := tool.NewRegistry()
	parentReg.Register(tool.ToolSpec{Name: "search_text"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"search result"`), nil
	})

	delegator := &mockDelegator{registry: parentReg}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	// Verify delegate_agent is registered
	_, handler, ok := reg.Get("delegate_agent")
	if !ok {
		t.Fatal("delegate_agent not registered")
	}

	// Call delegate_agent
	input, _ := json.Marshal(delegateInput{Agent: "researcher", Task: "find Go news"})
	result, err := handler(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "completed" {
		t.Errorf("status = %q", resp["status"])
	}
	if resp["agent"] != "researcher" {
		t.Errorf("agent = %q", resp["agent"])
	}
}

func TestDelegateAgent_NotFound(t *testing.T) {
	agents := NewRegistry()
	tracker := NewTaskTracker()
	delegator := &mockDelegator{registry: tool.NewRegistry()}

	reg := tool.NewRegistry()
	RegisterTools(reg, agents, tracker, delegator)

	_, handler, _ := reg.Get("delegate_agent")
	input, _ := json.Marshal(delegateInput{Agent: "nonexistent", Task: "x"})
	_, err := handler(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestDelegateAgent_DepthLimit(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{
		Name:         "deep",
		SystemPrompt: "Deep.",
		Tools:        []string{},
	})

	tracker := NewTaskTracker()
	delegator := &mockDelegator{registry: tool.NewRegistry()}

	reg := tool.NewRegistry()
	RegisterTools(reg, agents, tracker, delegator)

	_, handler, _ := reg.Get("delegate_agent")
	input, _ := json.Marshal(delegateInput{Agent: "deep", Task: "x"})

	// Set depth to max
	ctx := WithDepth(context.Background(), MaxDelegationDepth)
	_, err := handler(ctx, input)
	if err == nil {
		t.Fatal("expected depth limit error")
	}
}

func TestSpawnAndQueryAgent(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{},
	})

	tracker := NewTaskTracker()

	done := make(chan struct{})
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(_ context.Context, sess *session.Session, _ tool.Registry) (*loop.SessionResult, error) {
			defer close(done)
			return &loop.SessionResult{
				SessionID:  sess.ID,
				Success:    true,
				Output:     "async done",
				TokensUsed: port.TokenUsage{TotalTokens: 42},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	RegisterTools(reg, agents, tracker, delegator)

	// Spawn
	_, spawnHandler, _ := reg.Get("spawn_agent")
	input, _ := json.Marshal(spawnInput{Agent: "worker", Task: "background work"})
	result, err := spawnHandler(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	var spawnResp map[string]string
	json.Unmarshal(result, &spawnResp)
	taskID := spawnResp["task_id"]
	if taskID == "" {
		t.Fatal("no task_id returned")
	}
	if spawnResp["status"] != "running" {
		t.Errorf("status = %q", spawnResp["status"])
	}

	// Wait for completion
	<-done

	// Query
	_, queryHandler, _ := reg.Get("query_agent")
	qInput, _ := json.Marshal(queryInput{TaskID: taskID})
	qResult, err := queryHandler(context.Background(), qInput)
	if err != nil {
		t.Fatal(err)
	}

	var qResp map[string]string
	json.Unmarshal(qResult, &qResp)
	if qResp["status"] != "completed" {
		t.Errorf("query status = %q, want completed", qResp["status"])
	}
	if qResp["result"] != "async done" {
		t.Errorf("result = %q", qResp["result"])
	}
}

func TestScopedToolIsolation(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{
		Name:         "limited",
		SystemPrompt: "Limited.",
		Tools:        []string{"read_file"},
	})

	tracker := NewTaskTracker()

	parentReg := tool.NewRegistry()
	parentReg.Register(tool.ToolSpec{Name: "read_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})
	parentReg.Register(tool.ToolSpec{Name: "write_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})

	var capturedTools tool.Registry
	delegator := &mockDelegator{
		registry: parentReg,
		runFn: func(_ context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error) {
			capturedTools = tools
			return &loop.SessionResult{Success: true, Output: "ok"}, nil
		},
	}

	reg := tool.NewRegistry()
	RegisterTools(reg, agents, tracker, delegator)

	_, handler, _ := reg.Get("delegate_agent")
	input, _ := json.Marshal(delegateInput{Agent: "limited", Task: "test isolation"})
	handler(context.Background(), input)

	// Verify scoped tools
	if capturedTools == nil {
		t.Fatal("tools not captured")
	}
	list := capturedTools.List()
	if len(list) != 1 {
		t.Fatalf("scoped tools = %d, want 1", len(list))
	}
	if list[0].Name != "read_file" {
		t.Errorf("tool name = %q, want read_file", list[0].Name)
	}

	// write_file should not be accessible
	_, _, ok := capturedTools.Get("write_file")
	if ok {
		t.Error("write_file should not be accessible in scoped registry")
	}
}

func TestSpawnAgent_CancelledContext(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{},
	})

	tracker := NewTaskTracker()
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, _ *session.Session, _ tool.Registry) (*loop.SessionResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	_, spawnHandler, ok := reg.Get("spawn_agent")
	if !ok {
		t.Fatal("spawn_agent not registered")
	}

	ctx, cancel := context.WithCancel(context.Background())
	input, _ := json.Marshal(spawnInput{Agent: "worker", Task: "background work"})
	result, err := spawnHandler(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	var spawnResp map[string]string
	if err := json.Unmarshal(result, &spawnResp); err != nil {
		t.Fatal(err)
	}
	taskID := spawnResp["task_id"]
	if taskID == "" {
		t.Fatal("expected task_id")
	}

	cancel()

	_, queryHandler, ok := reg.Get("query_agent")
	if !ok {
		t.Fatal("query_agent not registered")
	}

	deadline := time.After(1 * time.Second)
	for {
		task, found := tracker.Get(taskID)
		if !found {
			t.Fatal("task not found in tracker")
		}
		if task.Status == TaskCancelled {
			if task.Error == "" {
				t.Fatal("expected cancellation error message")
			}

			qInput, _ := json.Marshal(queryInput{TaskID: taskID})
			qResult, qErr := queryHandler(context.Background(), qInput)
			if qErr != nil {
				t.Fatalf("query_agent failed: %v", qErr)
			}

			var qResp map[string]string
			if err := json.Unmarshal(qResult, &qResp); err != nil {
				t.Fatalf("failed to decode query response: %v", err)
			}
			if qResp["status"] != string(TaskCancelled) {
				t.Fatalf("query status = %q, want %q", qResp["status"], TaskCancelled)
			}
			if qResp["error"] == "" {
				t.Fatal("expected query error for cancelled task")
			}
			return
		}

		select {
		case <-deadline:
			t.Fatalf("expected task status cancelled, got %s", task.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
