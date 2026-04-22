package kernel

import (
	"context"
	"iter"
	"sync"

	kerr "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/model"
	kruntime "github.com/mossagents/moss/kernel/runtime"
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
	if result.BudgetExhausted != nil {
		detail := *result.BudgetExhausted
		cp.BudgetExhausted = &detail
	}
	return &cp
}

func lifecycleResultFromLoop(result *loop.SessionResult) *session.LifecycleResult {
	if result == nil {
		return nil
	}
	var detail *session.BudgetExhaustedDetail
	if result.BudgetExhausted != nil {
		cloned := *result.BudgetExhausted
		detail = &cloned
	}
	return &session.LifecycleResult{
		Success:         result.Success,
		Status:          result.Status,
		Output:          result.Output,
		Steps:           result.Steps,
		TokensUsed:      result.TokensUsed,
		Error:           result.Error,
		BudgetExhausted: detail,
	}
}

// CollectRunAgentFromBlueprint 执行 RunAgentFromBlueprint 到完成并返回 LifecycleResult。
// 它是 CollectRunAgentResult 的 blueprint 路径对应版本。
func CollectRunAgentFromBlueprint(
	ctx context.Context,
	k *Kernel,
	bp kruntime.SessionBlueprint,
	layers []kruntime.PromptLayerProvider,
	agent Agent,
	userMsg *model.Message,
	userIO io.UserIO,
) (*session.LifecycleResult, error) {
	capture := &runResultCapture{}
	for _, err := range k.RunAgentFromBlueprint(ctx, bp, layers, agent, userMsg, userIO,
		WithBlueprintOnResult(capture.set),
	) {
		if err != nil {
			return capture.resultValue(), err
		}
	}
	result := capture.resultValue()
	if result == nil {
		return nil, kerr.New(kerr.ErrValidation, "run agent from blueprint did not produce a lifecycle result")
	}
	return result, nil
}
