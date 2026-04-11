package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
)

const maxInlineCommandOutput = 8000
const maxHTTPResponseBodyBytes = 256 * 1024

// RegisteredBuiltinToolNames 返回 runtime 一方自带的 builtin tools 名称列表。
// 这些工具由 appkit/runtime 直接提供，不是 prompt skills，也不是通过 MCP 桥接的外部工具。
// 当 Workspace 或 Sandbox 至少有一个可用时，注册文件系统工具。
// 当 Executor 或 Sandbox 至少有一个可用时，注册 run_command。
func RegisteredBuiltinToolNames(sb sandbox.Sandbox, ws workspace.Workspace, exec workspace.Executor) []string {
	surface := newExecutionSurface(sb, ws, exec)
	names := []string{}
	if surface.HasWorkspace() {
		names = append(names, "read_file", "write_file", "edit_file", "glob", "ls", "grep")
	}
	if surface.HasExecutor() {
		names = append(names, "run_command")
	}
	names = append(names, "http_request", "datetime", "ask_user")
	return names
}

// RegisterBuiltinTools 注册 runtime 自带的 builtin tools 到 registry。
// 优先使用 Workspace/Executor 接口；未提供时回退到 Sandbox。
// builtin tools 是 first-party runtime capability，不经过 skill prompt 解析，也不依赖 MCP transport。
func RegisterBuiltinTools(reg tool.Registry, sb sandbox.Sandbox, io kernio.UserIO, ws workspace.Workspace, exec workspace.Executor) error {
	return RegisterBuiltinToolsForKernel(nil, reg, sb, io, ws, exec)
}

func RegisterBuiltinToolsForKernel(k *kernel.Kernel, reg tool.Registry, sb sandbox.Sandbox, io kernio.UserIO, ws workspace.Workspace, exec workspace.Executor) error {
	type entry struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}

	var tools []entry
	surface := newExecutionSurface(sb, ws, exec)

	// 文件系统工具：统一通过 execution surface 判断可用性，但保留原始实现的优先级和路径语义。
	if ws != nil {
		tools = append(tools,
			entry{builtinToolSpec(readFileSpec, "runtime", true, false, false), readFileHandlerWS(ws)},
			entry{builtinToolSpec(writeFileSpec, "runtime", true, false, false), writeFileHandlerWS(ws)},
			entry{builtinToolSpec(editFileSpec, "runtime", true, false, false), editFileHandlerWS(ws)},
			entry{builtinToolSpec(globSpec, "runtime", true, false, false), globHandlerWS(ws)},
			entry{builtinToolSpec(listFilesSpec, "runtime", true, false, false), listFilesHandlerWS(ws)},
			entry{builtinToolSpec(grepSpec, "runtime", true, false, false), grepHandlerWS(ws)},
		)
	} else if surface.Sandbox() != nil {
		tools = append(tools,
			entry{builtinToolSpec(readFileSpec, "runtime", false, false, true), readFileHandler(surface.Sandbox())},
			entry{builtinToolSpec(writeFileSpec, "runtime", false, false, true), writeFileHandler(surface.Sandbox())},
			entry{builtinToolSpec(editFileSpec, "runtime", false, false, true), editFileHandler(surface.Sandbox())},
			entry{builtinToolSpec(globSpec, "runtime", false, false, true), globHandler(surface.Sandbox())},
			entry{builtinToolSpec(listFilesSpec, "runtime", false, false, true), listFilesHandler(surface.Sandbox())},
			entry{builtinToolSpec(grepSpec, "runtime", false, false, true), grepHandler(surface.Sandbox())},
		)
	}

	// 命令执行：统一通过 execution surface 判断可用性，但保留原始执行后端。
	if exec != nil {
		tools = append(tools, entry{builtinToolSpec(runCommandSpec, "runtime", surface.WorkspacePort() != nil, true, false), runCommandHandlerExecWithPolicy(k, exec, surface.WorkspacePort())})
	} else if surface.Sandbox() != nil {
		tools = append(tools, entry{builtinToolSpec(runCommandSpec, "runtime", false, false, true), runCommandHandlerWithPolicy(k, surface.Sandbox())})
	}

	tools = append(tools, entry{builtinToolSpec(httpRequestSpec, "runtime", false, false, false), httpRequestHandlerWithPolicy(k)})
	tools = append(tools, entry{builtinToolSpec(datetimeSpec, "runtime", false, false, false), datetimeHandler()})
	tools = append(tools, entry{builtinToolSpec(askUserSpec, "runtime", false, false, false), askUserHandler(io)})

	for _, t := range tools {
		if err := reg.Register(tool.NewRawTool(t.spec, t.handler)); err != nil {
			return err
		}
	}
	return nil
}

