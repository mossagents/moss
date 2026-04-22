package kernel

// runtime_ops.go — 阶段 2-5 的 kernel-centric runtime 操作 API。
//
// 这些方法与旧路径（RunAgent / session.Session）并存（阶段 1-2），
// 阶段 3 开始接管主链路，阶段 4 完成后删除旧路径。
//
// 所有方法都要求 WithEventStore 已配置，否则返回 ErrValidation。

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
)

// ────────────────────────────────────────────────────────────────────
// 阶段 2：EventStore 恢复面 reader API
// ────────────────────────────────────────────────────────────────────

// LoadRuntimeSession 从 EventStore 加载并返回指定 session 的当前物化视图。
// 等同于对 EventStore.LoadSessionView 的直接委托，提供 kernel 级入口。
// 需要 WithEventStore 已配置，否则返回 ErrValidation。
func (k *Kernel) LoadRuntimeSession(ctx context.Context, sessionID string) (*kruntime.MaterializedState, error) {
	if k.eventStore == nil {
		return nil, errors.New(errors.ErrValidation, "LoadRuntimeSession requires an EventStore (use kernel.WithEventStore())")
	}
	return k.eventStore.LoadSessionView(ctx, sessionID)
}

// ListRuntimeResumeCandidates 列出满足 filter 条件的可恢复 session。
// 需要 WithEventStore 已配置，否则返回 ErrValidation。
func (k *Kernel) ListRuntimeResumeCandidates(ctx context.Context, filter kruntime.ResumeCandidateFilter) ([]kruntime.ResumeCandidate, error) {
	if k.eventStore == nil {
		return nil, errors.New(errors.ErrValidation, "ListRuntimeResumeCandidates requires an EventStore (use kernel.WithEventStore())")
	}
	return k.eventStore.ListResumeCandidates(ctx, filter)
}

// ResumeRuntimeSession 从 EventStore 恢复已有 session 的蓝图。
// 加载原始 session_created 事件中持久化的 canonical blueprint，
// 供调用方继续向该 session 追加新 turn 事件。
// 实现 §5.5 恢复契约：默认读取持久化 blueprint + event stream，不重新 resolve。
func (k *Kernel) ResumeRuntimeSession(ctx context.Context, sessionID string) (kruntime.SessionBlueprint, error) {
	if k.eventStore == nil {
		return kruntime.SessionBlueprint{}, errors.New(errors.ErrValidation, "ResumeRuntimeSession requires an EventStore")
	}

	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("load session view for resume: %w", err)
	}
	if state == nil || state.Blueprint == nil {
		return kruntime.SessionBlueprint{}, kruntime.ErrSessionNotFound
	}
	if state.Status == "completed" || state.Status == "failed" {
		return kruntime.SessionBlueprint{}, kruntime.ErrSessionEnded
	}

	return *state.Blueprint, nil
}

