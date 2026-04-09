package product

import (
	"context"
	kernio "github.com/mossagents/moss/kernel/io"
	"testing"
)

func TestRecordingIOConfirmDeniesApprovals(t *testing.T) {
	recIO := NewRecordingIO(ApprovalModeConfirm)
	resp, err := recIO.Ask(context.Background(), kernio.InputRequest{
		Type:     kernio.InputConfirm,
		Prompt:   "Allow tool write_file?",
		Approval: &kernio.ApprovalRequest{ID: "req-1"},
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
	recIO := NewRecordingIO(ApprovalModeFullAuto)
	resp, err := recIO.Ask(context.Background(), kernio.InputRequest{
		Type:     kernio.InputConfirm,
		Prompt:   "Allow tool write_file?",
		Approval: &kernio.ApprovalRequest{ID: "req-2"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !resp.Approved {
		t.Fatal("full-auto mode should auto-approve")
	}

	if err := recIO.Send(context.Background(), kernio.OutputMessage{Type: kernio.OutputToolResult, Content: "done", Meta: map[string]any{"is_error": true}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	events := recIO.Events()
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	if !events[0].IsError || events[0].Type != kernio.OutputToolResult {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}
