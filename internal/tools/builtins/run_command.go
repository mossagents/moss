package builtins

import (
	"context"
	"time"

	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

type RunCommandTool struct {
	ws *workspace.Manager
}

func NewRunCommandTool(ws *workspace.Manager) *RunCommandTool {
	return &RunCommandTool{ws: ws}
}

func (t *RunCommandTool) Name() string           { return "run_command" }
func (t *RunCommandTool) Description() string    { return "Runs a shell command in the workspace directory" }
func (t *RunCommandTool) Risk() tools.RiskLevel  { return tools.RiskHigh }
func (t *RunCommandTool) Capabilities() []string { return []string{"execute"} }

func (t *RunCommandTool) Execute(ctx context.Context, input tools.ToolInput) (tools.ToolOutput, error) {
	cmd, _ := input["command"].(string)
	if cmd == "" {
		return tools.ToolOutput{Success: false, Error: "command is required"}, nil
	}
	var args []string
	if rawArgs, ok := input["args"]; ok {
		switch v := rawArgs.(type) {
		case []string:
			args = v
		case []any:
			for _, a := range v {
				if s, ok := a.(string); ok {
					args = append(args, s)
				}
			}
		}
	}
	timeout := 30 * time.Second
	out, exitCode, err := t.ws.RunCommand(cmd, args, timeout)
	if err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}
	return tools.ToolOutput{
		Success: exitCode == 0,
		Data:    map[string]any{"output": out, "exit_code": exitCode},
	}, nil
}
