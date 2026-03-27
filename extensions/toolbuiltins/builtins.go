package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
)

const maxInlineCommandOutput = 8000

// RegisteredToolNames 返回给定配置下会注册的工具名列表。
// 当 Workspace 或 Sandbox 至少有一个可用时，注册文件系统工具。
// 当 Executor 或 Sandbox 至少有一个可用时，注册 run_command。
func RegisteredToolNames(sb sandbox.Sandbox, ws port.Workspace, exec port.Executor) []string {
	names := []string{}
	if ws != nil || sb != nil {
		names = append(names, "read_file", "write_file", "edit_file", "glob", "list_files", "grep")
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
			entry{grepSpec, grepHandlerWS(ws)},
		)
	} else if sb != nil {
		tools = append(tools,
			entry{readFileSpec, readFileHandler(sb)},
			entry{writeFileSpec, writeFileHandler(sb)},
			entry{editFileSpec, editFileHandler(sb)},
			entry{globSpec, globHandler(sb)},
			entry{listFilesSpec, listFilesHandler(sb)},
			entry{grepSpec, grepHandler(sb)},
		)
	}

	// 命令执行：优先 Executor，回退 Sandbox
	if exec != nil {
		tools = append(tools, entry{runCommandSpec, runCommandHandlerExec(exec, ws)})
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
	Description: "List files matching a glob pattern relative to the workspace root. Hidden files are excluded by default.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern (default: \"**/*\")"},
			"max_results": {"type": "integer", "description": "Maximum files to return (default: 200, hard cap: 1000)"},
			"include_hidden": {"type": "boolean", "description": "Whether to include hidden files/dirs like .git (default: false)"}
		}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"filesystem"},
}

func listFilesHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern       string `json:"pattern"`
			MaxResults    int    `json:"max_results"`
			IncludeHidden bool   `json:"include_hidden"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if strings.TrimSpace(params.Pattern) == "" {
			params.Pattern = "**/*"
		}
		limit := normalizeMaxResults(params.MaxResults)
		files, err := sb.ListFiles(params.Pattern)
		if err != nil {
			return nil, err
		}
		root, _ := sb.ResolvePath(".")
		filtered := normalizeAndFilterPaths(files, root, params.IncludeHidden)
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		return json.Marshal(filtered)
	}
}

// ─── grep ────────────────────────────────────────────

var grepSpec = tool.ToolSpec{
	Name:        "grep",
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

func grepHandler(sb sandbox.Sandbox) tool.ToolHandler {
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
		return marshalCommandOutput(ctx, output, func(path string, data []byte) error {
			return sb.WriteFile(path, data)
		})
	}
}

func runCommandHandlerExec(exec port.Executor, ws port.Workspace) tool.ToolHandler {
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
		return marshalCommandOutput(ctx, output, func(path string, data []byte) error {
			if ws == nil {
				return fmt.Errorf("workspace is nil")
			}
			return ws.WriteFile(ctx, path, data)
		})
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
			Pattern       string `json:"pattern"`
			MaxResults    int    `json:"max_results"`
			IncludeHidden bool   `json:"include_hidden"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if strings.TrimSpace(params.Pattern) == "" {
			params.Pattern = "**/*"
		}
		limit := normalizeMaxResults(params.MaxResults)
		files, err := ws.ListFiles(ctx, params.Pattern)
		if err != nil {
			return nil, err
		}
		filtered := normalizeAndFilterPaths(files, "", params.IncludeHidden)
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		return json.Marshal(filtered)
	}
}

func grepHandlerWS(ws port.Workspace) tool.ToolHandler {
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
	Description: "Ask the user a question and wait for their response. Supports free text and schema-driven forms.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "The question to ask the user"},
			"requestedSchema": {"type": "object", "description": "Optional JSON Schema-like definition for structured input"}
		},
		"required": ["question"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"interaction"},
}

