package kernel_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	taskrt "github.com/mossagents/moss/kernel/task"
)

// ---- A2A Tests ----

func TestA2A_RoundTrip(t *testing.T) {
	payload := taskrt.TaskDelegatePayload{TaskID: "t1", Goal: "do something"}
	env := taskrt.A2AEnvelope{Kind: taskrt.A2AKindTaskDelegate, CorrelID: "c1"}
	if err := env.MarshalPayload(payload); err != nil {
		t.Fatal(err)
	}

	base := taskrt.MailMessage{From: "agent-a", To: "agent-b", Content: "delegate task"}
	msg, err := taskrt.NewA2AMessage(base, env)
	if err != nil {
		t.Fatal(err)
	}

	if !taskrt.IsA2AMessage(msg) {
		t.Fatal("expected IsA2AMessage=true")
	}

	got, err := taskrt.ExtractA2AEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil envelope")
	}
	if got.Kind != taskrt.A2AKindTaskDelegate {
		t.Fatalf("expected kind=%q got=%q", taskrt.A2AKindTaskDelegate, got.Kind)
	}
	if got.Protocol != "a2a/v1" {
		t.Fatalf("expected protocol=a2a/v1 got=%q", got.Protocol)
	}
	var p taskrt.TaskDelegatePayload
	if err := got.UnmarshalPayload(&p); err != nil {
		t.Fatal(err)
	}
	if p.TaskID != "t1" || p.Goal != "do something" {
		t.Fatalf("unexpected payload: %+v", p)
	}
}

func TestA2A_NonA2AMessage(t *testing.T) {
	msg := taskrt.MailMessage{From: "a", To: "b", Content: "plain"}
	if taskrt.IsA2AMessage(msg) {
		t.Fatal("expected IsA2AMessage=false for plain message")
	}
	env, err := taskrt.ExtractA2AEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if env != nil {
		t.Fatal("expected nil envelope for plain message")
	}
}

