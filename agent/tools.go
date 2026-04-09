package agent

import (
	"context"

	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

// Delegator 是 agent 包对 Kernel 能力的抽象，避免循环依赖。
// 由 Kernel 实现。
type Delegator interface {
	NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error)
	RunWithTools(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error)
	ToolRegistry() tool.Registry
}

// RuntimeDeps 描述委派工具可选依赖。
type RuntimeDeps struct {
	TaskRuntime taskrt.TaskRuntime
	Mailbox     taskrt.Mailbox
	Isolation   workspace.WorkspaceIsolation
}

// RegisterTools 向工具注册表注册委派相关工具。
func RegisterTools(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	return RegisterToolsWithDeps(reg, agents, tracker, delegator, RuntimeDeps{})
}

// RegisterToolsWithDeps 向工具注册表注册委派与协作相关工具。
func RegisterToolsWithDeps(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator, deps RuntimeDeps) error {
	if err := registerDelegate(reg, agents, delegator); err != nil {
		return err
	}
	if err := registerSpawn(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if err := registerQuery(reg, tracker); err != nil {
		return err
	}
	if err := registerReadAgentTool(reg, tracker); err != nil {
		return err
	}
	if err := registerListAgentsTool(reg, tracker); err != nil {
		return err
	}
	if err := registerListTasks(reg, tracker); err != nil {
		return err
	}
	if err := registerCancelTask(reg, tracker); err != nil {
		return err
	}
	if err := registerUpdateTask(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if err := registerWriteAgentTool(reg, agents, tracker, delegator, deps.TaskRuntime); err != nil {
		return err
	}
	if err := registerWaitAgentTool(reg, tracker); err != nil {
		return err
	}
	if err := registerCloseAgentTool(reg, tracker); err != nil {
		return err
	}
	if err := registerResumeAgentTool(reg, agents, tracker, delegator, deps.TaskRuntime); err != nil {
		return err
	}
	if err := registerTask(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if deps.TaskRuntime != nil {
		if err := registerPlanTask(reg, deps.TaskRuntime); err != nil {
			return err
		}
		if err := registerClaimTask(reg, deps.TaskRuntime); err != nil {
			return err
		}
	}
	if deps.Mailbox != nil {
		if err := registerSendMail(reg, deps.Mailbox); err != nil {
			return err
		}
		if err := registerReadMailbox(reg, deps.Mailbox); err != nil {
			return err
		}
	}
	if deps.Isolation != nil {
		if err := registerAcquireWorkspace(reg, deps.Isolation, deps.TaskRuntime); err != nil {
			return err
		}
		if err := registerReleaseWorkspace(reg, deps.Isolation); err != nil {
			return err
		}
	}
	return nil
}

func agentToolSpec(spec tool.ToolSpec) tool.ToolSpec {
	switch spec.Name {
	case "query_agent", "read_agent", "list_agents", "list_tasks", "wait_agent", "read_mailbox":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
		spec.ResourceScope = []string{"graph:tasks"}
		if spec.Name == "read_mailbox" {
			spec.ResourceScope = []string{"graph:mailbox"}
		}
	case "send_mail":
		spec.Effects = []tool.Effect{tool.EffectGraphMutation}
		spec.ResourceScope = []string{"graph:mailbox"}
		spec.LockScope = []string{"graph:mailbox"}
		spec.SideEffectClass = tool.SideEffectTaskGraph
		spec.ApprovalClass = tool.ApprovalClassPolicyGuarded
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "acquire_workspace", "release_workspace":
		spec.Effects = []tool.Effect{tool.EffectGraphMutation, tool.EffectWritesWorkspace}
		spec.ResourceScope = []string{"graph:workspace", "workspace:lease"}
		spec.LockScope = []string{"graph:workspace", "workspace:lease"}
		spec.SideEffectClass = tool.SideEffectWorkspace
		spec.ApprovalClass = tool.ApprovalClassPolicyGuarded
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "delegate_agent", "spawn_agent", "cancel_task", "update_task", "write_agent", "close_agent", "resume_agent", "task", "plan_task", "claim_task":
		spec.Effects = []tool.Effect{tool.EffectGraphMutation}
		spec.ResourceScope = []string{"graph:tasks"}
		spec.LockScope = []string{"graph:tasks"}
		spec.SideEffectClass = tool.SideEffectTaskGraph
		spec.ApprovalClass = tool.ApprovalClassPolicyGuarded
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	}
	return spec
}

// queueMetrics is shared across tools_lifecycle.go and tools_helpers.go.
type queueMetrics struct {
	ConsumedCount  int
	RemainingCount int
}
