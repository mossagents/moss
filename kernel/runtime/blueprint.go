package runtime

import "github.com/mossagents/moss/kernel/model"

// ─────────────────────────────────────────────
// RuntimeRequest（§5.1）
// ─────────────────────────────────────────────

// RuntimeRequest 是所有入口进入 runtime 的唯一输入对象。
// 它只表达入口收集到的原始选择，不包含任何已编译的运行时语义。
type RuntimeRequest struct {
	// RunMode 运行模式（interactive / scheduled / api / resume / fork / checkpoint_replay）。
	RunMode string `json:"run_mode,omitempty"`
	// CollaborationMode 协作模式（single / multi-agent 等）。
	CollaborationMode string `json:"collaboration_mode,omitempty"`
	// WorkspaceTrust 工作区信任级别（none / local / full）。
	WorkspaceTrust string `json:"workspace_trust,omitempty"`
	// PermissionProfile 权限配置名（如 "read-only"、"workspace-write"）。
	// approval_mode 在入口层必须先映射为 PermissionProfile，之后消失。
	PermissionProfile string `json:"permission_profile,omitempty"`
	// PromptPack 指定产品场景 prompt pack 标识符。
	PromptPack string `json:"prompt_pack,omitempty"`
	// SessionPolicy 会话策略名。
	SessionPolicy string `json:"session_policy,omitempty"`
	// ModelProfile 模型配置名。
	ModelProfile string `json:"model_profile,omitempty"`
	// Workspace 工作区路径或标识。
	Workspace string `json:"workspace,omitempty"`
	// RestoreSource 恢复来源（session_id / checkpoint_id）。
	RestoreSource string `json:"restore_source,omitempty"`
	// UserGoal 用户目标描述。
	UserGoal string `json:"user_goal,omitempty"`
}

// ─────────────────────────────────────────────
// SessionBlueprint（§5.2）
// ─────────────────────────────────────────────

// SessionBlueprint 是 RequestResolver 编译后的唯一运行时真相源。
// 它作为 session 初始化的唯一配置输入，同时作为
// resume / fork / checkpoint / review 的解释基线。
type SessionBlueprint struct {
	// Identity session 标识信息。
	Identity BlueprintIdentity `json:"identity"`
	// ModelConfig 模型配置（含 provider 路由策略）。
	ModelConfig BlueprintModelConfig `json:"model_config"`
	// EffectiveToolPolicy 经 PolicyCompiler 编译后的有效工具权限策略。
	EffectiveToolPolicy EffectiveToolPolicy `json:"effective_tool_policy"`
	// ContextBudget 上下文预算（主 token + thinking token + step）。
	ContextBudget ContextBudget `json:"context_budget"`
	// PromptPlan prompt 编译计划。
	PromptPlan PromptPlan `json:"prompt_plan"`
	// PersistencePlan 持久化策略配置。
	PersistencePlan PersistencePlan `json:"persistence_plan"`
	// CheckpointPlan checkpoint 策略配置。
	CheckpointPlan CheckpointPlan `json:"checkpoint_plan"`
	// CollaborationContract 协作合同（含 role transition rules）。
	CollaborationContract CollaborationContract `json:"collaboration_contract,omitempty"`
	// SessionBudget 整体 session 级预算（step / time）。
	SessionBudget SessionBudget `json:"session_budget,omitempty"`
	// Provenance blueprint 来源与版本信息（用于幂等校验和审计）。
	Provenance BlueprintProvenance `json:"provenance"`
	// ExecutionAffinity 可选，声明 session 对执行节点的亲和性偏好（§15.2）。
	ExecutionAffinity *ExecutionAffinity `json:"execution_affinity,omitempty"`
}

