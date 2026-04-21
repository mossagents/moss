package policy

import (
	"context"
	"fmt"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/harness/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/guardian"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	kplugin "github.com/mossagents/moss/kernel/plugin"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const (
	toolPolicySessionName = "runtime-tool-policy-session-sync"
)

func Apply(k *kernel.Kernel, policy ToolPolicy) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	if err := ValidateToolPolicy(policy); err != nil {
		return err
	}
	policy = NormalizeToolPolicy(policy)
	payload, err := EncodeToolPolicyMetadata(policy)
	if err != nil {
		return fmt.Errorf("encode tool policy metadata: %w", err)
	}
	summary := session.EncodeToolPolicySummary(SummarizeToolPolicy(policy))
	rules := CompileRules(policy)

	st := policystate.Ensure(k)
	st.Set(payload, summary, rules)
	installPolicyHook(k, st)
	installSessionSyncHook(k, st)
	syncExistingSessions(k, st)
	return nil
}

func ApplyResolved(k *kernel.Kernel, workspace, trust, approvalMode string) error {
	return Apply(k, ResolveToolPolicyForWorkspace(workspace, trust, approvalMode))
}

func Current(k *kernel.Kernel) (ToolPolicy, bool) {
	st, ok := policystate.Lookup(k)
	if !ok {
		return ToolPolicy{}, false
	}
	return DecodeToolPolicyMetadata(st.Payload())
}

// PolicyOf returns the effective tool policy for the kernel, falling back to
// a restricted default if no policy has been applied.
func PolicyOf(k *kernel.Kernel) ToolPolicy {
	if policy, ok := Current(k); ok {
		return policy
	}
	return ResolveToolPolicyForWorkspace("", appconfig.TrustRestricted, "confirm")
}

// PolicyForContext merges session-level granted permissions into the base policy.
func PolicyForContext(_ context.Context, tctx tool.ToolCallContext, k *kernel.Kernel, base ToolPolicy) ToolPolicy {
	if k == nil || strings.TrimSpace(tctx.SessionID) == "" {
		return base
	}
	sess, ok := k.SessionManager().Get(tctx.SessionID)
	if !ok {
		return base
	}
	return MergeToolPolicyPermissions(base, session.GrantedPermissionsOf(sess))
}

// MergeWithPermissions is an alias for MergeToolPolicyPermissions for use in
// packages that import runtime/policy directly.
func MergeWithPermissions(policy ToolPolicy, perms io.PermissionProfile) ToolPolicy {
	return MergeToolPolicyPermissions(policy, perms)
}

func installPolicyHook(k *kernel.Kernel, st *policystate.State) {
	if st == nil || st.MarkToolHookInstalled() {
		return
	}
	// 设置不可绕过的权限门控（拦截器无法绕过此门控）。
	k.SetToolPolicyGate(func(ctx context.Context, ev *hooks.ToolEvent) error {
		current, ok := policystate.Lookup(k)
		if !ok {
			return nil
		}
		rules := current.CompiledRules()
		if len(rules) == 0 {
			return nil
		}
		return governance.PolicyCheckWithAutoApprove(chainedAutoApproval(k), rules...)(ctx, ev)
	})
}

// chainedAutoApproval 将 EventStore-based 审批查询与 Guardian 链接。
// 查询顺序：
// 1. EventStore 持久审批记录（跨重启恢复）；
// 2. Guardian AI 自动审批。
func chainedAutoApproval(k *kernel.Kernel) governance.AutoApprovalFunc {
	return func(ctx context.Context, ev *hooks.ToolEvent, req *io.ApprovalRequest) *io.ApprovalDecision {
		// 1. 查询 EventStore （跨重启的持久审批 cache）
		if ev != nil && ev.Session != nil && req != nil && strings.TrimSpace(req.CacheKey) != "" {
			if entry, found := k.LookupSessionApproval(ctx, ev.Session.ID, req.CacheKey); found {
				return io.NormalizeApprovalDecisionForRequest(req, &io.ApprovalDecision{
					RequestID: req.ID,
					Type:      io.ApprovalDecisionType(entry.DecisionType),
					Approved:  entry.Approved,
					Reason:    "recalled from event store approval history",
					Source:    "event-store-cache",
					DecidedAt: entry.ResolvedAt,
				})
			}
		}
		// 2. Guardian 自动审批
		return guardianApprovalCheck(ctx, k, ev, req)
	}
}

