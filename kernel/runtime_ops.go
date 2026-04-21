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
	"time"

	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
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

// RunAgentFromBlueprint 是阶段 3 的主链路桥接方法。
// 它将 SessionBlueprint 翻译为 legacy session.Session，
// 然后委托 RunAgent 执行，同时在 EventStore（若已配置）中写入 turn_started / turn_completed 事件。
//
// 这使现有 RunAgent 执行引擎无需修改即可被新 blueprint 路径驱动。
// 阶段 4 完成后，本方法将成为唯一推荐入口，而 RunAgent 将被标记为 Deprecated。
func (k *Kernel) RunAgentFromBlueprint(
	ctx context.Context,
	bp kruntime.SessionBlueprint,
	agent Agent,
	userMsg *model.Message,
	userIO io.UserIO,
) iter.Seq2[*session.Event, error] {
	return k.runAgentFromBlueprintImpl(ctx, bp, agent, userMsg, userIO)
}

// runAgentFromBlueprintImpl 实际逻辑。
func (k *Kernel) runAgentFromBlueprintImpl(
	ctx context.Context,
	bp kruntime.SessionBlueprint,
	agent Agent,
	userMsg *model.Message,
	userIO io.UserIO,
) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// 1. 从 blueprint 构造 legacy SessionConfig
		cfg := blueprintToSessionConfig(bp)

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
		req := RunAgentRequest{
			Session:     sess,
			Agent:       agent,
			UserContent: userMsg,
			IO:          userIO,
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
