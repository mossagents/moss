package agent

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/harness/sandbox"
)

// mockDelegator 模拟 Kernel 的委派能力。
type mockDelegator struct {
	registry tool.Registry
	runFn    func(ctx context.Context, sess *session.Session, tools tool.Registry) (*session.LifecycleResult, error)
}

func (m *mockDelegator) NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	mgr := session.NewManager()
	return mgr.Create(ctx, cfg)
}

func (m *mockDelegator) BuildLLMAgent(name string) *kernel.LLMAgent {
	return kernel.NewLLMAgent(kernel.LLMAgentConfig{Name: name, Tools: m.registry})
}

func (m *mockDelegator) RunAgent(ctx context.Context, req kernel.RunAgentRequest) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		var (
			result *session.LifecycleResult
			err    error
		)
		if m.runFn != nil {
			var scopedTools tool.Registry
			if llmAgent, ok := req.Agent.(*kernel.LLMAgent); ok {
				scopedTools = llmAgent.Tools()
			}
			result, err = m.runFn(ctx, req.Session, scopedTools)
		} else {
			result = &session.LifecycleResult{
				Success: true,
				Output:  "mock result for: " + req.Session.Config.Goal,
			}
		}
		if result != nil && req.OnResult != nil {
			req.OnResult(result)
		}
		if err != nil {
			yield(nil, err)
		}
	}
}

func (m *mockDelegator) ToolRegistry() tool.Registry {
	return m.registry
}

