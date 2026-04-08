package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/tool"
	"strings"
	"time"
)

// ThinkToolConfig controls the shared think_tool registration details.
type ThinkToolConfig struct {
	Name         string
	Description  string
	Capabilities []string
}

// ThinkToolOption customizes ThinkToolConfig.
type ThinkToolOption func(*ThinkToolConfig)

func defaultThinkToolConfig() ThinkToolConfig {
	return ThinkToolConfig{
		Name:         "think_tool",
		Description:  "Record a short reflection about what was found, what is missing, and what to do next.",
		Capabilities: []string{"thinking"},
	}
}

func WithThinkToolName(name string) ThinkToolOption {
	return func(cfg *ThinkToolConfig) {
		if strings.TrimSpace(name) != "" {
			cfg.Name = strings.TrimSpace(name)
		}
	}
}

func WithThinkToolDescription(description string) ThinkToolOption {
	return func(cfg *ThinkToolConfig) {
		if strings.TrimSpace(description) != "" {
			cfg.Description = strings.TrimSpace(description)
		}
	}
}

func WithThinkToolCapabilities(capabilities ...string) ThinkToolOption {
	return func(cfg *ThinkToolConfig) {
		if len(capabilities) == 0 {
			return
		}
		out := make([]string, 0, len(capabilities))
		for _, cap := range capabilities {
			if trimmed := strings.TrimSpace(cap); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			cfg.Capabilities = out
		}
	}
}

func NewThinkToolSpec(opts ...ThinkToolOption) tool.ToolSpec {
	cfg := defaultThinkToolConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return tool.ToolSpec{
		Name:        cfg.Name,
		Description: cfg.Description,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"thought": {"type": "string", "description": "Reflection or next-step note"}
			},
			"required": ["thought"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: append([]string(nil), cfg.Capabilities...),
	}
}

func NewThinkToolHandler(toolName string) tool.ToolHandler {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "think_tool"
	}
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Thought string `json:"thought"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse %s input: %w", name, err)
		}
		return json.Marshal(map[string]any{
			"recorded":    true,
			"thought":     strings.TrimSpace(params.Thought),
			"recorded_at": time.Now().Format(time.RFC3339),
		})
	}
}

func RegisterThinkTool(reg tool.Registry, opts ...ThinkToolOption) error {
	spec := NewThinkToolSpec(opts...)
	if err := reg.Register(spec, NewThinkToolHandler(spec.Name)); err != nil {
		return fmt.Errorf("register %s: %w", spec.Name, err)
	}
	return nil
}
