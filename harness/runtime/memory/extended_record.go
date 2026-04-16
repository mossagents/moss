package memstore

import "time"

// MemoryCitationEntry 描述 memory 内容的来源片段。
type MemoryCitationEntry struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	Note      string `json:"note,omitempty"`
}

// MemoryCitation 是 memory 的来源引用集合。
type MemoryCitation struct {
	Entries     []MemoryCitationEntry `json:"entries,omitempty"`
	MemoryPaths []string              `json:"memory_paths,omitempty"`
	RolloutIDs  []string              `json:"rollout_ids,omitempty"`
}

// MemoryStage 表示 memory 的生命周期阶段。
type MemoryStage string

const (
	MemoryStageManual       MemoryStage = "manual"
	MemoryStageSnapshot     MemoryStage = "snapshot"
	MemoryStageConsolidated MemoryStage = "consolidated"
	MemoryStagePromoted     MemoryStage = "promoted"
)

// MemoryStatus 表示 memory 记录的状态。
type MemoryStatus string

const (
	MemoryStatusActive     MemoryStatus = "active"
	MemoryStatusSuperseded MemoryStatus = "superseded"
	MemoryStatusArchived   MemoryStatus = "archived"
)

// ExtendedMemoryRecord 是 harness 层的完整 memory 记录，包含 kernel 核心字段和
// harness 扩展字段（Citation、Stage、源信息等）。
type ExtendedMemoryRecord struct {
	// 核心字段（与 kernel memory.MemoryRecord 对应）
	ID        string         `json:"id"`
	Path      string         `json:"path"`
	Content   string         `json:"content"`
	Summary   string         `json:"summary,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`

	// 扩展字段（harness 层）
	Citation        MemoryCitation `json:"citation,omitempty"`
	Stage           MemoryStage    `json:"stage,omitempty"`
	Status          MemoryStatus   `json:"status,omitempty"`
	Group           string         `json:"group,omitempty"`
	Workspace       string         `json:"workspace,omitempty"`
	CWD             string         `json:"cwd,omitempty"`
	GitBranch       string         `json:"git_branch,omitempty"`
	SourceKind      string         `json:"source_kind,omitempty"`
	SourceID        string         `json:"source_id,omitempty"`
	SourcePath      string         `json:"source_path,omitempty"`
	SourceUpdatedAt time.Time      `json:"source_updated_at,omitempty"`
	UsageCount      int            `json:"usage_count,omitempty"`
	LastUsedAt      time.Time      `json:"last_used_at,omitempty"`
}

// ExtendedMemoryQuery 是 harness 层的完整 memory 查询参数。
type ExtendedMemoryQuery struct {
	Query     string         `json:"query,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Stages    []MemoryStage  `json:"stages,omitempty"`
	Statuses  []MemoryStatus `json:"statuses,omitempty"`
	Group     string         `json:"group,omitempty"`
	Workspace string         `json:"workspace,omitempty"`
	Limit     int            `json:"limit,omitempty"`
}
