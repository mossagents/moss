package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
)

const memoryStateKey kernel.ExtensionStateKey = "memory.state"

type state struct {
	workspace port.Workspace
}

// WithMemoryWorkspace 将持久化 memory 工作区接入 Kernel。
func WithMemoryWorkspace(ws port.Workspace) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureMemoryState(k).workspace = ws
	}
}

// RegisterMemoryToolsCompat 为 memory 命名空间注册标准工具集。
func RegisterMemoryToolsCompat(reg tool.Registry, ws port.Workspace) error {
	return RegisterMemoryTools(reg, ws)
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
		return RegisterMemoryTools(k.ToolRegistry(), st.workspace)
	})
	bridge.OnSystemPrompt(220, func(_ *kernel.Kernel) string {
		if st.workspace == nil {
			return ""
		}
		return "You have persistent memory tools backed by /memories: list_memories, read_memory, write_memory, delete_memory."
	})
	return st
}

func RegisterMemoryTools(reg tool.Registry, ws port.Workspace) error {
	if ws == nil {
		return fmt.Errorf("memory workspace is nil")
	}
	tools := []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{readMemorySpec, readMemoryHandler(ws)},
		{writeMemorySpec, writeMemoryHandler(ws)},
		{listMemoriesSpec, listMemoriesHandler(ws)},
		{deleteMemorySpec, deleteMemoryHandler(ws)},
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
