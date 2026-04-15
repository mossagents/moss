package builtintools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"github.com/mossagents/moss/kernel/workspace"
	policy "github.com/mossagents/moss/runtime/policy"
)

const maxInlineCommandOutput = 8000
const maxHTTPResponseBodyBytes = 256 * 1024

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
		pol := effectiveToolPolicy(ctx, k, nil, "")
		if pol.HTTP.Access == policy.ToolAccessDeny {
			return nil, fmt.Errorf("http_request is disabled by tool policy")
		}
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("invalid url: %w", err)
		}
		method := strings.ToUpper(strings.TrimSpace(params.Method))
		if method == "" {
			method = http.MethodGet
		}
		if err := validateHTTPRequestPolicy(parsed, method, pol.HTTP); err != nil {
			return nil, err
		}
		timeout := pol.HTTP.DefaultTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		if params.TimeoutSeconds > 0 {
			timeout = time.Duration(params.TimeoutSeconds) * time.Second
		}
		if pol.HTTP.MaxTimeout > 0 && timeout > pol.HTTP.MaxTimeout {
			timeout = pol.HTTP.MaxTimeout
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
		followRedirects := pol.HTTP.FollowRedirects
		if params.FollowRedirects != nil {
			followRedirects = followRedirects && *params.FollowRedirects
		}
		if !followRedirects {
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
		} else {
			client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
				return validateHTTPRequestPolicy(req.URL, req.Method, pol.HTTP)
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

func runCommandHandlerExec(exec workspace.Executor, ws workspace.Workspace) tool.ToolHandler {
	return runCommandHandlerExecWithPolicy(nil, exec, ws)
}

func runCommandHandlerExecWithPolicy(k *kernel.Kernel, exec workspace.Executor, ws workspace.Workspace) tool.ToolHandler {
	return runCommandHandlerWithExecutor(k, exec, ws, "")
}

func runCommandHandlerWithExecutor(k *kernel.Kernel, exec workspace.Executor, ws workspace.Workspace, workspaceRoot string) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		pol := effectiveToolPolicy(ctx, k, ws, workspaceRoot)
		if pol.Command.Access == policy.ToolAccessDeny {
			return nil, fmt.Errorf("run_command is disabled by tool policy")
		}
		output, err := exec.Execute(ctx, buildExecRequest(params.Command, params.Args, pol))
		if err != nil {
			return nil, err
		}

		var writeFile func(path string, data []byte) error
		if ws != nil {
			writeFile = func(path string, data []byte) error {
				return ws.WriteFile(ctx, path, data)
			}
		}
		return marshalCommandOutput(ctx, output, writeFile)
	}
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

func effectiveToolPolicy(ctx context.Context, k *kernel.Kernel, ws workspace.Workspace, workspaceRoot string) policy.ToolPolicy {
	if k != nil {
		p := policy.PolicyOf(k)
		if meta, ok := toolctx.ToolCallContextFromContext(ctx); ok {
			return policy.PolicyForContext(ctx, meta, k, p)
		}
		return p
	}
	return policy.ResolveToolPolicyForWorkspace(workspaceRoot, appconfig.TrustTrusted, "full-auto")
}

func buildExecRequest(command string, args []string, p policy.ToolPolicy) workspace.ExecRequest {
	req := workspace.ExecRequest{
		Command:  command,
		Args:     append([]string(nil), args...),
		Timeout:  p.Command.DefaultTimeout,
		ClearEnv: p.Command.ClearEnv,
		Env:      policy.CloneStringMap(p.Command.Env),
		Network:  p.Command.Network,
	}
	if len(p.Command.AllowedPaths) > 0 {
		req.WorkingDir = "."
		req.AllowedPaths = append([]string(nil), p.Command.AllowedPaths...)
	}
	if p.Command.MaxTimeout > 0 && req.Timeout > p.Command.MaxTimeout {
		req.Timeout = p.Command.MaxTimeout
	}
	return req
}

func validateHTTPRequestPolicy(parsed *url.URL, method string, p policy.HTTPPolicy) error {
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if !containsFolded(p.AllowedSchemes, scheme) {
		return fmt.Errorf("url scheme %q is not allowed by tool policy", scheme)
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if !containsFolded(p.AllowedMethods, method) {
		return fmt.Errorf("http method %q is not allowed by tool policy", method)
	}
	if len(p.AllowedHosts) > 0 {
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if !containsFolded(p.AllowedHosts, host) {
			return fmt.Errorf("url host %q is not allowed by tool policy", host)
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



