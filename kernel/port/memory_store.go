package port

import (
	"context"
	"time"
)

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

type MemoryStage string

const (
	MemoryStageManual       MemoryStage = "manual"
	MemoryStageSnapshot     MemoryStage = "snapshot"
	MemoryStageConsolidated MemoryStage = "consolidated"
)

type MemoryStatus string

const (
	MemoryStatusActive     MemoryStatus = "active"
	MemoryStatusSuperseded MemoryStatus = "superseded"
	MemoryStatusArchived   MemoryStatus = "archived"
)

// MemoryRecord 是结构化 memory 记录。
type MemoryRecord struct {
	ID              string         `json:"id"`
	Path            string         `json:"path"`
	Content         string         `json:"content"`
	Summary         string         `json:"summary,omitempty"`
	Tags            []string       `json:"tags,omitempty"`
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
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
}

// MemoryQuery 是 memory 查询参数。
type MemoryQuery struct {
	Query     string         `json:"query,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Stages    []MemoryStage  `json:"stages,omitempty"`
	Statuses  []MemoryStatus `json:"statuses,omitempty"`
	Group     string         `json:"group,omitempty"`
	Workspace string         `json:"workspace,omitempty"`
	Limit     int            `json:"limit,omitempty"`
}

// MemoryStore 提供结构化 memory 的持久化与检索能力。
type MemoryStore interface {
	Upsert(ctx context.Context, record MemoryRecord) (*MemoryRecord, error)
	GetByPath(ctx context.Context, path string) (*MemoryRecord, error)
	DeleteByPath(ctx context.Context, path string) error
	List(ctx context.Context, limit int) ([]MemoryRecord, error)
	Search(ctx context.Context, query MemoryQuery) ([]MemoryRecord, error)
	RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error
}
