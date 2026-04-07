package port_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

// ---- A2A Tests ----

func TestA2A_RoundTrip(t *testing.T) {
	payload := port.TaskDelegatePayload{TaskID: "t1", Goal: "do something"}
	env := port.A2AEnvelope{Kind: port.A2AKindTaskDelegate, CorrelID: "c1"}
	if err := env.MarshalPayload(payload); err != nil {
		t.Fatal(err)
	}

	base := port.MailMessage{From: "agent-a", To: "agent-b", Content: "delegate task"}
	msg, err := port.NewA2AMessage(base, env)
	if err != nil {
		t.Fatal(err)
	}

	if !port.IsA2AMessage(msg) {
		t.Fatal("expected IsA2AMessage=true")
	}

	got, err := port.ExtractA2AEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil envelope")
	}
	if got.Kind != port.A2AKindTaskDelegate {
		t.Fatalf("expected kind=%q got=%q", port.A2AKindTaskDelegate, got.Kind)
	}
	if got.Protocol != "a2a/v1" {
		t.Fatalf("expected protocol=a2a/v1 got=%q", got.Protocol)
	}
	var p port.TaskDelegatePayload
	if err := got.UnmarshalPayload(&p); err != nil {
		t.Fatal(err)
	}
	if p.TaskID != "t1" || p.Goal != "do something" {
		t.Fatalf("unexpected payload: %+v", p)
	}
}

func TestA2A_NonA2AMessage(t *testing.T) {
	msg := port.MailMessage{From: "a", To: "b", Content: "plain"}
	if port.IsA2AMessage(msg) {
		t.Fatal("expected IsA2AMessage=false for plain message")
	}
	env, err := port.ExtractA2AEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if env != nil {
		t.Fatal("expected nil envelope for plain message")
	}
}

