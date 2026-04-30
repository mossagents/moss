package kernel

import (
	"context"
	stderrors "errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/kernel/testing"
)

type runtimeParityObserver struct {
	observe.NoOpObserver

	mu             sync.Mutex
	sessionTypes   []string
	executionTypes []observe.ExecutionEventType
	llmCalls       int
	toolCalls      int
	errors         []string
}

type runtimeParityTrace struct {
	sessionTypes   []string
	executionTypes []observe.ExecutionEventType
	llmCalls       int
	toolCalls      int
	errors         []string
}

type runtimeParityRun struct {
	result            *session.LifecycleResult
	err               error
	trace             runtimeParityTrace
	runtimeEventTypes []kruntime.EventType
	blueprint         kruntime.SessionBlueprint
}

func (o *runtimeParityObserver) OnSessionEvent(_ context.Context, e observe.SessionEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessionTypes = append(o.sessionTypes, e.Type)
}

func (o *runtimeParityObserver) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.executionTypes = append(o.executionTypes, e.Type)
}

func (o *runtimeParityObserver) OnLLMCall(_ context.Context, _ observe.LLMCallEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.llmCalls++
}

func (o *runtimeParityObserver) OnToolCall(_ context.Context, _ observe.ToolCallEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.toolCalls++
}

func (o *runtimeParityObserver) OnError(_ context.Context, e observe.ErrorEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.errors = append(o.errors, e.Message)
}

func (o *runtimeParityObserver) snapshot() runtimeParityTrace {
	o.mu.Lock()
	defer o.mu.Unlock()
	return runtimeParityTrace{
		sessionTypes:   append([]string(nil), o.sessionTypes...),
		executionTypes: append([]observe.ExecutionEventType(nil), o.executionTypes...),
		llmCalls:       o.llmCalls,
		toolCalls:      o.toolCalls,
		errors:         append([]string(nil), o.errors...),
	}
}

func TestBlueprintRun_ResultLifecycleAndObserver(t *testing.T) {
	responses := []model.CompletionResponse{{
		Message: model.Message{
			Role:         model.RoleAssistant,
			ContentParts: []model.ContentPart{model.TextPart("done")},
		},
		StopReason: "end_turn",
		Usage:      model.TokenUsage{PromptTokens: 2, CompletionTokens: 5, TotalTokens: 7},
	}}

	run := runBlueprintParityPath(t, context.Background(), &kt.MockLLM{Responses: cloneResponses(responses)})
	if run.err != nil {
		t.Fatalf("blueprint run failed: %v", run.err)
	}
	if run.result == nil || !run.result.Success {
		t.Fatalf("expected successful lifecycle result, got %+v err=%v", run.result, run.err)
	}
	assertSessionTypes(t, run.trace.sessionTypes, []string{"created", "running", "completed"})

	wantRuntimeEvents := []kruntime.EventType{
		kruntime.EventTypeSessionCreated,
		kruntime.EventTypeTurnStarted,
		kruntime.EventTypeLLMCalled,
		kruntime.EventTypeTurnCompleted,
	}
	if !reflect.DeepEqual(run.runtimeEventTypes, wantRuntimeEvents) {
		t.Fatalf("runtime event types = %v, want %v", run.runtimeEventTypes, wantRuntimeEvents)
	}
}

func TestBlueprintRun_ContextTermination(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		run := runBlueprintParityPath(t, ctx, &blockingLLM{})
		assertBlueprintTermination(t, run, context.DeadlineExceeded, []string{"created", "running", "failed"})
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		run := runBlueprintParityPath(t, ctx, &blockingLLM{})
		assertBlueprintTermination(t, run, context.Canceled, []string{"created", "running", "cancelled"})
	})
}

