package loop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	mdl "github.com/mossagents/moss/kernel/model"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/logging"
)

func (l *AgentLoop) withSideEffectsLock(fn func()) {
	l.sidefxMu.Lock()
	defer l.sidefxMu.Unlock()
	fn()
}

func (l *AgentLoop) toolSpecs(plan TurnPlan) []mdl.ToolSpec {
	allowed := allowedToolNames(plan.ToolRoute)
	if len(allowed) == 0 {
		return nil
	}
	tools := tool.Scoped(l.Tools, allowed).List()
	specs := make([]mdl.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = mdl.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return specs
}

func (l *AgentLoop) toolAllowed(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if len(l.currentTurn.ToolRoute) == 0 {
		return true
	}
	for _, decision := range l.currentTurn.ToolRoute {
		if decision.Name != name {
			continue
		}
		return decision.Status != ToolRouteHidden
	}
	return false
}

func (l *AgentLoop) nextEventID(prefix string) string {
	seq := atomic.AddUint64(&l.eventSeq, 1)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "evt"
	}
	runID := strings.TrimSpace(l.RunID)
	if runID == "" {
		runID = "run"
	}
	return runID + "-" + prefix + "-" + strconv.FormatUint(seq, 10)
}

func (l *AgentLoop) executionEventBase(sess *session.Session, eventType kobs.ExecutionEventType, phase, actor, payloadKind string) kobs.ExecutionEvent {
	return kobs.ExecutionEvent{
		Type:         eventType,
		EventID:      l.nextEventID(string(eventType)),
		EventVersion: 1,
		RunID:        strings.TrimSpace(l.RunID),
		TurnID:       strings.TrimSpace(l.currentTurn.TurnID),
		SessionID:    sessionIDOf(sess),
		Timestamp:    time.Now().UTC(),
		Phase:        strings.TrimSpace(phase),
		Actor:        strings.TrimSpace(actor),
		PayloadKind:  strings.TrimSpace(payloadKind),
	}
}

func sessionIDOf(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.ID)
}

func (l *AgentLoop) recordBreakerSuccess() {
	if b := l.Config.LLMBreaker; b != nil {
		b.RecordSuccess()
	}
}

func (l *AgentLoop) recordBreakerFailure() {
	if b := l.Config.LLMBreaker; b != nil {
		b.RecordFailure()
	}
}

// safeChain returns a non-nil Registry, using the loop's Chain or a shared empty fallback.
var emptyRegistry = hooks.NewRegistry()

func (l *AgentLoop) safeChain() *hooks.Registry {
	if l.Chain != nil {
		return l.Chain
	}
	return emptyRegistry
}

func (l *AgentLoop) runErrorHook(ctx context.Context, sess *session.Session, err error) {
	if l.Chain == nil {
		return
	}
	ev := &hooks.ErrorEvent{
		Session:  sess,
		Error:    err,
		IO:       l.IO,
		Observer: l.observer(),
	}
	if runErr := l.Chain.OnError.Run(ctx, ev); runErr != nil {
		logging.GetLogger().DebugContext(ctx, "error hook failed", "session_id", sess.ID, "error", runErr)
	}
}

func (l *AgentLoop) emitLifecycle(ctx context.Context, event session.LifecycleEvent) {
	if l.LifecycleHook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			sessionID := ""
			if event.Session != nil {
				sessionID = event.Session.ID
			}
			err := fmt.Errorf("session lifecycle hook panic: %v", r)
			slog.Default().ErrorContext(ctx, "session lifecycle hook panic",
				slog.String("stage", string(event.Stage)),
				slog.String("session_id", sessionID),
				slog.Any("panic", r),
			)
			kobs.ObserveError(context.Background(), l.observer(), kobs.ErrorEvent{
				SessionID: sessionID,
				Phase:     "session_lifecycle_hook",
				Error:     err,
				Message:   err.Error(),
			})
		}
	}()
	l.LifecycleHook(ctx, event)
}

