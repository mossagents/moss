package interaction

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// ─── NoOpIO ─────────────────────────────────────────

// NoOpIO 是 UserIO 的空实现，忽略所有输出，Ask 返回安全默认值。
// 适用于不需要用户交互的场景（如后台任务、测试）。
type NoOpIO struct{}

var _ UserIO = (*NoOpIO)(nil)

func (n *NoOpIO) Send(_ context.Context, _ OutputMessage) error {
	return nil
}

func (n *NoOpIO) Ask(_ context.Context, req InputRequest) (InputResponse, error) {
	switch req.Type {
	case InputConfirm:
		resp := InputResponse{Approved: false}
		if req.Approval != nil {
			resp.Decision = &ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  false,
				Source:    "noop",
			}
		}
		return resp, nil
	case InputSelect:
		if len(req.Options) > 0 {
			return InputResponse{Selected: 0}, nil
		}
		return InputResponse{}, nil
	case InputForm:
		form := map[string]any{}
		for _, f := range req.Fields {
			if f.Default != nil {
				form[f.Name] = f.Default
				continue
			}
			switch f.Type {
			case InputFieldBoolean:
				form[f.Name] = false
			case InputFieldMultiSelect:
				form[f.Name] = []string{}
			case InputFieldSingleSelect:
				if len(f.Options) > 0 {
					form[f.Name] = f.Options[0]
				} else {
					form[f.Name] = ""
				}
			default:
				form[f.Name] = ""
			}
		}
		return InputResponse{Form: form}, nil
	default:
		return InputResponse{}, nil
	}
}

// ─── PrintfIO ───────────────────────────────────────

// PrintfIO 将所有输出消息格式化写入 io.Writer。
// Ask 自动批准所有确认请求，适用于非交互式 CLI 或日志场景。
type PrintfIO struct {
	w io.Writer
}

var _ UserIO = (*PrintfIO)(nil)

// NewPrintfIO 创建 PrintfIO，将消息输出到指定 Writer。
func NewPrintfIO(w io.Writer) *PrintfIO {
	return &PrintfIO{w: w}
}

func (p *PrintfIO) Send(_ context.Context, msg OutputMessage) error {
	var err error
	switch msg.Type {
	case OutputText:
		_, err = fmt.Fprintln(p.w, msg.Content)
	case OutputStream:
		_, err = fmt.Fprint(p.w, msg.Content)
	case OutputStreamEnd:
		_, err = fmt.Fprintln(p.w)
	case OutputReasoning:
		_, err = fmt.Fprintf(p.w, "💭 %s\n", msg.Content)
	case OutputProgress:
		_, err = fmt.Fprintf(p.w, "⏳ %s\n", msg.Content)
	case OutputToolStart:
		_, err = fmt.Fprintf(p.w, "🔧 %s\n", msg.Content)
	case OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			_, err = fmt.Fprintf(p.w, "❌ %s\n", msg.Content)
		} else {
			_, err = fmt.Fprintf(p.w, "✅ %s\n", msg.Content)
		}
	}
	return err
}

func (p *PrintfIO) Ask(_ context.Context, req InputRequest) (InputResponse, error) {
	if _, err := fmt.Fprintln(p.w, req.Prompt); err != nil {
		return InputResponse{}, err
	}
	switch req.Type {
	case InputConfirm:
		resp := InputResponse{Approved: true}
		if req.Approval != nil {
			resp.Decision = &ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  true,
				Source:    "printf",
			}
		}
		return resp, nil
	case InputSelect:
		return InputResponse{Selected: 0}, nil
	case InputForm:
		form := map[string]any{}
		for _, f := range req.Fields {
			if f.Default != nil {
				form[f.Name] = f.Default
				continue
			}
			switch f.Type {
			case InputFieldBoolean:
				form[f.Name] = true
			case InputFieldMultiSelect:
				form[f.Name] = []string{}
			case InputFieldSingleSelect:
				if len(f.Options) > 0 {
					form[f.Name] = f.Options[0]
				} else {
					form[f.Name] = ""
				}
			default:
				form[f.Name] = ""
			}
		}
		return InputResponse{Form: form}, nil
	default:
		return InputResponse{}, nil
	}
}

// ─── BufferIO ───────────────────────────────────────

// BufferIO 缓冲所有消息，用于测试和验证。线程安全。
type BufferIO struct {
	mu      sync.Mutex
	Sent    []OutputMessage
	Asked   []InputRequest
	AskFunc func(InputRequest) InputResponse // 可选：自定义 Ask 回复逻辑
}

var _ UserIO = (*BufferIO)(nil)

// NewBufferIO 创建一个空的 BufferIO。
func NewBufferIO() *BufferIO {
	return &BufferIO{}
}

func (b *BufferIO) Send(_ context.Context, msg OutputMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Sent = append(b.Sent, msg)
	return nil
}

func (b *BufferIO) Ask(_ context.Context, req InputRequest) (InputResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Asked = append(b.Asked, req)

	if b.AskFunc != nil {
		return b.AskFunc(req), nil
	}

	switch req.Type {
	case InputConfirm:
		resp := InputResponse{Approved: true}
		if req.Approval != nil {
			resp.Decision = &ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  true,
				Source:    "buffer",
			}
		}
		return resp, nil
	case InputSelect:
		return InputResponse{Selected: 0}, nil
	case InputForm:
		form := map[string]any{}
		for _, f := range req.Fields {
			if f.Default != nil {
				form[f.Name] = f.Default
				continue
			}
			switch f.Type {
			case InputFieldBoolean:
				form[f.Name] = true
			case InputFieldMultiSelect:
				form[f.Name] = []string{}
			case InputFieldSingleSelect:
				if len(f.Options) > 0 {
					form[f.Name] = f.Options[0]
				} else {
					form[f.Name] = ""
				}
			default:
				form[f.Name] = ""
			}
		}
		return InputResponse{Form: form}, nil
	default:
		return InputResponse{Value: ""}, nil
	}
}

// SentTexts 返回所有 OutputText 类型消息的内容。
func (b *BufferIO) SentTexts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var texts []string
	for _, msg := range b.Sent {
		if msg.Type == OutputText {
			texts = append(texts, msg.Content)
		}
	}
	return texts
}
