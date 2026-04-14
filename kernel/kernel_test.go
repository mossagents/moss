package kernel

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	kt "github.com/mossagents/moss/testing"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingObserver struct {
	mu     sync.Mutex
	events []observe.ExecutionEvent
}

type observerAwareSnapshotStore struct {
	observer observe.ExecutionObserver
}

type observerAwareCheckpointStore struct {
	observer observe.ExecutionObserver
}

func (o *recordingObserver) OnLLMCall(context.Context, observe.LLMCallEvent)      {}
func (o *recordingObserver) OnToolCall(context.Context, observe.ToolCallEvent)    {}
func (o *recordingObserver) OnApproval(context.Context, io.ApprovalEvent)         {}
func (o *recordingObserver) OnSessionEvent(context.Context, observe.SessionEvent) {}
func (o *recordingObserver) OnError(context.Context, observe.ErrorEvent)          {}

func (o *recordingObserver) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
}

func (o *recordingObserver) snapshot() []observe.ExecutionEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]observe.ExecutionEvent, len(o.events))
	copy(out, o.events)
	return out
}

func (s *observerAwareSnapshotStore) SetObserver(observer observe.ExecutionObserver) {
	s.observer = observer
}

func (s *observerAwareCheckpointStore) SetObserver(observer observe.ExecutionObserver) {
	s.observer = observer
}

func (*observerAwareSnapshotStore) Create(context.Context, workspace.WorktreeSnapshotRequest) (*workspace.WorktreeSnapshot, error) {
	return nil, nil
}

func (*observerAwareSnapshotStore) Load(context.Context, string) (*workspace.WorktreeSnapshot, error) {
	return nil, nil
}

func (*observerAwareSnapshotStore) List(context.Context) ([]workspace.WorktreeSnapshot, error) {
	return nil, nil
}

func (*observerAwareSnapshotStore) FindBySession(context.Context, string) ([]workspace.WorktreeSnapshot, error) {
	return nil, nil
}

func (*observerAwareCheckpointStore) Create(context.Context, checkpoint.CheckpointCreateRequest) (*checkpoint.CheckpointRecord, error) {
	return nil, nil
}

func (*observerAwareCheckpointStore) Load(context.Context, string) (*checkpoint.CheckpointRecord, error) {
	return nil, nil
}

func (*observerAwareCheckpointStore) List(context.Context) ([]checkpoint.CheckpointRecord, error) {
	return nil, nil
}

func (*observerAwareCheckpointStore) FindBySession(context.Context, string) ([]checkpoint.CheckpointRecord, error) {
	return nil, nil
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

type nonHookSessionManager struct {
	base session.Manager
}

func newNonHookSessionManager() *nonHookSessionManager {
	return &nonHookSessionManager{base: session.NewManager()}
}

func (m *nonHookSessionManager) Create(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	return m.base.Create(ctx, cfg)
}

func (m *nonHookSessionManager) Get(id string) (*session.Session, bool) { return m.base.Get(id) }
func (m *nonHookSessionManager) List() []*session.Session               { return m.base.List() }
func (m *nonHookSessionManager) Cancel(id string) error                 { return m.base.Cancel(id) }
func (m *nonHookSessionManager) Notify(id string, msg model.Message) error {
	return m.base.Notify(id, msg)
}

func runRootAgent(ctx context.Context, k *Kernel, sess *session.Session, userMsg *model.Message) (*session.LifecycleResult, error) {
	return CollectRunAgentResult(ctx, k, RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: userMsg,
	})
}

func runRootAgentWithIO(ctx context.Context, k *Kernel, sess *session.Session, userMsg *model.Message, runIO io.UserIO) (*session.LifecycleResult, error) {
	return CollectRunAgentResult(ctx, k, RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: userMsg,
		IO:          runIO,
	})
}

func runDelegatedAgent(ctx context.Context, k *Kernel, sess *session.Session, userMsg *model.Message, tools tool.Registry) (*session.LifecycleResult, error) {
	return CollectRunAgentResult(ctx, k, RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("delegated"),
		UserContent: userMsg,
		IO:          &io.NoOpIO{},
		Tools:       tools,
	})
}

