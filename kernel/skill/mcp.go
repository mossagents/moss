package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mossagi/moss/kernel/tool"
)

// MCPSkill 通过 MCP 协议连接外部 skill server，发现并注册工具。
type MCPSkill struct {
	cfg       SkillConfig
	client    mcpclient.MCPClient
	toolNames []string
}

var _ Skill = (*MCPSkill)(nil)

// NewMCPSkill 根据配置创建 MCPSkill（但不连接，连接在 Init 时执行）。
func NewMCPSkill(cfg SkillConfig) *MCPSkill {
	return &MCPSkill{cfg: cfg}
}

func (s *MCPSkill) Metadata() Metadata {
	return Metadata{
		Name:        s.cfg.Name,
		Version:     "0.0.0",
		Description: fmt.Sprintf("MCP skill: %s (transport: %s)", s.cfg.Name, s.cfg.Transport),
		Tools:       s.toolNames,
	}
}

func (s *MCPSkill) Init(ctx context.Context, deps Deps) error {
	// 1. 建立连接
	client, err := s.connect(ctx)
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

	// 4. 将 MCP tools 注册到 ToolRegistry
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
		}

		handler := s.makeHandler(mcpTool.Name)
		if err := deps.ToolRegistry.Register(spec, handler); err != nil {
			return fmt.Errorf("register mcp tool %s: %w", toolName, err)
		}

		s.toolNames = append(s.toolNames, toolName)
	}

	return nil
}

func (s *MCPSkill) Shutdown(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// connect 根据 transport 类型创建 MCP client 连接。
func (s *MCPSkill) connect(ctx context.Context) (mcpclient.MCPClient, error) {
	env := s.buildEnv()

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
func (s *MCPSkill) buildCommand() []string {
	parts := strings.Fields(s.cfg.Command)
	return append(parts, s.cfg.Args...)
}

// buildEnv 构建环境变量列表（KEY=VALUE 格式）。
func (s *MCPSkill) buildEnv() []string {
	env := os.Environ()
	for k, v := range s.cfg.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// makeHandler 为指定 MCP tool 创建 ToolHandler。
func (s *MCPSkill) makeHandler(mcpToolName string) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		// 解析 input 为 map
		var args map[string]any
		if len(input) > 0 {
			if err := json.Unmarshal(input, &args); err != nil {
				return nil, fmt.Errorf("unmarshal mcp tool input: %w", err)
			}
		}

		req := mcp.CallToolRequest{}
		req.Params.Name = mcpToolName
		req.Params.Arguments = args

		result, err := s.client.CallTool(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("mcp call %s: %w", mcpToolName, err)
		}

		// 将结果转换为 JSON
		return json.Marshal(result)
	}
}
