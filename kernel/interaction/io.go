package interaction

import (
	"context"
)

// UserIO 是 Kernel 与用户交互的 Port（结构化交互协议）。
type UserIO interface {
	// Send 向用户推送内容（单向，不等待回复）。
	Send(ctx context.Context, msg OutputMessage) error

	// Ask 向用户请求输入（阻塞等待回复）。
	Ask(ctx context.Context, req InputRequest) (InputResponse, error)
}

// OutputType 表示输出消息的类型。
type OutputType string

const (
	OutputText       OutputType = "text"        // 完整文本消息
	OutputStream     OutputType = "stream"      // 流式 chunk（追加到当前消息）
	OutputStreamEnd  OutputType = "stream_end"  // 流式结束
	OutputReasoning  OutputType = "reasoning"   // 模型推理内容
	OutputProgress   OutputType = "progress"    // 进度更新
	OutputToolStart  OutputType = "tool_start"  // 工具开始执行
	OutputToolResult OutputType = "tool_result" // 工具执行结果
)

// OutputMessage 是发送给用户的一条输出消息。
type OutputMessage struct {
	Type    OutputType     `json:"type"`
	Content string         `json:"content"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// InputType 表示向用户请求输入的类型。
type InputType string

const (
	InputFreeText InputType = "free_text" // 自由文本输入
	InputConfirm  InputType = "confirm"   // y/n 确认
	InputSelect   InputType = "select"    // 从选项中选择
	InputForm     InputType = "form"      // 结构化表单
)

// InputFieldType 表示表单字段类型。
type InputFieldType string

const (
	InputFieldString       InputFieldType = "string"
	InputFieldBoolean      InputFieldType = "boolean"
	InputFieldSingleSelect InputFieldType = "single_select"
	InputFieldMultiSelect  InputFieldType = "multi_select"
	InputFieldNumber       InputFieldType = "number"
	InputFieldInteger      InputFieldType = "integer"
)

// InputField 描述 InputForm 中的单个字段。
type InputField struct {
	Name        string         `json:"name"`
	Type        InputFieldType `json:"type"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Options     []string       `json:"options,omitempty"`
	Default     any            `json:"default,omitempty"`
}

// InputRequest 是向用户发出的输入请求。
type InputRequest struct {
	Type         InputType        `json:"type"`
	Prompt       string           `json:"prompt"`
	Options      []string         `json:"options,omitempty"`
	Fields       []InputField     `json:"fields,omitempty"`
	ConfirmLabel string           `json:"confirm_label,omitempty"`
	Approval     *ApprovalRequest `json:"approval,omitempty"`
	Meta         map[string]any   `json:"meta,omitempty"`
}

// InputResponse 是用户对输入请求的回复。
type InputResponse struct {
	Value    string            `json:"value,omitempty"`
	Selected int               `json:"selected,omitempty"`
	Approved bool              `json:"approved,omitempty"`
	Decision *ApprovalDecision `json:"decision,omitempty"`
	Form     map[string]any    `json:"form,omitempty"`
}