func (l *AgentLoop) fail(ctx context.Context, sess *session.Session, usage mdl.TokenUsage, err error) *SessionResult {
	eventType := kobs.ExecutionRunFailed
	stage := session.LifecycleFailed
	if errors.Is(err, context.Canceled) || sess.Status == session.StatusCancelled {
		sess.Status = session.StatusCancelled
		eventType = kobs.ExecutionRunCancelled
		stage = session.LifecycleCancelled
	} else {
		sess.Status = session.StatusFailed
	}
	sess.EndedAt = time.Now()
	runEvent := l.executionEventBase(sess, eventType, "run", "runtime", "run")
	runEvent.Error = err.Error()
	runEvent.Data = map[string]any{
		"steps":  sess.Budget.UsedStepsValue(),
		"tokens": usage.TotalTokens,
	}
	appendExecutionErrorMetadata(&runEvent, err)
	kobs.ObserveExecutionEvent(context.Background(), l.observer(), runEvent)
	result := &SessionResult{
		SessionID:  sess.ID,
		Success:    false,
		Steps:      sess.Budget.UsedStepsValue(),
		TokensUsed: usage,
		Error:      err.Error(),
	}
	l.emitLifecycle(ctx, session.LifecycleEvent{
		Stage:   stage,
		Session: sess,
		Result: &session.LifecycleResult{
			Success:    false,
			Steps:      sess.Budget.UsedStepsValue(),
			TokensUsed: usage,
			Error:      err.Error(),
		},
		Error:     err,
		Timestamp: sess.EndedAt.UTC(),
	})
	return result
}

// injectCompressionHooks 根据 LoopConfig.ContextCompression 配置自动注入压缩 hook。
// 幂等：无论 Run() 被调用多少次，每个 AgentLoop 实例只注入一次。
// 若已通过 kernel.OnBeforeLLM() 手动注册压缩 hook，不建议同时设置 Strategy，以避免双重压缩。
func (l *AgentLoop) injectCompressionHooks() {
	cfg := l.Config.ContextCompression
	if cfg.Strategy == "" || cfg.MaxContextTokens <= 0 {
		return
	}
	if l.compressionInjected {
		return
	}
	l.compressionInjected = true

	if l.Chain == nil {
		l.Chain = hooks.NewRegistry()
	}
	switch cfg.Strategy {
	case CompressionTruncate:
		l.Chain.BeforeLLM.On(builtins.AutoTruncate(builtins.TruncateConfig{
			MaxContextTokens: cfg.MaxContextTokens,
			KeepRecent:       cfg.KeepRecent,
			Tokenizer:        cfg.Tokenizer,
		}))
	case CompressionSummary:
		l.Chain.BeforeLLM.On(builtins.AutoSummarize(builtins.SummarizeConfig{
			LLM:              l.LLM,
			MaxContextTokens: cfg.MaxContextTokens,
			KeepRecent:       cfg.KeepRecent,
			SummaryPrompt:    cfg.SummaryPrompt,
			MaxSummaryTokens: cfg.MaxSummaryTokens,
			Tokenizer:        cfg.Tokenizer,
		}))
	case CompressionSliding:
		winSize := cfg.WindowSize
		if winSize <= 0 {
			winSize = 30
		}
		l.Chain.BeforeLLM.On(builtins.SlidingWindow(builtins.SlidingWindowConfig{
			WindowSize:       winSize,
			MaxContextTokens: cfg.MaxContextTokens,
			Tokenizer:        cfg.Tokenizer,
		}))
	case CompressionPriority:
		l.Chain.BeforeLLM.On(builtins.PriorityCompress(builtins.PriorityConfig{
			MaxContextTokens: cfg.MaxContextTokens,
			KeepRecent:       cfg.KeepRecent,
			MinScore:         cfg.MinScore,
			Tokenizer:        cfg.Tokenizer,
		}))
	}
}
