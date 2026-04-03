package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
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
		Tools:        []string{"grep"},
		MaxSteps:     10,
	})

	tracker := NewTaskTracker()
	parentReg := tool.NewRegistry()
	parentReg.Register(tool.ToolSpec{Name: "grep"}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
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

func TestTaskToolSyncBackgroundQuery(t *testing.T) {
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
			if sess.Config.Goal == "background work" {
				defer close(done)
			}
			return &loop.SessionResult{
				SessionID:  sess.ID,
				Success:    true,
				Output:     "ok: " + sess.Config.Goal,
				TokensUsed: port.TokenUsage{TotalTokens: 10},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	_, taskHandler, ok := reg.Get("task")
	if !ok {
		t.Fatal("task tool not registered")
	}

	// sync mode
	syncInput, _ := json.Marshal(taskInput{Mode: "sync", Agent: "worker", Task: "do sync"})
	syncResult, err := taskHandler(context.Background(), syncInput)
	if err != nil {
		t.Fatalf("task sync: %v", err)
	}
	var syncResp map[string]string
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
	var queryResp map[string]string
	if err := json.Unmarshal(queryResult, &queryResp); err != nil {
		t.Fatal(err)
	}
	if queryResp["mode"] != "query" || queryResp["status"] != "completed" {
		t.Fatalf("unexpected query response: %+v", queryResp)
	}
}

func TestTaskToolModeValidation(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."})
	tracker := NewTaskTracker()
	delegator := &mockDelegator{registry: tool.NewRegistry()}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	_, taskHandler, ok := reg.Get("task")
	if !ok {
		t.Fatal("task tool not registered")
	}

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

func TestUpdateTaskRestartsSameID(t *testing.T) {
	agents := NewRegistry()
	agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."})
	tracker := NewTaskTracker()

	firstStarted := make(chan struct{}, 1)
	var calls int32
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, sess *session.Session, _ tool.Registry) (*loop.SessionResult, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				firstStarted <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &loop.SessionResult{
				SessionID:  sess.ID,
				Success:    true,
				Output:     "updated: " + sess.Config.Goal,
				TokensUsed: port.TokenUsage{TotalTokens: 5},
			}, nil
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
	_, updateHandler, ok := reg.Get("update_task")
	if !ok {
		t.Fatal("update_task not registered")
	}

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
	agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."})
	tracker := NewTaskTracker()

	started := make(chan struct{}, 1)
	released := make(chan struct{}, 1)
	delegator := &mockDelegator{
		registry: tool.NewRegistry(),
		runFn: func(ctx context.Context, _ *session.Session, _ tool.Registry) (*loop.SessionResult, error) {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-released:
				return &loop.SessionResult{Success: true, Output: "done"}, nil
			}
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
	_, listHandler, ok := reg.Get("list_tasks")
	if !ok {
		t.Fatal("list_tasks not registered")
	}
	_, cancelHandler, ok := reg.Get("cancel_task")
	if !ok {
		t.Fatal("cancel_task not registered")
	}

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

func TestRegisterToolsWithDeps_AddsCollaborationTools(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerWithRuntime(port.NewMemoryTaskRuntime())
	delegator := &mockDelegator{registry: tool.NewRegistry()}
	reg := tool.NewRegistry()
	isolation, err := sandbox.NewLocalWorkspaceIsolation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := RuntimeDeps{
		TaskRuntime: port.NewMemoryTaskRuntime(),
		Mailbox:     port.NewMemoryMailbox(),
		Isolation:   isolation,
	}
	if err := RegisterToolsWithDeps(reg, agents, tracker, delegator, deps); err != nil {
		t.Fatalf("RegisterToolsWithDeps: %v", err)
	}
	for _, name := range []string{"plan_task", "claim_task", "send_mail", "read_mailbox", "acquire_workspace", "release_workspace"} {
		if _, _, ok := reg.Get(name); !ok {
			t.Fatalf("expected tool %q", name)
		}
	}
}

func TestPlanClaimMailAndWorkspaceFlow(t *testing.T) {
	rt := port.NewMemoryTaskRuntime()
	mb := port.NewMemoryMailbox()
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

	_, plan, _ := reg.Get("plan_task")
	_, claim, _ := reg.Get("claim_task")
	_, sendMail, _ := reg.Get("send_mail")
	_, readMailbox, _ := reg.Get("read_mailbox")
	_, acquire, _ := reg.Get("acquire_workspace")
	_, release, _ := reg.Get("release_workspace")

	if _, err := plan(context.Background(), json.RawMessage(`{"id":"t-dep","goal":"dep done"}`)); err != nil {
		t.Fatal(err)
	}
	if err := rt.UpsertTask(context.Background(), port.TaskRecord{ID: "t-dep", Goal: "dep done", Status: port.TaskCompleted}); err != nil {
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
	for _, name := range []string{"list_agents", "read_agent", "write_agent", "wait_agent"} {
		if _, _, ok := reg.Get(name); !ok {
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
		runFn: func(ctx context.Context, sess *session.Session, _ tool.Registry) (*loop.SessionResult, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				firstStarted <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &loop.SessionResult{
				SessionID:  sess.ID,
				Success:    true,
				Output:     "updated: " + sess.Config.Goal,
				TokensUsed: port.TokenUsage{TotalTokens: 3},
			}, nil
		},
	}

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, delegator); err != nil {
		t.Fatal(err)
	}
	_, spawnHandler, _ := reg.Get("spawn_agent")
	_, readHandler, _ := reg.Get("read_agent")
	_, writeHandler, _ := reg.Get("write_agent")

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

func TestWriteAgent_QueueOnlyReturnsQueued(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-queue",
		AgentName: "worker",
		Goal:      "running",
		Status:    TaskRunning,
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	_, writeHandler, _ := reg.Get("write_agent")
	raw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-queue","message":"queued note","trigger_turn":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"queued"`) {
		t.Fatalf("expected queued response, got %s", string(raw))
	}
}

func TestWriteAgent_InterruptFalseOnRunningReturnsQueued(t *testing.T) {
	agents := NewRegistry()
	if err := agents.Register(AgentConfig{Name: "worker", SystemPrompt: "Work."}); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTracker()
	tracker.Start(&Task{
		ID:        "t-running",
		AgentName: "worker",
		Goal:      "running",
		Status:    TaskRunning,
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	_, writeHandler, _ := reg.Get("write_agent")
	raw, err := writeHandler(context.Background(), json.RawMessage(`{"target":"t-running","message":"do later","interrupt":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"queued"`) {
		t.Fatalf("expected queued response, got %s", string(raw))
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
	_, writeHandler, _ := reg.Get("write_agent")
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
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	_, waitHandler, _ := reg.Get("wait_agent")
	go func() {
		time.Sleep(80 * time.Millisecond)
		tracker.CompleteIf("t-wait", revision, "ok", port.TokenUsage{})
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
	}, nil)

	reg := tool.NewRegistry()
	if err := RegisterTools(reg, agents, tracker, &mockDelegator{registry: tool.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	_, waitHandler, _ := reg.Get("wait_agent")
	raw, err := waitHandler(context.Background(), json.RawMessage(`{"target":"t-timeout","timeout_seconds":1,"poll_millis":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"timed_out":true`) {
		t.Fatalf("expected timed_out=true, got %s", string(raw))
	}
}
