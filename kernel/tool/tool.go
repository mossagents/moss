package tool

import (
	"context"
	"encoding/json"
	"strings"
)

// RiskLevel 表示工具的风险等级。
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"    // 只读操作
	RiskMedium RiskLevel = "medium" // 有限副作用
	RiskHigh   RiskLevel = "high"   // 文件写入、命令执行
)

// Effect 表示工具的细粒度执行语义。
type Effect string

const (
	EffectReadOnly           Effect = "read_only"
	EffectWritesWorkspace    Effect = "writes_workspace"
	EffectWritesMemory       Effect = "writes_memory"
	EffectExternalSideEffect Effect = "external_side_effect"
	EffectGraphMutation      Effect = "graph_mutation"
)

// SideEffectClass 是调度、审批、持久化使用的粗粒度副作用分类。
type SideEffectClass string

const (
	SideEffectNone      SideEffectClass = "none"
	SideEffectWorkspace SideEffectClass = "workspace"
	SideEffectMemory    SideEffectClass = "memory"
	SideEffectNetwork   SideEffectClass = "network"
	SideEffectProcess   SideEffectClass = "process"
	SideEffectTaskGraph SideEffectClass = "task_graph"
)

// ApprovalClass 表示工具默认需要的审批等级。
type ApprovalClass string

const (
	ApprovalClassNone           ApprovalClass = "none"
	ApprovalClassPolicyGuarded  ApprovalClass = "policy_guarded"
	ApprovalClassExplicitUser   ApprovalClass = "explicit_user_approval"
	ApprovalClassSupervisorOnly ApprovalClass = "supervisor_only"
)

// PlannerVisibility 表示 planner 能否直接暴露并调度该工具。
type PlannerVisibility string

const (
	PlannerVisibilityVisible                PlannerVisibility = "visible"
	PlannerVisibilityVisibleWithConstraints PlannerVisibility = "visible_with_constraints"
	PlannerVisibilityHidden                 PlannerVisibility = "hidden"
)

// CommutativityClass 表示副作用并发时的可交换性等级。
type CommutativityClass string

const (
	CommutativityNonCommutative   CommutativityClass = "non_commutative"
	CommutativityTargetSafe       CommutativityClass = "target_commutative"
	CommutativityFullyCommutative CommutativityClass = "fully_commutative"
)

// ToolSpec 描述一个工具的元信息。
type ToolSpec struct {
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	InputSchema        json.RawMessage    `json:"input_schema"`
	Risk               RiskLevel          `json:"risk"`
	Capabilities       []string           `json:"capabilities,omitempty"`
	Effects            []Effect           `json:"effects,omitempty"`
	ResourceScope      []string           `json:"resource_scope,omitempty"`
	LockScope          []string           `json:"lock_scope,omitempty"`
	Idempotent         bool               `json:"idempotent,omitempty"`
	SideEffectClass    SideEffectClass    `json:"side_effect_class,omitempty"`
	ApprovalClass      ApprovalClass      `json:"approval_class,omitempty"`
	PlannerVisibility  PlannerVisibility  `json:"planner_visibility,omitempty"`
	CommutativityClass CommutativityClass `json:"commutativity_class,omitempty"`
	Source             string             `json:"source,omitempty"`
	Owner              string             `json:"owner,omitempty"`
	RequiresWorkspace  bool               `json:"requires_workspace,omitempty"`
	RequiresExecutor   bool               `json:"requires_executor,omitempty"`
	RequiresSandbox    bool               `json:"requires_sandbox,omitempty"`
}