func TestA2A_TaskResultPayload(t *testing.T) {
	env := port.A2AEnvelope{Kind: port.A2AKindTaskResult}
	result := port.TaskResultPayload{TaskID: "t1", Success: true, Output: "done"}
	if err := env.MarshalPayload(result); err != nil {
		t.Fatal(err)
	}
	var got port.TaskResultPayload
	if err := env.UnmarshalPayload(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Success || got.Output != "done" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestA2A_StatusUpdatePayload(t *testing.T) {
	env := port.A2AEnvelope{Kind: port.A2AKindStatusUpdate}
	su := port.StatusUpdatePayload{TaskID: "t2", Status: "running", Progress: 0.5}
	if err := env.MarshalPayload(su); err != nil {
		t.Fatal(err)
	}
	var got port.StatusUpdatePayload
	if err := env.UnmarshalPayload(&got); err != nil {
		t.Fatal(err)
	}
	if got.Progress != 0.5 || got.Status != "running" {
		t.Fatalf("unexpected status update: %+v", got)
	}
}

func TestA2A_EnvelopeFromJSONMetadata(t *testing.T) {
	// Simulate metadata stored as map[string]any after JSON decode
	env := port.A2AEnvelope{Kind: port.A2AKindCancel, Protocol: "a2a/v1", SentAt: time.Now()}
	raw, _ := json.Marshal(env)

	// Mimic what happens after JSON round-trip (Metadata["a2a"] becomes map[string]any)
	var meta map[string]any
	json.Unmarshal(raw, &meta)

	msg := port.MailMessage{
		From:     "a",
		To:       "b",
		Metadata: map[string]any{"a2a": meta},
	}
	got, err := port.ExtractA2AEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != port.A2AKindCancel {
		t.Fatalf("unexpected envelope: %+v", got)
	}
}

// ---- ApprovalStore Tests ----

func TestMemoryApprovalStore_SaveAndGet(t *testing.T) {
	store := port.NewMemoryApprovalStore()
	ctx := context.Background()

	req := port.ApprovalRequest{ID: "req-1", SessionID: "s1", Prompt: "allow?"}
	record := port.ApprovalRecord{
		Request:   req,
		Status:    port.ApprovalStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != port.ApprovalStatusPending {
		t.Fatalf("expected pending, got %s", got.Status)
	}
}

func TestMemoryApprovalStore_NotFound(t *testing.T) {
	store := port.NewMemoryApprovalStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if err != port.ErrApprovalNotFound {
		t.Fatalf("expected ErrApprovalNotFound, got %v", err)
	}
}

func TestMemoryApprovalStore_List(t *testing.T) {
	store := port.NewMemoryApprovalStore()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		r := port.ApprovalRecord{
			Request:   port.ApprovalRequest{ID: "req-" + string(rune('1'+i)), SessionID: "s1"},
			Status:    port.ApprovalStatusApproved,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		store.Save(ctx, r)
	}
	// Different session
	store.Save(ctx, port.ApprovalRecord{
		Request:   port.ApprovalRequest{ID: "req-other", SessionID: "s2"},
		Status:    port.ApprovalStatusDenied,
		CreatedAt: time.Now(),
	})
	list, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 records for s1, got %d", len(list))
	}
}

func TestMemoryApprovalStore_RequiresID(t *testing.T) {
	store := port.NewMemoryApprovalStore()
	err := store.Save(context.Background(), port.ApprovalRecord{})
	if err == nil {
		t.Fatal("expected error when request ID is empty")
	}
}

// ---- TimedApproval Tests ----

type mockUserIO struct {
	resp port.InputResponse
	err  error
	// delay simulates async processing
	delay time.Duration
}

func (m *mockUserIO) Send(_ context.Context, _ port.OutputMessage) error { return nil }
func (m *mockUserIO) Ask(_ context.Context, _ port.InputRequest) (port.InputResponse, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.resp, m.err
}

func TestTimedApproval_Approved(t *testing.T) {
	mock := &mockUserIO{resp: port.InputResponse{Approved: true}}
	store := port.NewMemoryApprovalStore()
	ta := port.NewTimedApproval(mock, store, 5*time.Second)

	req := port.InputRequest{
		Type:     port.InputConfirm,
		Prompt:   "allow?",
		Approval: &port.ApprovalRequest{ID: "r1", SessionID: "s1"},
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
	if record.Status != port.ApprovalStatusApproved {
		t.Fatalf("expected approved record, got %s", record.Status)
	}
}

func TestTimedApproval_TimedOut(t *testing.T) {
	mock := &mockUserIO{delay: 200 * time.Millisecond}
	store := port.NewMemoryApprovalStore()
	ta := port.NewTimedApproval(mock, store, 20*time.Millisecond)

	req := port.InputRequest{
		Type:     port.InputConfirm,
		Prompt:   "allow?",
		Approval: &port.ApprovalRequest{ID: "r2", SessionID: "s1"},
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
	if record != nil && record.Status != port.ApprovalStatusTimedOut {
		t.Fatalf("expected timed_out record, got %s", record.Status)
	}
}

func TestTimedApproval_NonApproval_PassThrough(t *testing.T) {
	mock := &mockUserIO{resp: port.InputResponse{Value: "hello"}}
	ta := port.NewTimedApproval(mock, nil, 5*time.Second)
	req := port.InputRequest{Type: port.InputFreeText, Prompt: "say something"}
	resp, err := ta.Ask(context.Background(), req)
	if err != nil || resp.Value != "hello" {
		t.Fatalf("expected passthrough, got resp=%v err=%v", resp, err)
	}
}

// ---- ModelConfig Validation Tests ----

func TestValidateModelConfigExtra_Valid(t *testing.T) {
	schema := port.ExtraSchema{
		"temperature_scale": {Kind: port.ExtraFieldString, AllowedValues: []string{"celsius", "fahrenheit"}},
		"use_cache":         {Kind: port.ExtraFieldBool},
		"top_k":             {Kind: port.ExtraFieldNumber},
	}
	cfg := port.ModelConfig{
		Extra: map[string]any{
			"temperature_scale": "celsius",
			"use_cache":         true,
			"top_k":             40.0,
		},
	}
	if err := port.ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateModelConfigExtra_InvalidType(t *testing.T) {
	schema := port.ExtraSchema{"flag": {Kind: port.ExtraFieldBool}}
	cfg := port.ModelConfig{Extra: map[string]any{"flag": "yes"}}
	if err := port.ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected type error")
	}
}

func TestValidateModelConfigExtra_DisallowedValue(t *testing.T) {
	schema := port.ExtraSchema{"mode": {Kind: port.ExtraFieldString, AllowedValues: []string{"fast", "slow"}}}
	cfg := port.ModelConfig{Extra: map[string]any{"mode": "turbo"}}
	if err := port.ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected disallowed value error")
	}
}

func TestValidateModelConfigExtra_Required_Missing(t *testing.T) {
	schema := port.ExtraSchema{"api_key": {Kind: port.ExtraFieldString, Required: true}}
	cfg := port.ModelConfig{Extra: map[string]any{}}
	if err := port.ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected required field error")
	}
}

func TestValidateModelConfigExtraStrict_UnknownField(t *testing.T) {
	schema := port.ExtraSchema{"known": {Kind: port.ExtraFieldAny}}
	cfg := port.ModelConfig{Extra: map[string]any{"known": "ok", "unknown": "bad"}}
	if err := port.ValidateModelConfigExtraStrict(cfg, schema); err == nil {
		t.Fatal("expected unknown field error in strict mode")
	}
}

func TestValidateModelConfigExtra_NilExtra(t *testing.T) {
	schema := port.ExtraSchema{"opt": {Kind: port.ExtraFieldString}}
	cfg := port.ModelConfig{}
	if err := port.ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error for nil extra: %v", err)
	}
}
