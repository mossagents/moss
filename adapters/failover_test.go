package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/retry"
)

func TestFailoverLLMComplete_FirstCandidateSuccess(t *testing.T) {
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: &fakeLLM{name: "primary"}},
		{profile: ModelProfile{Name: "secondary"}, llm: &fakeLLM{name: "secondary"}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	resp, err := llm.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "primary" {
		t.Fatalf("expected actual model primary, got %+v", resp.Metadata)
	}
	if len(resp.Metadata.Attempts) != 1 || resp.Metadata.Attempts[0].Outcome != "selected" {
		t.Fatalf("unexpected attempts: %+v", resp.Metadata.Attempts)
	}
}

func TestFailoverLLMComplete_FailsOverToSecondCandidate(t *testing.T) {
	primary := &syncSequenceLLM{results: []syncResult{{err: retryableErr(io.ErrUnexpectedEOF)}}}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: &fakeLLM{name: "secondary"}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	resp, err := llm.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "secondary" {
		t.Fatalf("expected actual model secondary, got %+v", resp.Metadata)
	}
	if got := atomic.LoadInt32(&primary.completeCalls); got != 1 {
		t.Fatalf("expected 1 primary call, got %d", got)
	}
	if len(resp.Metadata.Attempts) < 2 || resp.Metadata.Attempts[0].FailoverTo != "secondary" {
		t.Fatalf("unexpected failover attempts: %+v", resp.Metadata.Attempts)
	}
}

func TestFailoverLLMComplete_RetriesCandidateBeforeSwitching(t *testing.T) {
	primary := &syncSequenceLLM{results: []syncResult{
		{err: retryableErr(io.ErrUnexpectedEOF)},
		{err: retryableErr(io.ErrUnexpectedEOF)},
	}}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: &fakeLLM{name: "secondary"}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{
		MaxCandidates:         2,
		RetryConfig:           retry.Config{MaxRetries: 1},
		FailoverOnBreakerOpen: true,
	})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	resp, err := llm.Complete(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := atomic.LoadInt32(&primary.completeCalls); got != 2 {
		t.Fatalf("expected 2 primary attempts, got %d", got)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "secondary" {
		t.Fatalf("expected actual model secondary, got %+v", resp.Metadata)
	}
}

func TestFailoverLLMComplete_BreakerOpenSkipsPrimaryOnLaterCall(t *testing.T) {
	primary := &syncSequenceLLM{results: []syncResult{{err: retryableErr(io.ErrUnexpectedEOF)}}}
	secondary := &fakeLLM{name: "secondary"}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: secondary},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{
		MaxCandidates:         2,
		BreakerConfig:         &retry.BreakerConfig{MaxFailures: 1, ResetAfter: time.Hour},
		FailoverOnBreakerOpen: true,
	})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	if _, err := llm.Complete(context.Background(), port.CompletionRequest{}); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if _, err := llm.Complete(context.Background(), port.CompletionRequest{}); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if got := atomic.LoadInt32(&primary.completeCalls); got != 1 {
		t.Fatalf("expected breaker-open second call to skip primary, got %d primary calls", got)
	}
}

func TestFailoverLLMComplete_AllCandidatesFailReturnsAggregatedError(t *testing.T) {
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: &syncSequenceLLM{results: []syncResult{{err: retryableErr(io.ErrUnexpectedEOF)}}}},
		{profile: ModelProfile{Name: "secondary"}, llm: &syncSequenceLLM{results: []syncResult{{err: retryableErr(io.ErrUnexpectedEOF)}}}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	_, err = llm.Complete(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected failover exhaustion error")
	}
	if !strings.Contains(err.Error(), "primary") || !strings.Contains(err.Error(), "secondary") {
		t.Fatalf("expected aggregated candidate names in error, got %v", err)
	}
	var callErr *port.LLMCallError
	if !errorAs(err, &callErr) || len(callErr.Metadata.Attempts) < 2 {
		t.Fatalf("expected metadata attempts on error, got %v", err)
	}
}

