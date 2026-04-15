package providers

import (
	"context"
	"errors"
	"fmt"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/retry"
	"io"
	"iter"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

	resp, err := model.Complete(context.Background(), llm, model.CompletionRequest{})
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

	resp, err := model.Complete(context.Background(), llm, model.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Metadata == nil || resp.Metadata.ActualModel != "secondary" {
		t.Fatalf("expected actual model secondary, got %+v", resp.Metadata)
	}
	if got := atomic.LoadInt32(&primary.generateCalls); got != 1 {
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

	resp, err := model.Complete(context.Background(), llm, model.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := atomic.LoadInt32(&primary.generateCalls); got != 2 {
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

	if _, err := model.Complete(context.Background(), llm, model.CompletionRequest{}); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if _, err := model.Complete(context.Background(), llm, model.CompletionRequest{}); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if got := atomic.LoadInt32(&primary.generateCalls); got != 1 {
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

	_, err = model.Complete(context.Background(), llm, model.CompletionRequest{})
	if err == nil {
		t.Fatal("expected failover exhaustion error")
	}
	if !strings.Contains(err.Error(), "primary") || !strings.Contains(err.Error(), "secondary") {
		t.Fatalf("expected aggregated candidate names in error, got %v", err)
	}
	var callErr *model.LLMCallError
	if !errorAs(err, &callErr) || len(callErr.Metadata.Attempts) < 2 {
		t.Fatalf("expected metadata attempts on error, got %v", err)
	}
}

func TestFailoverLLMStream_StartupFailureFallsThroughToNextCandidate(t *testing.T) {
	primary := &streamScriptLLM{startupErr: retryableErr(io.ErrUnexpectedEOF)}
	secondary := &streamScriptLLM{chunks: []model.StreamChunk{
		{Delta: "ok"},
		{Done: true, Usage: &model.TokenUsage{TotalTokens: 1}},
	}}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: secondary},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	output, meta, err := drainGenerateContent(llm, context.Background(), model.CompletionRequest{})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	if meta == nil || meta.ActualModel != "secondary" {
		t.Fatalf("expected actual model secondary, got %+v", meta)
	}
}

func TestFailoverLLMStream_PostEmissionErrorDoesNotFailover(t *testing.T) {
	primary := &streamScriptLLM{chunks: []model.StreamChunk{{Delta: "partial"}}, midStreamErr: retryableErr(io.ErrUnexpectedEOF)}
	secondary := &streamScriptLLM{chunks: []model.StreamChunk{
		{Delta: "ok"},
		{Done: true, Usage: &model.TokenUsage{TotalTokens: 1}},
	}}
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: primary},
		{profile: ModelProfile{Name: "secondary"}, llm: secondary},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 2, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	// Consume via pull-based iterator so we can observe partial + error.
	it := model.SeqToIterator(llm.GenerateContent(context.Background(), model.CompletionRequest{}))
	defer it.Close()
	chunk, err := it.Next()
	if err != nil || chunk.Delta != "partial" {
		t.Fatalf("expected first partial chunk, got chunk=%+v err=%v", chunk, err)
	}
	_, err = it.Next()
	if err == nil {
		t.Fatal("expected post-emission stream error")
	}
	if got := atomic.LoadInt32(&secondary.generateCalls); got != 0 {
		t.Fatalf("expected no secondary failover after emission, got %d calls", got)
	}
	var callErr *model.LLMCallError
	if !errorAs(err, &callErr) || callErr.FallbackSafe {
		t.Fatalf("expected unsafe post-emission error, got %v", err)
	}
}

func TestFailoverLLMGenerateContent_AllModelsWork(t *testing.T) {
	router := newTestRouter([]routedModel{
		{profile: ModelProfile{Name: "primary", IsDefault: true}, llm: &fakeLLM{name: "primary"}},
	}, 0)
	llm, err := NewFailoverLLM(router, FailoverConfig{MaxCandidates: 1, FailoverOnBreakerOpen: true})
	if err != nil {
		t.Fatalf("NewFailoverLLM: %v", err)
	}

	output, meta, err := drainGenerateContent(llm, context.Background(), model.CompletionRequest{})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if output != "response from primary" {
		t.Fatalf("output = %q, want response from primary", output)
	}
	if meta == nil || meta.ActualModel != "primary" {
		t.Fatalf("expected metadata with primary, got %+v", meta)
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

	_, err = model.Complete(context.Background(), llm, model.CompletionRequest{})
	if err == nil {
		t.Fatal("expected plain error to stop without failover")
	}
	if strings.Contains(err.Error(), "secondary") {
		t.Fatalf("unexpected failover on plain error: %v", err)
	}
}

type syncResult struct {
	resp *model.CompletionResponse
	err  error
}

// syncSequenceLLM yields errors or responses in sequence via GenerateContent.
type syncSequenceLLM struct {
	results       []syncResult
	generateCalls int32
}

func (s *syncSequenceLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	call := int(atomic.AddInt32(&s.generateCalls, 1)) - 1
	if call >= len(s.results) {
		return func(yield func(model.StreamChunk, error) bool) {
			yield(model.StreamChunk{}, context.DeadlineExceeded)
		}
	}
	r := s.results[call]
	if r.err != nil {
		return func(yield func(model.StreamChunk, error) bool) {
			yield(model.StreamChunk{}, r.err)
		}
	}
	return model.ResponseToSeq(r.resp)
}

// streamScriptLLM yields a sequence of chunks, optionally with startup or mid-stream errors.
type streamScriptLLM struct {
	chunks        []model.StreamChunk
	startupErr    error // error yielded as first chunk
	midStreamErr  error // error yielded after all chunks
	generateCalls int32
}

func (s *streamScriptLLM) GenerateContent(_ context.Context, _ model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	atomic.AddInt32(&s.generateCalls, 1)
	return func(yield func(model.StreamChunk, error) bool) {
		if s.startupErr != nil {
			yield(model.StreamChunk{}, s.startupErr)
			return
		}
		for _, chunk := range s.chunks {
			if !yield(chunk, nil) {
				return
			}
		}
		if s.midStreamErr != nil {
			yield(model.StreamChunk{}, s.midStreamErr)
		}
	}
}

// drainGenerateContent consumes all chunks from GenerateContent, accumulating deltas and metadata.
func drainGenerateContent(llm model.LLM, ctx context.Context, req model.CompletionRequest) (string, *model.LLMCallMetadata, error) {
	var builder strings.Builder
	var meta *model.LLMCallMetadata
	for chunk, err := range llm.GenerateContent(ctx, req) {
		if err != nil {
			return builder.String(), meta, err
		}
		builder.WriteString(chunk.Delta)
		if chunk.Metadata != nil {
			meta = chunk.Metadata
		}
	}
	return builder.String(), meta, nil
}

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}

func retryableErr(err error) error {
	return &model.LLMCallError{Err: err, Retryable: true}
}