func TestResumeRuntimeSession_PreservesBlueprintAfterTurn(t *testing.T) {
	ctx := context.Background()
	store, err := kruntime.NewSQLiteEventStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteEventStore: %v", err)
	}
	k := New(
		WithLLM(&kt.MockLLM{Responses: []model.CompletionResponse{{
			Message: model.Message{
				Role:         model.RoleAssistant,
				ContentParts: []model.ContentPart{model.TextPart("resumed")},
			},
			StopReason: "end_turn",
			Usage:      model.TokenUsage{TotalTokens: 3},
		}}}),
		WithUserIO(&io.NoOpIO{}),
		WithEventStore(store),
	)

	bp, err := k.StartRuntimeSession(ctx, kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
		PromptPack:        "coding",
		Workspace:         t.TempDir(),
		ModelProfile:      "gpt-5",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}

	userMsg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("say resumed")},
	}
	result, err := CollectRunAgentFromBlueprint(ctx, k, bp, nil, k.BuildLLMAgent("root"), &userMsg, &io.NoOpIO{})
	if err != nil {
		t.Fatalf("CollectRunAgentFromBlueprint: %v", err)
	}
	if result.Output != "resumed" {
		t.Fatalf("Output = %q, want resumed", result.Output)
	}

	resumed, err := k.ResumeRuntimeSession(ctx, bp.Identity.SessionID)
	if err != nil {
		t.Fatalf("ResumeRuntimeSession: %v", err)
	}
	assertBlueprintStable(t, resumed, bp)
}

func TestForkRuntimeSession_PersistsResolvedBlueprintAcrossSourceAndChild(t *testing.T) {
	ctx := context.Background()
	store, err := kruntime.NewSQLiteEventStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteEventStore: %v", err)
	}
	k := New(WithEventStore(store))

	sourceBP, err := k.StartRuntimeSession(ctx, kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
		PromptPack:        "coding",
		Workspace:         t.TempDir(),
		ModelProfile:      "gpt-5",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}

	childReq := kruntime.RuntimeRequest{
		PermissionProfile: "read-only",
		PromptPack:        "plan",
		Workspace:         t.TempDir(),
		ModelProfile:      "gpt-4o-mini",
	}
	childBP, err := k.ForkRuntimeSession(ctx, sourceBP.Identity.SessionID, childReq)
	if err != nil {
		t.Fatalf("ForkRuntimeSession: %v", err)
	}

	sourceEvents, err := store.LoadEvents(ctx, sourceBP.Identity.SessionID, 0)
	if err != nil {
		t.Fatalf("LoadEvents source: %v", err)
	}
	var forkPayload *kruntime.SessionForkedPayload
	for _, ev := range sourceEvents {
		if ev.Type != kruntime.EventTypeSessionForked {
			continue
		}
		payload, ok := ev.Payload.(*kruntime.SessionForkedPayload)
		if !ok {
			t.Fatalf("session_forked payload type mismatch: %T", ev.Payload)
		}
		forkPayload = payload
		break
	}
	if forkPayload == nil {
		t.Fatal("expected session_forked payload")
	}
	if forkPayload.ChildSessionID != childBP.Identity.SessionID {
		t.Fatalf("ChildSessionID = %q, want %q", forkPayload.ChildSessionID, childBP.Identity.SessionID)
	}
	if forkPayload.BlueprintPayload == nil {
		t.Fatal("expected session_forked to persist child blueprint")
	}
	assertBlueprintStable(t, *forkPayload.BlueprintPayload, childBP)

	childState, err := k.LoadRuntimeSession(ctx, childBP.Identity.SessionID)
	if err != nil {
		t.Fatalf("LoadRuntimeSession child: %v", err)
	}
	if childState == nil || childState.Blueprint == nil {
		t.Fatal("child runtime state should include blueprint")
	}
	assertBlueprintStable(t, *childState.Blueprint, childBP)
	if childState.EffectiveToolPolicy == nil {
		t.Fatal("child runtime state should include effective tool policy")
	}
	if childState.EffectiveToolPolicy.PolicyHash != childBP.EffectiveToolPolicy.PolicyHash {
		t.Fatalf("child policy hash = %q, want %q", childState.EffectiveToolPolicy.PolicyHash, childBP.EffectiveToolPolicy.PolicyHash)
	}
	if childState.Blueprint.PromptPlan.PromptPackID != childReq.PromptPack {
		t.Fatalf("child prompt pack = %q, want %q", childState.Blueprint.PromptPlan.PromptPackID, childReq.PromptPack)
	}
	if childState.Blueprint.ModelConfig.ModelID != childReq.ModelProfile {
		t.Fatalf("child model = %q, want %q", childState.Blueprint.ModelConfig.ModelID, childReq.ModelProfile)
	}
}

