package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/session"
)

const tokenOverrunPersistReason = "token_overrun_continue"

func (a *agentState) installTokenOverrunNegotiation(k *kernel.Kernel) error {
	if a == nil || k == nil {
		return nil
	}
	return k.PatchLoopConfig(func(cfg *loop.LoopConfig) {
		cfg.TokenOverrun.PromptUser = true
		cfg.TokenOverrun.ContinueMultiplier = 2
		cfg.TokenOverrun.PersistLimit = a.persistTokenOverrunLimit
	})
}

func (a *agentState) persistTokenOverrunLimit(ctx context.Context, sess *session.Session, req loop.TokenOverrunRequest) error {
	if a == nil || sess == nil {
		return nil
	}
	a.mu.Lock()
	blueprint := a.blueprint
	store := a.store
	k := a.k
	a.mu.Unlock()

	if blueprint != nil {
		if k == nil {
			return fmt.Errorf("runtime is unavailable")
		}
		updated, err := k.RecordBudgetLimitUpdated(ctx, *blueprint, req.ProposedLimit, tokenOverrunPersistReason)
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.blueprint = &updated
		a.mu.Unlock()
		return nil
	}

	if store == nil {
		return nil
	}
	saveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := store.Save(saveCtx, sess); err != nil {
		return fmt.Errorf("save updated thread token limit: %w", err)
	}
	return nil
}
