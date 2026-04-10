package budget

import (
	"context"
	"github.com/mossagents/moss/kernel/hooks"
)

// BudgetGuard 在 BeforeLLM 阶段检查全局预算是否耗尽。
// 耗尽时返回 ErrBudgetExhausted 阻止 LLM 调用。
// 同时使用 TryReserve 预检 1 步的余量，防止超支。
func BudgetGuard(gov Governor) hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		if !gov.Check() {
			return ErrBudgetExhausted
		}
		if !gov.TryReserve(0, 1) {
			return ErrBudgetExhausted
		}
		return nil
	}
}

// BudgetRecorder 在 AfterLLM 阶段将 LLM 调用步数记录到 Governor。
// 每次 LLM 调用计 1 步；token 统计需通过 Observer 或外部集成追踪。
func BudgetRecorder(gov Governor) hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		sessionID := ""
		if ev.Session != nil {
			sessionID = ev.Session.ID
		}
		gov.Record(sessionID, 0, 1)
		return nil
	}
}

// BudgetTokenRecorder 在指定 token 数确定后手动记录到 Governor。
// 用于在 AfterLLM 回调外部有 token 数据时调用（如 Observer）。
func BudgetTokenRecorder(gov Governor, sessionID string, tokens int) {
	gov.Record(sessionID, tokens, 0)
}
