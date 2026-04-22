package loop

import (
	"context"
	"fmt"
	"strings"

	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
)

type budgetStopError struct {
	detail  *session.BudgetExhaustedDetail
	message string
}

func (e *budgetStopError) Error() string {
	if e == nil {
		return "budget exhausted"
	}
	return e.message
}

func (l *AgentLoop) resolveBudgetStop(
	ctx context.Context,
	sess *session.Session,
	snapshot session.Budget,
	responseTokens int,
) error {
	detail := budgetExhaustedDetailFromAttempt(snapshot, responseTokens, 1)
	if detail == nil {
		detail = &session.BudgetExhaustedDetail{
			BudgetKind:    "token",
			ConsumedValue: snapshot.UsedTokens + responseTokens,
			LimitValue:    snapshot.MaxTokens,
		}
	}
	if detail.BudgetKind != "token" {
		return &budgetStopError{
			detail:  detail,
			message: formatBudgetStopMessage(detail, false),
		}
	}
	return l.resolveTokenOverrun(ctx, sess, snapshot, responseTokens, detail)
}

func (l *AgentLoop) resolveTokenOverrun(
	ctx context.Context,
	sess *session.Session,
	snapshot session.Budget,
	responseTokens int,
	detail *session.BudgetExhaustedDetail,
) error {
	req := buildTokenOverrunRequest(snapshot, responseTokens, l.Config.TokenOverrun.continueMultiplier())
	if !l.Config.TokenOverrun.PromptUser || l.IO == nil {
		l.sendBudgetProgress(ctx, "Token ceiling reached for the current thread. Stopping before accepting the over-limit response.")
		return &budgetStopError{
			detail:  detail,
			message: formatBudgetStopMessage(detail, false),
		}
	}

	resp, err := l.IO.Ask(ctx, buildTokenOverrunInputRequest(req))
	if err != nil {
		return fmt.Errorf("ask token overrun decision: %w", err)
	}
	if resp.Selected != 0 {
		l.sendBudgetProgress(ctx, "Token ceiling reached for the current thread. Current run stopped at your request.")
		return &budgetStopError{
			detail:  detail,
			message: formatBudgetStopMessage(detail, true),
		}
	}

	oldLimit := req.CurrentLimit
	sess.UpdateTokenBudgetLimit(req.ProposedLimit)
	if persist := l.Config.TokenOverrun.PersistLimit; persist != nil {
		if err := persist(ctx, sess, req); err != nil {
			sess.UpdateTokenBudgetLimit(oldLimit)
			return fmt.Errorf("persist token budget limit %d: %w", req.ProposedLimit, err)
		}
	}
	if !sess.Budget.TryConsume(responseTokens, 1) {
		return fmt.Errorf("updated token budget limit %d still rejected response consume", req.ProposedLimit)
	}
	l.sendBudgetProgress(ctx, fmt.Sprintf(
		"Token ceiling raised to %d for the current thread. Continuing this run.",
		req.ProposedLimit,
	))
	return nil
}

func buildTokenOverrunRequest(snapshot session.Budget, responseTokens, multiplier int) TokenOverrunRequest {
	used := snapshot.UsedTokens
	current := snapshot.MaxTokens
	required := used + responseTokens
	proposed := current * multiplier
	if proposed < required {
		headroom := current * (multiplier - 1)
		proposed = required + headroom
	}
	if proposed <= current {
		proposed = required
	}
	return TokenOverrunRequest{
		UsedTokens:     used,
		CurrentLimit:   current,
		ResponseTokens: responseTokens,
		ProposedLimit:  proposed,
	}
}

func buildTokenOverrunInputRequest(req TokenOverrunRequest) kernio.InputRequest {
	promptLines := []string{
		"This response would exceed the current thread token ceiling.",
		fmt.Sprintf("Current usage: %d / %d tokens", req.UsedTokens, req.CurrentLimit),
		fmt.Sprintf("This response: %d tokens", req.ResponseTokens),
		fmt.Sprintf("If accepted: %d tokens", req.UsedTokens+req.ResponseTokens),
		fmt.Sprintf("Choose whether to stop or raise the limit for this thread to %d.", req.ProposedLimit),
	}
	continueTitle := fmt.Sprintf("Continue with doubled limit (%d)", req.ProposedLimit)
	continueDesc := "Applies to the current thread and accepts this response."
	if doubled := req.CurrentLimit * 2; doubled > 0 && req.ProposedLimit > doubled {
		continueTitle = fmt.Sprintf("Continue with raised limit (%d)", req.ProposedLimit)
		continueDesc = "Default doubling is not enough for this response, so the limit will be raised with extra headroom for the current thread."
	}
	return kernio.InputRequest{
		Type:   kernio.InputSelect,
		Prompt: strings.Join(promptLines, "\n"),
		Options: []string{
			continueTitle + "\n" + continueDesc,
			fmt.Sprintf(
				"Terminate current run\nKeep the limit at %d and stop before accepting this response.",
				req.CurrentLimit,
			),
		},
		Meta: map[string]any{
			kernio.InputMetaInlineSelectAllowChatEscape: false,
		},
	}
}

func budgetExhaustedDetailFromAttempt(snapshot session.Budget, responseTokens, responseSteps int) *session.BudgetExhaustedDetail {
	if snapshot.MaxSteps > 0 && snapshot.UsedSteps+responseSteps > snapshot.MaxSteps {
		return &session.BudgetExhaustedDetail{
			BudgetKind:    "step",
			ConsumedValue: snapshot.UsedSteps + responseSteps,
			LimitValue:    snapshot.MaxSteps,
		}
	}
	return &session.BudgetExhaustedDetail{
		BudgetKind:    "token",
		ConsumedValue: snapshot.UsedTokens + responseTokens,
		LimitValue:    snapshot.MaxTokens,
	}
}

func budgetExhaustedDetailFromSnapshot(snapshot session.Budget) *session.BudgetExhaustedDetail {
	if snapshot.MaxSteps > 0 && snapshot.UsedSteps >= snapshot.MaxSteps {
		return &session.BudgetExhaustedDetail{
			BudgetKind:    "step",
			ConsumedValue: snapshot.UsedSteps,
			LimitValue:    snapshot.MaxSteps,
		}
	}
	return &session.BudgetExhaustedDetail{
		BudgetKind:    "token",
		ConsumedValue: snapshot.UsedTokens,
		LimitValue:    snapshot.MaxTokens,
	}
}

func formatBudgetStopMessage(detail *session.BudgetExhaustedDetail, userTerminated bool) string {
	if detail == nil {
		if userTerminated {
			return "budget exhausted; current run stopped by user"
		}
		return "budget exhausted"
	}
	if detail.BudgetKind == "token" && userTerminated {
		return fmt.Sprintf(
			"token budget exhausted: accepting this response would require %d tokens over the limit of %d; current run stopped by user",
			detail.ConsumedValue,
			detail.LimitValue,
		)
	}
	return fmt.Sprintf(
		"%s budget exhausted: attempted %d with limit %d",
		detail.BudgetKind,
		detail.ConsumedValue,
		detail.LimitValue,
	)
}

func (l *AgentLoop) sendBudgetProgress(ctx context.Context, content string) {
	if l.IO == nil || strings.TrimSpace(content) == "" {
		return
	}
	if err := l.IO.Send(ctx, kernio.OutputMessage{
		Type:    kernio.OutputProgress,
		Content: content,
		Meta: map[string]any{
			"kind": "budget",
		},
	}); err != nil {
		l.logger().DebugContext(ctx, "send budget progress failed", "error", err)
	}
}
