package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/memory"
	 memstore "github.com/mossagents/moss/runtime/memory"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

const memoryStateKey kernel.ServiceKey = "memory.state"

type memoryState struct {
	workspace workspace.Workspace
	store     memory.MemoryStore
	runtime   taskrt.TaskRuntime
	pipeline  *memstore.PipelineManager
}

// WithMemoryWorkspace configures the runtime-owned memory substrate on the kernel.
func WithMemoryWorkspace(ws workspace.Workspace) kernel.Option {
	return func(k *kernel.Kernel) {
		st := ensureMemoryState(k)
		st.workspace = ws
		if ws != nil && st.store == nil {
			st.store = memstore.NewWorkspaceMemoryStore(ws)
		}
	}
}

func WithMemoryStore(store memory.MemoryStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureMemoryState(k).store = store
	}
}

// NewSQLiteMemoryStore creates a SQLite-backed memory store.
func NewSQLiteMemoryStore(dbPath string) (memory.MemoryStore, error) {
	return memstore.NewSQLiteMemoryStore(dbPath)
}

// ensureMemoryState owns the memory substrate slot on the kernel service registry.
func ensureMemoryState(k *kernel.Kernel) *memoryState {
	actual, loaded := k.Services().LoadOrStore(memoryStateKey, &memoryState{})
	st := actual.(*memoryState)
	if loaded {
		return st
	}
	k.Stages().OnBoot(120, func(_ context.Context, k *kernel.Kernel) error {
		if st.workspace == nil {
			if st.store != nil || st.runtime != nil {
				return fmt.Errorf("memory workspace is nil")
			}
			return nil
		}
		if st.store == nil {
			st.store = memstore.NewWorkspaceMemoryStore(st.workspace)
		}
		if st.runtime == nil {
			st.runtime = k.TaskRuntime()
		}
		if st.runtime == nil {
			st.runtime = taskrt.NewMemoryTaskRuntime()
		}
		if st.pipeline == nil {
			st.store = newIndexedMemoryStore(st.store, StateCatalogOf(k))
			st.pipeline = memstore.NewPipelineManager(st.workspace, st.store, st.runtime)
			st.pipeline.Start()
		}
		return registerMemoryToolsWithPipeline(k.ToolRegistry(), st.workspace, st.store, st.pipeline)
	})
	k.Stages().OnShutdown(120, func(_ context.Context, _ *kernel.Kernel) error {
		if st.pipeline != nil {
			st.pipeline.Stop()
		}
		closer, ok := st.store.(io.Closer)
		if !ok || closer == nil {
			return nil
		}
		return closer.Close()
	})
	k.Prompts().Add(220, func(_ *kernel.Kernel) string {
		if st.workspace == nil {
			return ""
		}
		return "You have staged persistent memory tools backed by /memories. Prefer memory_summary.md and MEMORY.md for quick context, then inspect rollout_summaries/ or individual memory records when needed."
	})
	return st
}

func memoryStateOf(k *kernel.Kernel) *memoryState {
	if k == nil {
		return nil
	}
	actual, ok := k.Services().Load(memoryStateKey)
	if !ok {
		return nil
	}
	st, _ := actual.(*memoryState)
	return st
}

// MemoryStoreOf looks up the runtime-owned memory store without creating memory
// substrate state on first access.
func MemoryStoreOf(k *kernel.Kernel) memory.MemoryStore {
	if st := memoryStateOf(k); st != nil {
		return st.store
	}
	return nil
}

func registerMemoryToolsWithPipeline(reg tool.Registry, ws workspace.Workspace, store memory.MemoryStore, pipeline *memstore.PipelineManager) error {
	if ws == nil {
		return fmt.Errorf("memory workspace is nil")
	}
	if store == nil {
		return fmt.Errorf("memory store is nil")
	}
	tools := []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{readMemorySpec, readMemoryHandler(ws, store, pipeline)},
		{writeMemorySpec, writeMemoryHandler(ws, store, pipeline)},
		{listMemoriesSpec, listMemoriesHandler(store)},
		{deleteMemorySpec, deleteMemoryHandler(ws, store, pipeline)},
		{readMemoryRecordSpec, readMemoryRecordHandler(store)},
		{writeMemoryRecordSpec, writeMemoryRecordHandler(ws, store, pipeline)},
		{searchMemoriesSpec, searchMemoriesHandler(store)},
		{ingestMemoryTraceSpec, ingestMemoryTraceHandler(pipeline)},
	}
	for _, t := range tools {
		spec := runtimeMemoryToolSpec(t.spec)
		if _, exists := reg.Get(spec.Name); exists {
			continue
		}
		if err := reg.Register(tool.NewRawTool(spec, t.handler)); err != nil {
			return err
		}
	}
	return nil
}

