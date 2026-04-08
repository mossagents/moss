package providers

import (
	"context"
	"errors"
	"fmt"
	kerrors "github.com/mossagents/moss/kernel/errors"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/retry"
	"io"
	"strings"
	"sync"
	"time"
)

const defaultFailoverMaxCandidates = 2

// FailoverConfig 控制 router 候选链上的运行时 failover 行为。
type FailoverConfig struct {
	MaxCandidates         int
	RetryConfig           retry.Config
	BreakerConfig         *retry.BreakerConfig
	FailoverOnBreakerOpen bool
}

// FailoverLLM 在 router 候选链上执行逐候选 failover。
type FailoverLLM struct {
	router   *ModelRouter
	cfg      FailoverConfig
	mu       sync.Mutex
	breakers map[string]*retry.Breaker
}

func NewFailoverLLM(router *ModelRouter, cfg FailoverConfig) (*FailoverLLM, error) {
	if router == nil {
		return nil, fmt.Errorf("failover llm: router is required")
	}
	return &FailoverLLM{
		router:   router,
		cfg:      cfg,
		breakers: map[string]*retry.Breaker{},
	}, nil
}

func (f *FailoverLLM) Complete(ctx context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	candidates, err := f.candidates(req.Config.Requirements)
	if err != nil {
		return nil, err
	}

	attempts := make([]mdl.LLMCallAttempt, 0, len(candidates))
	maxRetries := f.maxRetries()
	lastModel := ""

	for idx, candidate := range candidates {
		lastModel = candidate.profile.Name
		skip, err := f.handleBreakerOpen(candidate.profile.Name, idx, candidates, &attempts)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}

		for candidateRetry := 0; candidateRetry <= maxRetries; candidateRetry++ {
			resp, err := candidate.llm.Complete(ctx, req)
			if err == nil {
				f.recordBreakerSuccess(candidate.profile.Name)
				attempts = append(attempts, mdl.LLMCallAttempt{
					CandidateModel: candidate.profile.Name,
					AttemptIndex:   idx + 1,
					CandidateRetry: candidateRetry,
					Outcome:        "selected",
				})
				return ensureResponseMetadata(resp, candidate.profile.Name, attempts), nil
			}

			f.recordBreakerFailure(candidate.profile.Name)
			failoverEligible := f.shouldFailover(err)
			attempt := mdl.LLMCallAttempt{
				CandidateModel: candidate.profile.Name,
				AttemptIndex:   idx + 1,
				CandidateRetry: candidateRetry,
				FailureReason:  err.Error(),
				Outcome:        "failed",
			}

			if f.canRetryCandidate(err) && candidateRetry < maxRetries {
				attempts = append(attempts, attempt)
				if sleepErr := f.sleepRetry(ctx, candidateRetry); sleepErr != nil {
					return nil, withMetadata(sleepErr, false, false, candidate.profile.Name, attempts)
				}
				continue
			}

			attempts = append(attempts, attempt)
			if failoverEligible && idx+1 < len(candidates) {
				attempts[len(attempts)-1].FailoverTo = candidates[idx+1].profile.Name
				break
			}
			if failoverEligible {
				return nil, exhaustedFailoverError(err, candidate.profile.Name, attempts)
			}
			return nil, withMetadata(err, llmErrorRetryable(err), false, candidate.profile.Name, attempts)
		}
	}

	return nil, exhaustedFailoverError(fmt.Errorf("llm failover exhausted"), lastModel, attempts)
}

func (f *FailoverLLM) Stream(ctx context.Context, req mdl.CompletionRequest) (mdl.StreamIterator, error) {
	candidates, err := f.candidates(req.Config.Requirements)
	if err != nil {
		return nil, err
	}
	return &failoverStreamIterator{
		parent:     f,
		ctx:        ctx,
		req:        req,
		candidates: candidates,
	}, nil
}

func (f *FailoverLLM) candidates(req *mdl.TaskRequirement) ([]routedModel, error) {
	candidates, err := f.router.orderedCandidates(req)
	if err != nil {
		return nil, err
	}
	limit := f.cfg.MaxCandidates
	if limit <= 0 {
		limit = defaultFailoverMaxCandidates
	}
	if limit < len(candidates) {
		candidates = append([]routedModel(nil), candidates[:limit]...)
	}
	return candidates, nil
}

func (f *FailoverLLM) maxRetries() int {
	if !f.cfg.RetryConfig.Enabled() {
		return 0
	}
	return f.cfg.RetryConfig.MaxRetriesOrDefault()
}

func (f *FailoverLLM) shouldFailover(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var callErr *mdl.LLMCallError
	if !errors.As(err, &callErr) || !callErr.Retryable {
		return false
	}
	return f.cfg.RetryConfig.ShouldRetryOrDefault(context.TODO(), callErr)
}