func TestA2A_TaskResultPayload(t *testing.T) {
	env := taskrt.A2AEnvelope{Kind: taskrt.A2AKindTaskResult}
	result := taskrt.TaskResultPayload{TaskID: "t1", Success: true, Output: "done"}
	if err := env.MarshalPayload(result); err != nil {
		t.Fatal(err)
	}
	var got taskrt.TaskResultPayload
	if err := env.UnmarshalPayload(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Success || got.Output != "done" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestA2A_StatusUpdatePayload(t *testing.T) {
	env := taskrt.A2AEnvelope{Kind: taskrt.A2AKindStatusUpdate}
	su := taskrt.StatusUpdatePayload{TaskID: "t2", Status: "running", Progress: 0.5}
	if err := env.MarshalPayload(su); err != nil {
		t.Fatal(err)
	}
	var got taskrt.StatusUpdatePayload
	if err := env.UnmarshalPayload(&got); err != nil {
		t.Fatal(err)
	}
	if got.Progress != 0.5 || got.Status != "running" {
		t.Fatalf("unexpected status update: %+v", got)
	}
}

func TestA2A_EnvelopeFromJSONMetadata(t *testing.T) {
	// Simulate metadata stored as map[string]any after JSON decode
	env := taskrt.A2AEnvelope{Kind: taskrt.A2AKindCancel, Protocol: "a2a/v1", SentAt: time.Now()}
	raw, _ := json.Marshal(env)

	// Mimic what happens after JSON round-trip (Metadata["a2a"] becomes map[string]any)
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}

	msg := taskrt.MailMessage{
		From:     "a",
		To:       "b",
		Metadata: map[string]any{"a2a": meta},
	}
	got, err := taskrt.ExtractA2AEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != taskrt.A2AKindCancel {
		t.Fatalf("unexpected envelope: %+v", got)
	}
}

// ---- ApprovalStore Tests ----

func TestMemoryApprovalStore_SaveAndGet(t *testing.T) {
	store := io.NewMemoryApprovalStore()
	ctx := context.Background()

	req := io.ApprovalRequest{ID: "req-1", SessionID: "s1", Prompt: "allow?"}
	record := io.ApprovalRecord{
		Request:   req,
		Status:    io.ApprovalStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != io.ApprovalStatusPending {
		t.Fatalf("expected pending, got %s", got.Status)
	}
}

func TestMemoryApprovalStore_NotFound(t *testing.T) {
	store := io.NewMemoryApprovalStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if err != io.ErrApprovalNotFound {
		t.Fatalf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestMemoryApprovalStore_List(t *testing.T) {
	store := io.NewMemoryApprovalStore()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		r := io.ApprovalRecord{
			Request:   io.ApprovalRequest{ID: "req-" + string(rune('1'+i)), SessionID: "s1"},
			Status:    io.ApprovalStatusApproved,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := store.Save(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// Different session
	if err := store.Save(ctx, io.ApprovalRecord{
		Request:   io.ApprovalRequest{ID: "req-other", SessionID: "s2"},
		Status:    io.ApprovalStatusDenied,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 records for s1, got %d", len(list))
	}
}

func TestMemoryApprovalStore_RequiresID(t *testing.T) {
	store := io.NewMemoryApprovalStore()
	err := store.Save(context.Background(), io.ApprovalRecord{})
	if err == nil {
		t.Fatal("expected error when request ID is empty")
	}
}

// ---- TimedApproval Tests ----

type mockUserIO struct {
	resp io.InputResponse
	err  error
	// delay simulates async processing
	delay time.Duration
}

func (m *mockUserIO) Send(_ context.Context, _ io.OutputMessage) error { return nil }
func (m *mockUserIO) Ask(_ context.Context, _ io.InputRequest) (io.InputResponse, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.resp, m.err
}

func TestTimedApproval_Approved(t *testing.T) {
	mock := &mockUserIO{resp: io.InputResponse{Approved: true}}
	store := io.NewMemoryApprovalStore()
	ta := io.NewTimedApproval(mock, store, 5*time.Second)

	req := io.InputRequest{
		Type:     io.InputConfirm,
		Prompt:   "allow?",
		Approval: &io.ApprovalRequest{ID: "r1", SessionID: "s1"},
	}
	resp, err := ta.Ask(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Approved {
		t.Fatal("expected approved")
	}
	record, err := store.Get(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != io.ApprovalStatusApproved {
		t.Fatalf("expected approved record, got %s", record.Status)
	}
}

func TestTimedApproval_TimedOut(t *testing.T) {
	mock := &mockUserIO{delay: 200 * time.Millisecond}
	store := io.NewMemoryApprovalStore()
	ta := io.NewTimedApproval(mock, store, 20*time.Millisecond)

	req := io.InputRequest{
		Type:     io.InputConfirm,
		Prompt:   "allow?",
		Approval: &io.ApprovalRequest{ID: "r2", SessionID: "s1"},
	}
	resp, err := ta.Ask(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Approved {
		t.Fatal("expected not approved on timeout")
	}
	if resp.Decision == nil || resp.Decision.Reason == "" {
		t.Fatal("expected timeout reason in decision")
	}
	record, _ := store.Get(context.Background(), "r2")
	if record != nil && record.Status != io.ApprovalStatusTimedOut {
		t.Fatalf("expected timed_out record, got %s", record.Status)
	}
}

func TestTimedApproval_NonApproval_PassThrough(t *testing.T) {
	mock := &mockUserIO{resp: io.InputResponse{Value: "hello"}}
	ta := io.NewTimedApproval(mock, nil, 5*time.Second)
	req := io.InputRequest{Type: io.InputFreeText, Prompt: "say something"}
	resp, err := ta.Ask(context.Background(), req)
	if err != nil || resp.Value != "hello" {
		t.Fatalf("expected passthrough, got resp=%v err=%v", resp, err)
	}
}

// ---- ModelConfig Validation Tests ----

func TestValidateModelConfigExtra_Valid(t *testing.T) {
	schema := model.ExtraSchema{
		"temperature_scale": {Kind: model.ExtraFieldString, AllowedValues: []string{"celsius", "fahrenheit"}},
		"use_cache":         {Kind: model.ExtraFieldBool},
		"top_k":             {Kind: model.ExtraFieldNumber},
	}
	cfg := model.ModelConfig{
		Extra: map[string]any{
			"temperature_scale": "celsius",
			"use_cache":         true,
			"top_k":             40.0,
		},
	}
	if err := model.ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateModelConfigExtra_InvalidType(t *testing.T) {
	schema := model.ExtraSchema{"flag": {Kind: model.ExtraFieldBool}}
	cfg := model.ModelConfig{Extra: map[string]any{"flag": "yes"}}
	if err := model.ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected type error")
	}
}

func TestValidateModelConfigExtra_DisallowedValue(t *testing.T) {
	schema := model.ExtraSchema{"mode": {Kind: model.ExtraFieldString, AllowedValues: []string{"fast", "slow"}}}
	cfg := model.ModelConfig{Extra: map[string]any{"mode": "turbo"}}
	if err := model.ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected disallowed value error")
	}
}

func TestValidateModelConfigExtra_Required_Missing(t *testing.T) {
	schema := model.ExtraSchema{"api_key": {Kind: model.ExtraFieldString, Required: true}}
	cfg := model.ModelConfig{Extra: map[string]any{}}
	if err := model.ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected required field error")
	}
}

func TestValidateModelConfigExtraStrict_UnknownField(t *testing.T) {
	schema := model.ExtraSchema{"known": {Kind: model.ExtraFieldAny}}
	cfg := model.ModelConfig{Extra: map[string]any{"known": "ok", "unknown": "bad"}}
	if err := model.ValidateModelConfigExtraStrict(cfg, schema); err == nil {
		t.Fatal("expected unknown field error in strict mode")
	}
}

func TestValidateModelConfigExtra_NilExtra(t *testing.T) {
	schema := model.ExtraSchema{"opt": {Kind: model.ExtraFieldString}}
	cfg := model.ModelConfig{}
	if err := model.ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error for nil extra: %v", err)
	}
}
