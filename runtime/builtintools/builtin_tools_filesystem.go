package builtintools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
)

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

func readFileHandlerWS(ws workspace.Workspace) tool.ToolHandler {
	return readFileHandlerPort(ws)
}

func readFileHandlerPort(ws workspace.Workspace) tool.ToolHandler {
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

func writeFileHandlerWS(ws workspace.Workspace) tool.ToolHandler {
	return writeFileHandlerPort(ws)
}

func writeFileHandlerPort(ws workspace.Workspace) tool.ToolHandler {
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

func editFileHandlerWS(ws workspace.Workspace) tool.ToolHandler {
	return editFileHandlerPort(ws)
}

func editFileHandlerPort(ws workspace.Workspace) tool.ToolHandler {
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

func globHandlerPort(ws workspace.Workspace, root string) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		pattern := scopedPattern(params.Pattern, params.Path)
		paths, err := ws.ListFiles(ctx, pattern)
		if err != nil {
			return nil, err
		}
		paths = normalizeAndFilterPaths(paths, root, true)
		return json.Marshal(paths)
	}
}

// ─── ls ──────────────────────────────────────────────

var listFilesSpec = tool.ToolSpec{
	Name:        "ls",
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

func listFilesHandlerPort(ws workspace.Workspace, root string) tool.ToolHandler {
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
		paths, err := ws.ListFiles(ctx, params.Pattern)
		if err != nil {
			return nil, err
		}
		paths = normalizeAndFilterPaths(paths, root, params.IncludeHidden)
		if len(paths) > limit {
			paths = paths[:limit]
		}
		return json.Marshal(paths)
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

func grepHandlerPort(ws workspace.Workspace, root string) tool.ToolHandler {
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
			relPath := file
			if strings.TrimSpace(root) != "" {
				if rel, err := filepath.Rel(root, file); err == nil {
					relPath = rel
				}
			}
			relPath = filepath.Clean(relPath)
			relPath = strings.ReplaceAll(relPath, "\\", "/")
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

func sandboxRoot(sb sandbox.Sandbox) string {
	if sb == nil {
		return ""
	}
	root, err := sb.ResolvePath(".")
	if err != nil {
		return ""
	}
	return filepath.Clean(root)
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

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func scopedPattern(pattern, scopePath string) string {
	if scopePath == "" || scopePath == "." {
		return pattern
	}
	return filepath.Join(scopePath, pattern)
}