func (f *FailoverLLM) canRetryCandidate(err error) bool {
	if !f.cfg.RetryConfig.Enabled() {
		return false
	}
	return f.shouldFailover(err)
}

func (f *FailoverLLM) sleepRetry(ctx context.Context, candidateRetry int) error {
	if !f.cfg.RetryConfig.Enabled() {
		return nil
	}
	delay := f.cfg.RetryConfig.InitialDelayOrDefault()
	for i := 0; i < candidateRetry; i++ {
		delay = time.Duration(float64(delay) * f.cfg.RetryConfig.MultiplierOrDefault())
		if delay > f.cfg.RetryConfig.MaxDelayOrDefault() {
			delay = f.cfg.RetryConfig.MaxDelayOrDefault()
		}
	}
	if delay <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (f *FailoverLLM) breakerFor(model string) *retry.Breaker {
	if f.cfg.BreakerConfig == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.breakers[model]; ok {
		return existing
	}
	created := retry.NewBreaker(*f.cfg.BreakerConfig)
	f.breakers[model] = created
	return created
}

func (f *FailoverLLM) recordBreakerSuccess(model string) {
	if breaker := f.breakerFor(model); breaker != nil {
		breaker.RecordSuccess()
	}
}

func (f *FailoverLLM) recordBreakerFailure(model string) {
	if breaker := f.breakerFor(model); breaker != nil {
		breaker.RecordFailure()
	}
}

func (f *FailoverLLM) handleBreakerOpen(model string, idx int, candidates []routedModel, attempts *[]mdl.LLMCallAttempt) (bool, error) {
	breaker := f.breakerFor(model)
	if breaker == nil || breaker.Allow() {
		return false, nil
	}
	attempt := mdl.LLMCallAttempt{
		CandidateModel: model,
		AttemptIndex:   idx + 1,
		BreakerState:   "open",
		Outcome:        "skipped",
	}
	*attempts = append(*attempts, attempt)
	if f.cfg.FailoverOnBreakerOpen && idx+1 < len(candidates) {
		(*attempts)[len(*attempts)-1].FailoverTo = candidates[idx+1].profile.Name
		return true, nil
	}
	err := kerrors.New(kerrors.ErrLLMRejected, fmt.Sprintf("LLM circuit breaker is open for %s", model))
	if f.cfg.FailoverOnBreakerOpen {
		return false, exhaustedFailoverError(err, model, *attempts)
	}
	return false, withMetadata(err, false, false, model, *attempts)
}

type failoverStreamIterator struct {
	parent          *FailoverLLM
	ctx             context.Context
	req             mdl.CompletionRequest
	candidates      []routedModel
	attempts        []mdl.LLMCallAttempt
	currentIndex    int
	currentRetry    int
	currentIter     mdl.StreamIterator
	currentModel    string
	emitted         bool
	selectedModel   string
	completedStream bool
}

func (it *failoverStreamIterator) Next() (mdl.StreamChunk, error) {
	for {
		if it.currentIter == nil {
			if err := it.openCurrent(); err != nil {
				return mdl.StreamChunk{}, err
			}
		}

		chunk, err := it.currentIter.Next()
		if err == nil {
			if chunk.Delta != "" || chunk.ToolCall != nil {
				it.emitted = true
			}
			if chunk.Done {
				it.parent.recordBreakerSuccess(it.currentModel)
				it.selectedModel = it.currentModel
				it.completedStream = true
				it.recordSelectedAttempt()
			}
			return chunk, nil
		}
		if err == io.EOF {
			it.parent.recordBreakerSuccess(it.currentModel)
			it.selectedModel = it.currentModel
			if !it.completedStream {
				it.recordSelectedAttempt()
			}
			return mdl.StreamChunk{}, io.EOF
		}
		if it.emitted {
			it.parent.recordBreakerFailure(it.currentModel)
			return mdl.StreamChunk{}, withMetadata(err, false, false, it.currentModel, it.Metadata().Attempts)
		}

		_ = it.currentIter.Close()
		it.currentIter = nil
		it.parent.recordBreakerFailure(it.currentModel)
		if finalErr := it.handlePreEmissionError(err); finalErr != nil {
			return mdl.StreamChunk{}, finalErr
		}
	}
}

func (it *failoverStreamIterator) Close() error {
	if it.currentIter == nil {
		return nil
	}
	return it.currentIter.Close()
}

func (it *failoverStreamIterator) Metadata() mdl.LLMCallMetadata {
	meta := mdl.LLMCallMetadata{
		ActualModel: it.selectedModel,
		Attempts:    append([]mdl.LLMCallAttempt(nil), it.attempts...),
	}
	if strings.TrimSpace(meta.ActualModel) == "" {
		meta.ActualModel = it.currentModel
	}
	if provider, ok := it.currentIter.(mdl.MetadataStreamIterator); ok {
		meta = mergeResponseMetadata(meta, provider.Metadata())
	}
	return meta
}

func (it *failoverStreamIterator) openCurrent() error {
	for {
		if it.currentIndex >= len(it.candidates) {
			return exhaustedFailoverError(fmt.Errorf("llm failover exhausted"), it.currentModel, it.attempts)
		}

		candidate := it.candidates[it.currentIndex]
		it.currentModel = candidate.profile.Name
		if breaker := it.parent.breakerFor(candidate.profile.Name); breaker != nil && !breaker.Allow() {
			attempt := mdl.LLMCallAttempt{
				CandidateModel: candidate.profile.Name,
				AttemptIndex:   it.currentIndex + 1,
				BreakerState:   "open",
				Outcome:        "skipped",
			}
			it.attempts = append(it.attempts, attempt)
			if it.parent.cfg.FailoverOnBreakerOpen && it.currentIndex+1 < len(it.candidates) {
				it.attempts[len(it.attempts)-1].FailoverTo = it.candidates[it.currentIndex+1].profile.Name
				it.currentIndex++
				it.currentRetry = 0
				continue
			}
			err := kerrors.New(kerrors.ErrLLMRejected, fmt.Sprintf("LLM circuit breaker is open for %s", candidate.profile.Name))
			if it.parent.cfg.FailoverOnBreakerOpen {
				return exhaustedFailoverError(err, candidate.profile.Name, it.attempts)
			}
			return withMetadata(err, false, false, candidate.profile.Name, it.attempts)
		}

		sllm, ok := candidate.llm.(mdl.StreamingLLM)
		if !ok {
			fallbackErr := &mdl.LLMCallError{
				Err:          fmt.Errorf("model %q does not support streaming", candidate.profile.Name),
				Retryable:    false,
				FallbackSafe: true,
				Metadata:     mdl.LLMCallMetadata{ActualModel: candidate.profile.Name},
			}
			if it.trySyncFallback(candidate, fallbackErr) {
				return nil
			}
			if finalErr := it.handlePreEmissionError(fallbackErr); finalErr != nil {
				return finalErr
			}
			continue
		}

		streamIter, err := sllm.Stream(it.ctx, it.req)
		if err != nil {
			if it.trySyncFallback(candidate, err) {
				return nil
			}
			if finalErr := it.handlePreEmissionError(err); finalErr != nil {
				return finalErr
			}
			continue
		}
		it.currentIter = streamIter
		it.completedStream = false
		return nil
	}
}

func (it *failoverStreamIterator) handlePreEmissionError(err error) error {
	failoverEligible := it.parent.shouldFailover(err)
	attempt := mdl.LLMCallAttempt{
		CandidateModel: it.currentModel,
		AttemptIndex:   it.currentIndex + 1,
		CandidateRetry: it.currentRetry,
		FailureReason:  err.Error(),
		Outcome:        "failed",
	}

	if it.parent.canRetryCandidate(err) && it.currentRetry < it.parent.maxRetries() {
		it.attempts = append(it.attempts, attempt)
		if sleepErr := it.parent.sleepRetry(it.ctx, it.currentRetry); sleepErr != nil {
			return withMetadata(sleepErr, false, false, it.currentModel, it.attempts)
		}
		it.currentRetry++
		return nil
	}

	it.attempts = append(it.attempts, attempt)
	if failoverEligible && it.currentIndex+1 < len(it.candidates) {
		it.attempts[len(it.attempts)-1].FailoverTo = it.candidates[it.currentIndex+1].profile.Name
		it.currentIndex++
		it.currentRetry = 0
		return nil
	}
	if failoverEligible {
		return exhaustedFailoverError(err, it.currentModel, it.attempts)
	}
	return withMetadata(err, llmErrorRetryable(err), false, it.currentModel, it.attempts)
}

func (it *failoverStreamIterator) trySyncFallback(candidate routedModel, cause error) bool {
	if !llmErrorFallbackSafe(cause) {
		return false
	}
	resp, err := candidate.llm.Complete(it.ctx, it.req)
	if err != nil {
		return false
	}
	resp = ensureResponseMetadata(resp, candidate.profile.Name, it.attempts)
	it.currentIter = newCompletionIterator(resp)
	it.currentModel = candidate.profile.Name
	it.completedStream = false
	return true
}

func (it *failoverStreamIterator) recordSelectedAttempt() {
	if len(it.attempts) > 0 {
		last := it.attempts[len(it.attempts)-1]
		if last.CandidateModel == it.currentModel && last.Outcome == "selected" && last.CandidateRetry == it.currentRetry {
			return
		}
	}
	it.attempts = append(it.attempts, mdl.LLMCallAttempt{
		CandidateModel: it.currentModel,
		AttemptIndex:   it.currentIndex + 1,
		CandidateRetry: it.currentRetry,
		Outcome:        "selected",
	})
}

type failoverExhaustedError struct {
	cause    error
	attempts []mdl.LLMCallAttempt
}

func (e *failoverExhaustedError) Error() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, len(e.attempts))
	for _, attempt := range e.attempts {
		if attempt.FailureReason == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s[%d]: %s", attempt.CandidateModel, attempt.CandidateRetry, attempt.FailureReason))
	}
	if len(parts) == 0 {
		if e.cause != nil {
			return "llm failover exhausted: " + e.cause.Error()
		}
		return "llm failover exhausted"
	}
	return "llm failover exhausted: " + strings.Join(parts, "; ")
}

