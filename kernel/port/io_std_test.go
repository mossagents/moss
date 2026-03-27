package port

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNoOpIO_Send(t *testing.T) {
	io := &NoOpIO{}
	err := io.Send(context.Background(), OutputMessage{Type: OutputText, Content: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNoOpIO_Ask(t *testing.T) {
	io := &NoOpIO{}

	// Confirm defaults to false (safe default)
	resp, err := io.Ask(context.Background(), InputRequest{Type: InputConfirm, Prompt: "ok?"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Approved {
		t.Error("NoOpIO should not approve by default")
	}

	// Select defaults to 0
	resp, err = io.Ask(context.Background(), InputRequest{Type: InputSelect, Options: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Selected != 0 {
		t.Errorf("expected Selected=0, got %d", resp.Selected)
	}

	resp, err = io.Ask(context.Background(), InputRequest{
		Type: InputForm,
		Fields: []InputField{
			{Name: "db", Type: InputFieldSingleSelect, Options: []string{"pg", "mysql"}},
			{Name: "cache", Type: InputFieldBoolean},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Form["db"] != "pg" {
		t.Fatalf("expected db=pg, got %v", resp.Form["db"])
	}
	if resp.Form["cache"] != false {
		t.Fatalf("expected cache=false, got %v", resp.Form["cache"])
	}
}

func TestPrintfIO_Send(t *testing.T) {
	var buf bytes.Buffer
	io := NewPrintfIO(&buf)

	tests := []struct {
		msg      OutputMessage
		contains string
	}{
		{OutputMessage{Type: OutputText, Content: "hello"}, "hello"},
		{OutputMessage{Type: OutputStream, Content: "chunk"}, "chunk"},
		{OutputMessage{Type: OutputProgress, Content: "working"}, "working"},
		{OutputMessage{Type: OutputToolStart, Content: "run_command"}, "run_command"},
		{OutputMessage{Type: OutputToolResult, Content: "done"}, "done"},
		{OutputMessage{Type: OutputToolResult, Content: "failed", Meta: map[string]any{"is_error": true}}, "failed"},
	}

	for _, tt := range tests {
		buf.Reset()
		if err := io.Send(context.Background(), tt.msg); err != nil {
			t.Fatalf("Send(%v): %v", tt.msg.Type, err)
		}
		if !strings.Contains(buf.String(), tt.contains) {
			t.Errorf("Send(%v): output %q should contain %q", tt.msg.Type, buf.String(), tt.contains)
		}
	}
}

func TestPrintfIO_Ask(t *testing.T) {
	var buf bytes.Buffer
	io := NewPrintfIO(&buf)

	resp, err := io.Ask(context.Background(), InputRequest{Type: InputConfirm, Prompt: "proceed?"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Approved {
		t.Error("PrintfIO should auto-approve confirms")
	}
	if !strings.Contains(buf.String(), "proceed?") {
		t.Error("PrintfIO should print the prompt")
	}
}

func TestBufferIO_Collect(t *testing.T) {
	bio := NewBufferIO()
	ctx := context.Background()

	_ = bio.Send(ctx, OutputMessage{Type: OutputText, Content: "msg1"})
	_ = bio.Send(ctx, OutputMessage{Type: OutputStream, Content: "stream"})
	_ = bio.Send(ctx, OutputMessage{Type: OutputText, Content: "msg2"})

	if len(bio.Sent) != 3 {
		t.Fatalf("expected 3 sent messages, got %d", len(bio.Sent))
	}

	texts := bio.SentTexts()
	if len(texts) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(texts))
	}
	if texts[0] != "msg1" || texts[1] != "msg2" {
		t.Errorf("unexpected texts: %v", texts)
	}
}

func TestBufferIO_Ask(t *testing.T) {
	bio := NewBufferIO()
	ctx := context.Background()

	// Default behavior
	resp, err := bio.Ask(ctx, InputRequest{Type: InputConfirm})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Approved {
		t.Error("default BufferIO should approve")
	}

	// Custom AskFunc
	bio.AskFunc = func(req InputRequest) InputResponse {
		return InputResponse{Value: "custom"}
	}
	resp, err = bio.Ask(ctx, InputRequest{Type: InputFreeText, Prompt: "name?"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Value != "custom" {
		t.Errorf("expected custom, got %q", resp.Value)
	}
	if len(bio.Asked) != 2 {
		t.Errorf("expected 2 asked, got %d", len(bio.Asked))
	}
}

func TestBufferIO_ThreadSafe(t *testing.T) {
	bio := NewBufferIO()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = bio.Send(ctx, OutputMessage{Type: OutputText, Content: "concurrent"})
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_ = bio.Send(ctx, OutputMessage{Type: OutputText, Content: "main"})
	}
	<-done

	if len(bio.Sent) != 200 {
		t.Errorf("expected 200, got %d", len(bio.Sent))
	}
}
