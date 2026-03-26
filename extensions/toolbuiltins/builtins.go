package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
)

// RegisteredToolNames 返回给定配置下会注册的工具名列表。
// 当 Workspace 或 Sandbox 至少有一个可用时，注册文件系统工具。
// 当 Executor 或 Sandbox 至少有一个可用时，注册 run_command。
func RegisteredToolNames(sb sandbox.Sandbox, ws port.Workspace, exec port.Executor) []string {
	names := []string{}
	if ws != nil || sb != nil {
		names = append(names, "read_file", "write_file", "edit_file", "glob", "list_files", "search_text")
	}
	if exec != nil || sb != nil {
		names = append(names, "run_command")
	}
	names = append(names, "ask_user")
	return names
}

// RegisterAll 注册所有内置工具到 registry。
// 优先使用 Workspace/Executor 接口；未提供时回退到 Sandbox。
func RegisterAll(reg tool.Registry, sb sandbox.Sandbox, io port.UserIO, ws port.Workspace, exec port.Executor) error {
	type entry struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}

	var tools []entry

	// 文件系统工具：优先 Workspace，回退 Sandbox
	if ws != nil {
		tools = append(tools,
			entry{readFileSpec, readFileHandlerWS(ws)},
			entry{writeFileSpec, writeFileHandlerWS(ws)},
			entry{editFileSpec, editFileHandlerWS(ws)},
			entry{globSpec, globHandlerWS(ws)},
			entry{listFilesSpec, listFilesHandlerWS(ws)},
			entry{searchTextSpec, searchTextHandlerWS(ws)},
		)
	} else if sb != nil {
		tools = append(tools,
			entry{readFileSpec, readFileHandler(sb)},
			entry{writeFileSpec, writeFileHandler(sb)},
			entry{editFileSpec, editFileHandler(sb)},
			entry{globSpec, globHandler(sb)},
			entry{listFilesSpec, listFilesHandler(sb)},
			entry{searchTextSpec, searchTextHandler(sb)},
		)
	}

	// 命令执行：优先 Executor，回退 Sandbox
	if exec != nil {
		tools = append(tools, entry{runCommandSpec, runCommandHandlerExec(exec)})
	} else if sb != nil {
		tools = append(tools, entry{runCommandSpec, runCommandHandler(sb)})
	}

	tools = append(tools, entry{askUserSpec, askUserHandler(io)})

	for _, t := range tools {
		if err := reg.Register(t.spec, t.handler); err != nil {
			return err
		}
	}
	return nil
}

// ─── read_file ───────────────────────────────────────

var readFileSpec = tool.ToolSpec{
	Name:        "read_file",
	Description: "Read the contents of a file. Returns the file content as text.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path (relative to workspace root)"}
		},
		"required": ["path"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"filesystem"},
}

func readFileHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		data, err := sb.ReadFile(params.Path)
		if err != nil {
			return nil, err
		}
		return json.Marshal(string(data))
	}
}

// ─── write_file ──────────────────────────────────────

var writeFileSpec = tool.ToolSpec{
	Name:        "write_file",
	Description: "Write content to a file. Creates parent directories if needed. Overwrites existing content.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path":    {"type": "string", "description": "File path (relative to workspace root)"},
			"content": {"type": "string", "description": "Content to write"}
		},
		"required": ["path", "content"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"filesystem"},
}

func writeFileHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if err := sb.WriteFile(params.Path, []byte(params.Content)); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "ok", "path": params.Path})
	}
}

// ─── edit_file ───────────────────────────────────────

var editFileSpec = tool.ToolSpec{
	Name:        "edit_file",
	Description: "Edit a file by replacing old_string with new_string. Supports replace_all mode.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path (relative to workspace root)"},
			"old_string": {"type": "string", "description": "Text to replace"},
			"new_string": {"type": "string", "description": "Replacement text"},
			"replace_all": {"type": "boolean", "description": "Whether to replace all occurrences (default: false)"}
		},
		"required": ["path", "old_string", "new_string"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"filesystem"},
}

func editFileHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		data, err := sb.ReadFile(params.Path)
		if err != nil {
			return nil, err
		}
		updated, occurrences, err := applyEdit(string(data), params.OldString, params.NewString, params.ReplaceAll)
		if err != nil {
			return nil, err
		}
		if err := sb.WriteFile(params.Path, []byte(updated)); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status":      "ok",
			"path":        params.Path,
			"occurrences": occurrences,
		})
	}
}

// ─── glob ────────────────────────────────────────────

var globSpec = tool.ToolSpec{
	Name:        "glob",
	Description: "Find files by glob pattern. Optionally scope search under a relative path.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern (e.g., \"**/*.go\")"},
			"path": {"type": "string", "description": "Optional relative directory prefix"}
		},
		"required": ["pattern"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"filesystem"},
}

func globHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		pattern := scopedPattern(params.Pattern, params.Path)
		files, err := sb.ListFiles(pattern)
		if err != nil {
			return nil, err
		}
		return json.Marshal(files)
	}
}

// ─── list_files ──────────────────────────────────────

var listFilesSpec = tool.ToolSpec{
	Name:        "list_files",
	Description: "List files matching a glob pattern relative to the workspace root.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern (e.g., \"**/*.go\", \"src/\")"}
		},
		"required": ["pattern"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"filesystem"},
}

func listFilesHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		files, err := sb.ListFiles(params.Pattern)
		if err != nil {
			return nil, err
		}
		return json.Marshal(files)
	}
}

// ─── search_text ─────────────────────────────────────