func runtimeMemoryToolSpec(spec tool.ToolSpec) tool.ToolSpec {
	switch spec.Name {
	case "read_memory", "list_memories", "read_memory_record", "search_memories":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.ResourceScope = []string{"memory:*"}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
	case "write_memory", "delete_memory", "write_memory_record":
		spec.Effects = []tool.Effect{tool.EffectWritesMemory}
		spec.ResourceScope = []string{"memory:*"}
		spec.LockScope = []string{"memory:*"}
		spec.SideEffectClass = tool.SideEffectMemory
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "ingest_memory_trace":
		spec.Effects = []tool.Effect{tool.EffectWritesMemory}
		spec.ResourceScope = []string{"memory:*"}
		spec.LockScope = []string{"memory:*"}
		spec.SideEffectClass = tool.SideEffectMemory
		spec.ApprovalClass = tool.ApprovalClassPolicyGuarded
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	}
	return spec
}

var readMemorySpec = tool.ToolSpec{
	Name:        "read_memory",
	Description: "Read a persistent memory file from /memories namespace and refresh usage when it maps to a structured record.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"path":{"type":"string","description":"Memory file path relative to /memories"}},
		"required":["path"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory"},
}

var writeMemorySpec = tool.ToolSpec{
	Name:        "write_memory",
	Description: "Write or update a persistent memory file in /memories namespace.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"Memory file path relative to /memories"},
			"content":{"type":"string","description":"Memory content"}
		},
		"required":["path","content"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"memory"},
}

var listMemoriesSpec = tool.ToolSpec{
	Name:        "list_memories",
	Description: "List persistent memory files under /memories by glob pattern.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"pattern":{"type":"string","description":"Glob pattern (default: **/*)"}}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory"},
}

var deleteMemorySpec = tool.ToolSpec{
	Name:        "delete_memory",
	Description: "Delete a persistent memory file and matching structured record from /memories namespace.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"path":{"type":"string","description":"Memory file path relative to /memories"}},
		"required":["path"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"memory"},
}

var readMemoryRecordSpec = tool.ToolSpec{
	Name:        "read_memory_record",
	Description: "Read a structured memory record with metadata, usage, and citations.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{"path":{"type":"string","description":"Memory record path"}},
		"required":["path"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory"},
}

var writeMemoryRecordSpec = tool.ToolSpec{
	Name:        "write_memory_record",
	Description: "Write a structured memory record with stage, usage metadata, and citations.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"path":{"type":"string"},
			"content":{"type":"string"},
			"summary":{"type":"string"},
			"tags":{"type":"array","items":{"type":"string"}},
			"citation":{"type":"object"},
			"stage":{"type":"string","enum":["manual","snapshot","consolidated","promoted"]},
			"status":{"type":"string","enum":["active","superseded","archived"]},
			"group":{"type":"string"},
			"workspace":{"type":"string"},
			"cwd":{"type":"string"},
			"git_branch":{"type":"string"},
			"source_kind":{"type":"string"},
			"source_id":{"type":"string"},
			"source_path":{"type":"string"},
			"source_updated_at":{"type":"string","description":"RFC3339 timestamp"}
		},
		"required":["path","content"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"memory"},
}

var searchMemoriesSpec = tool.ToolSpec{
	Name:        "search_memories",
	Description: "Search structured memories by text, tags, stage, status, workspace, and group.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"query":{"type":"string"},
			"tags":{"type":"array","items":{"type":"string"}},
			"stages":{"type":"array","items":{"type":"string","enum":["manual","snapshot","consolidated","promoted"]}},
			"statuses":{"type":"array","items":{"type":"string","enum":["active","superseded","archived"]}},
			"group":{"type":"string"},
			"workspace":{"type":"string"},
			"limit":{"type":"integer"}
		}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory"},
}

var ingestMemoryTraceSpec = tool.ToolSpec{
	Name:        "ingest_memory_trace",
	Description: "Queue a staged memory pipeline that converts trace content into snapshot and consolidated memories.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"source_path":{"type":"string","description":"Trace source path"},
			"trace":{"type":"string","description":"Raw trace text (json array or jsonl)"},
			"target_path":{"type":"string","description":"Consolidated memory target path"},
			"tags":{"type":"array","items":{"type":"string"}},
			"workspace":{"type":"string"},
			"cwd":{"type":"string"},
			"git_branch":{"type":"string"},
			"source_updated_at":{"type":"string","description":"Optional RFC3339 timestamp for the source trace"},
			"job_id":{"type":"string","description":"Optional external job id to report once the pipeline finishes"},
			"item_id":{"type":"string","description":"Optional external item id to report once the pipeline finishes"},
			"executor":{"type":"string","description":"Optional external executor id to report once the pipeline finishes"}
		},
		"required":["source_path","trace","target_path"]
	}`),
	Risk:         tool.RiskMedium,
	Capabilities: []string{"memory"},
}

func readMemoryHandler(ws workspace.Workspace, store memory.MemoryStore, pipeline *memstore.PipelineManager) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		record, reconciled, err := ensureMemoryRecord(ctx, ws, store, in.Path)
		if err != nil {
			return nil, err
		}
		if reconciled {
			if err := syncMemoryArtifacts(ctx, pipeline); err != nil {
				return nil, err
			}
		}
		if err := recordMemoryUsage(ctx, store, record.Path); err != nil {
			return nil, err
		}
		return json.Marshal(record.Content)
	}
}

func writeMemoryHandler(ws workspace.Workspace, store memory.MemoryStore, pipeline *memstore.PipelineManager) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		in.Path = memstore.NormalizePath(in.Path)
		record, err := store.Upsert(ctx, memory.MemoryRecord{
			Path:       in.Path,
			Content:    in.Content,
			Stage:      memory.MemoryStageManual,
			Status:     memory.MemoryStatusActive,
			SourceKind: "tool.write_memory",
			SourcePath: in.Path,
		})
		if err != nil {
			return nil, err
		}
		if err := ws.WriteFile(ctx, record.Path, []byte(record.Content)); err != nil {
			return nil, err
		}
		if err := syncMemoryArtifacts(ctx, pipeline); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "ok", "path": record.Path})
	}
}

func listMemoriesHandler(store memory.MemoryStore) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		pattern := strings.TrimSpace(in.Pattern)
		if pattern == "" {
			pattern = "**/*"
		}
		items, err := store.List(ctx, 0)
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(items))
		for _, item := range items {
			if item.Status != "" && item.Status != memory.MemoryStatusActive {
				continue
			}
			if matchesMemoryPattern(pattern, item.Path) {
				paths = append(paths, item.Path)
			}
		}
		return json.Marshal(paths)
	}
}

