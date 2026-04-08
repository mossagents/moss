package session

import "strings"

const (
	BudgetPolicyOff         = "off"
	BudgetPolicyObserveOnly = "observe-only"
	BudgetPolicyEnforce     = "enforce"
)

// BudgetGovernanceConfig defines global governance settings used for aggregation and gating.
type BudgetGovernanceConfig struct {
	Policy          string  `json:"policy,omitempty"`
	GlobalMaxTokens int64   `json:"global_max_tokens,omitempty"`
	GlobalMaxSteps  int64   `json:"global_max_steps,omitempty"`
	WarnAt          float64 `json:"warn_at,omitempty"`
}

// AggregatedBudgetSession captures per-session usage in a global report.
type AggregatedBudgetSession struct {
	SessionID  string `json:"session_id"`
	UsedTokens int    `json:"used_tokens"`
	UsedSteps  int    `json:"used_steps"`
	OverBudget bool   `json:"over_budget"`
}

// AggregatedBudgetReport summarizes budget usage under one governance policy.
type AggregatedBudgetReport struct {
	Policy          string                    `json:"policy"`
	GlobalMaxTokens int64                     `json:"global_max_tokens,omitempty"`
	GlobalMaxSteps  int64                     `json:"global_max_steps,omitempty"`
	WarnAt          float64                   `json:"warn_at,omitempty"`
	GlobalUsedTokens int64                    `json:"global_used_tokens"`
	GlobalUsedSteps  int64                    `json:"global_used_steps"`
	WarnTriggered   bool                      `json:"warn_triggered"`
	Blocked         bool                      `json:"blocked"`
	SessionCount    int                       `json:"session_count"`
	BlockedCount    int                       `json:"blocked_count"`
	Sessions        []AggregatedBudgetSession `json:"sessions,omitempty"`
}

func NormalizeBudgetPolicy(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case BudgetPolicyOff, BudgetPolicyObserveOnly, BudgetPolicyEnforce:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return BudgetPolicyObserveOnly
	}
}

func ShouldBlockByBudgetPolicy(policy string, overBudget bool) bool {
	return NormalizeBudgetPolicy(policy) == BudgetPolicyEnforce && overBudget
}

// AggregateBudgetReport builds a session-level + global budget summary under a governance policy.
func AggregateBudgetReport(cfg BudgetGovernanceConfig, sessions []*Session) AggregatedBudgetReport {
	policy := NormalizeBudgetPolicy(cfg.Policy)
	report := AggregatedBudgetReport{
		Policy:           policy,
		GlobalMaxTokens:  cfg.GlobalMaxTokens,
		GlobalMaxSteps:   cfg.GlobalMaxSteps,
		WarnAt:           cfg.WarnAt,
		Sessions:         make([]AggregatedBudgetSession, 0, len(sessions)),
	}
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		snap := sess.Budget.Clone()
		over := (cfg.GlobalMaxTokens > 0 && int64(snap.UsedTokens) > cfg.GlobalMaxTokens) ||
			(cfg.GlobalMaxSteps > 0 && int64(snap.UsedSteps) > cfg.GlobalMaxSteps)
		report.Sessions = append(report.Sessions, AggregatedBudgetSession{
			SessionID:  strings.TrimSpace(sess.ID),
			UsedTokens: snap.UsedTokens,
			UsedSteps:  snap.UsedSteps,
			OverBudget: over,
		})
		report.GlobalUsedTokens += int64(snap.UsedTokens)
		report.GlobalUsedSteps += int64(snap.UsedSteps)
		if over {
			report.BlockedCount++
		}
	}
	report.SessionCount = len(report.Sessions)
	overGlobal := (cfg.GlobalMaxTokens > 0 && report.GlobalUsedTokens > cfg.GlobalMaxTokens) ||
		(cfg.GlobalMaxSteps > 0 && report.GlobalUsedSteps > cfg.GlobalMaxSteps)
	report.Blocked = ShouldBlockByBudgetPolicy(policy, overGlobal)
	if cfg.WarnAt > 0 {
		tokensPct := 0.0
		stepsPct := 0.0
		if cfg.GlobalMaxTokens > 0 {
			tokensPct = float64(report.GlobalUsedTokens) / float64(cfg.GlobalMaxTokens)
		}
		if cfg.GlobalMaxSteps > 0 {
			stepsPct = float64(report.GlobalUsedSteps) / float64(cfg.GlobalMaxSteps)
		}
		report.WarnTriggered = tokensPct >= cfg.WarnAt || stepsPct >= cfg.WarnAt
	}
	return report
}