func builtinToolSpec(spec tool.ToolSpec, owner string, requiresWorkspace, requiresExecutor, requiresSandbox bool) tool.ToolSpec {
	spec = applyRuntimeBuiltinExecutionMetadata(spec)
	spec.Source = "builtin"
	spec.Owner = strings.TrimSpace(owner)
	spec.RequiresWorkspace = requiresWorkspace
	spec.RequiresExecutor = requiresExecutor
	spec.RequiresSandbox = requiresSandbox
	return spec
}

func applyRuntimeBuiltinExecutionMetadata(spec tool.ToolSpec) tool.ToolSpec {
	switch spec.Name {
	case "datetime":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
	case "read_file", "glob", "ls", "grep":
		spec.Effects = []tool.Effect{tool.EffectReadOnly}
		spec.ResourceScope = []string{"workspace:*"}
		spec.SideEffectClass = tool.SideEffectNone
		spec.ApprovalClass = tool.ApprovalClassNone
		spec.PlannerVisibility = tool.PlannerVisibilityVisible
		spec.Idempotent = true
		spec.CommutativityClass = tool.CommutativityFullyCommutative
	case "write_file", "edit_file":
		spec.Effects = []tool.Effect{tool.EffectWritesWorkspace}
		spec.ResourceScope = []string{"workspace:*"}
		spec.LockScope = []string{"workspace:*"}
		spec.SideEffectClass = tool.SideEffectWorkspace
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "run_command":
		spec.Effects = []tool.Effect{tool.EffectExternalSideEffect}
		spec.ResourceScope = []string{"workspace:*", "process:command"}
		spec.LockScope = []string{"process:command"}
		spec.SideEffectClass = tool.SideEffectProcess
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	case "http_request":
		spec.Effects = []tool.Effect{tool.EffectExternalSideEffect}
		spec.ResourceScope = []string{"network:http"}
		spec.LockScope = []string{"network:http"}
		spec.SideEffectClass = tool.SideEffectNetwork
		spec.ApprovalClass = tool.ApprovalClassExplicitUser
		spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
		spec.CommutativityClass = tool.CommutativityNonCommutative
	}
	return spec
}

// ─── datetime ────────────────────────────────────────

var datetimeSpec = tool.ToolSpec{
	Name:        "datetime",
	Description: "Get current local date and time with timezone information.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"time"},
}

func datetimeHandler() tool.ToolHandler {
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		now := time.Now()
		tzName, tzOffsetSeconds := now.Zone()
		offsetMinutes := tzOffsetSeconds / 60
		sign := "+"
		if offsetMinutes < 0 {
			sign = "-"
			offsetMinutes = -offsetMinutes
		}
		offset := fmt.Sprintf("%s%02d:%02d", sign, offsetMinutes/60, offsetMinutes%60)
		return json.Marshal(map[string]any{
			"iso8601":        now.Format(time.RFC3339Nano),
			"timezone_name":  tzName,
			"utc_offset":     offset,
			"unix_timestamp": now.Unix(),
		})
	}
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
		root, _ := sb.ResolvePath(".")
		return json.Marshal(normalizeAndFilterPaths(files, root, true))
	}
}

// ─── ls ───────────────────────────────────────────────

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

// ─── http_request ─────────────────────────────────────

var httpRequestSpec = tool.ToolSpec{
	Name:        "http_request",
	Description: "Send an HTTP request and return status, headers, and body.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "Request URL (http/https)"},
			"method": {"type": "string", "description": "HTTP method, default GET"},
			"headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional request headers"},
			"body": {"type": "string", "description": "Optional request body"},
			"timeout_seconds": {"type": "integer", "description": "Request timeout in seconds (default 30, max 120)"},
			"follow_redirects": {"type": "boolean", "description": "Whether to follow redirects (default false)"}
		},
		"required": ["url"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"network"},
}

func httpRequestHandler() tool.ToolHandler {
	return httpRequestHandlerWithPolicy(nil)
}

