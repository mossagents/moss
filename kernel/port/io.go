package port

import "context"

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
)

// InputRequest 是向用户发出的输入请求。
type InputRequest struct {
	Type    InputType      `json:"type"`
	Prompt  string         `json:"prompt"`
	Options []string       `json:"options,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// InputResponse 是用户对输入请求的回复。
type InputResponse struct {
	Value    string `json:"value,omitempty"`
	Selected int    `json:"selected,omitempty"`
	Approved bool   `json:"approved,omitempty"`
}
