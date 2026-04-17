package io

import (
	"context"
	"strings"
	"testing"
)

func testCtx() context.Context { return context.Background() }

// ── NewConsoleIO / maxLen ─────────────────────────────────────────────────────

func TestNewConsoleIO(t *testing.T) {
	c := NewConsoleIO()
	if c == nil {
		t.Fatal("expected non-nil ConsoleIO")
	}
	if c.W == nil || c.R == nil {
		t.Fatal("expected non-nil W and R")
	}
	if c.MaxResultLen != 300 {
		t.Fatalf("MaxResultLen = %d, want 300", c.MaxResultLen)
	}
}

func TestConsoleIO_MaxLen(t *testing.T) {
	c := &ConsoleIO{}
	if c.maxLen() != 300 {
		t.Errorf("zero MaxResultLen should default to 300")
	}
	c.MaxResultLen = 50
	if c.maxLen() != 50 {
		t.Errorf("MaxResultLen 50 should be 50")
	}
}

// ── Send ─────────────────────────────────────────────────────────────────────

func TestConsoleIO_Send_AllOutputTypes(t *testing.T) {
	ctx := testCtx()
	cases := []struct {
		msg      OutputMessage
		contains string
	}{
		{OutputMessage{Type: OutputText, Content: "hello"}, "hello"},
		{OutputMessage{Type: OutputStream, Content: "stream"}, "stream"},
		{OutputMessage{Type: OutputStreamEnd, Content: ""}, ""},
		{OutputMessage{Type: OutputReasoning, Content: "think"}, "💭"},
		{OutputMessage{Type: OutputRefusal, Content: "blocked"}, "⛔"},
		{OutputMessage{Type: OutputHostedTool, Content: "file_search completed"}, "🛰"},
		{OutputMessage{Type: OutputProgress, Content: "progress"}, "⏳"},
		{OutputMessage{Type: OutputToolStart, Content: "calling"}, "🔧"},
		{OutputMessage{Type: OutputToolResult, Content: "done"}, "✅"},
		{OutputMessage{Type: OutputToolResult, Content: "err", Meta: map[string]any{"is_error": true}}, "❌"},
	}

	for _, tc := range cases {
		var w strings.Builder
		c := &ConsoleIO{W: &w, R: strings.NewReader(""), MaxResultLen: 300}
		if err := c.Send(ctx, tc.msg); err != nil {
			t.Fatalf("Send(%v): %v", tc.msg.Type, err)
		}
		if tc.contains != "" && !strings.Contains(w.String(), tc.contains) {
			t.Errorf("Send(%v): output %q missing %q", tc.msg.Type, w.String(), tc.contains)
		}
	}
}

func TestConsoleIO_Send_ToolResult_Truncated(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader(""), MaxResultLen: 10}
	long := strings.Repeat("x", 50)
	if err := c.Send(ctx, OutputMessage{Type: OutputToolResult, Content: long}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := w.String()
	if !strings.Contains(got, "...") {
		t.Errorf("expected truncation marker in %q", got)
	}
	// should be short
	if len(got) > 30 {
		t.Errorf("output too long: %q", got)
	}
}

// ── Ask ──────────────────────────────────────────────────────────────────────

func TestConsoleIO_Ask_Confirm_Yes(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("y\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{Type: InputConfirm, Prompt: "Proceed?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !resp.Approved {
		t.Error("expected Approved=true for 'y'")
	}
}

func TestConsoleIO_Ask_Confirm_No(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("n\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{Type: InputConfirm, Prompt: "Proceed?"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Approved {
		t.Error("expected Approved=false for 'n'")
	}
}

func TestConsoleIO_Ask_Confirm_WithApproval(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("yes\n"), MaxResultLen: 300}
	req := InputRequest{
		Type:   InputConfirm,
		Prompt: "Allow?",
		Approval: &ApprovalRequest{
			ID:       "req-1",
			ToolName: "run_cmd",
			Risk:     "high",
		},
	}
	resp, err := c.Ask(ctx, req)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !resp.Approved {
		t.Error("expected Approved=true for 'yes'")
	}
	if resp.Decision == nil || resp.Decision.RequestID != "req-1" {
		t.Errorf("expected decision with RequestID 'req-1', got %+v", resp.Decision)
	}
	// The prompt should mention tool name
	if !strings.Contains(w.String(), "run_cmd") {
		t.Errorf("expected prompt to contain tool name: %q", w.String())
	}
}