func runBlueprintParityPath(t *testing.T, ctx context.Context, llm model.LLM) runtimeParityRun {
	t.Helper()
	obs := &runtimeParityObserver{}
	store, err := kruntime.NewSQLiteEventStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteEventStore: %v", err)
	}
	k := New(
		WithLLM(llm),
		WithUserIO(&io.NoOpIO{}),
		WithObserver(obs),
		WithEventStore(store),
	)
	bp, err := k.StartRuntimeSession(context.Background(), kruntime.RuntimeRequest{
		PermissionProfile: "workspace-write",
		PromptPack:        "coding",
		Workspace:         t.TempDir(),
		ModelProfile:      "gpt-5",
	})
	if err != nil {
		t.Fatalf("StartRuntimeSession: %v", err)
	}
	userMsg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("say done")},
	}
	result, err := CollectRunAgentFromBlueprint(ctx, k, bp, nil, k.BuildLLMAgent("root"), &userMsg, &io.NoOpIO{})
	eventTypes := loadRuntimeEventTypes(t, store, bp.Identity.SessionID)
	return runtimeParityRun{
		result:            result,
		err:               err,
		trace:             obs.snapshot(),
		runtimeEventTypes: eventTypes,
		blueprint:         bp,
	}
}

func loadRuntimeEventTypes(t *testing.T, store kruntime.EventStore, sessionID string) []kruntime.EventType {
	t.Helper()
	events, err := store.LoadEvents(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("LoadEvents(%s): %v", sessionID, err)
	}
	types := make([]kruntime.EventType, 0, len(events))
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	return types
}

func assertBlueprintTermination(t *testing.T, run runtimeParityRun, want error, wantSessionTypes []string) {
	t.Helper()
	if !stderrors.Is(run.err, want) {
		t.Fatalf("error = %v, want %v", run.err, want)
	}
	assertSessionTypes(t, run.trace.sessionTypes, wantSessionTypes)
	if run.result != nil && run.result.Success {
		t.Fatalf("expected failed run, got success: %+v", run.result)
	}
}

func assertSessionTypes(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("session types = %v, want %v", got, want)
	}
}

func assertBlueprintStable(t *testing.T, got, want kruntime.SessionBlueprint) {
	t.Helper()
	if got.Identity.SessionID != want.Identity.SessionID {
		t.Fatalf("SessionID mismatch: got=%q want=%q", got.Identity.SessionID, want.Identity.SessionID)
	}
	if got.Identity.WorkspaceID != want.Identity.WorkspaceID {
		t.Fatalf("WorkspaceID mismatch: got=%q want=%q", got.Identity.WorkspaceID, want.Identity.WorkspaceID)
	}
	if !reflect.DeepEqual(got.ModelConfig, want.ModelConfig) {
		t.Fatalf("ModelConfig mismatch: got=%+v want=%+v", got.ModelConfig, want.ModelConfig)
	}
	if !reflect.DeepEqual(got.ContextBudget, want.ContextBudget) {
		t.Fatalf("ContextBudget mismatch: got=%+v want=%+v", got.ContextBudget, want.ContextBudget)
	}
	if !reflect.DeepEqual(got.PromptPlan, want.PromptPlan) {
		t.Fatalf("PromptPlan mismatch: got=%+v want=%+v", got.PromptPlan, want.PromptPlan)
	}
	if !reflect.DeepEqual(got.SessionBudget, want.SessionBudget) {
		t.Fatalf("SessionBudget mismatch: got=%+v want=%+v", got.SessionBudget, want.SessionBudget)
	}
	if got.EffectiveToolPolicy.PolicyHash != want.EffectiveToolPolicy.PolicyHash {
		t.Fatalf("PolicyHash mismatch: got=%q want=%q", got.EffectiveToolPolicy.PolicyHash, want.EffectiveToolPolicy.PolicyHash)
	}
	if got.EffectiveToolPolicy.TrustLevel != want.EffectiveToolPolicy.TrustLevel {
		t.Fatalf("TrustLevel mismatch: got=%q want=%q", got.EffectiveToolPolicy.TrustLevel, want.EffectiveToolPolicy.TrustLevel)
	}
	if got.Provenance.Hash != want.Provenance.Hash {
		t.Fatalf("Blueprint hash mismatch: got=%q want=%q", got.Provenance.Hash, want.Provenance.Hash)
	}
}

func cloneResponses(in []model.CompletionResponse) []model.CompletionResponse {
	out := make([]model.CompletionResponse, len(in))
	copy(out, in)
	return out
}
