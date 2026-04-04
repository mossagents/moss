package port

import "encoding/json"

// Role 表示消息的角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 是 Kernel 中的统一消息结构。
type Message struct {
	Role         Role          `json:"role"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolResults  []ToolResult  `json:"tool_results,omitempty"`
}

// ToolCall 表示 LLM 请求的一次工具调用。
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult 表示一次工具调用的执行结果。
type ToolResult struct {
	CallID       string        `json:"call_id"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	IsError      bool          `json:"is_error,omitempty"`
}

// ContentPartType 表示消息内容分片类型。
type ContentPartType string

const (
	ContentPartText        ContentPartType = "text"
	ContentPartReasoning   ContentPartType = "reasoning"
	ContentPartInputImage  ContentPartType = "input_image"
	ContentPartOutputImage ContentPartType = "output_image"
	ContentPartInputAudio  ContentPartType = "input_audio"
	ContentPartOutputAudio ContentPartType = "output_audio"
	ContentPartInputVideo  ContentPartType = "input_video"
	ContentPartOutputVideo ContentPartType = "output_video"
)

// ContentPart 是多模态消息内容的统一结构。
type ContentPart struct {
	Type       ContentPartType `json:"type"`
	Text       string          `json:"text,omitempty"`
	MIMEType   string          `json:"mime_type,omitempty"`
	DataBase64 string          `json:"data_base64,omitempty"`
	URL        string          `json:"url,omitempty"`
	SourcePath string          `json:"source_path,omitempty"`
}

// TokenUsage 记录 token 用量。
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
