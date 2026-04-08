package budget

import (
	"context"
	"github.com/mossagents/moss/kernel/middleware"
)

// BudgetGuard 在 BeforeLLM 阶段检查全局预算是否耗尽。
// 耗尽时返回 ErrBudgetExhausted 阻止 LLM 调用。
func BudgetGuard(gov Governor) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeLLM {
			return next(ctx)
		}
		if !gov.Check() {
			return ErrBudgetExhausted
		}
		return next(ctx)
	}
}

// BudgetRecorder 在 AfterLLM 阶段将 LLM 调用步数记录到 Governor。
// 每次 LLM 调用计 1 步；token 统计需通过 Observer 或外部集成追踪。
func BudgetRecorder(gov Governor) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.AfterLLM {
			return next(ctx)
		}
		if err := next(ctx); err != nil {
			return err
		}
		sessionID := ""
		if mc.Session != nil {
			sessionID = mc.Session.ID
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
