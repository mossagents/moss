package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
)

const memoryStateKey kernel.ExtensionStateKey = "memory.state"

type state struct {
	workspace port.Workspace
	store     port.MemoryStore
}

// WithMemoryWorkspace 将持久化 memory 工作区接入 Kernel。
func WithMemoryWorkspace(ws port.Workspace) kernel.Option {
	return func(k *kernel.Kernel) {
		st := ensureMemoryState(k)
		st.workspace = ws
		if ws != nil {
			st.store = NewWorkspaceMemoryStore(ws)
		}
	}
}

func WithMemoryStore(store port.MemoryStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureMemoryState(k).store = store
	}
}

// RegisterMemoryToolsCompat 为 memory 命名空间注册标准工具集。
func RegisterMemoryToolsCompat(reg tool.Registry, ws port.Workspace) error {
	return RegisterMemoryTools(reg, ws, NewWorkspaceMemoryStore(ws))
}

func ensureMemoryState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(memoryStateKey, &state{})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnBoot(120, func(_ context.Context, k *kernel.Kernel) error {
		if st.workspace == nil {
			return nil
		}
		if st.store == nil {
			st.store = NewWorkspaceMemoryStore(st.workspace)
		}
		return RegisterMemoryToolsWithRuntime(k.ToolRegistry(), st.workspace, st.store, k.TaskRuntime())
	})
	bridge.OnShutdown(120, func(_ context.Context, _ *kernel.Kernel) error {
		closer, ok := st.store.(io.Closer)
		if !ok || closer == nil {
			return nil
		}
		return closer.Close()
	})
	bridge.OnSystemPrompt(220, func(_ *kernel.Kernel) string {
		if st.workspace == nil {
			return ""
		}
		return "You have persistent memory tools backed by /memories: list_memories, read_memory, write_memory, delete_memory, read_memory_record, write_memory_record, search_memories."
	})
	return st
}

func RegisterMemoryTools(reg tool.Registry, ws port.Workspace, store port.MemoryStore) error {
	return RegisterMemoryToolsWithRuntime(reg, ws, store, nil)
}

func RegisterMemoryToolsWithRuntime(reg tool.Registry, ws port.Workspace, store port.MemoryStore, runtime port.TaskRuntime) error {
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
		{readMemorySpec, readMemoryHandler(ws)},
		{writeMemorySpec, writeMemoryHandler(ws)},
		{listMemoriesSpec, listMemoriesHandler(ws)},
		{deleteMemorySpec, deleteMemoryHandler(ws)},
		{readMemoryRecordSpec, readMemoryRecordHandler(store)},
		{writeMemoryRecordSpec, writeMemoryRecordHandler(store)},
		{searchMemoriesSpec, searchMemoriesHandler(store)},
		{ingestMemoryTraceSpec, ingestMemoryTraceHandler(store, runtime)},
	}
	for _, t := range tools {
		if _, _, exists := reg.Get(t.spec.Name); exists {
			continue
		}
		if err := reg.Register(t.spec, t.handler); err != nil {
			return err
		}
	}
	return nil
}

var readMemorySpec = tool.ToolSpec{
	Name:        "read_memory",
	Description: "Read a persistent memory file from /memories namespace.",
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
	Description: "Delete a persistent memory file from /memories namespace.",
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
	Description: "Read a structured memory record with summary and citations.",
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
	Description: "Write a structured memory record with optional tags and citations.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"path":{"type":"string"},
			"content":{"type":"string"},
			"summary":{"type":"string"},
			"tags":{"type":"array","items":{"type":"string"}},
			"citation":{"type":"object"}
		},
		"required":["path","content"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"memory"},
}

var searchMemoriesSpec = tool.ToolSpec{
	Name:        "search_memories",
	Description: "Search structured memories by text and tags.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"query":{"type":"string"},
			"tags":{"type":"array","items":{"type":"string"}},
			"limit":{"type":"integer"}
		}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory"},
}

