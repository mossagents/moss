package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mossagents/moss/harness/extensions/capability"
	config "github.com/mossagents/moss/harness/config"
	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/harness/sandbox"
	"os"
	"strings"
)

const (
	maxMCPToolInputBytes  = 64 * 1024
	maxMCPToolOutputBytes = 1024 * 1024
	maxJSONDepth          = 64
)

// MCPServer 通过 MCP 协议连接外部工具服务器，并将远端工具桥接到本地 ToolRegistry。
// 它实现了 capability.Provider，以便纳入统一生命周期管理，但它不是 prompt skill，
// 也不是 runtime 包内建的 builtin tools。
type MCPServer struct {
	cfg       config.SkillConfig
	client    mcpclient.MCPClient
	toolNames []string
	guard     ToolGuard
}

var _ capability.Provider = (*MCPServer)(nil)

// ToolGuard 负责 MCP 工具调用的输入/输出守卫。
// Policy middleware 负责访问决策与审计，ToolGuard 仅负责 I/O 校验与约束。
type ToolGuard interface {
	ValidateInput(ctx context.Context, tool string, input []byte) error
	ValidateOutput(ctx context.Context, tool string, output []byte) error
}

type defaultToolGuard struct{}

func (defaultToolGuard) ValidateInput(_ context.Context, _ string, input []byte) error {
	return validateMCPInput(input)
}

func (defaultToolGuard) ValidateOutput(_ context.Context, _ string, output []byte) error {
	return validateMCPOutput(output)
}

// NewMCPServer 根据配置创建 MCPServer（但不连接，连接在 Init 时执行）。
func NewMCPServer(cfg config.SkillConfig) *MCPServer {
	return NewMCPServerWithGuard(cfg, nil)
}

// NewMCPServerWithGuard 根据配置和 guard 创建 MCPServer。
func NewMCPServerWithGuard(cfg config.SkillConfig, guard ToolGuard) *MCPServer {
	if guard == nil {
		guard = defaultToolGuard{}
	}
	return &MCPServer{cfg: cfg, guard: guard}
}

func (s *MCPServer) Metadata() capability.Metadata {
	return capability.Metadata{
		Name:        s.cfg.Name,
		Version:     "0.0.0",
		Description: fmt.Sprintf("MCP server: %s (transport: %s)", s.cfg.Name, s.cfg.Transport),
		Tools:       s.toolNames,
		DependsOn:   append([]string(nil), s.cfg.DependsOn...),
		RequiredEnv: append([]string(nil), s.cfg.RequiredEnv...),
	}
}

func (s *MCPServer) Init(ctx context.Context, deps capability.Deps) error {
	// 1. 建立连接
	client, err := s.connect(ctx, deps)
	if err != nil {
		return fmt.Errorf("mcp connect %s: %w", s.cfg.Name, err)
	}
	s.client = client

	// 2. 初始化 MCP 会话
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "moss",
		Version: "0.3.0",
	}
	_, err = s.client.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("mcp initialize %s: %w", s.cfg.Name, err)
	}

	// 3. 发现工具
	toolsResult, err := s.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("mcp list tools %s: %w", s.cfg.Name, err)
	}

	// 4. 将 MCP tools 注册到 ToolRegistry。
	// 远端工具会加上 "<server>_" 前缀，和 runtime builtin tools 明确区分。
	prefix := s.cfg.Name + "_"
	for _, t := range toolsResult.Tools {
		mcpTool := t // capture
		toolName := prefix + mcpTool.Name

		schema, err := json.Marshal(mcpTool.InputSchema)
		if err != nil {
			return fmt.Errorf("marshal schema for %s: %w", toolName, err)
		}

		spec := tool.ToolSpec{
			Name:         toolName,
			Description:  mcpTool.Description,
			InputSchema:  schema,
			Risk:         tool.RiskMedium, // MCP tools default to medium risk
			Capabilities: []string{"mcp", s.cfg.Name},
			Source:       "mcp",
			Owner:        s.cfg.Name,
		}

		handler := s.makeHandler(mcpTool.Name)
		if err := deps.ToolRegistry.Register(tool.NewRawTool(spec, handler)); err != nil {
			return fmt.Errorf("register mcp tool %s: %w", toolName, err)
		}

		s.toolNames = append(s.toolNames, toolName)
	}

	return nil
}

func (s *MCPServer) Shutdown(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// connect 根据 transport 类型创建 MCP client 连接。
func (s *MCPServer) connect(ctx context.Context, deps capability.Deps) (mcpclient.MCPClient, error) {
	env, err := s.buildEnv(ctx, deps.UserIO)
	if err != nil {
		return nil, err
	}

	switch s.cfg.Transport {
	case "stdio":
		parts := s.buildCommand()
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty command for stdio transport")
		}
		client, err := mcpclient.NewStdioMCPClient(parts[0], env, parts[1:]...)
		if err != nil {
			return nil, err
		}
		return client, nil

	case "sse":
		if s.cfg.URL == "" {
			return nil, fmt.Errorf("empty url for sse transport")
		}
		client, err := mcpclient.NewSSEMCPClient(s.cfg.URL)
		if err != nil {
			return nil, err
		}
		if err := client.Start(ctx); err != nil {
			return nil, err
		}
		return client, nil

	default:
		return nil, fmt.Errorf("unsupported transport: %s", s.cfg.Transport)
	}
}

// buildCommand 构建启动命令和参数。
func (s *MCPServer) buildCommand() []string {
	parts := strings.Fields(s.cfg.Command)
	return append(parts, s.cfg.Args...)
}