func TestKernelIntegration(t *testing.T) {
	// MockLLM: 先请求 tool call，然后 text 回复
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("Let me read the file.")},
					ToolCalls:    []model.ToolCall{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}},
				},
				ToolCalls:  []model.ToolCall{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 20},
			},
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("Now let me write a fix.")},
					ToolCalls:    []model.ToolCall{{ID: "c2", Name: "write_file", Arguments: json.RawMessage(`{"path":"main.go","content":"fixed"}`)}},
				},
				ToolCalls:  []model.ToolCall{{ID: "c2", Name: "write_file", Arguments: json.RawMessage(`{"path":"main.go","content":"fixed"}`)}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 25},
			},
			{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("I've fixed the null pointer bug in main.go.")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 15},
			},
		},
	}

	recIO := kt.NewRecorderIO()
	// 当被要求审批时，批准
	recIO.AskFunc = func(req io.InputRequest) (io.InputResponse, error) {
		return io.InputResponse{Approved: true}, nil
	}

	obs := &recordingObserver{}
	k := New(
		WithLLM(mock),
		WithUserIO(recIO),
		WithObserver(obs),
	)

	// 注册工具
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name: "read_file", Description: "Read file contents", Risk: tool.RiskLow,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"package main\nfunc main() {}"`), nil
	})); err != nil {
		t.Fatalf("register read_file: %v", err)
	}
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name: "write_file", Description: "Write file contents", Risk: tool.RiskHigh,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatalf("register write_file: %v", err)
	}

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
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Fix the null pointer in main.go")}}
	sess.AppendMessage(userMsg)

	// 运行
	result, err := runRootAgent(context.Background(), k, sess, &userMsg)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	if len(recIO.Asked) == 0 {
		t.Fatal("expected approval prompt to be asked")
	}
	if recIO.Asked[0].Approval == nil {
		t.Fatal("expected structured approval request on confirm prompt")
	}
	if recIO.Asked[0].Approval.ToolName != "write_file" {
		t.Fatalf("expected approval for write_file, got %+v", recIO.Asked[0].Approval)
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
	if len(recIO.Asked) != 1 {
		t.Fatalf("Ask calls = %d, want 1 (write_file approval)", len(recIO.Asked))
	}

	// 验证事件被触发
	if len(events) == 0 {
		t.Fatal("expected tool events")
	}

	execEvents := obs.snapshot()
	if len(execEvents) == 0 {
		t.Fatal("expected execution events")
	}
	expected := map[observe.ExecutionEventType]bool{
		observe.ExecutionRunStarted:       false,
		observe.ExecutionLLMStarted:       false,
		observe.ExecutionLLMCompleted:     false,
		observe.ExecutionToolStarted:      false,
		observe.ExecutionToolCompleted:    false,
		observe.ExecutionApprovalRequest:  false,
		observe.ExecutionApprovalResolved: false,
		observe.ExecutionRunCompleted:     false,
	}
	for _, e := range execEvents {
		if _, ok := expected[e.Type]; ok {
			expected[e.Type] = true
		}
	}
	for typ, seen := range expected {
		if !seen {
			t.Fatalf("expected execution event %s", typ)
		}
	}
}

func TestSetObserverUpdatesObserverAwareStores(t *testing.T) {
	initial := &recordingObserver{}
	snapshots := &observerAwareSnapshotStore{}
	checkpoints := &observerAwareCheckpointStore{}
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
		WithObserver(initial),
		WithWorktreeSnapshots(snapshots),
		WithCheckpoints(checkpoints),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if snapshots.observer != initial || checkpoints.observer != initial {
		t.Fatal("expected initial observer to propagate on boot")
	}

	next := &recordingObserver{}
	k.SetObserver(next)
	if snapshots.observer != next {
		t.Fatalf("snapshot store observer not updated: got %T want %T", snapshots.observer, next)
	}
	if checkpoints.observer != next {
		t.Fatalf("checkpoint store observer not updated: got %T want %T", checkpoints.observer, next)
	}
}

func TestKernelBootRequiresLLM(t *testing.T) {
	k := New()
	if err := k.Boot(context.Background()); err == nil {
		t.Fatal("expected error when LLM not configured")
	}
}

func TestKernelBootWiresObserverIntoSnapshotStore(t *testing.T) {
	obs := &recordingObserver{}
	store := &observerAwareSnapshotStore{}
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
		WithObserver(obs),
		WithWorktreeSnapshots(store),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if store.observer != obs {
		t.Fatal("expected snapshot store observer to be wired during boot")
	}
}

func TestKernelBootWiresObserverIntoCheckpointStore(t *testing.T) {
	obs := &recordingObserver{}
	store := &observerAwareCheckpointStore{}
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
		WithObserver(obs),
		WithCheckpoints(store),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if store.observer != obs {
		t.Fatal("expected checkpoint store observer to be wired during boot")
	}
}

func TestKernelRunAgentRequestIOOverridesDefaultIO(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("hello from override")}},
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
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hi")}}
	sess.AppendMessage(userMsg)

	result, err := runRootAgentWithIO(context.Background(), k, sess, &userMsg, overrideIO)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if result.Output != "hello from override" {
		t.Fatalf("Output = %q, want hello from override", result.Output)
	}
	if len(defaultIO.Sent) != 0 {
		t.Fatalf("default IO should be unused, got %d messages", len(defaultIO.Sent))
	}
	if len(overrideIO.Sent) < 1 {
		t.Fatalf("override IO messages = %d, want >= 1", len(overrideIO.Sent))
	}
	// Find the stream content message.
	var found bool
	for _, msg := range overrideIO.Sent {
		if msg.Content == "hello from override" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("override IO missing expected content; got %v", overrideIO.Sent)
	}
}

func TestKernelRunRejectedWhenShuttingDown(t *testing.T) {
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
	)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "test", MaxSteps: 1})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	_, err = runRootAgent(context.Background(), k, sess, nil)
	if err == nil {
		t.Fatal("expected shutdown rejection error")
	}

	var kerr *errors.Error
	if !stderrors.As(err, &kerr) || kerr.Code != errors.ErrShutdown {
		t.Fatalf("expected ErrShutdown, got: %v", err)
	}
}

func TestKernelShutdownCancelsInFlightRun(t *testing.T) {
	bl := &blockingLLM{}
	k := New(
		WithLLM(bl),
		WithUserIO(&io.NoOpIO{}),
	)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "long-running", MaxSteps: 5})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("wait")}}
	sess.AppendMessage(userMsg)

	runErrCh := make(chan error, 1)
	go func() {
		_, runErr := runRootAgent(context.Background(), k, sess, &userMsg)
		runErrCh <- runErr
	}()

	deadline := time.After(500 * time.Millisecond)
	for atomic.LoadInt32(&bl.calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("LLM was not called before timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case runErr := <-runErrCh:
		if runErr == nil {
			t.Fatal("expected run error after shutdown cancellation")
		}
		if !stderrors.Is(runErr, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight run did not exit after shutdown")
	}
}

func TestSessionManagerCancelCancelsInFlightRun(t *testing.T) {
	bl := &blockingLLM{}
	k := New(
		WithLLM(bl),
		WithUserIO(&io.NoOpIO{}),
	)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "cancel", MaxSteps: 5})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("wait")}}
	sess.AppendMessage(userMsg)

	runErrCh := make(chan error, 1)
	go func() {
		_, runErr := runRootAgent(context.Background(), k, sess, &userMsg)
		runErrCh <- runErr
	}()

	deadline := time.After(500 * time.Millisecond)
	for atomic.LoadInt32(&bl.calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("LLM was not called before timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if err := k.SessionManager().Cancel(sess.ID); err != nil {
		t.Fatalf("SessionManager.Cancel: %v", err)
	}

	select {
	case runErr := <-runErrCh:
		if runErr == nil {
			t.Fatal("expected run error after session cancel")
		}
		if !stderrors.Is(runErr, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight run did not exit after session cancel")
	}
}

func TestWithSessionManager_NonHookManagerStillCancelsInFlightRun(t *testing.T) {
	bl := &blockingLLM{}
	mgr := newNonHookSessionManager()
	k := New(
		WithLLM(bl),
		WithUserIO(&io.NoOpIO{}),
		WithSessionManager(mgr),
	)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "cancel", MaxSteps: 5})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("wait")}}
	sess.AppendMessage(userMsg)

	runErrCh := make(chan error, 1)
	go func() {
		_, runErr := runRootAgent(context.Background(), k, sess, &userMsg)
		runErrCh <- runErr
	}()

	deadline := time.After(500 * time.Millisecond)
	for atomic.LoadInt32(&bl.calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("LLM was not called before timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if err := k.SessionManager().Cancel(sess.ID); err != nil {
		t.Fatalf("SessionManager.Cancel: %v", err)
	}

	select {
	case runErr := <-runErrCh:
		if runErr == nil {
			t.Fatal("expected run error after session cancel")
		}
		if !stderrors.Is(runErr, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight run did not exit after session cancel")
	}
}

func TestKernelRunAgentRequestsShareTimeoutSemantics(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Kernel, *session.Session, *model.Message) (*session.LifecycleResult, error)
	}{
		{
			name: "default",
			run: func(k *Kernel, sess *session.Session, userMsg *model.Message) (*session.LifecycleResult, error) {
				return runRootAgent(context.Background(), k, sess, userMsg)
			},
		},
		{
			name: "io-override",
			run: func(k *Kernel, sess *session.Session, userMsg *model.Message) (*session.LifecycleResult, error) {
				return runRootAgentWithIO(context.Background(), k, sess, userMsg, kt.NewRecorderIO())
			},
		},
		{
			name: "tools-override",
			run: func(k *Kernel, sess *session.Session, userMsg *model.Message) (*session.LifecycleResult, error) {
				return runDelegatedAgent(context.Background(), k, sess, userMsg, k.ToolRegistry())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bl := &blockingLLM{}
			k := New(
				WithLLM(bl),
				WithUserIO(&io.NoOpIO{}),
			)

			if err := k.Boot(context.Background()); err != nil {
				t.Fatalf("Boot: %v", err)
			}

			sess, err := k.NewSession(context.Background(), session.SessionConfig{
				Goal:     "timeout",
				MaxSteps: 5,
				Timeout:  30 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("NewSession: %v", err)
			}
			userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("wait")}}
			sess.AppendMessage(userMsg)

			start := time.Now()
			_, err = tt.run(k, sess, &userMsg)
			if err == nil {
				t.Fatal("expected timeout error")
			}
			if !stderrors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected context.DeadlineExceeded, got %v", err)
			}
			if time.Since(start) > time.Second {
				t.Fatalf("run exceeded expected timeout window: %v", time.Since(start))
			}
		})
	}
}

func TestKernelRunRejectsConcurrentSameSession(t *testing.T) {
	bl := &blockingLLM{}
	k := New(
		WithLLM(bl),
		WithUserIO(&io.NoOpIO{}),
	)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "serialize", MaxSteps: 5})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("wait")}}
	sess.AppendMessage(userMsg)

	firstRunErrCh := make(chan error, 1)
	go func() {
		_, runErr := runRootAgent(context.Background(), k, sess, &userMsg)
		firstRunErrCh <- runErr
	}()

	deadline := time.After(500 * time.Millisecond)
	for atomic.LoadInt32(&bl.calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("LLM was not called before timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}

	_, err = runRootAgent(context.Background(), k, sess, &userMsg)
	if err == nil {
		t.Fatal("expected second run to be rejected for same session")
	}
	var kerr *errors.Error
	if !stderrors.As(err, &kerr) || kerr.Code != errors.ErrSessionRunning {
		t.Fatalf("expected ErrSessionRunning, got: %v", err)
	}

	if err := k.SessionManager().Cancel(sess.ID); err != nil {
		t.Fatalf("SessionManager.Cancel: %v", err)
	}
	select {
	case runErr := <-firstRunErrCh:
		if !stderrors.Is(runErr, context.Canceled) {
			t.Fatalf("expected first run to end by context.Canceled, got: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not exit after cancel")
	}
}

func TestKernelStagesAndPromptsRunInOrder(t *testing.T) {
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
	)

	var order []string
	k.Stages().OnBoot(20, func(context.Context, *Kernel) error {
		order = append(order, "boot-20")
		return nil
	})
	k.Stages().OnBoot(10, func(context.Context, *Kernel) error {
		order = append(order, "boot-10")
		return nil
	})
	k.Stages().OnShutdown(20, func(context.Context, *Kernel) error {
		order = append(order, "shutdown-20")
		return nil
	})
	k.Stages().OnShutdown(10, func(context.Context, *Kernel) error {
		order = append(order, "shutdown-10")
		return nil
	})
	k.Prompts().Add(20, func(*Kernel) string { return "prompt-20" })
	k.Prompts().Add(10, func(*Kernel) string { return "prompt-10" })

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{
		Goal:         "test",
		SystemPrompt: "base",
		MaxSteps:     1,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(sess.Messages) == 0 {
		t.Fatal("expected system prompt message to be injected")
	}
	if got, want := model.ContentPartsToPlainText(sess.Messages[0].ContentParts), "base\n\nprompt-10\n\nprompt-20"; got != want {
		t.Fatalf("system prompt = %q, want %q", got, want)
	}

	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	wantOrder := []string{"boot-10", "boot-20", "shutdown-10", "shutdown-20"}
	if len(order) != len(wantOrder) {
		t.Fatalf("hook order len = %d, want %d (%v)", len(order), len(wantOrder), order)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("hook order[%d] = %q, want %q (full=%v)", i, order[i], wantOrder[i], order)
		}
	}
}

func TestKernelSessionLifecycleHooksRunInOrder(t *testing.T) {
	k := New(
		WithLLM(&kt.MockLLM{
			Responses: []model.CompletionResponse{{
				Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 3},
			}},
		}),
		WithUserIO(&io.NoOpIO{}),
	)

	var order []string
	k.chain.OnSessionLifecycle.AddHook("", func(_ context.Context, event *session.LifecycleEvent) error {
		if event != nil {
			order = append(order, fmt.Sprintf("%s-20", event.Stage))
		}
		return nil
	}, 20)
	k.chain.OnSessionLifecycle.AddHook("", func(_ context.Context, event *session.LifecycleEvent) error {
		if event != nil {
			order = append(order, fmt.Sprintf("%s-10", event.Stage))
		}
		return nil
	}, 10)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{
		Goal:     "test lifecycle hooks",
		MaxSteps: 1,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := runRootAgent(context.Background(), k, sess, nil); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	wantOrder := []string{
		"created-10", "created-20",
		"started-10", "started-20",
		"completed-10", "completed-20",
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("hook order len = %d, want %d (%v)", len(order), len(wantOrder), order)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("hook order[%d] = %q, want %q (full=%v)", i, order[i], wantOrder[i], order)
		}
	}
}

func TestKernelToolLifecycleHooksRunInOrder(t *testing.T) {
	k := New(
		WithLLM(&kt.MockLLM{
			Responses: []model.CompletionResponse{
				{
					Message: model.Message{
						Role:         model.RoleAssistant,
						ContentParts: []model.ContentPart{model.TextPart("")},
						ToolCalls:    []model.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
					},
					ToolCalls:  []model.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
					StopReason: "tool_use",
					Usage:      model.TokenUsage{TotalTokens: 5},
				},
				{
					Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
					StopReason: "end_turn",
					Usage:      model.TokenUsage{TotalTokens: 3},
				},
			},
		}),
		WithUserIO(&io.NoOpIO{}),
	)
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{Name: "greet", Description: "Greet someone"}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"hello world"`), nil
	})); err != nil {
		t.Fatalf("register greet: %v", err)
	}

	var order []string
	k.chain.OnToolLifecycle.AddHook("", func(_ context.Context, event *hooks.ToolEvent) error {
		if event != nil {
			order = append(order, fmt.Sprintf("%s-20", event.Stage))
		}
		return nil
	}, 20)
	k.chain.OnToolLifecycle.AddHook("", func(_ context.Context, event *hooks.ToolEvent) error {
		if event != nil {
			order = append(order, fmt.Sprintf("%s-10", event.Stage))
		}
		return nil
	}, 10)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{
		Goal:     "test tool lifecycle hooks",
		MaxSteps: 4,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := runRootAgent(context.Background(), k, sess, nil); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	wantOrder := []string{"before-10", "before-20", "after-10", "after-20"}
	if len(order) != len(wantOrder) {
		t.Fatalf("hook order len = %d, want %d (%v)", len(order), len(wantOrder), order)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("hook order[%d] = %q, want %q (full=%v)", i, order[i], wantOrder[i], order)
		}
	}
}

