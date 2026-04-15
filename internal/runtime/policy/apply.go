package policy

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/internal/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/toolpolicy"
)

const (
	toolPolicySessionName = "runtime-tool-policy-session-sync"
)

func Apply(k *kernel.Kernel, policy toolpolicy.ToolPolicy) error {
	if k == nil {
		return fmt.Errorf("kernel is nil")
	}
	if err := toolpolicy.ValidateToolPolicy(policy); err != nil {
		return err
	}
	policy = toolpolicy.NormalizeToolPolicy(policy)
	payload, err := toolpolicy.EncodeToolPolicyMetadata(policy)
	if err != nil {
		return fmt.Errorf("encode tool policy metadata: %w", err)
	}
	summary := session.EncodeToolPolicySummary(toolpolicy.SummarizeToolPolicy(policy))
	rules := CompileRules(policy)

	st := policystate.Ensure(k)
	st.Set(payload, summary, rules)
	installPolicyHook(k, st)
	installSessionSyncHook(k, st)
	syncExistingSessions(k, st)
	return nil
}

func ApplyResolved(k *kernel.Kernel, workspace, trust, approvalMode string) error {
	return Apply(k, toolpolicy.ResolveToolPolicyForWorkspace(workspace, trust, approvalMode))
}

func Current(k *kernel.Kernel) (toolpolicy.ToolPolicy, bool) {
	st, ok := policystate.Lookup(k)
	if !ok {
		return toolpolicy.ToolPolicy{}, false
	}
	return toolpolicy.DecodeToolPolicyMetadata(st.Payload())
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
