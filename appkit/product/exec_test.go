package product

import (
	"context"
	intr "github.com/mossagents/moss/kernel/interaction"
	"testing"
)

func TestRecordingIOConfirmDeniesApprovals(t *testing.T) {
	io := NewRecordingIO(ApprovalModeConfirm)
	resp, err := io.Ask(context.Background(), intr.InputRequest{
		Type:     intr.InputConfirm,
		Prompt:   "Allow tool write_file?",
		Approval: &intr.ApprovalRequest{ID: "req-1"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Approved {
		t.Fatal("confirm mode should not auto-approve in recording IO")
	}
	if resp.Decision == nil || resp.Decision.Source != "recording-deny" {
		t.Fatalf("unexpected decision: %+v", resp.Decision)
	}
}

func TestRecordingIOFullAutoApprovesAndCapturesEvents(t *testing.T) {
	io := NewRecordingIO(ApprovalModeFullAuto)
	resp, err := io.Ask(context.Background(), intr.InputRequest{
		Type:     intr.InputConfirm,
		Prompt:   "Allow tool write_file?",
		Approval: &intr.ApprovalRequest{ID: "req-2"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !resp.Approved {
		t.Fatal("full-auto mode should auto-approve")
	}

	if err := io.Send(context.Background(), intr.OutputMessage{Type: intr.OutputToolResult, Content: "done", Meta: map[string]any{"is_error": true}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	events := io.Events()
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	if !events[0].IsError || events[0].Type != intr.OutputToolResult {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}