func TestKernelServiceRegistrySlots(t *testing.T) {
	k := New()

	if _, ok := k.Services().Load("missing"); ok {
		t.Fatal("missing state should not exist")
	}

	actual, loaded := k.Services().LoadOrStore("slot", "first")
	if loaded {
		t.Fatal("first LoadOrStoreState should store new value")
	}
	if got := actual.(string); got != "first" {
		t.Fatalf("stored value = %q, want %q", got, "first")
	}

	actual, loaded = k.Services().LoadOrStore("slot", "second")
	if !loaded {
		t.Fatal("second LoadOrStoreState should load existing value")
	}
	if got := actual.(string); got != "first" {
		t.Fatalf("loaded value = %q, want %q", got, "first")
	}

	k.Services().Store("slot", "updated")
	value, ok := k.Services().Load("slot")
	if !ok {
		t.Fatal("expected slot state to exist")
	}
	if got := value.(string); got != "updated" {
		t.Fatalf("state value = %q, want %q", got, "updated")
	}
}

func TestKernelPortAccessors(t *testing.T) {
	tasks := taskrt.NewMemoryTaskRuntime()
	mailbox := taskrt.NewMemoryMailbox()
	k := New(
		WithTaskRuntime(tasks),
		WithMailbox(mailbox),
	)
	if k.TaskRuntime() == nil || k.Mailbox() == nil {
		t.Fatal("expected task runtime and mailbox accessors to return configured ports")
	}
}
