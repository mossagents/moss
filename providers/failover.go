package providers

import (
	"context"
	"errors"
	"fmt"
	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/retry"
	"io"
	"iter"
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

func (f *FailoverLLM) GenerateContent(ctx context.Context, req model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	return func(yield func(model.StreamChunk, error) bool) {
		candidates, err := f.candidates(req.Config.Requirements)
		if err != nil {
			yield(model.StreamChunk{}, err)
			return
		}
		it := &failoverStreamIterator{
			parent:     f,
			ctx:        ctx,
			req:        req,
			candidates: candidates,
		}
		defer it.Close()

		for {
			chunk, err := it.Next()
			if err == io.EOF {
				return
			}
			if err != nil {
				yield(model.StreamChunk{}, err)
				return
			}
			// Attach metadata to final chunk.
			if chunk.Done {
				meta := it.Metadata()
				chunk.Metadata = &meta
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func (f *FailoverLLM) candidates(req *model.TaskRequirement) ([]routedModel, error) {
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
	var callErr *model.LLMCallError
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

func (f *FailoverLLM) handleBreakerOpen(modelName string, idx int, candidates []routedModel, attempts *[]model.LLMCallAttempt) (bool, error) {
	breaker := f.breakerFor(modelName)
	if breaker == nil || breaker.Allow() {
		return false, nil
	}
	attempt := model.LLMCallAttempt{
		CandidateModel: modelName,
		AttemptIndex:   idx + 1,
		BreakerState:   "open",
		Outcome:        "skipped",
	}
	*attempts = append(*attempts, attempt)
	if f.cfg.FailoverOnBreakerOpen && idx+1 < len(candidates) {
		(*attempts)[len(*attempts)-1].FailoverTo = candidates[idx+1].profile.Name
		return true, nil
	}
	err := kerrors.New(kerrors.ErrLLMRejected, fmt.Sprintf("LLM circuit breaker is open for %s", modelName))
	if f.cfg.FailoverOnBreakerOpen {
		return false, exhaustedFailoverError(err, modelName, *attempts)
	}
	return false, withMetadata(err, false, false, modelName, *attempts)
}

type failoverStreamIterator struct {
	parent          *FailoverLLM
	ctx             context.Context
	req             model.CompletionRequest
	candidates      []routedModel
	attempts        []model.LLMCallAttempt
	currentIndex    int
	currentRetry    int
	currentIter     model.StreamIterator
	currentModel    string
	emitted         bool
	selectedModel   string
	completedStream bool
}

func (it *failoverStreamIterator) Next() (model.StreamChunk, error) {
	for {
		if it.currentIter == nil {
			if err := it.openCurrent(); err != nil {
				return model.StreamChunk{}, err
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
			return model.StreamChunk{}, io.EOF
		}
		if it.emitted {
			it.parent.recordBreakerFailure(it.currentModel)
			return model.StreamChunk{}, withMetadata(err, false, false, it.currentModel, it.Metadata().Attempts)
		}

		_ = it.currentIter.Close()
		it.currentIter = nil
		it.parent.recordBreakerFailure(it.currentModel)
		if finalErr := it.handlePreEmissionError(err); finalErr != nil {
			return model.StreamChunk{}, finalErr
		}
	}
}

func (it *failoverStreamIterator) Close() error {
	if it.currentIter == nil {
		return nil
	}
	return it.currentIter.Close()
}

func (it *failoverStreamIterator) Metadata() model.LLMCallMetadata {
	meta := model.LLMCallMetadata{
		ActualModel: it.selectedModel,
		Attempts:    append([]model.LLMCallAttempt(nil), it.attempts...),
	}
	if strings.TrimSpace(meta.ActualModel) == "" {
		meta.ActualModel = it.currentModel
	}
	if provider, ok := it.currentIter.(model.MetadataStreamIterator); ok {
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
			attempt := model.LLMCallAttempt{
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

		// Use unified GenerateContent and convert to pull-based iterator.
		streamIter := model.SeqToIterator(candidate.llm.GenerateContent(it.ctx, it.req))
		// Probe the first chunk to detect startup errors before committing.
		firstChunk, firstErr := streamIter.Next()
		if firstErr != nil && firstErr != io.EOF {
			_ = streamIter.Close()
			it.parent.recordBreakerFailure(candidate.profile.Name)
			if finalErr := it.handlePreEmissionError(firstErr); finalErr != nil {
				return finalErr
			}
			continue
		}
		// Wrap with a prefetched iterator that replays the first result.
		it.currentIter = &prefetchedIterator{first: firstChunk, firstErr: firstErr, inner: streamIter}
		it.completedStream = false
		return nil
	}
}

func (it *failoverStreamIterator) handlePreEmissionError(err error) error {
	failoverEligible := it.parent.shouldFailover(err)
	attempt := model.LLMCallAttempt{
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

// prefetchedIterator wraps a StreamIterator and replays the first chunk that was
// already consumed during probe (in openCurrent) before delegating to the inner iterator.
type prefetchedIterator struct {
	first    model.StreamChunk
	firstErr error
	consumed bool
	inner    model.StreamIterator
}

func (p *prefetchedIterator) Next() (model.StreamChunk, error) {
	if !p.consumed {
		p.consumed = true
		return p.first, p.firstErr
	}
	return p.inner.Next()
}

func (p *prefetchedIterator) Close() error {
	return p.inner.Close()
}

func (it *failoverStreamIterator) recordSelectedAttempt() {
	if len(it.attempts) > 0 {
		last := it.attempts[len(it.attempts)-1]
		if last.CandidateModel == it.currentModel && last.Outcome == "selected" && last.CandidateRetry == it.currentRetry {
			return
		}
	}
	it.attempts = append(it.attempts, model.LLMCallAttempt{
		CandidateModel: it.currentModel,
		AttemptIndex:   it.currentIndex + 1,
		CandidateRetry: it.currentRetry,
		Outcome:        "selected",
	})
}

type failoverExhaustedError struct {
	cause    error
	attempts []model.LLMCallAttempt
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

func exhaustedFailoverError(err error, modelName string, attempts []model.LLMCallAttempt) error {
	return withMetadata(&failoverExhaustedError{
		cause:    err,
		attempts: append([]model.LLMCallAttempt(nil), attempts...),
	}, false, false, modelName, attempts)
}

func ensureResponseMetadata(resp *model.CompletionResponse, modelName string, attempts []model.LLMCallAttempt) *model.CompletionResponse {
	if resp == nil {
		return nil
	}
	base := model.LLMCallMetadata{
		ActualModel: modelName,
		Attempts:    append([]model.LLMCallAttempt(nil), attempts...),
	}
	if resp.Metadata != nil {
		merged := mergeResponseMetadata(base, *resp.Metadata)
		resp.Metadata = &merged
		return resp
	}
	resp.Metadata = &base
	return resp
}

func mergeResponseMetadata(base, overlay model.LLMCallMetadata) model.LLMCallMetadata {
	if strings.TrimSpace(overlay.ActualModel) != "" {
		base.ActualModel = overlay.ActualModel
	}
	if len(overlay.Attempts) > 0 {
		base.Attempts = append(base.Attempts, overlay.Attempts...)
	}
	return base
}

func withMetadata(err error, retryable, fallbackSafe bool, modelName string, attempts []model.LLMCallAttempt) error {
	if err == nil {
		return nil
	}
	metadata := model.LLMCallMetadata{
		ActualModel: modelName,
		Attempts:    append([]model.LLMCallAttempt(nil), attempts...),
	}
	if callErr, ok := err.(*model.LLMCallError); ok {
		merged := *callErr
		merged.Metadata = mergeResponseMetadata(metadata, merged.Metadata)
		return &merged
	}
	return &model.LLMCallError{
		Err:          err,
		Retryable:    retryable,
		FallbackSafe: fallbackSafe,
		Metadata:     metadata,
	}
}

func llmErrorRetryable(err error) bool {
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.Retryable
	}
	return true
}

func llmErrorFallbackSafe(err error) bool {
	var callErr *model.LLMCallError
	if errors.As(err, &callErr) {
		return callErr.FallbackSafe
	}
	return false
}