// ToolHandler 是工具的执行函数。
type ToolHandler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// EffectiveEffects returns the configured effects or a deterministic fallback.
func (s ToolSpec) EffectiveEffects() []Effect {
	if len(s.Effects) > 0 {
		return normalizeEffects(s.Effects)
	}
	switch {
	case hasCapability(s.Capabilities, "execution"), hasCapability(s.Capabilities, "network"):
		return []Effect{EffectExternalSideEffect}
	case hasCapability(s.Capabilities, "memory"):
		if s.Risk == RiskLow && isReadLikeToolName(s.Name) {
			return []Effect{EffectReadOnly}
		}
		return []Effect{EffectWritesMemory}
	case hasCapability(s.Capabilities, "delegation"), hasCapability(s.Capabilities, "planning"), hasCapability(s.Capabilities, "scheduling"), hasCapability(s.Capabilities, "context"):
		if s.Risk == RiskLow && isReadLikeToolName(s.Name) {
			return []Effect{EffectReadOnly}
		}
		return []Effect{EffectGraphMutation}
	case hasCapability(s.Capabilities, "filesystem"), hasCapability(s.Capabilities, "workspace"):
		if s.Risk == RiskLow || isReadLikeToolName(s.Name) {
			return []Effect{EffectReadOnly}
		}
		return []Effect{EffectWritesWorkspace}
	case isReadLikeToolName(s.Name):
		return []Effect{EffectReadOnly}
	case s.Risk == RiskHigh:
		return []Effect{EffectExternalSideEffect}
	case s.Risk == RiskMedium:
		return []Effect{EffectGraphMutation}
	default:
		return []Effect{EffectReadOnly}
	}
}

func (s ToolSpec) EffectiveSideEffectClass() SideEffectClass {
	if s.SideEffectClass != "" {
		return s.SideEffectClass
	}
	for _, effect := range s.EffectiveEffects() {
		switch effect {
		case EffectWritesWorkspace:
			return SideEffectWorkspace
		case EffectWritesMemory:
			return SideEffectMemory
		case EffectExternalSideEffect:
			if hasCapability(s.Capabilities, "network") {
				return SideEffectNetwork
			}
			return SideEffectProcess
		case EffectGraphMutation:
			return SideEffectTaskGraph
		}
	}
	return SideEffectNone
}

func (s ToolSpec) EffectiveApprovalClass() ApprovalClass {
	if s.ApprovalClass != "" {
		return s.ApprovalClass
	}
	switch s.Risk {
	case RiskHigh:
		return ApprovalClassExplicitUser
	case RiskMedium:
		return ApprovalClassPolicyGuarded
	default:
		return ApprovalClassNone
	}
}

func (s ToolSpec) EffectivePlannerVisibility() PlannerVisibility {
	if s.PlannerVisibility != "" {
		return s.PlannerVisibility
	}
	if s.RequiresWorkspace || s.RequiresExecutor || s.RequiresSandbox {
		return PlannerVisibilityVisibleWithConstraints
	}
	return PlannerVisibilityVisible
}

func (s ToolSpec) EffectiveCommutativityClass() CommutativityClass {
	if s.CommutativityClass != "" {
		return s.CommutativityClass
	}
	return CommutativityNonCommutative
}

func (s ToolSpec) IsReadOnly() bool {
	effects := s.EffectiveEffects()
	return len(effects) == 1 && effects[0] == EffectReadOnly
}

func normalizeEffects(in []Effect) []Effect {
	out := make([]Effect, 0, len(in))
	seen := make(map[Effect]struct{}, len(in))
	for _, effect := range in {
		if effect == "" {
			continue
		}
		if _, ok := seen[effect]; ok {
			continue
		}
		seen[effect] = struct{}{}
		out = append(out, effect)
	}
	if len(out) == 0 {
		return []Effect{EffectReadOnly}
	}
	return out
}

func hasCapability(caps []string, want string) bool {
	for _, cap := range caps {
		if strings.EqualFold(strings.TrimSpace(cap), want) {
			return true
		}
	}
	return false
}

func isReadLikeToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(name, "read_"),
		strings.HasPrefix(name, "list_"),
		strings.HasPrefix(name, "search_"),
		strings.HasPrefix(name, "query_"),
		strings.HasPrefix(name, "get_"):
		return true
	}
	switch name {
	case "ls", "glob", "grep", "datetime", "read_agent":
		return true
	default:
		return false
	}
}