func TestConsoleIO_Ask_Select_Valid(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("2\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type:    InputSelect,
		Prompt:  "Pick one:",
		Options: []string{"alpha", "beta", "gamma"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Selected != 1 {
		t.Errorf("expected Selected=1 (beta), got %d", resp.Selected)
	}
}

func TestConsoleIO_Ask_Select_Invalid(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	// "99" is out of range → defaults to 0
	c := &ConsoleIO{W: &w, R: strings.NewReader("99\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type:    InputSelect,
		Prompt:  "Pick:",
		Options: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Selected != 0 {
		t.Errorf("out-of-range → Selected should be 0, got %d", resp.Selected)
	}
}

func TestConsoleIO_Ask_Form_Boolean(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("true\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "flag", Title: "Enable?", Type: InputFieldBoolean},
		},
	})
	if err != nil {
		t.Fatalf("Ask Form: %v", err)
	}
	if resp.Form["flag"] != true {
		t.Errorf("expected flag=true, got %v", resp.Form["flag"])
	}
}

func TestConsoleIO_Ask_Form_SingleSelect(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("2\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "mode", Type: InputFieldSingleSelect, Options: []string{"read", "write", "exec"}},
		},
	})
	if err != nil {
		t.Fatalf("Ask Form: %v", err)
	}
	if resp.Form["mode"] != "write" {
		t.Errorf("expected mode=write, got %v", resp.Form["mode"])
	}
}

func TestConsoleIO_Ask_Form_SingleSelect_Invalid(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("99\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "mode", Type: InputFieldSingleSelect, Options: []string{"a", "b"}},
		},
	})
	if err != nil {
		t.Fatalf("Ask Form: %v", err)
	}
	// invalid → first option
	if resp.Form["mode"] != "a" {
		t.Errorf("invalid selection → expect first option, got %v", resp.Form["mode"])
	}
}

func TestConsoleIO_Ask_Form_MultiSelect(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("1,3\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "items", Type: InputFieldMultiSelect, Options: []string{"x", "y", "z"}},
		},
	})
	if err != nil {
		t.Fatalf("Ask Form: %v", err)
	}
	choices, _ := resp.Form["items"].([]string)
	if len(choices) != 2 || choices[0] != "x" || choices[1] != "z" {
		t.Errorf("expected [x z], got %v", choices)
	}
}

func TestConsoleIO_Ask_Form_MultiSelect_Empty(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("\n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "items", Type: InputFieldMultiSelect, Options: []string{"a", "b"}},
		},
	})
	if err != nil {
		t.Fatalf("Ask Form: %v", err)
	}
	choices, _ := resp.Form["items"].([]string)
	if len(choices) != 0 {
		t.Errorf("empty input → 0 choices, got %v", choices)
	}
}

func TestConsoleIO_Ask_Form_String(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("  my value  \n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "note", Type: InputFieldString},
		},
	})
	if err != nil {
		t.Fatalf("Ask Form: %v", err)
	}
	if resp.Form["note"] != "my value" {
		t.Errorf("expected trimmed 'my value', got %v", resp.Form["note"])
	}
}

func TestConsoleIO_Ask_FreeText(t *testing.T) {
	ctx := testCtx()
	var w strings.Builder
	c := &ConsoleIO{W: &w, R: strings.NewReader("  answer  \n"), MaxResultLen: 300}
	resp, err := c.Ask(ctx, InputRequest{Type: InputFreeText, Prompt: "Enter text:"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if resp.Value != "answer" {
		t.Errorf("expected 'answer', got %q", resp.Value)
	}
}