func (e *failoverExhaustedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func exhaustedFailoverError(err error, model string, attempts []mdl.LLMCallAttempt) error {
	return withMetadata(&failoverExhaustedError{
		cause:    err,
		attempts: append([]mdl.LLMCallAttempt(nil), attempts...),
	}, false, false, model, attempts)
}

func ensureResponseMetadata(resp *mdl.CompletionResponse, model string, attempts []mdl.LLMCallAttempt) *mdl.CompletionResponse {
	if resp == nil {
		return nil
	}
	base := mdl.LLMCallMetadata{
		ActualModel: model,
		Attempts:    append([]mdl.LLMCallAttempt(nil), attempts...),
	}
	if resp.Metadata != nil {
		merged := mergeResponseMetadata(base, *resp.Metadata)
		resp.Metadata = &merged
		return resp
	}
	resp.Metadata = &base
	return resp
}

func mergeResponseMetadata(base, overlay mdl.LLMCallMetadata) mdl.LLMCallMetadata {
	if strings.TrimSpace(overlay.ActualModel) != "" {
		base.ActualModel = overlay.ActualModel
	}
	if len(overlay.Attempts) > 0 {
		base.Attempts = append(base.Attempts, overlay.Attempts...)
	}
	return base
}

func withMetadata(err error, retryable, fallbackSafe bool, model string, attempts []mdl.LLMCallAttempt) error {
	if err == nil {
		return nil
	}
	metadata := mdl.LLMCallMetadata{
		ActualModel: model,
		Attempts:    append([]mdl.LLMCallAttempt(nil), attempts...),
	}
	if callErr, ok := err.(*mdl.LLMCallError); ok {
		merged := *callErr
		merged.Metadata = mergeResponseMetadata(metadata, merged.Metadata)
		return &merged
	}
	return &mdl.LLMCallError{
		Err:          err,
		Retryable:    retryable,
		FallbackSafe: fallbackSafe,
		Metadata:     metadata,
	}
}

func llmErrorRetryable(err error) bool {
	var callErr *mdl.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.Retryable
	}
	return true
}

