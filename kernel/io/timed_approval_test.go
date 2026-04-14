package io

import (
	"context"
	"errors"
	"testing"
	"time"
)

// syncApprovalIO 是一个简单的同步 IO mock，用于测试 TimedApproval。
type syncApprovalIO struct {
	approved bool
	err      error
	delay    time.Duration
}

func (s *syncApprovalIO) Send(_ context.Context, _ OutputMessage) error { return nil }
func (s *syncApprovalIO) Ask(ctx context.Context, req InputRequest) (InputResponse, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return InputResponse{}, ctx.Err()
		}
	}
	if s.err != nil {
		return InputResponse{}, s.err
	}
	return InputResponse{Approved: s.approved}, nil
}

func makeApprovalRequest(id string) InputRequest {
	return InputRequest{
		Type: InputConfirm,
		Approval: &ApprovalRequest{
			ID:       id,
			ToolName: "test_tool",
			Prompt:   "Allow this?",
		},
	}
}

func TestTimedApproval_NonApprovalDelegates(t *testing.T) {
	inner := &syncApprovalIO{approved: true}
	ta := NewTimedApproval(inner, nil, 0)
	ctx := context.Background()

	// Non-approval requests delegate directly
	resp, err := ta.Ask(ctx, InputRequest{Type: InputFreeText, Prompt: "name?"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp
}

func TestTimedApproval_ApprovedByUser(t *testing.T) {
	store := NewMemoryApprovalStore()
	inner := &syncApprovalIO{approved: true}
	ta := NewTimedApproval(inner, store, 0)
	ctx := context.Background()

	resp, err := ta.Ask(ctx, makeApprovalRequest("req-approve"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Approved {
		t.Fatal("expected Approved=true")
	}

	// Store should record approved status
	rec, _ := store.Get(ctx, "req-approve")
	if rec.Status != ApprovalStatusApproved {
		t.Fatalf("expected ApprovalStatusApproved in store, got %s", rec.Status)
	}
}

func TestTimedApproval_DeniedByUser(t *testing.T) {
	store := NewMemoryApprovalStore()
	inner := &syncApprovalIO{approved: false}
	ta := NewTimedApproval(inner, store, 0)
	ctx := context.Background()

	resp, err := ta.Ask(ctx, makeApprovalRequest("req-deny"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Approved {
		t.Fatal("expected Approved=false")
	}

	rec, _ := store.Get(ctx, "req-deny")
	if rec.Status != ApprovalStatusDenied {
		t.Fatalf("expected ApprovalStatusDenied in store, got %s", rec.Status)
	}
}

func TestTimedApproval_InnerError(t *testing.T) {
	store := NewMemoryApprovalStore()
	sentinel := errors.New("io error")
	inner := &syncApprovalIO{err: sentinel}
	ta := NewTimedApproval(inner, store, 0)
	ctx := context.Background()

	_, err := ta.Ask(ctx, makeApprovalRequest("req-err"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	rec, _ := store.Get(ctx, "req-err")
	if rec.Status != ApprovalStatusDenied {
		t.Fatalf("expected ApprovalStatusDenied for error case, got %s", rec.Status)
	}
}

func TestTimedApproval_Timeout(t *testing.T) {
	store := NewMemoryApprovalStore()
	// inner takes 100ms but timeout is 10ms
	inner := &syncApprovalIO{delay: 100 * time.Millisecond}
	ta := NewTimedApproval(inner, store, 10*time.Millisecond)
	ctx := context.Background()

	resp, err := ta.Ask(ctx, makeApprovalRequest("req-timeout"))
	if err != nil {
		t.Fatalf("timeout should return nil error with deny response, got %v", err)
	}
	if resp.Approved {
		t.Fatal("timed-out response should be Approved=false")
	}
	if resp.Decision == nil || resp.Decision.Type != ApprovalDecisionDeny {
		t.Fatalf("expected deny decision, got %+v", resp.Decision)
	}

	rec, _ := store.Get(ctx, "req-timeout")
	if rec.Status != ApprovalStatusTimedOut {
		t.Fatalf("expected ApprovalStatusTimedOut in store, got %s", rec.Status)
	}
}

func TestTimedApproval_NilStore(t *testing.T) {
	inner := &syncApprovalIO{approved: true}
	ta := NewTimedApproval(inner, nil, 0)
	ctx := context.Background()

	// Should not panic with nil store
	resp, err := ta.Ask(ctx, makeApprovalRequest("req-nil-store"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Approved {
		t.Fatal("expected approval with nil store")
	}
}

func TestTimedApproval_Send(t *testing.T) {
	inner := &syncApprovalIO{}
	ta := NewTimedApproval(inner, nil, 0)
	if err := ta.Send(context.Background(), OutputMessage{Type: OutputText, Content: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