func deleteMemoryHandler(ws workspace.Workspace, store memory.MemoryStore, pipeline *memstore.PipelineManager) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		in.Path = memstore.NormalizePath(in.Path)
		if err := ws.DeleteFile(ctx, in.Path); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil, err
		}
		if err := store.DeleteByPath(ctx, in.Path); err != nil {
			return nil, err
		}
		if err := syncMemoryArtifacts(ctx, pipeline); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "deleted", "path": in.Path})
	}
}

func readMemoryRecordHandler(store memory.MemoryStore) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		path := memstore.NormalizePath(in.Path)
		if err := recordMemoryUsage(ctx, store, path); err != nil {
			return nil, err
		}
		record, err := store.GetByPath(ctx, path)
		if err != nil {
			return nil, err
		}
		return json.Marshal(record)
	}
}

func writeMemoryRecordHandler(ws workspace.Workspace, store memory.MemoryStore, pipeline *memstore.PipelineManager) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path            string                `json:"path"`
			Content         string                `json:"content"`
			Summary         string                `json:"summary"`
			Tags            []string              `json:"tags"`
			Citation        memory.MemoryCitation `json:"citation"`
			Stage           memory.MemoryStage    `json:"stage"`
			Status          memory.MemoryStatus   `json:"status"`
			Group           string                `json:"group"`
			Workspace       string                `json:"workspace"`
			CWD             string                `json:"cwd"`
			GitBranch       string                `json:"git_branch"`
			SourceKind      string                `json:"source_kind"`
			SourceID        string                `json:"source_id"`
			SourcePath      string                `json:"source_path"`
			SourceUpdatedAt string                `json:"source_updated_at"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		record, err := store.Upsert(ctx, memory.MemoryRecord{
			Path:            in.Path,
			Content:         in.Content,
			Summary:         in.Summary,
			Tags:            in.Tags,
			Citation:        in.Citation,
			Stage:           in.Stage,
			Status:          in.Status,
			Group:           in.Group,
			Workspace:       in.Workspace,
			CWD:             in.CWD,
			GitBranch:       in.GitBranch,
			SourceKind:      in.SourceKind,
			SourceID:        in.SourceID,
			SourcePath:      in.SourcePath,
			SourceUpdatedAt: memstore.ParseMemoryTime(in.SourceUpdatedAt),
		})
		if err != nil {
			return nil, err
		}
		if err := ws.WriteFile(ctx, record.Path, []byte(record.Content)); err != nil {
			return nil, err
		}
		if err := recordMemoryUsages(ctx, store, record.Citation.MemoryPaths); err != nil {
			return nil, err
		}
		if err := syncMemoryArtifacts(ctx, pipeline); err != nil {
			return nil, err
		}
		return json.Marshal(record)
	}
}

func searchMemoriesHandler(store memory.MemoryStore) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Query     string                `json:"query"`
			Tags      []string              `json:"tags"`
			Stages    []memory.MemoryStage  `json:"stages"`
			Statuses  []memory.MemoryStatus `json:"statuses"`
			Group     string                `json:"group"`
			Workspace string                `json:"workspace"`
			Limit     int                   `json:"limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		items, err := store.Search(ctx, memory.MemoryQuery{
			Query:     in.Query,
			Tags:      in.Tags,
			Stages:    in.Stages,
			Statuses:  in.Statuses,
			Group:     in.Group,
			Workspace: in.Workspace,
			Limit:     in.Limit,
		})
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		paths := make([]string, 0, len(items))
		for i := range items {
			paths = append(paths, items[i].Path)
			items[i] = memstore.BumpMemoryUsage(items[i], now)
		}
		if err := recordMemoryUsages(ctx, store, paths); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"count": len(items),
			"items": items,
		})
	}
}

