package kernel

import (
	"context"
	"iter"
	"sync"

	kerr "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/session"
)

type runResultCapture struct {
	mu     sync.Mutex
	result *session.LifecycleResult
}

func (c *runResultCapture) set(result *session.LifecycleResult) {
	if c == nil || result == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.result = cloneLifecycleResult(result)
}

func (c *runResultCapture) resultValue() *session.LifecycleResult {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneLifecycleResult(c.result)
}

// CollectRunAgentResult executes RunAgent to completion and returns the
// authoritative lifecycle result captured on the canonical agent path.
func CollectRunAgentResult(ctx context.Context, runner interface {
	RunAgent(context.Context, RunAgentRequest) iter.Seq2[*session.Event, error]
}, req RunAgentRequest) (*session.LifecycleResult, error) {
	capture := &runResultCapture{}
	prev := req.OnResult
	req.OnResult = func(result *session.LifecycleResult) {
		capture.set(result)
		if prev != nil {
			prev(result)
		}
	}
	for _, err := range runner.RunAgent(ctx, req) {
		if err != nil {
			return capture.resultValue(), err
		}
	}
	result := capture.resultValue()
	if result == nil {
		return nil, kerr.New(kerr.ErrValidation, "run agent did not produce a lifecycle result")
	}
	return result, nil
}

func cloneLifecycleResult(result *session.LifecycleResult) *session.LifecycleResult {
	if result == nil {
		return nil
	}
	cp := *result
	return &cp
}

func lifecycleResultFromLoop(result *loop.SessionResult) *session.LifecycleResult {
	if result == nil {
		return nil
	}
	return &session.LifecycleResult{
		Success:    result.Success,
		Output:     result.Output,
		Steps:      result.Steps,
		TokensUsed: result.TokensUsed,
		Error:      result.Error,
	}
}