func TestDelegateAgent(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "researcher",
		SystemPrompt: "You research.",
		Tools:        []string{"grep"},
		MaxSteps:     10,
	}); err != nil {
		t.Fatal(err)
	}

	tracker := NewTaskTracker()
	parentReg := tool.NewRegistry()
	if err := parentReg.Register(tool.NewRawTool(tool.ToolSpec{Name: "grep"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"search result"`), nil
	})); err != nil {
		t.Fatal(err)
	}

	delegator := &mockDelegator{registry: parentReg}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	// Verify delegate_agent is registered
	tl, ok := reg.Get("delegate_agent")
	if !ok {
		t.Fatal("delegate_agent not registered")
	}

	// Call delegate_agent
	input, _ := json.Marshal(delegateInput{Agent: "researcher", Task: "find Go news"})
	result, err := tl.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	var resp map[string]any
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "completed" {
		t.Errorf("status = %q", resp["status"])
	}
	if resp["agent"] != "researcher" {
		t.Errorf("agent = %q", resp["agent"])
	}
	if _, ok := resp["supervisor_artifact"]; !ok {
		t.Fatalf("expected supervisor_artifact in response: %+v", resp)
	}
}

func TestDelegateAgent_NotFound(t *testing.T) {
	agents := NewRegistry()
	tracker := NewTaskTracker()
	delegator := &mockDelegator{registry: tool.NewRegistry()}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	handlerTool, _ := reg.Get("delegate_agent")
	handler := handlerTool.Execute
	input, _ := json.Marshal(delegateInput{Agent: "nonexistent", Task: "x"})
	_, err := handler(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestDelegateAgent_DepthLimit(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "deep",
		SystemPrompt: "Deep.",
		Tools:        []string{},
	}); err != nil {
		t.Fatal(err)
	}

	tracker := NewTaskTracker()
	delegator := &mockDelegator{registry: tool.NewRegistry()}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	handlerTool, _ := reg.Get("delegate_agent")
	handler := handlerTool.Execute
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
	if err := agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{},
	}); err != nil {
		t.Fatal(err)
	}

	tracker := NewTaskTracker()

	done := make(chan struct{})
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(_ context.Context, sess *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			defer close(done)
			return &session.LifecycleResult{
				Success:    true,
				Output:     "async done",
				TokensUsed: model.TokenUsage{TotalTokens: 42},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	// Spawn
	spawnHandlerTool, _ := reg.Get("spawn_agent")
	spawnHandler := spawnHandlerTool.Execute
	input, _ := json.Marshal(spawnInput{Agent: "worker", Task: "background work"})
	result, err := spawnHandler(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	var spawnResp map[string]string
	if err := json.Unmarshal(result, &spawnResp); err != nil {
		t.Fatal(err)
	}
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
	queryHandlerTool, _ := reg.Get("query_agent")
	queryHandler := queryHandlerTool.Execute
	qInput, _ := json.Marshal(queryInput{TaskID: taskID})
	qResult, err := queryHandler(context.Background(), qInput)
	if err != nil {
		t.Fatal(err)
	}

	var qResp map[string]any
	if err := json.Unmarshal(qResult, &qResp); err != nil {
		t.Fatal(err)
	}
	if qResp["status"] != "completed" {
		t.Errorf("query status = %q, want completed", qResp["status"])
	}
	if qResp["result"] != "async done" {
		t.Errorf("result = %q", qResp["result"])
	}
}

func TestTaskToolSyncBackgroundQuery(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{},
	}); err != nil {
		t.Fatal(err)
	}

	tracker := NewTaskTracker()
	done := make(chan struct{})
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(_ context.Context, sess *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			if sess.Config.Goal == "background work" {
				defer close(done)
			}
			return &session.LifecycleResult{
				Success:    true,
				Output:     "ok: " + sess.Config.Goal,
				TokensUsed: model.TokenUsage{TotalTokens: 10},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	taskHandlerTool, ok := reg.Get("task")
	if !ok {
		t.Fatal("task tool not registered")
	}
	taskHandler := taskHandlerTool.Execute

	// sync mode
	syncInput, _ := json.Marshal(taskInput{Mode: "sync", Agent: "worker", Task: "do sync"})
	syncResult, err := taskHandler(context.Background(), syncInput)
	if err != nil {
		t.Fatalf("task sync: %v", err)
	}
	var syncResp map[string]any
	if err := json.Unmarshal(syncResult, &syncResp); err != nil {
		t.Fatal(err)
	}
	if syncResp["status"] != "completed" || syncResp["mode"] != "sync" {
		t.Fatalf("unexpected sync response: %+v", syncResp)
	}

	// background mode
	bgInput, _ := json.Marshal(taskInput{Mode: "background", Agent: "worker", Task: "background work"})
	bgResult, err := taskHandler(context.Background(), bgInput)
	if err != nil {
		t.Fatalf("task background: %v", err)
	}
	var bgResp map[string]string
	if err := json.Unmarshal(bgResult, &bgResp); err != nil {
		t.Fatal(err)
	}
	taskID := bgResp["task_id"]
	if taskID == "" {
		t.Fatalf("expected task_id from background response: %+v", bgResp)
	}
	<-done

	// query mode
	queryInput, _ := json.Marshal(taskInput{Mode: "query", TaskID: taskID})
	queryResult, err := taskHandler(context.Background(), queryInput)
	if err != nil {
		t.Fatalf("task query: %v", err)
	}
	var queryResp map[string]any
	if err := json.Unmarshal(queryResult, &queryResp); err != nil {
		t.Fatal(err)
	}
	if queryResp["mode"] != "query" || queryResp["status"] != "completed" {
		t.Fatalf("unexpected query response: %+v", queryResp)
	}
	if _, ok := queryResp["supervisor_artifact"]; !ok {
		t.Fatalf("expected supervisor_artifact in query response: %+v", queryResp)
	}
}

func TestTaskToolModeValidation(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	delegator := &mockDelegator{registry: tool.NewRegistry()}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	taskHandlerTool, ok := reg.Get("task")
	if !ok {
		t.Fatal("task tool not registered")
	}
	taskHandler := taskHandlerTool.Execute

	cases := []taskInput{
		{Mode: "sync", Task: "missing agent"},
		{Mode: "background", Agent: "worker"},
		{Mode: "query"},
		{Mode: "cancel"},
		{Mode: "update", TaskID: "x"},
		{Mode: "update", Task: "x"},
		{Mode: "list", Status: "not-a-status"},
		{Mode: "invalid"},
	}
	for _, c := range cases {
		input, _ := json.Marshal(c)
		if _, err := taskHandler(context.Background(), input); err == nil {
			t.Fatalf("expected validation error for input: %+v", c)
		}
	}
}

func TestTaskToolBackgroundPersistsContractAndScopesTools(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{"read_file", "write_file"},
	}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	parentReg := tool.NewRegistry()
	if err := parentReg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:         "read_file",
		Risk:         tool.RiskLow,
		Capabilities: []string{"filesystem"},
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"read ok"`), nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := parentReg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:          "write_file",
		Risk:          tool.RiskHigh,
		Capabilities:  []string{"filesystem"},
		Effects:       []tool.Effect{tool.EffectWritesWorkspace},
		ApprovalClass: tool.ApprovalClassExplicitUser,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ok"}`), nil
	})); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	delegator := &mockDelegator{
		registry: parentReg,
		runFn: func(_ context.Context, sess *session.Session, tools tool.Registry) (*session.LifecycleResult, error) {
			defer close(done)
			if sess.Config.MaxSteps != 3 || sess.Config.MaxTokens != 111 {
				t.Fatalf("unexpected budget in session config: %+v", sess.Config)
			}
			if !strings.Contains(sess.Config.SystemPrompt, "<child_task_contract>") {
				t.Fatalf("expected contract prompt in system prompt, got %q", sess.Config.SystemPrompt)
			}
			if len(tools.List()) != 1 || tools.List()[0].Name() != "read_file" {
				t.Fatalf("unexpected scoped tools: %+v", tools.List())
			}
			writeTool, ok := tools.Get("write_file")
			if !ok {
				t.Fatal("expected write_file to be addressable for contract violation")
			}
			if _, err := writeTool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "notes/out.txt"})); err == nil || !strings.Contains(err.Error(), "violates child task contract") {
				t.Fatalf("expected contract violation, got %v", err)
			}
			return &session.LifecycleResult{
				Success:    true,
				Output:     "contract ok",
				TokensUsed: model.TokenUsage{TotalTokens: 9},
			}, nil
		},
	}
	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	taskHandlerTool, ok := reg.Get("task")
	if !ok {
		t.Fatal("task tool not registered")
	}
	taskHandler := taskHandlerTool.Execute
	raw, err := taskHandler(context.Background(), mustJSON(t, taskInput{
		Mode:  "background",
		Agent: "worker",
		Task:  "bounded execution",
		Contract: taskrt.TaskContract{
			Budget: taskrt.TaskBudget{MaxSteps: 3, MaxTokens: 111},
			AllowedEffects: []tool.Effect{
				tool.EffectReadOnly,
			},
			ApprovalCeiling: tool.ApprovalClassPolicyGuarded,
		},
	}))
	if err != nil {
		t.Fatalf("task background: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	taskID, _ := resp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("missing task_id: %+v", resp)
	}
	<-done
	deadline := time.After(2 * time.Second)
	for {
		task, ok := tracker.Get(taskID)
		if ok && task.Status == TaskCompleted {
			if task.Contract.Budget.MaxSteps != 3 || task.Contract.ApprovalCeiling != tool.ApprovalClassPolicyGuarded {
				t.Fatalf("unexpected persisted contract: %+v", task.Contract)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("task %q did not complete with persisted contract", taskID)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestDelegateAgentContractWritableScope(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{"write_file"},
	}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	parentReg := tool.NewRegistry()
	if err := parentReg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:          "write_file",
		Risk:          tool.RiskHigh,
		Capabilities:  []string{"filesystem"},
		Effects:       []tool.Effect{tool.EffectWritesWorkspace},
		ApprovalClass: tool.ApprovalClassExplicitUser,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ok"}`), nil
	})); err != nil {
		t.Fatal(err)
	}
	delegator := &mockDelegator{
		registry: parentReg,
		runFn: func(_ context.Context, _ *session.Session, tools tool.Registry) (*session.LifecycleResult, error) {
			writeHandlerTool, ok := tools.Get("write_file")
			if !ok {
				t.Fatal("write_file not available")
			}
			writeHandler := writeHandlerTool.Execute
			if _, err := writeHandler(context.Background(), mustJSON(t, map[string]any{"path": "notes/out.txt"})); err == nil || !strings.Contains(err.Error(), "outside writable scopes") {
				t.Fatalf("expected writable scope violation, got %v", err)
			}
			if _, err := writeHandler(context.Background(), mustJSON(t, map[string]any{"path": "docs/ok.txt"})); err != nil {
				t.Fatalf("expected in-scope write to succeed, got %v", err)
			}
			return &session.LifecycleResult{Success: true, Output: "scoped"}, nil
		},
	}
	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("delegate_agent")
	if !ok {
		t.Fatal("delegate_agent not registered")
	}
	_, err := tl.Execute(context.Background(), mustJSON(t, delegateInput{
		Agent: "worker",
		Task:  "write docs",
		Contract: taskrt.TaskContract{
			AllowedEffects:  []tool.Effect{tool.EffectWritesWorkspace},
			ApprovalCeiling: tool.ApprovalClassExplicitUser,
			WritableScopes:  []string{"workspace:docs/**"},
		},
	}))
	if err != nil {
		t.Fatalf("delegate_agent: %v", err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return raw
}

func TestUpdateTaskRestartsSameID(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()

	firstStarted := make(chan struct{}, 1)
	var calls int32
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, sess *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				firstStarted <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &session.LifecycleResult{
				Success:    true,
				Output:     "updated: " + sess.Config.Goal,
				TokensUsed: model.TokenUsage{TotalTokens: 5},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	spawnHandlerTool, ok := reg.Get("spawn_agent")
	if !ok {
		t.Fatal("spawn_agent not registered")
	}
	spawnHandler := spawnHandlerTool.Execute
	updateHandlerTool, ok := reg.Get("update_task")
	if !ok {
		t.Fatal("update_task not registered")
	}
	updateHandler := updateHandlerTool.Execute

	spawnInputRaw, _ := json.Marshal(spawnInput{
		Agent: "worker",
		Task:  "initial task",
	})
	spawnRaw, err := spawnHandler(context.Background(), spawnInputRaw)
	if err != nil {
		t.Fatalf("spawn_agent: %v", err)
	}
	var spawnResp map[string]string
	if err := json.Unmarshal(spawnRaw, &spawnResp); err != nil {
		t.Fatal(err)
	}
	taskID := spawnResp["task_id"]
	if taskID == "" {
		t.Fatal("missing task_id")
	}
	<-firstStarted

	updateInputRaw, _ := json.Marshal(map[string]string{
		"task_id": taskID,
		"task":    "please include extra checks",
	})
	updateRaw, err := updateHandler(context.Background(), updateInputRaw)
	if err != nil {
		t.Fatalf("update_task: %v", err)
	}
	var updateResp map[string]any
	if err := json.Unmarshal(updateRaw, &updateResp); err != nil {
		t.Fatal(err)
	}
	if updateResp["task_id"] != taskID {
		t.Fatalf("expected same task_id, got %+v", updateResp)
	}

	deadline := time.After(2 * time.Second)
	for {
		task, ok := tracker.Get(taskID)
		if !ok {
			t.Fatalf("task %q not found", taskID)
		}
		if task.Status == TaskCompleted {
			if task.Revision < 2 {
				t.Fatalf("expected revision >=2 after update, got %d", task.Revision)
			}
			if !strings.Contains(task.Goal, "Follow-up update") {
				t.Fatalf("expected follow-up marker in goal, got %q", task.Goal)
			}
			if !strings.Contains(task.Goal, "please include extra checks") {
				t.Fatalf("expected updated goal content, got %q", task.Goal)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("task not completed after update, current status=%s", task.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestListAndCancelTaskTools(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()

	started := make(chan struct{}, 1)
	released := make(chan struct{}, 1)
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, _ *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-released:
				return &session.LifecycleResult{Success: true, Output: "done"}, nil
			}
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	spawnHandlerTool, ok := reg.Get("spawn_agent")
	if !ok {
		t.Fatal("spawn_agent not registered")
	}
	spawnHandler := spawnHandlerTool.Execute
	listHandlerTool, ok := reg.Get("list_tasks")
	if !ok {
		t.Fatal("list_tasks not registered")
	}
	listHandler := listHandlerTool.Execute
	cancelHandlerTool, ok := reg.Get("cancel_task")
	if !ok {
		t.Fatal("cancel_task not registered")
	}
	cancelHandler := cancelHandlerTool.Execute

	spawnInput, _ := json.Marshal(spawnInput{Agent: "worker", Task: "blocking work"})
	raw, err := spawnHandler(context.Background(), spawnInput)
	if err != nil {
		t.Fatalf("spawn_agent: %v", err)
	}
	var spawnResp map[string]string
	if err := json.Unmarshal(raw, &spawnResp); err != nil {
		t.Fatal(err)
	}
	taskID := spawnResp["task_id"]
	if taskID == "" {
		t.Fatal("expected task_id")
	}
	<-started

	listInput, _ := json.Marshal(map[string]any{"status": "running", "limit": 10})
	listRaw, err := listHandler(context.Background(), listInput)
	if err != nil {
		t.Fatalf("list_tasks: %v", err)
	}
	var listResp struct {
		Tasks []Task `json:"tasks"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(listRaw, &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Count == 0 {
		t.Fatalf("expected running task in list response: %s", string(listRaw))
	}

	cancelInput, _ := json.Marshal(map[string]any{"task_id": taskID, "reason": "stop now"})
	cancelRaw, err := cancelHandler(context.Background(), cancelInput)
	if err != nil {
		t.Fatalf("cancel_task: %v", err)
	}
	var cancelled Task
	if err := json.Unmarshal(cancelRaw, &cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != TaskCancelled {
		t.Fatalf("expected cancelled status, got %s", cancelled.Status)
	}
}

func TestScopedToolIsolation(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "limited",
		SystemPrompt: "Limited.",
		Tools:        []string{"read_file"},
	}); err != nil {
		t.Fatal(err)
	}

	tracker := NewTaskTracker()

	parentReg := tool.NewRegistry()
	if err := parentReg.Register(tool.NewRawTool(tool.ToolSpec{Name: "read_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := parentReg.Register(tool.NewRawTool(tool.ToolSpec{Name: "write_file"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatal(err)
	}

	var capturedTools tool.Registry
	delegator := &mockDelegator{
		registry: parentReg,
		runFn: func(_ context.Context, sess *session.Session, tools tool.Registry) (*session.LifecycleResult, error) {
			capturedTools = tools
			return &session.LifecycleResult{Success: true, Output: "ok"}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	tl, _ := reg.Get("delegate_agent")
	input, _ := json.Marshal(delegateInput{Agent: "limited", Task: "test isolation"})
	if _, err := tl.Execute(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	// Verify scoped tools
	if capturedTools == nil {
		t.Fatal("tools not captured")
	}
	list := capturedTools.List()
	if len(list) != 1 {
		t.Fatalf("scoped tools = %d, want 1", len(list))
	}
	if list[0].Name() != "read_file" {
		t.Errorf("tool name = %q, want read_file", list[0].Name())
	}

	// write_file should not be accessible
	_, ok := capturedTools.Get("write_file")
	if ok {
		t.Error("write_file should not be accessible in scoped registry")
	}
}

func TestSpawnAgent_CancelledContext(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{
		Name:         "worker",
		SystemPrompt: "Work.",
		Tools:        []string{},
	}); err != nil {
		t.Fatal(err)
	}

	tracker := NewTaskTracker()
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, _ *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}

	spawnHandlerTool, ok := reg.Get("spawn_agent")
	if !ok {
		t.Fatal("spawn_agent not registered")
	}
	spawnHandler := spawnHandlerTool.Execute

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

	queryHandlerTool, ok := reg.Get("query_agent")
	if !ok {
		t.Fatal("query_agent not registered")
	}
	queryHandler := queryHandlerTool.Execute

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

			var qResp map[string]any
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

func TestRegisterToolsWithDeps_AddsCollaborationTools(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerWithRuntime(taskrt.NewMemoryTaskRuntime())
	delegator := &mockDelegator{registry: tool.NewRegistry()}
	reg := tool.NewRegistry()
	isolation, err := sandbox.NewLocalWorkspaceIsolation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := RuntimeDeps{
		TaskRuntime: taskrt.NewMemoryTaskRuntime(),
		Mailbox:     taskrt.NewMemoryMailbox(),
		Isolation:   isolation,
	}
	if err := RegisterToolsWithDeps(reg, agents, tracker, delegator, deps); err != nil {
		t.Fatalf("RegisterToolsWithDeps: %v", err)
	}
	for _, name := range []string{"plan_task", "claim_task", "send_mail", "read_mailbox", "acquire_workspace", "release_workspace"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("expected tool %q", name)
		}
	}
}

func TestRegisterTools_ExecutionMetadata(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	tracker := NewTaskTracker()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	cases := []struct {
		name       string
		effect     tool.Effect
		sideEffect tool.SideEffectClass
		approval   tool.ApprovalClass
	}{
		{"spawn_agent", tool.EffectGraphMutation, tool.SideEffectTaskGraph, tool.ApprovalClassPolicyGuarded},
		{"query_agent", tool.EffectReadOnly, tool.SideEffectNone, tool.ApprovalClassNone},
		{"task", tool.EffectGraphMutation, tool.SideEffectTaskGraph, tool.ApprovalClassPolicyGuarded},
	}
	for _, tc := range cases {
		specTool, ok := reg.Get(tc.name)
		if !ok {
			t.Fatalf("tool %q not found", tc.name)
		}
		spec := specTool.Spec()
		if effects := spec.EffectiveEffects(); len(effects) == 0 || effects[0] != tc.effect {
			t.Fatalf("%s effects = %v", tc.name, effects)
		}
		if spec.SideEffectClass != tc.sideEffect {
			t.Fatalf("%s side_effect_class = %q", tc.name, spec.SideEffectClass)
		}
		if spec.ApprovalClass != tc.approval {
			t.Fatalf("%s approval_class = %q", tc.name, spec.ApprovalClass)
		}
	}
}

func TestPlanClaimMailAndWorkspaceFlow(t *testing.T) {
	rt := taskrt.NewMemoryTaskRuntime()
	mb := taskrt.NewMemoryMailbox()
	iso, err := sandbox.NewLocalWorkspaceIsolation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerWithRuntime(rt)
	reg := tool.NewRegistry()
	if err := RegisterToolsWithDeps(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}, RuntimeDeps{
		TaskRuntime: rt,
		Mailbox:     mb,
		Isolation:   iso,
	}); err != nil {
		t.Fatal(err)
	}

	planTool, _ := reg.Get("plan_task")
	plan := planTool.Execute
	claimTool, _ := reg.Get("claim_task")
	claim := claimTool.Execute
	sendMailTool, _ := reg.Get("send_mail")
	sendMail := sendMailTool.Execute
	readMailboxTool, _ := reg.Get("read_mailbox")
	readMailbox := readMailboxTool.Execute
	acquireTool, _ := reg.Get("acquire_workspace")
	acquire := acquireTool.Execute
	releaseTool, _ := reg.Get("release_workspace")
	release := releaseTool.Execute

	if _, err := plan(context.Background(), json.RawMessage(`{"id":"t-dep","goal":"dep done"}`)); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(context.Background(), taskrt.TaskRecord{ID: "t-dep", Goal: "dep done", Status: taskrt.TaskCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan(context.Background(), json.RawMessage(`{"id":"t-main","agent":"worker","goal":"main","depends_on":["t-dep"]}`)); err != nil {
		t.Fatal(err)
	}
	claimedRaw, err := claim(context.Background(), json.RawMessage(`{"claimer":"worker-1","agent":"worker"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claimedRaw), `"claimed"`) {
		t.Fatalf("expected claimed status, got %s", string(claimedRaw))
	}

	if _, err := sendMail(context.Background(), json.RawMessage(`{"from":"worker-1","to":"manager","content":"done?"}`)); err != nil {
		t.Fatal(err)
	}
	mailRaw, err := readMailbox(context.Background(), json.RawMessage(`{"owner":"manager","limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mailRaw), `"count":1`) {
		t.Fatalf("expected one mail, got %s", string(mailRaw))
	}

	acquireRaw, err := acquire(context.Background(), json.RawMessage(`{"task_id":"t-main"}`))
	if err != nil {
		t.Fatal(err)
	}
	var acquireResp map[string]any
	if err := json.Unmarshal(acquireRaw, &acquireResp); err != nil {
		t.Fatal(err)
	}
	wsID, _ := acquireResp["workspace_id"].(string)
	if wsID == "" {
		t.Fatalf("expected workspace_id in acquire response: %s", string(acquireRaw))
	}
	releaseInput, _ := json.Marshal(map[string]any{"workspace_id": wsID})
	if _, err := release(context.Background(), releaseInput); err != nil {
		t.Fatal(err)
	}
}

func TestTaskTrackerHydratesPersistedTaskRuntime(t *testing.T) {
	rt := taskrt.NewMemoryTaskRuntime()
	if err := rt.UpsertTask(context.Background(), taskrt.TaskRecord{
		ID:              "persisted-task",
		AgentName:       "worker",
		Goal:            "resume later",
		Status:          taskrt.TaskRunning,
		SessionID:       "sess-child",
		ParentSessionID: "sess-parent",
		JobID:           "persisted-task",
		JobItemID:       "turn-1",
		CreatedAt:       time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertJob(context.Background(), taskrt.AgentJob{
		ID:        "persisted-task",
		AgentName: "worker",
		Goal:      "resume later",
		Status:    taskrt.JobRunning,
	}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerWithRuntime(rt)
	task, ok := tracker.Get("persisted-task")
	if !ok {
		t.Fatal("expected persisted task to be hydrated")
	}
	if task.SessionID != "sess-child" || task.ParentSessionID != "sess-parent" {
		t.Fatalf("unexpected hydrated task lineage: %+v", task)
	}
	if task.JobID != "persisted-task" || task.JobItemID != "turn-1" {
		t.Fatalf("unexpected hydrated job metadata: %+v", task)
	}
	if task.Revision == 0 {
		t.Fatalf("expected hydrated revision, got %+v", task)
	}
	if task.Active {
		t.Fatalf("expected hydrated task to be recoverable, got %+v", task)
	}
}

func TestWaitAgent_ReturnsRecoverableForHydratedRunningTask(t *testing.T) {
	rt := taskrt.NewMemoryTaskRuntime()
	if err := rt.UpsertTask(context.Background(), taskrt.TaskRecord{
		ID:        "persisted-task",
		AgentName: "worker",
		Goal:      "resume later",
		Status:    taskrt.TaskRunning,
	}); err != nil {
		t.Fatal(err)
	}
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerWithRuntime(rt)
	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	waitHandlerTool, _ := reg.Get("wait_agent")
	waitHandler := waitHandlerTool.Execute
	raw, err := waitHandler(context.Background(), json.RawMessage(`{"target":"persisted-task","timeout_seconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"recoverable":true`) {
		t.Fatalf("expected recoverable wait response, got %s", string(raw))
	}
	if !strings.Contains(string(raw), `"active":false`) {
		t.Fatalf("expected inactive wait response, got %s", string(raw))
	}
}

func TestResumeAgent_RestartsHydratedRunningTask(t *testing.T) {
	rt := taskrt.NewMemoryTaskRuntime()
	if err := rt.UpsertTask(context.Background(), taskrt.TaskRecord{
		ID:        "persisted-task",
		AgentName: "worker",
		Goal:      "resume later",
		Status:    taskrt.TaskRunning,
	}); err != nil {
		t.Fatal(err)
	}
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, sess *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	tracker := NewTaskTrackerWithRuntime(rt)
	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	resumeHandlerTool, _ := reg.Get("resume_agent")
	resumeHandler := resumeHandlerTool.Execute
	raw, err := resumeHandler(context.Background(), json.RawMessage(`{"target":"persisted-task","message":"continue"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"resumed":true`) {
		t.Fatalf("expected resumed=true, got %s", string(raw))
	}
	if !strings.Contains(string(raw), `"active":true`) {
		t.Fatalf("expected active=true, got %s", string(raw))
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected restarted worker to begin running")
	}
	task, ok := tracker.Get("persisted-task")
	if !ok {
		t.Fatal("expected restarted task")
	}
	if !task.Active || task.Status != TaskRunning {
		t.Fatalf("expected active running task, got %+v", task)
	}
	tracker.Cancel("persisted-task", "cleanup")
}

func TestRegisterToolsWithDeps_AddsP1ControlPlaneTools(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	reg := tool.NewRegistry()
	if err := RegisterToolsWithDeps(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}, RuntimeDeps{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_agents", "read_agent", "write_agent", "wait_agent", "close_agent", "resume_agent"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("expected tool %q", name)
		}
	}
}

func TestReadAndWriteAgentTools_ByTaskID(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()

	firstStarted := make(chan struct{}, 1)
	var calls int32
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, sess *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				firstStarted <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &session.LifecycleResult{
				Success:    true,
				Output:     "updated: " + sess.Config.Goal,
				TokensUsed: model.TokenUsage{TotalTokens: 3},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	spawnHandlerTool, _ := reg.Get("spawn_agent")
	spawnHandler := spawnHandlerTool.Execute
	readHandlerTool, _ := reg.Get("read_agent")
	readHandler := readHandlerTool.Execute
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute

	spawnInputRaw, _ := json.Marshal(spawnInput{Agent: "worker", Task: "initial"})
	spawnRaw, err := spawnHandler(context.Background(), spawnInputRaw)
	if err != nil {
		t.Fatal(err)
	}
	var spawnResp map[string]string
	if err := json.Unmarshal(spawnRaw, &spawnResp); err != nil {
		t.Fatal(err)
	}
	taskID := spawnResp["task_id"]
	if taskID == "" {
		t.Fatal("missing task_id")
	}
	<-firstStarted

	readRaw, err := readHandler(context.Background(), json.RawMessage(`{"target":"`+taskID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readRaw), `"task_id":"`+taskID+`"`) && !strings.Contains(string(readRaw), `"id":"`+taskID+`"`) {
		t.Fatalf("unexpected read response: %s", string(readRaw))
	}

	writeRaw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"`+taskID+`","message":"follow up"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(writeRaw), `"task_id":"`+taskID+`"`) {
		t.Fatalf("unexpected write response: %s", string(writeRaw))
	}
}

func TestWriteAgent_AmbiguousAgentTargetRequiresTaskID(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{ID: "t-1", AgentName: "worker", Goal: "first", Status: TaskRunning}, nil)
	tracker.Start(&Task{ID: "t-2", AgentName: "worker", Goal: "second", Status: TaskRunning}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute
	_, err := writeHandler(context.Background(), json.RawMessage(`{"target":"worker","message":"follow up"}`))
	if err == nil || !strings.Contains(err.Error(), "please specify task_id") {
		t.Fatalf("expected ambiguous target error, got %v", err)
	}
}

func TestWriteAgent_QueueOnlyReturnsQueued(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	rt := taskrt.NewMemoryTaskRuntime()
	tracker := NewTaskTrackerWithRuntime(rt)
	tracker.Start(&Task{
		ID:        "t-queue",
		AgentName: "worker",
		Goal:      "running",
		Status:    TaskRunning,
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterToolsWithDeps(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}, RuntimeDeps{TaskRuntime: rt}); err != nil {
		t.Fatal(err)
	}
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute
	raw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-queue","message":"queued note","trigger_turn":false}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "queued" || resp["queued"] != true {
		t.Fatalf("expected queued response, got %s", string(raw))
	}
	if resp["consumed_count"] != float64(0) || resp["remaining_count"] != float64(1) {
		t.Fatalf("expected consumed_count=0 and remaining_count=1, got %s", string(raw))
	}
}

func TestWriteAgent_QueueOnlyRequiresPersistentRuntime(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-queue-no-runtime",
		AgentName: "worker",
		Goal:      "running",
		Status:    TaskRunning,
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute
	_, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-queue-no-runtime","message":"queued note","trigger_turn":false}`))
	if err == nil || !strings.Contains(err.Error(), "task message persistence") {
		t.Fatalf("expected persistence requirement error, got %v", err)
	}
}

func TestWriteAgent_InterruptFalseOnRunningReturnsQueued(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	rt := taskrt.NewMemoryTaskRuntime()
	tracker := NewTaskTrackerWithRuntime(rt)
	tracker.Start(&Task{
		ID:        "t-running",
		AgentName: "worker",
		Goal:      "running",
		Status:    TaskRunning,
	}, func() {})

	reg := tool.NewRegistry()
	if err := RegisterToolsWithDeps(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}, RuntimeDeps{TaskRuntime: rt}); err != nil {
		t.Fatal(err)
	}
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute
	raw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-running","message":"do later","interrupt":false}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "queued" || resp["queued"] != true {
		t.Fatalf("expected queued response, got %s", string(raw))
	}
	if resp["consumed_count"] != float64(0) || resp["remaining_count"] != float64(1) {
		t.Fatalf("expected consumed_count=0 and remaining_count=1, got %s", string(raw))
	}
}

func TestWriteAgent_TriggerTurnConsumesQueuedMessages(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	rt := taskrt.NewMemoryTaskRuntime()
	tracker := NewTaskTrackerWithRuntime(rt)
	tracker.Start(&Task{
		ID:        "t-consume",
		AgentName: "worker",
		Goal:      "base goal",
		Status:    TaskRunning,
	}, nil)

	done := make(chan struct{}, 1)
	var capturedGoal string
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(_ context.Context, sess *session.Session, _ tool.Registry) (*session.LifecycleResult, error) {
			capturedGoal = sess.Config.Goal
			done <- struct{}{}
			return &session.LifecycleResult{Success: true, Output: "ok"}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterToolsWithDeps(reg, agents, tracker, delegator, RuntimeDeps{TaskRuntime: rt}); err != nil {
		t.Fatal(err)
	}
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute

	if _, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-consume","message":"queued first","trigger_turn":false}`)); err != nil {
		t.Fatal(err)
	}
	queuedBefore, err := rt.ListTaskMessages(context.Background(), "t-consume", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queuedBefore) != 1 {
		t.Fatalf("expected one queued message before trigger, got %+v", queuedBefore)
	}

	raw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-consume","message":"run now","trigger_turn":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["consumed_count"] != float64(1) || resp["remaining_count"] != float64(0) {
		t.Fatalf("expected consumed_count=1 and remaining_count=0, got %s", string(raw))
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected triggered run to start")
	}
	if !strings.Contains(capturedGoal, "queued first") || !strings.Contains(capturedGoal, "run now") {
		t.Fatalf("expected merged goal to include consumed queued + direct message, got %q", capturedGoal)
	}
	queuedAfter, err := rt.ListTaskMessages(context.Background(), "t-consume", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queuedAfter) != 0 {
		t.Fatalf("expected queued messages consumed after trigger, got %+v", queuedAfter)
	}
}

func TestWriteAgent_TriggerTurnOnCompletedRestartsTask(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-completed",
		AgentName: "worker",
		Goal:      "done",
		Status:    TaskCompleted,
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	writeHandlerTool, _ := reg.Get("write_agent")
	writeHandler := writeHandlerTool.Execute
	raw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-completed","message":"run again"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"task_id":"t-completed"`) {
		t.Fatalf("expected same task id in response, got %s", string(raw))
	}
}

func TestWaitAgent_ReturnsOnStateChange(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	revision := tracker.Start(&Task{
		ID:        "t-wait",
		AgentName: "worker",
		Goal:      "wait me",
		Status:    TaskRunning,
	}, func() {})

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	waitHandlerTool, _ := reg.Get("wait_agent")
	waitHandler := waitHandlerTool.Execute
	go func() {
		time.Sleep(80 * time.Millisecond)
		tracker.CompleteIf("t-wait", revision, "ok", model.TokenUsage{})
	}()
	raw, err := waitHandler(context.Background(), json.RawMessage(`{"target":"t-wait","timeout_seconds":2,"poll_millis":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"completed":true`) {
		t.Fatalf("expected completed=true, got %s", string(raw))
	}
}

func TestWaitAgent_TimesOut(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-timeout",
		AgentName: "worker",
		Goal:      "still running",
		Status:    TaskRunning,
	}, func() {})

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	waitHandlerTool, _ := reg.Get("wait_agent")
	waitHandler := waitHandlerTool.Execute
	raw, err := waitHandler(context.Background(), json.RawMessage(`{"target":"t-timeout","timeout_seconds":1,"poll_millis":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"timed_out":true`) {
		t.Fatalf("expected timed_out=true, got %s", string(raw))
	}
}

func TestCloseAgent_CancelsRunningTask(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-close",
		AgentName: "worker",
		Goal:      "running",
		Status:    TaskRunning,
	}, func() {})

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	closeHandlerTool, _ := reg.Get("close_agent")
	closeHandler := closeHandlerTool.Execute
	raw, err := closeHandler(context.Background(), json.RawMessage(`{"target":"t-close","reason":"stop now"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"closed":true`) {
		t.Fatalf("expected closed=true, got %s", string(raw))
	}
	updated, ok := tracker.Get("t-close")
	if !ok || updated.Status != TaskCancelled {
		t.Fatalf("expected cancelled task, got %+v", updated)
	}
}

func TestResumeAgent_RestartsCompletedTask(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-resume",
		AgentName: "worker",
		Goal:      "done",
		Status:    TaskCompleted,
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	resumeHandlerTool, _ := reg.Get("resume_agent")
	resumeHandler := resumeHandlerTool.Execute
	raw, err := resumeHandler(context.Background(), json.RawMessage(`{"target":"t-resume","message":"run again"}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["consumed_count"] != float64(0) || resp["remaining_count"] != float64(0) {
		t.Fatalf("expected consumed_count=0 and remaining_count=0, got %s", string(raw))
	}
	if !strings.Contains(string(raw), `"resumed":true`) {
		t.Fatalf("expected resumed=true, got %s", string(raw))
	}
	updated, ok := tracker.Get("t-resume")
	if !ok {
		t.Fatal("missing resumed task")
	}
	if updated.Status != TaskRunning && updated.Status != TaskCompleted {
		t.Fatalf("expected running or completed status, got %+v", updated)
	}
	if updated.Revision < 2 {
		t.Fatalf("expected resumed task revision >= 2, got %+v", updated)
	}
}