var searchTextSpec = tool.ToolSpec{
	Name:        "search_text",
	Description: "Search for a text pattern in files under the workspace. Returns matching lines with file paths and line numbers.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern":  {"type": "string", "description": "Regex pattern to match (RE2 syntax, case-sensitive by default)"},
			"glob":     {"type": "string", "description": "File glob to scope the search (default: \"**/*\")"},
			"max_results": {"type": "integer", "description": "Maximum number of results (default: 50)"}
		},
		"required": ["pattern"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"filesystem"},
}

type searchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func searchTextHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern    string `json:"pattern"`
			Glob       string `json:"glob"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if params.Glob == "" {
			params.Glob = "**/*"
		}
		if params.MaxResults <= 0 {
			params.MaxResults = 50
		}
		re, err := regexp.Compile(params.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}

		files, err := sb.ListFiles(params.Glob)
		if err != nil {
			return nil, err
		}

		var matches []searchMatch
		for _, file := range files {
			if len(matches) >= params.MaxResults {
				break
			}
			data, err := sb.ReadFile(file)
			if err != nil {
				continue
			}
			// Compute relative path from sandbox root
			relPath := file
			if root, err := sb.ResolvePath("."); err == nil {
				if rel, err := filepath.Rel(root, file); err == nil {
					relPath = rel
				}
			}
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if len(matches) >= params.MaxResults {
					break
				}
				if re.MatchString(line) {
					matches = append(matches, searchMatch{
						File: relPath,
						Line: i + 1,
						Text: truncateString(line, 200),
					})
				}
			}
		}
		return json.Marshal(matches)
	}
}

// ─── run_command ─────────────────────────────────────

var runCommandSpec = tool.ToolSpec{
	Name:        "run_command",
	Description: "Execute a shell command in the workspace directory. Returns stdout, stderr, and exit code.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The command to execute"},
			"args":    {"type": "array", "items": {"type": "string"}, "description": "Command arguments"}
		},
		"required": ["command"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"execution"},
}

func runCommandHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		output, err := sb.Execute(ctx, params.Command, params.Args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(output)
	}
}

func runCommandHandlerExec(exec port.Executor) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		output, err := exec.Execute(ctx, params.Command, params.Args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(output)
	}
}

// ─── Workspace-based handlers ────────────────────────

func readFileHandlerWS(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		data, err := ws.ReadFile(ctx, params.Path)
		if err != nil {
			return nil, err
		}
		return json.Marshal(string(data))
	}
}

func writeFileHandlerWS(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if err := ws.WriteFile(ctx, params.Path, []byte(params.Content)); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "ok", "path": params.Path})
	}
}

func editFileHandlerWS(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		data, err := ws.ReadFile(ctx, params.Path)
		if err != nil {
			return nil, err
		}
		updated, occurrences, err := applyEdit(string(data), params.OldString, params.NewString, params.ReplaceAll)
		if err != nil {
			return nil, err
		}
		if err := ws.WriteFile(ctx, params.Path, []byte(updated)); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status":      "ok",
			"path":        params.Path,
			"occurrences": occurrences,
		})
	}
}

func globHandlerWS(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		pattern := scopedPattern(params.Pattern, params.Path)
		files, err := ws.ListFiles(ctx, pattern)
		if err != nil {
			return nil, err
		}
		return json.Marshal(files)
	}
}

func listFilesHandlerWS(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern string `json:"pattern"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		files, err := ws.ListFiles(ctx, params.Pattern)
		if err != nil {
			return nil, err
		}
		return json.Marshal(files)
	}
}

func searchTextHandlerWS(ws port.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern    string `json:"pattern"`
			Glob       string `json:"glob"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if params.Glob == "" {
			params.Glob = "**/*"
		}
		if params.MaxResults <= 0 {
			params.MaxResults = 50
		}
		re, err := regexp.Compile(params.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}

		files, err := ws.ListFiles(ctx, params.Glob)
		if err != nil {
			return nil, err
		}

		var matches []searchMatch
		for _, file := range files {
			if len(matches) >= params.MaxResults {
				break
			}
			data, err := ws.ReadFile(ctx, file)
			if err != nil {
				continue
			}
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if len(matches) >= params.MaxResults {
					break
				}
				if re.MatchString(line) {
					matches = append(matches, searchMatch{
						File: file,
						Line: i + 1,
						Text: truncateString(line, 200),
					})
				}
			}
		}
		return json.Marshal(matches)
	}
}

// ─── ask_user ────────────────────────────────────────

var askUserSpec = tool.ToolSpec{
	Name:        "ask_user",
	Description: "Ask the user a question and wait for their response. Use this when you need clarification or confirmation.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "The question to ask the user"}
		},
		"required": ["question"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"interaction"},
}

func askUserHandler(io port.UserIO) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if io == nil {
			return json.Marshal("ask_user: no user IO available")
		}
		resp, err := io.Ask(ctx, port.InputRequest{
			Type:   port.InputFreeText,
			Prompt: params.Question,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp.Value)
	}
}

// ─── helpers ─────────────────────────────────────────

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func applyEdit(content, oldString, newString string, replaceAll bool) (string, int, error) {
	if oldString == "" {
		return "", 0, fmt.Errorf("old_string cannot be empty")
	}
	occurrences := strings.Count(content, oldString)
	if occurrences == 0 {
		return "", 0, fmt.Errorf("old_string not found")
	}
	if !replaceAll && occurrences > 1 {
		return "", 0, fmt.Errorf("old_string appears %d times; set replace_all=true to replace all occurrences", occurrences)
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldString, newString), occurrences, nil
	}
	return strings.Replace(content, oldString, newString, 1), 1, nil
}

func scopedPattern(pattern, scopePath string) string {
	if scopePath == "" || scopePath == "." {
		return pattern
	}
	return filepath.Join(scopePath, pattern)
}