// buildEnv 构建环境变量列表（KEY=VALUE 格式）。
func (s *MCPServer) buildEnv(ctx context.Context, userIO io.UserIO) ([]string, error) {
	base := sandbox.SafeInheritedEnvironment()
	resolved, err := resolveMCPRequiredEnv(ctx, userIO, s.cfg.Name, s.cfg.Env, s.cfg.RequiredEnv)
	if err != nil {
		return nil, err
	}
	for k, v := range resolved {
		base[k] = v
	}
	env := make([]string, 0, len(base))
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	return env, nil
}

func resolveMCPRequiredEnv(ctx context.Context, userIO io.UserIO, providerName string, configured map[string]string, required []string) (map[string]string, error) {
	resolved := make(map[string]string, len(configured))
	for key, value := range configured {
		resolved[key] = value
	}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(resolved[key]); value != "" {
			continue
		}
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			resolved[key] = value
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) == 0 {
		return resolved, nil
	}
	if userIO == nil {
		return nil, fmt.Errorf("mcp server %q requires env %s", providerName, strings.Join(missing, ", "))
	}
	fields := make([]io.InputField, 0, len(missing))
	for _, key := range missing {
		fields = append(fields, io.InputField{
			Name:        key,
			Type:        io.InputFieldString,
			Title:       key,
			Description: fmt.Sprintf("Required by MCP server %s", providerName),
			Required:    true,
		})
	}
	resp, err := userIO.Ask(ctx, io.InputRequest{
		Type:   io.InputForm,
		Prompt: fmt.Sprintf("Provide the missing environment values for MCP server %s.", providerName),
		Fields: fields,
	})
	if err != nil {
		return nil, err
	}
	for _, key := range missing {
		value := strings.TrimSpace(fmt.Sprint(resp.Form[key]))
		if value == "" {
			return nil, fmt.Errorf("mcp server %q requires env %s", providerName, key)
		}
		resolved[key] = value
	}
	return resolved, nil
}

// makeHandler 为指定 MCP tool 创建 ToolHandler。
func (s *MCPServer) makeHandler(mcpToolName string) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		guard := s.guard
		if guard == nil {
			guard = defaultToolGuard{}
		}
		if err := guard.ValidateInput(ctx, mcpToolName, input); err != nil {
			return nil, err
		}

		var args map[string]any
		if len(input) > 0 {
			var payload any
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, kerrors.Wrap(kerrors.ErrValidation, "unmarshal mcp tool input", err)
			}
			obj, ok := payload.(map[string]any)
			if !ok {
				return nil, kerrors.New(kerrors.ErrValidation, "mcp tool input must be a JSON object")
			}
			args = obj
		}

		req := mcp.CallToolRequest{}
		req.Params.Name = mcpToolName
		req.Params.Arguments = args

		result, err := s.client.CallTool(ctx, req)
		if err != nil {
			return nil, classifyMCPCallError(mcpToolName, err)
		}

		output, err := json.Marshal(result)
		if err != nil {
			return nil, kerrors.Wrap(kerrors.ErrInternal, "marshal mcp result", err)
		}
		if err := guard.ValidateOutput(ctx, mcpToolName, output); err != nil {
			return nil, err
		}
		return output, nil
	}
}

func validateMCPInput(input json.RawMessage) error {
	if len(input) > maxMCPToolInputBytes {
		return kerrors.New(kerrors.ErrValidation, fmt.Sprintf("mcp tool input too large: %d bytes (max %d)", len(input), maxMCPToolInputBytes))
	}
	if len(input) == 0 {
		return nil
	}
	if !json.Valid(input) {
		return kerrors.New(kerrors.ErrValidation, "mcp tool input must be valid JSON")
	}
	if err := validateJSONDepth(input, maxJSONDepth); err != nil {
		return err
	}
	return nil
}

func validateMCPOutput(output json.RawMessage) error {
	if len(output) > maxMCPToolOutputBytes {
		return kerrors.New(kerrors.ErrValidation, fmt.Sprintf("mcp tool output too large: %d bytes (max %d)", len(output), maxMCPToolOutputBytes))
	}
	if len(output) == 0 {
		return nil
	}
	if !json.Valid(output) {
		return kerrors.New(kerrors.ErrValidation, "mcp tool output must be valid JSON")
	}
	if err := validateJSONDepth(output, maxJSONDepth); err != nil {
		return err
	}
	return nil
}

func validateJSONDepth(raw json.RawMessage, maxDepth int) error {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return kerrors.Wrap(kerrors.ErrValidation, "decode JSON payload", err)
	}
	if depth(payload) > maxDepth {
		return kerrors.New(kerrors.ErrValidation, fmt.Sprintf("json nesting depth exceeds max %d", maxDepth))
	}
	return nil
}

func depth(v any) int {
	switch n := v.(type) {
	case map[string]any:
		d := 1
		for _, child := range n {
			if cd := 1 + depth(child); cd > d {
				d = cd
			}
		}
		return d
	case []any:
		d := 1
		for _, child := range n {
			if cd := 1 + depth(child); cd > d {
				d = cd
			}
		}
		return d
	default:
		return 1
	}
}

func classifyMCPCallError(toolName string, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return kerrors.Wrap(kerrors.ErrLLMTimeout, fmt.Sprintf("mcp call %s timed out", toolName), err)
	case errors.Is(err, context.Canceled):
		return kerrors.Wrap(kerrors.ErrInternal, fmt.Sprintf("mcp call %s canceled", toolName), err)
	default:
		return kerrors.Wrap(kerrors.ErrInternal, fmt.Sprintf("mcp call %s failed", toolName), err)
	}
}