func TestFailoverLLMStream_StartupFailureFallsThroughToNextCandidate(t *testing.T) {
	primary := &streamScriptLLM{streamErr: retryableErr(io.ErrUnexpectedEOF)}
	secondary := &streamScriptLLM{chunks: []port.StreamChunk{
		{Delta: "ok"},
		{Done: true, Usage: &port.TokenUsage{TotalTokens: 1}},
	}}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: secondary},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	iter, err := llm.Stream(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	output, err := drainStream(iter)
	if err != nil {
		t.Fatalf("drain stream: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	metaProvider, ok := iter.(port.MetadataStreamIterator)
	if !ok {
		t.Fatal("expected metadata stream iterator")
	}
	if meta := metaProvider.Metadata(); meta.ActualModel != "secondary" {
		t.Fatalf("expected actual model secondary, got %+v", meta)
	}
}

func TestFailoverLLMStream_PostEmissionErrorDoesNotFailover(t *testing.T) {
	primary := &streamScriptLLM{chunks: []port.StreamChunk{{Delta: "partial"}}, nextErr: retryableErr(io.ErrUnexpectedEOF)}
	secondary := &streamScriptLLM{chunks: []port.StreamChunk{
		{Delta: "ok"},
		{Done: true, Usage: &port.TokenUsage{TotalTokens: 1}},
	}}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: secondary},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	iter, err := llm.Stream(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunk, err := iter.Next()
	if err != nil || chunk.Delta != "partial" {
		t.Fatalf("expected first partial chunk, got chunk=%+v err=%v", chunk, err)
	}
	_, err = iter.Next()
	if err == nil {
		t.Fatal("expected post-emission stream error")
	}
	if got := atomic.LoadInt32(&secondary.streamCalls); got != 0 {
		t.Fatalf("expected no secondary stream failover after emission, got %d stream calls", got)
	}
	var callErr *port.LLMCallError
	if !errorAs(err, &callErr) || callErr.FallbackSafe {
		t.Fatalf("expected unsafe post-emission error, got %v", err)
	}
}

func TestFailoverLLMStream_NonStreamingCandidateFallsBackToSyncBeforeEmission(t *testing.T) {
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: &fakeLLM{name: "primary"}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 1, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	iter, err := llm.Stream(context.Background(), port.CompletionRequest{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	output, err := drainStream(iter)
	if err != nil {
		t.Fatalf("drain stream: %v", err)
	}
	if output != "response from primary" {
		t.Fatalf("output = %q, want response from primary", output)
	}
}

func TestFailoverLLMComplete_PlainErrorsDoNotFailover(t *testing.T) {
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: &syncSequenceLLM{results: []syncResult{{err: fmt.Errorf("bad api key")}}}},
		{profile: ModelProfile{Name: "secondary"}, llm: &fakeLLM{name: "secondary"}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	_, err = llm.Complete(context.Background(), port.CompletionRequest{})
	if err == nil {
		t.Fatal("expected plain error to stop without failover")
	}
	if strings.Contains(err.Error(), "secondary") {
		t.Fatalf("unexpected failover on plain error: %v", err)
	}
}

type syncResult struct {
	resp *port.CompletionResponse
	err  error
}

type syncSequenceLLM struct {
	results       []syncResult
	completeCalls int32
}

func (s *syncSequenceLLM) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	call := int(atomic.AddInt32(&s.completeCalls, 1)) - 1
	if call >= len(s.results) {
		return nil, context.DeadlineExceeded
	}
	return s.results[call].resp, s.results[call].err
}

type streamScriptLLM struct {
	chunks      []port.StreamChunk
	streamErr   error
	nextErr     error
	streamCalls int32
}

func (s *streamScriptLLM) Complete(_ context.Context, _ port.CompletionRequest) (*port.CompletionResponse, error) {
	return &port.CompletionResponse{
		Message:    port.Message{Role: port.RoleAssistant, Content: "sync"},
		StopReason: "end_turn",
	}, nil
}

func (s *streamScriptLLM) Stream(_ context.Context, _ port.CompletionRequest) (port.StreamIterator, error) {
	atomic.AddInt32(&s.streamCalls, 1)
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	return &scriptedStreamIterator{chunks: s.chunks, nextErr: s.nextErr}, nil
}

type scriptedStreamIterator struct {
	chunks  []port.StreamChunk
	nextErr error
	index   int
}

func (it *scriptedStreamIterator) Next() (port.StreamChunk, error) {
	if it.index < len(it.chunks) {
		chunk := it.chunks[it.index]
		it.index++
		return chunk, nil
	}
	if it.nextErr != nil {
		err := it.nextErr
		it.nextErr = nil
		return port.StreamChunk{}, err
	}
	return port.StreamChunk{}, io.EOF
}

func (it *scriptedStreamIterator) Close() error { return nil }

func drainStream(iter port.StreamIterator) (string, error) {
	var builder strings.Builder
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			return builder.String(), nil
		}
		if err != nil {
			return builder.String(), err
		}
		builder.WriteString(chunk.Delta)
	}
}

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}

func retryableErr(err error) error {
	return &port.LLMCallError{Err: err, Retryable: true}
}