func llmErrorFallbackSafe(err error) bool {
	var callErr *mdl.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.FallbackSafe
	}
	return false
}

type completionIterator struct {
	resp          *mdl.CompletionResponse
	metadata      mdl.LLMCallMetadata
	index         int
	sentReasoning bool
	sentDone      bool
}

func newCompletionIterator(resp *mdl.CompletionResponse) mdl.StreamIterator {
	meta := mdl.LLMCallMetadata{}
	if resp != nil && resp.Metadata != nil {
		meta = *resp.Metadata
	}
	return &completionIterator{resp: resp, metadata: meta}
}

func (it *completionIterator) Next() (mdl.StreamChunk, error) {
	if it.resp == nil {
		return mdl.StreamChunk{}, io.EOF
	}
	if !it.sentReasoning {
		it.sentReasoning = true
		if reasoning := mdl.ContentPartsToReasoningText(it.resp.Message.ContentParts); reasoning != "" {
			return mdl.StreamChunk{ReasoningDelta: reasoning}, nil
		}
	}
	if it.index < len(it.resp.ToolCalls) {
		call := it.resp.ToolCalls[it.index]
		it.index++
		return mdl.StreamChunk{ToolCall: &call}, nil
	}
	if !it.sentDone {
		it.sentDone = true
		content := mdl.ContentPartsToPlainText(it.resp.Message.ContentParts)
		if len(it.resp.ToolCalls) == 0 && content != "" {
			return mdl.StreamChunk{Delta: content, Done: true, Usage: &it.resp.Usage}, nil
		}
		return mdl.StreamChunk{Done: true, Usage: &it.resp.Usage}, nil
	}
	return mdl.StreamChunk{}, io.EOF
}

func (it *completionIterator) Close() error { return nil }

func (it *completionIterator) Metadata() mdl.LLMCallMetadata { return it.metadata }
