package tool

import (
	"context"
	"encoding/json"
)

// RiskLevel 表示工具的风险等级。
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"    // 只读操作
	RiskMedium RiskLevel = "medium" // 有限副作用
	RiskHigh   RiskLevel = "high"   // 文件写入、命令执行
)

// ToolSpec 描述一个工具的元信息。
type ToolSpec struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	InputSchema       json.RawMessage `json:"input_schema"`
	Risk              RiskLevel       `json:"risk"`
	Capabilities      []string        `json:"capabilities,omitempty"`
	Source            string          `json:"source,omitempty"`
	Owner             string          `json:"owner,omitempty"`
	RequiresWorkspace bool            `json:"requires_workspace,omitempty"`
	RequiresExecutor  bool            `json:"requires_executor,omitempty"`
	RequiresSandbox   bool            `json:"requires_sandbox,omitempty"`
}

// ToolHandler 是工具的执行函数。
type ToolHandler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