func ingestMemoryTraceHandler(pipeline *memstore.PipelineManager) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if pipeline == nil {
			return nil, fmt.Errorf("memory pipeline is unavailable")
		}
		var in struct {
			SourcePath      string   `json:"source_path"`
			Trace           string   `json:"trace"`
			TargetPath      string   `json:"target_path"`
			Tags            []string `json:"tags"`
			Workspace       string   `json:"workspace"`
			CWD             string   `json:"cwd"`
			GitBranch       string   `json:"git_branch"`
			SourceUpdatedAt string   `json:"source_updated_at"`
			JobID           string   `json:"job_id"`
			ItemID          string   `json:"item_id"`
			Executor        string   `json:"executor"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		job, err := pipeline.Enqueue(ctx, memstore.PipelineJob{
			SourcePath:       in.SourcePath,
			Trace:            in.Trace,
			TargetPath:       in.TargetPath,
			Tags:             in.Tags,
			Workspace:        in.Workspace,
			CWD:              in.CWD,
			GitBranch:        in.GitBranch,
			SourceUpdatedAt:  memstore.ParseMemoryTime(in.SourceUpdatedAt),
			ExternalJobID:    in.JobID,
			ExternalItemID:   in.ItemID,
			ExternalExecutor: in.Executor,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status":      "queued",
			"job_id":      job.ID,
			"target_path": memstore.NormalizePath(in.TargetPath),
			"agent_name":  job.AgentName,
		})
	}
}

func ensureMemoryRecord(ctx context.Context, ws workspace.Workspace, store memory.MemoryStore, path string) (*memory.MemoryRecord, bool, error) {
	path = memstore.NormalizePath(path)
	record, err := store.GetByPath(ctx, path)
	if err == nil && record != nil {
		return record, false, nil
	}
	data, readErr := ws.ReadFile(ctx, path)
	if readErr != nil {
		if err != nil {
			return nil, false, err
		}
		return nil, false, readErr
	}
	record, err = store.Upsert(ctx, memory.MemoryRecord{
		Path:       path,
		Content:    string(data),
		Stage:      memory.MemoryStageManual,
		Status:     memory.MemoryStatusActive,
		SourceKind: "projection.reconciled",
		SourcePath: path,
	})
	return record, true, err
}

func syncMemoryArtifacts(ctx context.Context, pipeline *memstore.PipelineManager) error {
	if pipeline == nil {
		return nil
	}
	return pipeline.SyncArtifacts(ctx)
}

func matchesMemoryPattern(pattern string, path string) bool {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if pattern == "" || pattern == "**/*" || pattern == "*" {
		return true
	}
	if matched, err := filepath.Match(pattern, path); err == nil && matched {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		return matchesMemoryPattern(strings.TrimPrefix(pattern, "**/"), path)
	}
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		offset := 0
		for _, part := range parts {
			if part == "" {
				continue
			}
			idx := strings.Index(path[offset:], part)
			if idx < 0 {
				return false
			}
			offset += idx + len(part)
		}
		return true
	}
	return path == pattern
}

func recordMemoryUsage(ctx context.Context, store memory.MemoryStore, path string) error {
	return recordMemoryUsages(ctx, store, []string{path})
}

func recordMemoryUsages(ctx context.Context, store memory.MemoryStore, paths []string) error {
	if store == nil || len(paths) == 0 {
		return nil
	}
	return store.RecordUsage(ctx, paths, time.Now().UTC())
}