// ForkRuntimeSession 从 sourceSessionID 派生出新 session：
//  1. 加载源 session 的 MaterializedState（含 blueprint）；
//  2. 用 resolver 以 req 作为覆盖重新 resolve 出子 blueprint；
//  3. 在源 session 写入 session_forked 事件；
//  4. 在新 session 写入 session_created 事件（含 parent_session_id）。
//
// 实现 §5.5 fork 契约：复制或派生新 canonical blueprint，再写 fork 事件。
// 需要 WithEventStore 已配置，否则返回 ErrValidation。
func (k *Kernel) ForkRuntimeSession(ctx context.Context, sourceSessionID string, req kruntime.RuntimeRequest) (kruntime.SessionBlueprint, error) {
	if k.eventStore == nil {
		return kruntime.SessionBlueprint{}, errors.New(errors.ErrValidation, "ForkRuntimeSession requires an EventStore")
	}

	// 1. 加载源 session 状态，确认可 fork
	srcState, err := k.eventStore.LoadSessionView(ctx, sourceSessionID)
	if err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("load source session: %w", err)
	}
	if srcState == nil {
		return kruntime.SessionBlueprint{}, kruntime.ErrSessionNotFound
	}

	// 2. Resolve 子 blueprint（使用 req 作为覆盖）
	resolver := k.runtimeResolver
	if resolver == nil {
		resolver = kruntime.NewDefaultRequestResolver(kruntime.NewDefaultPolicyCompiler())
	}
	childBP, err := resolver.Resolve(req)
	if err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("resolve fork blueprint: %w", err)
	}
	childSessionID := childBP.Identity.SessionID

	// 3. 向源 session 追加 session_forked 事件
	forkEv := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeSessionForked,
		SessionID:        sourceSessionID,
		Seq:              0, // 由 AppendEvents 分配
		BlueprintVersion: childBP.Provenance.Hash,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.SessionForkedPayload{
			ChildSessionID:   childSessionID,
			BlueprintPayload: &childBP,
		},
	}
	if err := k.eventStore.AppendEvents(ctx, sourceSessionID, srcState.CurrentSeq, "", []kruntime.RuntimeEvent{forkEv}); err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("append session_forked to source: %w", err)
	}

	// 4. 向新 session 追加 session_created 事件（含 parent_session_id）
	createdEv := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeSessionCreated,
		SessionID:        childSessionID,
		Seq:              0,
		BlueprintVersion: childBP.Provenance.Hash,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.SessionCreatedPayload{
			BlueprintPayload: &childBP,
			TriggerSource:    "fork",
			ParentSessionID:  sourceSessionID,
		},
	}
	if err := k.eventStore.AppendEvents(ctx, childSessionID, 0, "", []kruntime.RuntimeEvent{createdEv}); err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("append session_created for fork child: %w", err)
	}

	return childBP, nil
}

