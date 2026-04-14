package model

import (
	"context"
	"errors"
	"io"
	"iter"
	"testing"
)

// ─── stub LLM ─────────────────────────────────────────────────────────────

type stubLLM struct {
	chunks []StreamChunk
	err    error
}

func (s *stubLLM) GenerateContent(_ context.Context, _ CompletionRequest) iter.Seq2[StreamChunk, error] {
	return func(yield func(StreamChunk, error) bool) {
		if s.err != nil {
			yield(StreamChunk{}, s.err)
			return
		}
		for _, c := range s.chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
}

// ─── LLMCallError ─────────────────────────────────────────────────────────

func TestLLMCallError_Error(t *testing.T) {
	e := &LLMCallError{Err: errors.New("context deadline exceeded")}
	if e.Error() != "context deadline exceeded" {
		t.Fatalf("unexpected: %s", e.Error())
	}
}

func TestLLMCallError_ErrorNil(t *testing.T) {
	var e *LLMCallError
	if e.Error() != "" {
		t.Fatalf("nil LLMCallError.Error() should be empty")
	}
}

func TestLLMCallError_Unwrap(t *testing.T) {
	sentinel := errors.New("upstream error")
	e := &LLMCallError{Err: sentinel}
	if !errors.Is(e, sentinel) {
		t.Fatal("errors.Is should unwrap to sentinel")
	}
}

func TestLLMCallError_UnwrapNil(t *testing.T) {
	var e *LLMCallError
	if e.Unwrap() != nil {
		t.Fatal("nil LLMCallError.Unwrap() should return nil")
	}
}

// ─── Complete ─────────────────────────────────────────────────────────────

func TestComplete_TextOnly(t *testing.T) {
	llm := &stubLLM{
		chunks: []StreamChunk{
			{Delta: "Hello, "},
			{Delta: "world!"},
			{Done: true, Usage: &TokenUsage{PromptTokens: 10, CompletionTokens: 5}},
		},
	}
	resp, err := Complete(context.Background(), llm, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := ContentPartsToPlainText(resp.Message.ContentParts)
	if text != "Hello, world!" {
		t.Fatalf("unexpected text: %q", text)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("unexpected stop reason: %s", resp.StopReason)
	}
}

func TestComplete_WithToolCalls(t *testing.T) {
	tc := &ToolCall{Name: "read_file", Arguments: []byte(`{"path":"/etc/hosts"}`)}
	llm := &stubLLM{
		chunks: []StreamChunk{
			{ToolCall: tc},
			{Done: true},
		},
	}
	resp, err := Complete(context.Background(), llm, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop reason should be tool_use when tool calls present, got: %s", resp.StopReason)
	}
}

func TestComplete_WithReasoning(t *testing.T) {
	llm := &stubLLM{
		chunks: []StreamChunk{
			{ReasoningDelta: "thinking..."},
			{Delta: "answer"},
			{Done: true},
		},
	}
	resp, err := Complete(context.Background(), llm, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reasoning := ContentPartsToReasoningText(resp.Message.ContentParts)
	if reasoning != "thinking..." {
		t.Fatalf("unexpected reasoning: %q", reasoning)
	}
}

func TestComplete_Error(t *testing.T) {
	sentinel := errors.New("LLM unavailable")
	llm := &stubLLM{err: sentinel}
	_, err := Complete(context.Background(), llm, CompletionRequest{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
}

func TestComplete_EmptyStream(t *testing.T) {
	llm := &stubLLM{}
	resp, err := Complete(context.Background(), llm, CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("empty stream should still produce end_turn, got: %s", resp.StopReason)
	}
}

// ─── ResponseToSeq ─────────────────────────────────────────────────────────

func TestResponseToSeq_NilResponse(t *testing.T) {
	var count int
	for _, err := range ResponseToSeq(nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
	}
	if count != 0 {
		t.Fatalf("nil response should yield 0 chunks, got %d", count)
	}
}

func TestResponseToSeq_TextContent(t *testing.T) {
	resp := &CompletionResponse{
		Message:    Message{Role: RoleAssistant, ContentParts: []ContentPart{TextPart("hi")}},
		Usage:      TokenUsage{PromptTokens: 1, CompletionTokens: 1},
		StopReason: "end_turn",
	}
	var chunks []StreamChunk
	for chunk, err := range ResponseToSeq(resp) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	last := chunks[len(chunks)-1]
	if !last.Done {
		t.Fatal("last chunk should be Done=true")
	}
	if last.Delta != "hi" {
		t.Fatalf("unexpected delta: %q", last.Delta)
	}
}

func TestResponseToSeq_WithToolCalls(t *testing.T) {
	tc := ToolCall{Name: "bash", Arguments: []byte(`{}`)}
	resp := &CompletionResponse{
		Message:   Message{Role: RoleAssistant},
		ToolCalls: []ToolCall{tc},
	}
	var toolCallChunks int
	for chunk, _ := range ResponseToSeq(resp) {
		if chunk.ToolCall != nil {
			toolCallChunks++
		}
	}
	if toolCallChunks != 1 {
		t.Fatalf("expected 1 tool call chunk, got %d", toolCallChunks)
	}
}

func TestResponseToSeq_WithReasoning(t *testing.T) {
	resp := &CompletionResponse{
		Message: Message{
			Role:         RoleAssistant,
			ContentParts: []ContentPart{ReasoningPart("think"), TextPart("answer")},
		},
	}
	var reasoningChunks int
	for chunk, _ := range ResponseToSeq(resp) {
		if chunk.ReasoningDelta != "" {
			reasoningChunks++
		}
	}
	if reasoningChunks != 1 {
		t.Fatalf("expected 1 reasoning chunk, got %d", reasoningChunks)
	}
}

// ─── SeqToIterator ─────────────────────────────────────────────────────────

func TestSeqToIterator_BasicIteration(t *testing.T) {
	chunks := []StreamChunk{
		{Delta: "chunk1"},
		{Delta: "chunk2"},
		{Done: true},
	}
	seq := func(yield func(StreamChunk, error) bool) {
		for _, c := range chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
	it := SeqToIterator(seq)
	defer it.Close()

	for i, want := range chunks {
		got, err := it.Next()
		if err != nil {
			t.Fatalf("chunk %d: unexpected error: %v", i, err)
		}
		if got.Delta != want.Delta || got.Done != want.Done {
			t.Fatalf("chunk %d: got %+v, want %+v", i, got, want)
		}
	}
	// After all chunks, should return io.EOF
	_, err := it.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after exhaustion, got %v", err)
	}
}

func TestSeqToIterator_Close(t *testing.T) {
	seq := func(yield func(StreamChunk, error) bool) {
		for {
			if !yield(StreamChunk{Delta: "x"}, nil) {
				return
			}
		}
	}
	it := SeqToIterator(seq)
	_, _ = it.Next() // consume one
	if err := it.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}