var ingestMemoryTraceSpec = tool.ToolSpec{
	Name:        "ingest_memory_trace",
	Description: "Normalize conversation/tool trace content and persist as a structured memory record.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"source_path":{"type":"string","description":"Trace source path"},
			"trace":{"type":"string","description":"Raw trace text (json array or jsonl)"},
			"target_path":{"type":"string","description":"Memory target path"},
			"tags":{"type":"array","items":{"type":"string"}},
			"job_id":{"type":"string","description":"Optional job id for atomic item reporting"},
			"item_id":{"type":"string","description":"Optional item id for atomic item reporting"},
			"executor":{"type":"string","description":"Optional executor id for atomic item reporting"}
		},
		"required":["source_path","trace","target_path"]
	}`),
	Risk:         tool.RiskMedium,
	Capabilities: []string{"memory"},
}

func readMemoryHandler(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		data, err := ws.ReadFile(ctx, in.Path)
		if err != nil {
			return nil, err
		}
		return json.Marshal(string(data))
	}
}

func writeMemoryHandler(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if err := ws.WriteFile(ctx, in.Path, []byte(in.Content)); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "ok", "path": in.Path})
	}
}

func listMemoriesHandler(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if in.Pattern == "" {
			in.Pattern = "**/*"
		}
		files, err := ws.ListFiles(ctx, in.Pattern)
		if err != nil {
			return nil, err
		}
		return json.Marshal(files)
	}
}

func deleteMemoryHandler(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if err := ws.DeleteFile(ctx, in.Path); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "deleted", "path": in.Path})
	}
}

func readMemoryRecordHandler(store port.MemoryStore) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		record, err := store.GetByPath(ctx, in.Path)
		if err != nil {
			return nil, err
		}
		return json.Marshal(record)
	}
}

func writeMemoryRecordHandler(store port.MemoryStore) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path     string              `json:"path"`
			Content  string              `json:"content"`
			Summary  string              `json:"summary"`
			Tags     []string            `json:"tags"`
			Citation port.MemoryCitation `json:"citation"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		record, err := store.Upsert(ctx, port.MemoryRecord{
			Path:     in.Path,
			Content:  in.Content,
			Summary:  in.Summary,
			Tags:     in.Tags,
			Citation: in.Citation,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(record)
	}
}

func searchMemoriesHandler(store port.MemoryStore) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Query string   `json:"query"`
			Tags  []string `json:"tags"`
			Limit int      `json:"limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		items, err := store.Search(ctx, port.MemoryQuery{
			Query: in.Query,
			Tags:  in.Tags,
			Limit: in.Limit,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"count": len(items),
			"items": items,
		})
	}
}

func ingestMemoryTraceHandler(store port.MemoryStore, runtime port.TaskRuntime) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			SourcePath string   `json:"source_path"`
			Trace      string   `json:"trace"`
			TargetPath string   `json:"target_path"`
			Tags       []string `json:"tags"`
			JobID      string   `json:"job_id"`
			ItemID     string   `json:"item_id"`
			Executor   string   `json:"executor"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		items, err := normalizeTraceItems(in.Trace)
		if err != nil {
			return nil, err
		}
		content := strings.Join(items, "\n")
		record, err := store.Upsert(ctx, port.MemoryRecord{
			Path:    in.TargetPath,
			Content: content,
			Summary: summarizeMemoryContent(content),
			Tags:    in.Tags,
			Citation: port.MemoryCitation{
				Entries: []port.MemoryCitationEntry{
					{
						Path:      strings.TrimSpace(in.SourcePath),
						LineStart: 1,
						LineEnd:   len(strings.Split(strings.TrimSpace(in.Trace), "\n")),
						Note:      "normalized from trace input",
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		atomicUpdated := false
		if strings.TrimSpace(in.JobID) != "" || strings.TrimSpace(in.ItemID) != "" || strings.TrimSpace(in.Executor) != "" {
			atomicRuntime, ok := runtime.(port.AtomicJobRuntime)
			if !ok {
				return nil, fmt.Errorf("atomic job runtime is not available")
			}
			if strings.TrimSpace(in.JobID) == "" || strings.TrimSpace(in.ItemID) == "" || strings.TrimSpace(in.Executor) == "" {
				return nil, fmt.Errorf("job_id, item_id and executor are required together")
			}
			if _, err := atomicRuntime.ReportJobItemResult(
				ctx,
				strings.TrimSpace(in.JobID),
				strings.TrimSpace(in.ItemID),
				strings.TrimSpace(in.Executor),
				port.JobCompleted,
				fmt.Sprintf("ingested %d normalized trace items", len(items)),
				"",
			); err != nil {
				if errors.Is(err, port.ErrJobItemExecutorMismatch) || errors.Is(err, port.ErrInvalidJobTransition) {
					return nil, err
				}
				return nil, fmt.Errorf("atomic job item report failed: %w", err)
			}
			atomicUpdated = true
		}
		return json.Marshal(map[string]any{
			"status":         "ingested",
			"record":         record,
			"items":          len(items),
			"atomic_updated": atomicUpdated,
		})
	}
}