// CompleteRuntimeSession 向 EventStore 写入 session_completed 事件，标记 session 正常结束。
// 需要 WithEventStore 已配置。
func (k *Kernel) CompleteRuntimeSession(ctx context.Context, sessionID string, summary string) error {
	if k.eventStore == nil {
		return errors.New(errors.ErrValidation, "CompleteRuntimeSession requires an EventStore")
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:      kruntime.EventTypeSessionCompleted,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Payload:   kruntime.SessionCompletedPayload{Summary: summary},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// FailRuntimeSession 向 EventStore 写入 session_failed 事件，标记 session 因不可恢复错误中断。
// 需要 WithEventStore 已配置。
func (k *Kernel) FailRuntimeSession(ctx context.Context, sessionID, errorKind, errorMessage string) error {
	if k.eventStore == nil {
		return errors.New(errors.ErrValidation, "FailRuntimeSession requires an EventStore")
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:      kruntime.EventTypeSessionFailed,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Payload: kruntime.SessionFailedPayload{
			ErrorKind:    errorKind,
			ErrorMessage: errorMessage,
			LastSeq:      state.CurrentSeq,
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// ────────────────────────────────────────────────────────────────────
// 阶段 3：Blueprint → legacy session.Session bridge
// ────────────────────────────────────────────────────────────────────

// RunBlueprintOption 是 RunAgentFromBlueprint 的可选参数。
type RunBlueprintOption func(*runBlueprintOptions)

type runBlueprintOptions struct {
	onResult func(*session.LifecycleResult)
}

// WithBlueprintOnResult 设置 lifecycle result 回调，
// 供 CollectRunAgentFromBlueprint 等辅助函数捕获执行结果。
func WithBlueprintOnResult(cb func(*session.LifecycleResult)) RunBlueprintOption {
	return func(o *runBlueprintOptions) { o.onResult = cb }
}

// RunAgentFromBlueprint 是阶段 3 的主链路桥接方法。
// 它将 SessionBlueprint 翻译为 legacy session.Session，
// 然后委托 RunAgent 执行，同时在 EventStore（若已配置）中写入 turn_started / turn_completed 事件。
//
// layers 是本次 turn 可用的 PromptLayerProvider 列表（含 system / user scope）。
// 若 layers 非空，使用 DefaultPromptCompiler 编译出 system prompt 并写入 SessionConfig。
// 这使现有 RunAgent 执行引擎无需修改即可被新 blueprint 路径驱动。
// 阶段 4 完成后，本方法将成为唯一推荐入口，而 RunAgent 将被标记为 Deprecated。
func (k *Kernel) RunAgentFromBlueprint(
	ctx context.Context,
	bp kruntime.SessionBlueprint,
	layers []kruntime.PromptLayerProvider,
	agent Agent,
	userMsg *model.Message,
	userIO io.UserIO,
	opts ...RunBlueprintOption,
) iter.Seq2[*session.Event, error] {
	return k.runAgentFromBlueprintImpl(ctx, bp, layers, agent, userMsg, userIO, opts...)
}

// runAgentFromBlueprintImpl 实际逻辑。
func (k *Kernel) runAgentFromBlueprintImpl(
	ctx context.Context,
	bp kruntime.SessionBlueprint,
	layers []kruntime.PromptLayerProvider,
	agent Agent,
	userMsg *model.Message,
	userIO io.UserIO,
	opts ...RunBlueprintOption,
) iter.Seq2[*session.Event, error] {
	bpOpts := &runBlueprintOptions{}
	for _, o := range opts {
		o(bpOpts)
	}
	return func(yield func(*session.Event, error) bool) {
		// 0. §阶段4: 删除 approval mode runtime application
		// 若已注册 blueprint policy applier，在每次 turn 执行前应用 blueprint 编译后的 EffectiveToolPolicy，
		// 替代 buildKernel 中的 ApplyApprovalModeWithTrust 作为 policystate 的权威来源。
		if k.blueprintPolicyApplier != nil {
			k.blueprintPolicyApplier(bp)
		}

		// 1. 从 blueprint 构造 legacy SessionConfig
		cfg := blueprintToSessionConfig(bp)

		// 1a. 若提供了 PromptLayerProvider，通过 PromptCompiler 编译 system prompt
		if len(layers) > 0 {
			compiler := k.promptCompiler
			if compiler == nil {
				compiler = kruntime.NewDefaultPromptCompiler()
			}
			compiled, compileErr := compiler.Compile(bp, nil, layers)
			if compileErr == nil {
				cfg.SystemPrompt = extractSystemPromptText(compiled)
			} else {
				k.Logger().WarnContext(ctx, "compile prompt from blueprint failed", "error", compileErr)
			}
		}

		// 2. 创建 legacy session（走现有 NewSession 路径）
		sess, err := k.NewSession(ctx, cfg)
		if err != nil {
			yield(nil, fmt.Errorf("create session from blueprint: %w", err))
			return
		}

		// 3. 向 EventStore 写入 turn_started（若已配置）
		turnID := ""
		if k.eventStore != nil {
			turnID, err = k.appendTurnStarted(ctx, bp.Identity.SessionID, bp.Provenance.Hash)
			if err != nil {
				k.Logger().WarnContext(ctx, "append turn_started failed",
					"session_id", bp.Identity.SessionID, "error", err)
			}
		}

		// 4. 构造 RunAgentRequest 并执行
		// §14.2/§14.3/§14.8：若 EventStore 可用，注入 eventStoreObserver 记录审计事件
		var runObserver observe.Observer
		if k.eventStore != nil {
			runObserver = &eventStoreObserver{
				Observer:  k.observerOrNoOp(),
				store:     k.eventStore,
				sessionID: bp.Identity.SessionID,
				bpHash:    bp.Provenance.Hash,
				logger:    k.Logger(),
			}
		}
		var lifecycleResult *session.LifecycleResult
		onResult := bpOpts.onResult
		req := RunAgentRequest{
			Session:     sess,
			Agent:       agent,
			UserContent: userMsg,
			IO:          userIO,
			OnResult: func(result *session.LifecycleResult) {
				lifecycleResult = cloneLifecycleResult(result)
				if onResult != nil {
					onResult(result)
				}
			},
			Observer:    runObserver,
		}

		var outcome kruntime.TurnOutcome = kruntime.TurnOutcomeCompleted
		var lastErr error
		for ev, evErr := range k.RunAgent(ctx, req) {
			if evErr != nil {
				outcome = kruntime.TurnOutcomeError
				lastErr = evErr
			}
			if !yield(ev, evErr) {
				break
			}
		}

		// 4a. §14.4 预算耗尽检测：显式 budget result 优先，其次回退到 session budget 快照。
		if lastErr == nil && k.eventStore != nil {
			if lifecycleResult != nil && lifecycleResult.Status == session.LifecycleBudgetExhausted {
				outcome = kruntime.TurnOutcomeBudgetExhausted
				if appendErr := k.appendBudgetExhaustedDetail(
					ctx,
					bp.Identity.SessionID,
					bp.Provenance.Hash,
					lifecycleResult.BudgetExhausted,
				); appendErr != nil {
					k.Logger().WarnContext(ctx, "append budget_exhausted failed",
						"session_id", bp.Identity.SessionID, "error", appendErr)
				}
			} else if sess.Budget.Exhausted() {
				outcome = kruntime.TurnOutcomeBudgetExhausted
				if appendErr := k.appendBudgetExhausted(ctx, bp.Identity.SessionID, bp.Provenance.Hash, &sess.Budget); appendErr != nil {
					k.Logger().WarnContext(ctx, "append budget_exhausted failed",
						"session_id", bp.Identity.SessionID, "error", appendErr)
				}
			}
		}

		// 5. 向 EventStore 写入 turn_completed（若已配置）
		if k.eventStore != nil && turnID != "" {
			errKind := ""
			if lastErr != nil {
				errKind = "execution_error"
			}
			if appendErr := k.appendTurnCompleted(ctx, bp.Identity.SessionID, bp.Provenance.Hash, turnID, outcome, errKind); appendErr != nil {
				k.Logger().WarnContext(ctx, "append turn_completed failed",
					"session_id", bp.Identity.SessionID, "error", appendErr)
			}
		}
	}
}

// appendTurnStarted 向 EventStore 追加 turn_started 事件，返回 turnID。
func (k *Kernel) appendTurnStarted(ctx context.Context, sessionID, blueprintVersion string) (string, error) {
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("load session view for turn_started: %w", err)
	}
	if state == nil {
		return "", kruntime.ErrSessionNotFound
	}
	turnID := fmt.Sprintf("%s-t%d", sessionID[:8], state.CurrentSeq+1)
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeTurnStarted,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload:          kruntime.TurnStartedPayload{TurnID: turnID},
	}
	if err := k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev}); err != nil {
		return "", fmt.Errorf("append turn_started: %w", err)
	}
	return turnID, nil
}

// appendTurnCompleted 向 EventStore 追加 turn_completed 事件。
func (k *Kernel) appendTurnCompleted(ctx context.Context, sessionID, blueprintVersion, turnID string, outcome kruntime.TurnOutcome, errorKind string) error {
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view for turn_completed: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeTurnCompleted,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.TurnCompletedPayload{
			TurnID:    turnID,
			Outcome:   outcome,
			ErrorKind: errorKind,
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

func budgetExhaustedDetailFromBudget(budget *session.Budget) *session.BudgetExhaustedDetail {
	if budget == nil {
		return nil
	}
	kind := "token"
	consumed := budget.UsedTokensValue()
	limit := budget.MaxTokens
	if budget.MaxSteps > 0 && budget.UsedStepsValue() >= budget.MaxSteps {
		kind = "step"
		consumed = budget.UsedStepsValue()
		limit = budget.MaxSteps
	} else if budget.MaxThinkingTokens > 0 && budget.UsedThinkingTokensValue() >= budget.MaxThinkingTokens {
		kind = "thinking_token"
		consumed = budget.UsedThinkingTokensValue()
		limit = budget.MaxThinkingTokens
	}
	return &session.BudgetExhaustedDetail{
		BudgetKind:    kind,
		ConsumedValue: consumed,
		LimitValue:    limit,
	}
}

// appendBudgetExhausted 向 EventStore 追加 budget_exhausted 事件（§14.4/§14.10）。
// 根据 Budget 状态自动判断 budget_kind（thinking_token / step / token），优先级从高到低。
func (k *Kernel) appendBudgetExhausted(ctx context.Context, sessionID, blueprintVersion string, budget *session.Budget) error {
	return k.appendBudgetExhaustedDetail(ctx, sessionID, blueprintVersion, budgetExhaustedDetailFromBudget(budget))
}

func (k *Kernel) appendBudgetExhaustedDetail(
	ctx context.Context,
	sessionID, blueprintVersion string,
	detail *session.BudgetExhaustedDetail,
) error {
	if detail == nil {
		return nil
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view for budget_exhausted: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}

	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeBudgetExhausted,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.BudgetExhaustedPayload{
			BudgetKind:    kruntime.BudgetKind(detail.BudgetKind),
			ConsumedValue: int64(detail.ConsumedValue),
			LimitValue:    int64(detail.LimitValue),
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// RecordTurnStarted 向 EventStore 追加 turn_started 事件并返回 turnID（供外部调用）。
// 如果 EventStore 未配置，静默返回空 turnID。
func (k *Kernel) RecordTurnStarted(ctx context.Context, bp kruntime.SessionBlueprint) (string, error) {
	if k.eventStore == nil {
		return "", nil
	}
	return k.appendTurnStarted(ctx, bp.Identity.SessionID, bp.Provenance.Hash)
}

// RecordTurnCompleted 向 EventStore 追加 turn_completed 事件（供外部调用）。
// 如果 EventStore 未配置或 turnID 为空，静默返回 nil。
func (k *Kernel) RecordTurnCompleted(ctx context.Context, bp kruntime.SessionBlueprint, turnID string, outcome kruntime.TurnOutcome, errorKind string) error {
	if k.eventStore == nil || turnID == "" {
		return nil
	}
	return k.appendTurnCompleted(ctx, bp.Identity.SessionID, bp.Provenance.Hash, turnID, outcome, errorKind)
}

// RecordBudgetExhausted 向 EventStore 追加 budget_exhausted 事件（供外部调用）。
func (k *Kernel) RecordBudgetExhausted(ctx context.Context, bp kruntime.SessionBlueprint, detail *session.BudgetExhaustedDetail) error {
	if k.eventStore == nil {
		return nil
	}
	return k.appendBudgetExhaustedDetail(ctx, bp.Identity.SessionID, bp.Provenance.Hash, detail)
}

// RecordBudgetLimitUpdated 向 EventStore 追加 budget_limit_updated 事件，并返回更新后的 blueprint 副本。
func (k *Kernel) RecordBudgetLimitUpdated(
	ctx context.Context,
	bp kruntime.SessionBlueprint,
	newMainTokenBudget int,
	reason string,
) (kruntime.SessionBlueprint, error) {
	updated := bp
	updated.ContextBudget.MainTokenBudget = newMainTokenBudget
	if k.eventStore == nil {
		return updated, nil
	}
	state, err := k.eventStore.LoadSessionView(ctx, bp.Identity.SessionID)
	if err != nil {
		return bp, fmt.Errorf("load session view for budget_limit_updated: %w", err)
	}
	if state == nil {
		return bp, kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeBudgetLimitUpdated,
		SessionID:        bp.Identity.SessionID,
		BlueprintVersion: bp.Provenance.Hash,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.BudgetLimitUpdatedPayload{
			PreviousMainTokenBudget: int64(bp.ContextBudget.MainTokenBudget),
			MainTokenBudget:         int64(newMainTokenBudget),
			Reason:                  strings.TrimSpace(reason),
		},
	}
	if err := k.eventStore.AppendEvents(ctx, bp.Identity.SessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev}); err != nil {
		return bp, err
	}
	return updated, nil
}

// blueprintToSessionConfig 将 SessionBlueprint 映射到 legacy SessionConfig。
// 这是阶段 3 的过渡适配层，仅在阶段 4 完成前使用。
func blueprintToSessionConfig(bp kruntime.SessionBlueprint) session.SessionConfig {
	trustLevel := bp.EffectiveToolPolicy.TrustLevel
	if trustLevel == "" {
		trustLevel = "workspace-write"
	}
	return session.SessionConfig{
		Goal:       bp.Identity.AgentName,
		TrustLevel: trustLevel,
		MaxSteps:   bp.SessionBudget.MaxSteps,
		MaxTokens:  bp.ContextBudget.MainTokenBudget,
		Mode:       bp.PromptPlan.PromptPackID,
		ModelConfig: model.ModelConfig{
			Model:     bp.ModelConfig.ModelID,
			MaxTokens: bp.ContextBudget.MainTokenBudget,
		},
		Metadata: map[string]any{
			"blueprint_session_id":     bp.Identity.SessionID,
			"blueprint_hash":           bp.Provenance.Hash,
			"blueprint_schema_version": bp.Provenance.BlueprintSchemaVersion,
		},
	}
}

// ────────────────────────────────────────────────────────────────────
// §14.6 分布式 Task 事件 API
// ────────────────────────────────────────────────────────────────────

// RecordTaskStarted 向 EventStore 追加 task_started 事件（§14.6）。
// claimedBy 填写认领该任务的 agent 名称；planningItemID 可选，填写关联的规划条目。
// 若 EventStore 未配置，静默返回 nil。
func (k *Kernel) RecordTaskStarted(ctx context.Context, sessionID, blueprintVersion, taskID, claimedBy, planningItemID string) error {
	if k.eventStore == nil {
		return nil
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view for task_started: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeTaskStarted,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.TaskStartedPayload{
			TaskID:         taskID,
			ClaimedBy:      claimedBy,
			PlanningItemID: planningItemID,
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// RecordTaskCompleted 向 EventStore 追加 task_completed 事件（§14.6）。
// resultRef 填写结果引用（如 artifact 路径或 URL）。
// 若 EventStore 未配置，静默返回 nil。
func (k *Kernel) RecordTaskCompleted(ctx context.Context, sessionID, blueprintVersion, taskID, resultRef string) error {
	if k.eventStore == nil {
		return nil
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view for task_completed: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeTaskCompleted,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.TaskCompletedPayload{
			TaskID:    taskID,
			ResultRef: resultRef,
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// RecordTaskAbandoned 向 EventStore 追加 task_abandoned 事件（§14.6）。
// reason 填写放弃原因描述。
// 若 EventStore 未配置，静默返回 nil。
func (k *Kernel) RecordTaskAbandoned(ctx context.Context, sessionID, blueprintVersion, taskID, reason string) error {
	if k.eventStore == nil {
		return nil
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view for task_abandoned: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeTaskAbandoned,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.TaskAbandonedPayload{
			TaskID: taskID,
			Reason: reason,
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// ────────────────────────────────────────────────────────────────────
// §14.7 Sandbox Checkpoint 事件 API
// ────────────────────────────────────────────────────────────────────

// RecordCheckpointCreated 向 EventStore 追加 checkpoint_created 事件（§14.7）。
// workspaceSnapshotRef 填写 sandbox 工作区快照的引用（如 git commit hash 或 tar 路径）。
// 若 EventStore 未配置，静默返回 nil。
func (k *Kernel) RecordCheckpointCreated(ctx context.Context, sessionID, blueprintVersion, checkpointID, workspaceSnapshotRef string) error {
	if k.eventStore == nil {
		return nil
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session view for checkpoint_created: %w", err)
	}
	if state == nil {
		return kruntime.ErrSessionNotFound
	}
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeCheckpointCreated,
		SessionID:        sessionID,
		BlueprintVersion: blueprintVersion,
		Timestamp:        time.Now().UTC(),
		Payload: kruntime.CheckpointCreatedPayload{
			CheckpointID:         checkpointID,
			EventBoundarySeq:     state.CurrentSeq,
			WorkspaceSnapshotRef: workspaceSnapshotRef,
		},
	}
	return k.eventStore.AppendEvents(ctx, sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev})
}

// LookupSessionApproval 从 EventStore 查询指定 session 内是否存在匹配 cacheKey 的已决议审批。
// 这是 session.ApprovalState 旧路径的新路径替代品（§阶段 4 迁移）：
// 旧路径将审批 cache 存于 in-memory session.Session.State，重启后丢失；
// 新路径通过 EventStore 持久化，支持跨重启恢复。
//
// 返回 (entry, true) 表示找到匹配记录；返回 (zero, false) 表示无匹配或 EventStore 不可用。
func (k *Kernel) LookupSessionApproval(ctx context.Context, sessionID, cacheKey string) (kruntime.ResolvedApprovalEntry, bool) {
	if k.eventStore == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(cacheKey) == "" {
		return kruntime.ResolvedApprovalEntry{}, false
	}
	state, err := k.eventStore.LoadSessionView(ctx, sessionID)
	if err != nil || state == nil {
		return kruntime.ResolvedApprovalEntry{}, false
	}
	cacheKey = strings.TrimSpace(cacheKey)
	for _, entry := range state.ResolvedApprovals {
		if strings.EqualFold(entry.CacheKey, cacheKey) {
			return entry, true
		}
	}
	return kruntime.ResolvedApprovalEntry{}, false
}

// ────────────────────────────────────────────────────────────────────
// 阶段 5：导出与审计 API
// ────────────────────────────────────────────────────────────────────

// ExportRuntimeSession 将指定 session 的事件流导出为指定格式（JSONL / JSON）。
// 实现 §10 Export 契约，通过 EventStore.Export 接口统一输出。
// 需要 WithEventStore 已配置，否则返回 ErrValidation。
func (k *Kernel) ExportRuntimeSession(ctx context.Context, sessionID string, format kruntime.ExportFormat) ([]byte, error) {
	if k.eventStore == nil {
		return nil, errors.New(errors.ErrValidation, "ExportRuntimeSession requires an EventStore")
	}
	return k.eventStore.Export(ctx, sessionID, format)
}

// ImportRuntimeSession 从外部数据导入事件流到指定 session。
// 实现 §10 Import 契约，通过 EventStore.Import 接口统一导入。
// 需要 WithEventStore 已配置，否则返回 ErrValidation。
func (k *Kernel) ImportRuntimeSession(ctx context.Context, sessionID string, data []byte, format kruntime.ExportFormat) error {
	if k.eventStore == nil {
		return errors.New(errors.ErrValidation, "ImportRuntimeSession requires an EventStore")
	}
	return k.eventStore.Import(ctx, sessionID, data, format)
}

// extractSystemPromptText 从 CompiledPrompt 中提取 system role 的纯文本内容。
// 多个 system 消息的文本按换行拼接。
func extractSystemPromptText(compiled kruntime.CompiledPrompt) string {
	var parts []string
	for _, msg := range compiled.Messages {
		if msg.Role != model.RoleSystem {
			continue
		}
		for _, part := range msg.ContentParts {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// ────────────────────────────────────────────────────────────────────
// §14.1/§14.2/§14.3/§14.8：EventStore 审计 Observer
// ────────────────────────────────────────────────────────────────────

// eventStoreObserver 包装 base Observer，在关键运行时事件发生时先写入 EventStore，
// 再触发 base Observer 回调（§14.1 ordering）。
// 处理范围：
//   - approval_resolved（§14.2 Guardian resolver_type）
//   - tool_called / tool_completed（§14.3 MCP mcp_server_id/mcp_tool_name）
//   - llm_called（§14.8 Provider Failover provider_id/original_provider_id）
type eventStoreObserver struct {
	observe.Observer
	store     kruntime.EventStore
	sessionID string
	bpHash    string
	logger    interface {
		WarnContext(ctx context.Context, msg string, args ...any)
	}
}

// appendToStore 是便捷写入辅助，加载当前 seq 后追加单个事件；失败仅 warn。
func (a *eventStoreObserver) appendToStore(ctx context.Context, ev kruntime.RuntimeEvent) {
	state, err := a.store.LoadSessionView(ctx, a.sessionID)
	if err != nil || state == nil {
		return
	}
	if appendErr := a.store.AppendEvents(ctx, a.sessionID, state.CurrentSeq, "", []kruntime.RuntimeEvent{ev}); appendErr != nil {
		if a.logger != nil {
			a.logger.WarnContext(ctx, "eventStoreObserver: append event failed",
				"type", ev.Type, "session_id", a.sessionID, "error", appendErr)
		}
	}
}

// OnApproval 处理 approval_resolved 事件（§14.2）。
func (a *eventStoreObserver) OnApproval(ctx context.Context, e io.ApprovalEvent) {
	if e.Type == "resolved" && a.store != nil {
		resolverType := kruntime.ResolverTypeHuman
		if e.Decision != nil && e.Decision.Source == "guardian" {
			resolverType = kruntime.ResolverTypeGuardian
		} else if e.Decision != nil && e.Decision.Source == "policy" {
			resolverType = kruntime.ResolverTypePolicy
		}
		approvalID := ""
		approved := false
		reason := ""
		cacheKey := ""
		toolName := ""
		decisionType := ""
		if e.Decision != nil {
			approvalID = e.Decision.RequestID
			approved = e.Decision.Approved
			reason = e.Decision.Reason
			cacheKey = strings.TrimSpace(e.Decision.CacheKey)
			decisionType = string(e.Decision.Type)
		}
		// CacheKey 优先从 Decision 取，再从 Request 取
		if cacheKey == "" {
			cacheKey = strings.TrimSpace(e.Request.CacheKey)
		}
		toolName = strings.TrimSpace(e.Request.ToolName)
		a.appendToStore(ctx, kruntime.RuntimeEvent{
			Type:             kruntime.EventTypeApprovalResolved,
			SessionID:        a.sessionID,
			BlueprintVersion: a.bpHash,
			Timestamp:        time.Now().UTC(),
			Payload: kruntime.ApprovalResolvedPayload{
				ApprovalID:   approvalID,
				PolicyHash:   a.bpHash,
				ResolverType: resolverType,
				Approved:     approved,
				Reason:       reason,
				CacheKey:     cacheKey,
				ToolName:     toolName,
				DecisionType: decisionType,
			},
		})
	}
	a.Observer.OnApproval(ctx, e)
}

// OnExecutionEvent 处理工具执行事件（§14.3 MCP）。
// 对 tool_called / tool_completed 事件写入 RuntimeEvent，并携带 MCP 身份字段。
func (a *eventStoreObserver) OnExecutionEvent(ctx context.Context, e observe.ExecutionEvent) {
	if a.store != nil {
		switch e.Type {
		case observe.ExecutionToolStarted:
			a.appendToStore(ctx, kruntime.RuntimeEvent{
				Type:             kruntime.EventTypeToolCalled,
				SessionID:        a.sessionID,
				BlueprintVersion: a.bpHash,
				Timestamp:        e.Timestamp,
				Payload: kruntime.ToolCalledPayload{
					ToolCallID:  e.CallID,
					ToolName:    e.ToolName,
					PolicyHash:  a.bpHash,
					MCPServerID: e.MCPServerID,
					MCPToolName: e.MCPToolName,
				},
			})
		case observe.ExecutionToolCompleted:
			isError := false
			if e.Error != "" {
				isError = true
			}
			a.appendToStore(ctx, kruntime.RuntimeEvent{
				Type:             kruntime.EventTypeToolCompleted,
				SessionID:        a.sessionID,
				BlueprintVersion: a.bpHash,
				Timestamp:        e.Timestamp,
				Payload: kruntime.ToolCompletedPayload{
					ToolCallID:   e.CallID,
					ToolName:     e.ToolName,
					IsError:      isError,
					ErrorMessage: e.Error,
					MCPServerID:  e.MCPServerID,
					MCPToolName:  e.MCPToolName,
				},
			})
		}
	}
	// EventStore 写入后再触发 base Observer（§14.1 ordering）
	a.Observer.OnExecutionEvent(ctx, e)
}

// OnLLMCall 处理 LLM 调用事件（§14.8 Provider Failover）。
// 写入 llm_called RuntimeEvent，携带 provider_id/original_provider_id 审计字段。
func (a *eventStoreObserver) OnLLMCall(ctx context.Context, e observe.LLMCallEvent) {
	if a.store != nil {
		isError := e.Error != nil
		errorMsg := ""
		if isError {
			errorMsg = e.Error.Error()
		}
		a.appendToStore(ctx, kruntime.RuntimeEvent{
			Type:             kruntime.EventTypeLLMCalled,
			SessionID:        a.sessionID,
			BlueprintVersion: a.bpHash,
			Timestamp:        time.Now().UTC(),
			Payload: kruntime.LLMCalledPayload{
				ModelID:            e.Model,
				ProviderID:         e.ProviderID,
				OriginalProviderID: e.OriginalProviderID,
				TokensUsed:         e.Usage.TotalTokens,
				ThinkingTokensUsed: e.Usage.ThinkingTokens,
				StopReason:         e.StopReason,
				IsError:            isError,
				ErrorMessage:       errorMsg,
			},
		})
	}
	// EventStore 写入后再触发 base Observer（§14.1 ordering）
	a.Observer.OnLLMCall(ctx, e)
}
