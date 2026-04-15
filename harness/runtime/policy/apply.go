package policy

import (
	"context"
	"fmt"
	"strings"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
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
		return builtins.PolicyCheck(rules...)(ctx, ev)
	})
}

func installSessionSyncHook(k *kernel.Kernel, st *policystate.State) {
	if st == nil || st.MarkSessionHookInstalled() {
		return
	}
	k.InstallPlugin(kernel.Plugin{
		Name: toolPolicySessionName,
		OnSessionLifecycle: func(_ context.Context, ev *session.LifecycleEvent) error {
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
		},
	})
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
