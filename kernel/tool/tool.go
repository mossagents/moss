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
	// MCPServerID MCP 服务端标识（§14.3）。仅 MCP 桥接工具设置此字段。
	MCPServerID string `json:"mcp_server_id,omitempty"`
	// MCPToolName MCP 服务端内部工具名（无前缀，§14.3）。
	MCPToolName string `json:"mcp_tool_name,omitempty"`
}

// Tool 是可被 Agent 调用的工具接口。
// 每个 Tool 同时携带元信息（ToolSpec）和执行逻辑（Execute）。
type Tool interface {
	// Name 返回工具的唯一名称。
	Name() string
	// Description 返回工具的人类可读描述。
	Description() string
	// Spec 返回完整的工具元信息。
	Spec() ToolSpec
	// Execute 执行工具。args 是 LLM 生成的 JSON 参数。
	Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// ToolHandler 是原始工具执行函数的类型别名。
// 新代码应使用 NewFunctionTool 或 NewRawTool 构建 Tool 接口实例。
type ToolHandler = func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

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
		if s.Risk == RiskLow {
			return []Effect{EffectReadOnly}
		}
		return []Effect{EffectWritesWorkspace}
	case s.Risk == RiskHigh:
		return []Effect{EffectExternalSideEffect}
	case s.Risk == RiskMedium:
		return []Effect{EffectGraphMutation}
	case isReadLikeToolName(s.Name):
		return []Effect{EffectReadOnly}
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
	if s.RequiresWorkspace {
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

// IsExplicitlyReadOnly returns true only when read-only is explicitly declared
// via the Effects field. Tools without explicit effect declarations are
// conservatively treated as non-read-only for concurrency safety (fail-closed).
func (s ToolSpec) IsExplicitlyReadOnly() bool {
	return len(s.Effects) > 0 && s.IsReadOnly()
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