// BlueprintIdentity 标识 session 身份。
type BlueprintIdentity struct {
	SessionID   string `json:"session_id"`
	AgentName   string `json:"agent_name,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// BlueprintModelConfig 模型配置。
type BlueprintModelConfig struct {
	Provider     string            `json:"provider"`
	ModelID      string            `json:"model_id"`
	RouterConfig map[string]string `json:"router_config,omitempty"`
	// ThinkingEnabled 控制 extended thinking 的启用与禁用（§14.10）。
	ThinkingEnabled bool           `json:"thinking_enabled,omitempty"`
	ExtraParams     map[string]any `json:"extra_params,omitempty"`
}

// EffectiveToolPolicy 经 PolicyCompiler 编译后的有效工具权限策略（§7.1）。
type EffectiveToolPolicy struct {
	// PolicyHash 用于事件中的 policy hash 关联。
	PolicyHash            string   `json:"policy_hash"`
	TrustLevel            string   `json:"trust_level"`
	AllowedTools          []string `json:"allowed_tools,omitempty"`
	DeniedTools           []string `json:"denied_tools,omitempty"`
	ApprovalRequiredTools []string `json:"approval_required_tools,omitempty"`
	// Raw 原始策略快照（JSON），供 PolicyCompiler 重建或审计。
	Raw map[string]any `json:"raw,omitempty"`
}

// ContextBudget 上下文预算配置（§14.4、§14.10）。
type ContextBudget struct {
	// MainTokenBudget 主 token 预算。
	MainTokenBudget int `json:"main_token_budget"`
	// ThinkingTokenBudget extended thinking 独立 token 预算（§14.10）。
	ThinkingTokenBudget int `json:"thinking_token_budget,omitempty"`
}

// PromptPlan prompt 编译计划（§5.2 prompt_plan 约束）。
type PromptPlan struct {
	PromptPackID       string   `json:"prompt_pack_id"`
	RoleOverlayID      string   `json:"role_overlay_id,omitempty"`
	EnabledProviderIDs []string `json:"enabled_provider_ids,omitempty"`
	PromptBudgetPolicy string   `json:"prompt_budget_policy,omitempty"`
}

// PersistencePlan 持久化策略配置。
type PersistencePlan struct {
	// StoreKind EventStore 实现种类（"sqlite" / "jsonl"）。
	StoreKind string `json:"store_kind"`
	StoreDSN  string `json:"store_dsn,omitempty"`
}

// CheckpointPlan checkpoint 策略配置。
type CheckpointPlan struct {
	// AutoCheckpointEveryNTurns 每 N 个 turn 自动 checkpoint，0 表示禁用。
	AutoCheckpointEveryNTurns int  `json:"auto_checkpoint_every_n_turns,omitempty"`
	CaptureWorkspaceSnapshot  bool `json:"capture_workspace_snapshot,omitempty"`
}

// RoleTransitionType 角色切换时机（§7.2 collaboration_contract）。
type RoleTransitionType string

const (
	RoleTransitionImmediate        RoleTransitionType = "immediate"
	RoleTransitionNextTurnBoundary RoleTransitionType = "next_turn_boundary"
)

// RoleTransitionRule 定义单条角色切换规则（§7.2）。
type RoleTransitionRule struct {
	FromRole         string             `json:"from_role"`
	ToRole           string             `json:"to_role"`
	TriggerCondition string             `json:"trigger_condition,omitempty"`
	TransitionType   RoleTransitionType `json:"transition_type"`
}

// CollaborationContract 协作合同（§7.2 子 Agent 约束）。
type CollaborationContract struct {
	RoleTransitionRules []RoleTransitionRule `json:"role_transition_rules,omitempty"`
}

// SessionBudget session 级预算（step / time）。
type SessionBudget struct {
	MaxSteps int `json:"max_steps,omitempty"`
	// MaxTimeSeconds session 最大运行时间（秒），0 表示不限制。
	MaxTimeSeconds int `json:"max_time_seconds,omitempty"`
}

// BlueprintProvenance blueprint 来源与版本信息（§5.2 provenance 约束）。
type BlueprintProvenance struct {
	BlueprintSchemaVersion string `json:"blueprint_schema_version"`
	ResolverBuildVersion   string `json:"resolver_build_version"`
	ResolverCatalogDigest  string `json:"resolver_catalog_digest"`
	ProviderSetDigest      string `json:"provider_set_digest"`
	// Hash 整个 blueprint 的稳定 hash，用于幂等校验。
	Hash string `json:"hash,omitempty"`
}

// AffinityMode 声明 session 对执行节点的亲和性模式（§15.2）。
type AffinityMode string

const (
	AffinityModeNone         AffinityMode = "none"
	AffinityModeNodePinned   AffinityMode = "node_pinned"
	AffinityModeRegionPinned AffinityMode = "region_pinned"
)

// ExecutionAffinity 声明 session 对执行节点的亲和性偏好（§5.2、§15.2）。
type ExecutionAffinity struct {
	AffinityMode    AffinityMode `json:"affinity_mode"`
	PreferredNodeID string       `json:"preferred_node_id,omitempty"`
	// StickyReason 供调度器和审计使用（如 "has_local_worktree"）。
	StickyReason string `json:"sticky_reason,omitempty"`
}

// ─────────────────────────────────────────────
// PromptLayerProvider（§7.2）
// ─────────────────────────────────────────────

// PersistenceScope 声明 layer 的生命周期（§7.2、§7.3 layer 提升规则）。
type PersistenceScope string

const (
	PersistenceScopePersistent PersistenceScope = "persistent"
	PersistenceScopeTaskScoped PersistenceScope = "task-scoped"
	PersistenceScopeTurnLocal  PersistenceScope = "turn-local"
)

// PromptLayerProvider 是 PromptCompiler 消费的最小结构单元（§7.2）。
// content_parts 替代旧的单字段 content 设计，支持多模态内容。
type PromptLayerProvider struct {
	LayerID string `json:"layer_id"`
	// Scope 决定 layer 在 prompt 中的层级（system / user / assistant）。
	Scope    string `json:"scope"`
	Priority int    `json:"priority"`
	// ContentParts 对齐 model.ContentPart，支持文本、图片、文件等多模态内容。
	ContentParts []model.ContentPart `json:"content_parts"`
	DedupeKey    string              `json:"dedupe_key,omitempty"`
	Provenance   string              `json:"provenance,omitempty"`
	// PersistenceScope 声明 layer 的生命周期。
	PersistenceScope PersistenceScope `json:"persistence_scope"`
	// TaskBoundaryEventID 仅 PersistenceScope = task-scoped 时必填，
	// 绑定到对应的 task boundary 事件。
	TaskBoundaryEventID string `json:"task_boundary_event_id,omitempty"`
}
