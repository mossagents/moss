package kernel

import (
	"context"
	"log/slog"

	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

// emitSessionLifecycle 向 kernel plugin chain 广播 Session 生命周期事件。
//
// §14.1 Observer 顺序约束（待完整实现）：
//   - turn_started / turn_completed 事件在 EventStore.AppendEvents 成功后由 runtime_ops.go 写入，
//     之后 Observer 回调通过 plugin chain 触发——此层顺序已满足。
//   - llm_called / tool_called / tool_completed 的 Observer 顺序约束需要将 EventStore 注入 loop，
//     待阶段 4 完成旧路径删除后统一实现。
func (k *Kernel) emitSessionLifecycle(ctx context.Context, event session.LifecycleEvent) {
	if err := k.chain.OnSessionLifecycle.Run(contextOrBackground(ctx), &event); err != nil {
		sessionID := ""
		if event.Session != nil {
			sessionID = event.Session.ID
		}
		slog.Default().ErrorContext(contextOrBackground(ctx), "session lifecycle hook error",
			slog.String("stage", string(event.Stage)),
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		observe.ObserveError(contextOrBackground(ctx), k.observerOrNoOp(), observe.ErrorEvent{
			SessionID: sessionID,
			Phase:     "session_lifecycle_hook",
			Error:     err,
			Message:   err.Error(),
		})
	}
}
