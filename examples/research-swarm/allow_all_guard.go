package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/guardian"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/tool"
)

// researchGuardianPrompt is used when --allow-all is active. Unlike the default
// conservative guardian, this prompt is tailored for autonomous research workflows:
// it approves read operations and targeted data gathering, while still blocking
// genuinely destructive actions (irreversible writes, dangerous shell commands,
// credential access, etc.).
const researchGuardianPrompt = `You are a safety guardian for an autonomous research agent.
Return JSON only with fields: approved(boolean), reason(string), confidence(string).

APPROVE the action if it is:
- An HTTP read/fetch for gathering research data or evidence
- A file read, directory listing, or memory lookup
- Spawning a subtask or reporting results
- A shell command that only reads data (grep, cat, ls, find, curl GET, etc.)

DENY the action if it is:
- Destructive file operations (overwrite, delete, move critical files)
- Shell commands that install software, modify system state, or access credentials
- Actions clearly outside the stated session goal
- Exfiltrating data to unknown third-party endpoints

When in doubt about a read/fetch operation for research, approve it.
Only deny when the risk is concrete and the action is clearly outside research scope.`

// installAllowAllGuard replaces the kernel's tool policy gate with an AI-based
// guardian check. All tool calls with observable side effects are reviewed by
// the Guardian LLM; the guardian's decision is final — no human fallback prompt
// is shown. If the Guardian approves, the call proceeds; otherwise it is denied.
//
// Intended for --allow-all mode: the user wants no interactive approval prompts,
// but still maintains an automated AI safety review for every side-effectful
// operation. Tools with no side effects (read-only) pass through freely.
func installAllowAllGuard(k *kernel.Kernel) error {
	if k == nil {
		return fmt.Errorf("kernel required for --allow-all guard")
	}
	// Ensure Guardian is installed using the kernel's own LLM with the
	// research-appropriate system prompt.
	if _, ok := guardian.Lookup(k); !ok {
		llm := k.LLM()
		if llm == nil {
			return fmt.Errorf("--allow-all requires a model: no LLM available for AI permission guard")
		}
		g := guardian.New(llm, model.ModelConfig{Temperature: 0})
		g.SystemPrompt = researchGuardianPrompt
		guardian.Install(k, g)
	}
	// This replaces the interactive confirm-mode gate installed by the harness.
	k.SetToolPolicyGate(func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev == nil || ev.Stage != hooks.ToolLifecycleBefore || ev.Tool == nil {
			return nil
		}
		// Tools with no side effects and no approval requirement pass freely.
		if ev.Tool.EffectiveSideEffectClass() == tool.SideEffectNone &&
			ev.Tool.EffectiveApprovalClass() == tool.ApprovalClassNone {
			return nil
		}
		g, ok := guardian.Lookup(k)
		if !ok || g == nil {
			return fmt.Errorf("[allow-all] AI guardian not available for tool %q", ev.Tool.Name)
		}
		sessionID := ""
		sessionGoal := ""
		if ev.Session != nil {
			sessionID = ev.Session.ID
			sessionGoal = ev.Session.Config.Goal
		}
		review, err := g.ReviewToolApproval(ctx, guardian.ReviewInput{
			SessionID:   sessionID,
			SessionGoal: sessionGoal,
			ToolName:    ev.Tool.Name,
			Risk:        string(ev.Tool.Risk),
			Category:    string(ev.Tool.EffectiveSideEffectClass()),
			Input:       ev.Input,
		})
		if err != nil {
			// Fail-closed: a guardian error prevents the tool call.
			return fmt.Errorf("[allow-all] AI guard error for %q: %w", ev.Tool.Name, err)
		}
		if review == nil || !review.Approved {
			reason := "AI guardian denied this operation"
			if review != nil && strings.TrimSpace(review.Reason) != "" {
				reason = review.Reason
			}
			return fmt.Errorf("[allow-all] AI denied %q: %s", ev.Tool.Name, reason)
		}
		return nil
	})
	return nil
}