func guardianAutoApproval(k *kernel.Kernel) governance.AutoApprovalFunc {
	return func(ctx context.Context, ev *hooks.ToolEvent, req *io.ApprovalRequest) *io.ApprovalDecision {
		return guardianApprovalCheck(ctx, k, ev, req)
	}
}

func guardianApprovalCheck(ctx context.Context, k *kernel.Kernel, ev *hooks.ToolEvent, req *io.ApprovalRequest) *io.ApprovalDecision {
	if k == nil || ev == nil || ev.Tool == nil || req == nil {
		return nil
	}
	if ev.Tool.EffectiveApprovalClass() != tool.ApprovalClassPolicyGuarded {
		return nil
	}
	g, ok := guardian.Lookup(k)
	if !ok || g == nil {
		return nil
	}
	review, err := g.ReviewToolApproval(ctx, guardian.ReviewInput{
		SessionID:  req.SessionID,
		ToolName:   req.ToolName,
		Risk:       req.Risk,
		Category:   string(req.Category),
		Reason:     req.Reason,
		ReasonCode: req.ReasonCode,
		Input:      req.Input,
	})
	emitGuardianReviewEvent(ctx, ev, req, review, err)
	if err != nil {
		return nil
	}
	return guardian.AutoApprovalDecision(req, review)
}

func emitGuardianReviewEvent(ctx context.Context, ev *hooks.ToolEvent, req *io.ApprovalRequest, review *guardian.ReviewResult, err error) {
	if ev == nil || ev.Observer == nil || req == nil {
		return
	}
	outcome := "fallback"
	metadata := map[string]any{
		"source":  "guardian",
		"outcome": outcome,
	}
	if err != nil {
		outcome = "fallback_error"
		metadata["error"] = err.Error()
	} else if review == nil {
		outcome = "fallback_nil"
	} else {
		metadata["approved"] = review.Approved
		metadata["confidence"] = review.Confidence
		metadata["reason"] = review.Reason
		if review.Approved && strings.EqualFold(review.Confidence, "high") {
			outcome = "auto_approved"
		}
	}
	metadata["outcome"] = outcome
	observe.ObserveExecutionEvent(ctx, ev.Observer, observe.ExecutionEvent{
		Type:        observe.ExecutionGuardianReviewed,
		SessionID:   req.SessionID,
		Timestamp:   time.Now().UTC(),
		Phase:       "approval",
		Actor:       "guardian",
		PayloadKind: "guardian_review",
		ToolName:    req.ToolName,
		Risk:        req.Risk,
		ReasonCode:  req.ReasonCode,
		Enforcement: req.Enforcement,
		Metadata:    metadata,
	})
}

func installSessionSyncHook(k *kernel.Kernel, st *policystate.State) {
	if st == nil || st.MarkSessionHookInstalled() {
		return
	}
	_ = k.InstallPlugin(kplugin.SessionLifecycleHook(toolPolicySessionName, 0, func(_ context.Context, ev *session.LifecycleEvent) error {
		if ev == nil || ev.Session == nil {
			return nil
		}
		switch ev.Stage {
		case session.LifecycleCreated, session.LifecycleStarted:
			current, ok := policystate.Lookup(k)
			if ok {
				syncSessionMetadata(ev.Session, current)
			}
		}
		return nil
	}))
}

func syncExistingSessions(k *kernel.Kernel, st *policystate.State) {
	if k == nil || st == nil || k.SessionManager() == nil {
		return
	}
	for _, sess := range k.SessionManager().List() {
		syncSessionMetadata(sess, st)
	}
}

func syncSessionMetadata(sess *session.Session, st *policystate.State) {
	if sess == nil || st == nil {
		return
	}
	payload := st.Payload()
	summary := st.Summary()
	if len(payload) == 0 || len(summary) == 0 {
		return
	}
	sess.SetMetadataBatch(map[string]any{
		session.MetadataToolPolicy:        payload,
		session.MetadataToolPolicySummary: summary,
	})
}