func httpRequestHandlerWithPolicy(k *kernel.Kernel) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			URL             string            `json:"url"`
			Method          string            `json:"method"`
			Headers         map[string]string `json:"headers"`
			Body            string            `json:"body"`
			TimeoutSeconds  int               `json:"timeout_seconds"`
			FollowRedirects *bool             `json:"follow_redirects"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		rawURL := strings.TrimSpace(params.URL)
		if rawURL == "" {
			return nil, fmt.Errorf("url is required")
		}
		policy := effectiveExecutionPolicy(ctx, k, nil, nil)
		if policy.HTTP.Access == ExecutionAccessDeny {
			return nil, fmt.Errorf("http_request is disabled by execution policy")
		}
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("invalid url: %w", err)
		}
		method := strings.ToUpper(strings.TrimSpace(params.Method))
		if method == "" {
			method = http.MethodGet
		}
		if err := validateHTTPRequestPolicy(parsed, method, policy.HTTP); err != nil {
			return nil, err
		}
		timeout := policy.HTTP.DefaultTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		if params.TimeoutSeconds > 0 {
			timeout = time.Duration(params.TimeoutSeconds) * time.Second
		}
		if policy.HTTP.MaxTimeout > 0 && timeout > policy.HTTP.MaxTimeout {
			timeout = policy.HTTP.MaxTimeout
		}
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var bodyReader io.Reader
		if params.Body != "" {
			bodyReader = strings.NewReader(params.Body)
		}
		req, err := http.NewRequestWithContext(reqCtx, method, rawURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		for k, v := range params.Headers {
			if strings.TrimSpace(k) == "" {
				continue
			}
			req.Header.Set(k, v)
		}
		if params.Body != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		}

		client := &http.Client{}
		followRedirects := policy.HTTP.FollowRedirects
		if params.FollowRedirects != nil {
			followRedirects = followRedirects && *params.FollowRedirects
		}
		if !followRedirects {
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
		} else {
			client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
				return validateHTTPRequestPolicy(req.URL, req.Method, policy.HTTP)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()

		bodyData, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBodyBytes+1))
		if err != nil {
			return nil, err
		}
		truncated := len(bodyData) > maxHTTPResponseBodyBytes
		if truncated {
			bodyData = bodyData[:maxHTTPResponseBodyBytes]
		}
		return json.Marshal(map[string]any{
			"status_code":      resp.StatusCode,
			"status":           resp.Status,
			"headers":          resp.Header,
			"body":             string(bodyData),
			"body_truncated":   truncated,
			"url":              rawURL,
			"method":           method,
			"follow_redirects": followRedirects,
		})
	}
}

func runCommandHandler(sb sandbox.Sandbox) tool.ToolHandler {
	return runCommandHandlerWithPolicy(nil, sb)
}

func runCommandHandlerWithPolicy(k *kernel.Kernel, sb sandbox.Sandbox) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		policy := effectiveExecutionPolicy(ctx, k, sb, nil)
		if policy.Command.Access == ExecutionAccessDeny {
			return nil, fmt.Errorf("run_command is disabled by execution policy")
		}
		output, err := sb.Execute(ctx, buildExecRequest(params.Command, params.Args, policy))
		if err != nil {
			return nil, err
		}
		return marshalCommandOutput(ctx, output, func(path string, data []byte) error {
			return sb.WriteFile(path, data)
		})
	}
}

func runCommandHandlerExec(exec workspace.Executor, ws workspace.Workspace) tool.ToolHandler {
	return runCommandHandlerExecWithPolicy(nil, exec, ws)
}

func runCommandHandlerExecWithPolicy(k *kernel.Kernel, exec workspace.Executor, ws workspace.Workspace) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		policy := effectiveExecutionPolicy(ctx, k, nil, ws)
		if policy.Command.Access == ExecutionAccessDeny {
			return nil, fmt.Errorf("run_command is disabled by execution policy")
		}
		output, err := exec.Execute(ctx, buildExecRequest(params.Command, params.Args, policy))
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

func readFileHandlerWS(ws workspace.Workspace) tool.ToolHandler {
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

func writeFileHandlerWS(ws workspace.Workspace) tool.ToolHandler {
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

func editFileHandlerWS(ws workspace.Workspace) tool.ToolHandler {
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

func globHandlerWS(ws workspace.Workspace) tool.ToolHandler {
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

func listFilesHandlerWS(ws workspace.Workspace) tool.ToolHandler {
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

func grepHandlerWS(ws workspace.Workspace) tool.ToolHandler {
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

func askUserHandler(io kernio.UserIO) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Question        string         `json:"question"`
			RequestedSchema map[string]any `json:"requestedSchema"`
		}
		if err := unmarshalAskUserInputWithRetry(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if io == nil {
			return json.Marshal("ask_user: no user IO available")
		}
		req := kernio.InputRequest{
			Type:   kernio.InputFreeText,
			Prompt: params.Question,
		}
		fields, err := buildAskUserFields(params.RequestedSchema)
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			req.Type = kernio.InputForm
			req.Fields = fields
			req.ConfirmLabel = "Confirm"
		}
		resp, err := io.Ask(ctx, req)
		if err != nil {
			return nil, err
		}
		if req.Type == kernio.InputForm {
			return json.Marshal(resp.Form)
		}
		return json.Marshal(resp.Value)
	}
}

func buildAskUserFields(schema map[string]any) ([]kernio.InputField, error) {
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
	} else if reqStr, ok := schema["required"].([]string); ok {
		for _, name := range reqStr {
			requiredSet[name] = true
		}
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]kernio.InputField, 0, len(keys))
	for _, name := range keys {
		rawDef, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		field := kernio.InputField{
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
			field.Type = kernio.InputFieldBoolean
		case "array":
			field.Type = kernio.InputFieldMultiSelect
		case "number":
			field.Type = kernio.InputFieldNumber
		case "integer":
			field.Type = kernio.InputFieldInteger
		default:
			if enum := toStringSlice(rawDef["enum"]); len(enum) > 0 {
				field.Type = kernio.InputFieldSingleSelect
				field.Options = enum
			} else {
				field.Type = kernio.InputFieldString
			}
		}
		if field.Type == kernio.InputFieldMultiSelect {
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

func normalizeDefaultForField(ft kernio.InputFieldType, v any) any {
	switch ft {
	case kernio.InputFieldBoolean:
		if b, ok := v.(bool); ok {
			return b
		}
	case kernio.InputFieldNumber:
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
	case kernio.InputFieldInteger:
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
	case kernio.InputFieldMultiSelect:
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
	meta, ok := toolctx.ToolCallContextFromContext(ctx)
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

func effectiveExecutionPolicy(ctx context.Context, k *kernel.Kernel, sb sandbox.Sandbox, ws workspace.Workspace) ExecutionPolicy {
	if k != nil {
		policy := ExecutionPolicyOf(k)
		if meta, ok := toolctx.ToolCallContextFromContext(ctx); ok {
			return ExecutionPolicyForToolContext(meta, k, policy)
		}
		return policy
	}
	workspace := ""
	if sb != nil {
		if root, err := sb.ResolvePath("."); err == nil {
			workspace = root
		}
	}
	return resolveExecutionPolicy(appconfig.TrustTrusted, "full-auto", commandPolicyDefaults(sb, workspace, ws))
}

func buildExecRequest(command string, args []string, policy ExecutionPolicy) workspace.ExecRequest {
	req := workspace.ExecRequest{
		Command:  command,
		Args:     append([]string(nil), args...),
		Timeout:  policy.Command.DefaultTimeout,
		ClearEnv: policy.Command.ClearEnv,
		Env:      cloneStringMap(policy.Command.Env),
		Network:  policy.Command.Network,
	}
	if len(policy.Command.AllowedPaths) > 0 {
		req.WorkingDir = "."
		req.AllowedPaths = append([]string(nil), policy.Command.AllowedPaths...)
	}
	if policy.Command.MaxTimeout > 0 && req.Timeout > policy.Command.MaxTimeout {
		req.Timeout = policy.Command.MaxTimeout
	}
	return req
}

func validateHTTPRequestPolicy(parsed *url.URL, method string, policy HTTPExecutionPolicy) error {
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if !containsFolded(policy.AllowedSchemes, scheme) {
		return fmt.Errorf("url scheme %q is not allowed by execution policy", scheme)
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if !containsFolded(policy.AllowedMethods, method) {
		return fmt.Errorf("http method %q is not allowed by execution policy", method)
	}
	if len(policy.AllowedHosts) > 0 {
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if !containsFolded(policy.AllowedHosts, host) {
			return fmt.Errorf("url host %q is not allowed by execution policy", host)
		}
	}
	return nil
}

func containsFolded(items []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, item := range items {
		if strings.ToLower(strings.TrimSpace(item)) == target {
			return true
		}
	}
	return false
}

func unmarshalAskUserInputWithRetry(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err == nil {
		return nil
	} else if !isUnexpectedJSONEOF(err) {
		return err
	}

	repaired := repairTruncatedJSON(string(raw))
	if strings.TrimSpace(repaired) == "" {
		return fmt.Errorf("unexpected end of JSON input")
	}
	return json.Unmarshal([]byte(repaired), out)
}

func isUnexpectedJSONEOF(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") || strings.Contains(msg, "unexpected EOF")
}

func repairTruncatedJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	stack := make([]rune, 0, 8)
	inString := false
	escaped := false

	for _, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, r)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if inString && escaped {
		s += `\`
	}
	if inString {
		s += `"`
	}
	s = strings.TrimRight(s, ", \t\r\n")

	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
}