func askUserHandler(io port.UserIO) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Question        string         `json:"question"`
			RequestedSchema map[string]any `json:"requestedSchema"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if io == nil {
			return json.Marshal("ask_user: no user IO available")
		}
		req := port.InputRequest{
			Type:   port.InputFreeText,
			Prompt: params.Question,
		}
		fields, err := buildAskUserFields(params.RequestedSchema)
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			req.Type = port.InputForm
			req.Fields = fields
			req.ConfirmLabel = "Confirm"
		}
		resp, err := io.Ask(ctx, req)
		if err != nil {
			return nil, err
		}
		if req.Type == port.InputForm {
			return json.Marshal(resp.Form)
		}
		return json.Marshal(resp.Value)
	}
}

func buildAskUserFields(schema map[string]any) ([]port.InputField, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	propsAny, ok := schema["properties"]
	if !ok {
		return nil, nil
	}
	props, ok := propsAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("requestedSchema.properties must be an object")
	}
	requiredSet := map[string]bool{}
	if reqAny, ok := schema["required"].([]any); ok {
		for _, name := range reqAny {
			if s, ok := name.(string); ok {
				requiredSet[s] = true
			}
		}
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]port.InputField, 0, len(keys))
	for _, name := range keys {
		rawDef, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		field := port.InputField{
			Name:        name,
			Title:       toString(rawDef["title"]),
			Description: toString(rawDef["description"]),
			Required:    requiredSet[name],
		}
		if field.Title == "" {
			field.Title = name
		}
		ftype := strings.ToLower(toString(rawDef["type"]))
		switch ftype {
		case "boolean":
			field.Type = port.InputFieldBoolean
		case "array":
			field.Type = port.InputFieldMultiSelect
		case "number":
			field.Type = port.InputFieldNumber
		case "integer":
			field.Type = port.InputFieldInteger
		default:
			if enum := toStringSlice(rawDef["enum"]); len(enum) > 0 {
				field.Type = port.InputFieldSingleSelect
				field.Options = enum
			} else {
				field.Type = port.InputFieldString
			}
		}
		if field.Type == port.InputFieldMultiSelect {
			items, _ := rawDef["items"].(map[string]any)
			field.Options = toStringSlice(items["enum"])
		}
		if def, ok := rawDef["default"]; ok {
			field.Default = normalizeDefaultForField(field.Type, def)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func toString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func toStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func normalizeDefaultForField(ft port.InputFieldType, v any) any {
	switch ft {
	case port.InputFieldBoolean:
		if b, ok := v.(bool); ok {
			return b
		}
	case port.InputFieldNumber:
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return parsed
			}
		}
	case port.InputFieldInteger:
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				return parsed
			}
		}
	case port.InputFieldMultiSelect:
		if vals := toStringSlice(v); len(vals) > 0 {
			return vals
		}
	default:
		if s, ok := v.(string); ok {
			return s
		}
	}
	return v
}

// ─── helpers ─────────────────────────────────────────

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func normalizeMaxResults(n int) int {
	if n <= 0 {
		return 200
	}
	if n > 1000 {
		return 1000
	}
	return n
}

func normalizeAndFilterPaths(paths []string, root string, includeHidden bool) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		candidate := p
		if strings.TrimSpace(root) != "" {
			if rel, err := filepath.Rel(root, p); err == nil {
				candidate = rel
			}
		}
		candidate = filepath.Clean(candidate)
		if candidate == "." || candidate == "" {
			continue
		}
		norm := strings.ReplaceAll(candidate, "\\", "/")
		if !includeHidden {
			skip := false
			for _, part := range strings.Split(norm, "/") {
				if strings.HasPrefix(part, ".") {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		out = append(out, norm)
	}
	sort.Strings(out)
	return out
}

func marshalCommandOutput(ctx context.Context, output any, writeFile func(path string, data []byte) error) (json.RawMessage, error) {
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	if len(raw) <= maxInlineCommandOutput {
		return raw, nil
	}
	meta, ok := port.ToolCallContextFromContext(ctx)
	if !ok || meta.SessionID == "" || meta.CallID == "" {
		return raw, nil
	}
	path := filepath.Join(".moss", "large_tool_results", fmt.Sprintf("%s_%s.json", meta.SessionID, meta.CallID))
	if writeFile != nil && path != "" {
		if werr := writeFile(path, raw); werr == nil {
			preview := string(raw)
			if len(preview) > 1200 {
				preview = preview[:1200] + "...(truncated)"
			}
			return json.Marshal(map[string]any{
				"offloaded":     true,
				"path":          path,
				"preview":       preview,
				"message":       "Large command output was offloaded. Use read_file on the returned path.",
				"original_size": len(raw),
			})
		}
	}
	return raw, nil
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
