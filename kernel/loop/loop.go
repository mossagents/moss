package loop

import (
	"context"
	"fmt"
	"io"

	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
)

// LoopConfig 配置 Agent Loop 的行为。
type LoopConfig struct {
	MaxIterations int                     // 最大循环次数（默认 50）
	StopWhen      func(port.Message) bool // 自定义停止条件
}

func (c LoopConfig) maxIter() int {
	if c.MaxIterations <= 0 {
		return 50
	}
	return c.MaxIterations
}

// AgentLoop 组合所有子系统，驱动 Agent 的 think→act→observe 循环。
type AgentLoop struct {
	LLM    port.LLM
	Tools  tool.Registry
	Chain  *middleware.Chain
	IO     port.UserIO
	Config LoopConfig
}

// SessionResult 是一次 Session 执行的结果。
type SessionResult struct {
	SessionID  string          `json:"session_id"`
	Success    bool            `json:"success"`
	Output     string          `json:"output"`
	Steps      int             `json:"steps"`
	TokensUsed port.TokenUsage `json:"tokens_used"`
	Error      string          `json:"error,omitempty"`
}

// Run 执行 Agent Loop 直到完成、预算耗尽或达到最大迭代次数。
func (l *AgentLoop) Run(ctx context.Context, sess *session.Session) (*SessionResult, error) {
	sess.Status = session.StatusRunning

	// OnSessionStart middleware
	l.runMiddleware(ctx, middleware.OnSessionStart, sess, nil, nil, nil)

	var lastOutput string
	var totalUsage port.TokenUsage
	maxIter := l.Config.maxIter()

	for i := 0; i < maxIter; i++ {
		if sess.Budget.Exhausted() {
			break
		}
		if ctx.Err() != nil {
			return l.fail(sess, totalUsage, ctx.Err()), ctx.Err()
		}

		// 1. BeforeLLM middleware
		if err := l.runMiddleware(ctx, middleware.BeforeLLM, sess, nil, nil, nil); err != nil {
			return l.fail(sess, totalUsage, err), err
		}

		// 2. LLM 调用
		resp, err := l.callLLM(ctx, sess)
		if err != nil {
			l.runErrorMiddleware(ctx, sess, err)
			return l.fail(sess, totalUsage, err), err
		}

		totalUsage.PromptTokens += resp.Usage.PromptTokens
		totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		totalUsage.TotalTokens += resp.Usage.TotalTokens
		sess.Budget.Record(resp.Usage.TotalTokens, 1)

		// 3. AfterLLM middleware
		if err := l.runMiddleware(ctx, middleware.AfterLLM, sess, nil, nil, nil); err != nil {
			return l.fail(sess, totalUsage, err), err
		}

		// 4. 追加 assistant 消息
		sess.AppendMessage(resp.Message)

		// 5. 处理 tool calls 或文本回复
		if len(resp.ToolCalls) > 0 {
			if err := l.executeToolCalls(ctx, sess, resp.ToolCalls); err != nil {
				l.runErrorMiddleware(ctx, sess, err)
				return l.fail(sess, totalUsage, err), err
			}
		} else {
			lastOutput = resp.Message.Content
			if l.IO != nil {
				l.IO.Send(ctx, port.OutputMessage{
					Type:    port.OutputText,
					Content: resp.Message.Content,
				})
			}

			// 自定义停止条件
			if l.Config.StopWhen != nil && l.Config.StopWhen(resp.Message) {
				break
			}

			// end_turn 停止
			if resp.StopReason == "end_turn" {
				break
			}
		}
	}

	sess.Status = session.StatusCompleted
	l.runMiddleware(ctx, middleware.OnSessionEnd, sess, nil, nil, nil)

	return &SessionResult{
		SessionID:  sess.ID,
		Success:    true,
		Output:     lastOutput,
		Steps:      sess.Budget.UsedSteps,
		TokensUsed: totalUsage,
	}, nil
}

func (l *AgentLoop) callLLM(ctx context.Context, sess *session.Session) (*port.CompletionResponse, error) {
	specs := l.toolSpecs()
	req := port.CompletionRequest{
		Messages: sess.Messages,
		Tools:    specs,
	}

	// 优先使用 Streaming
	if sllm, ok := l.LLM.(port.StreamingLLM); ok {
		return l.streamLLM(ctx, sllm, req)
	}

	return l.LLM.Complete(ctx, req)
}

func (l *AgentLoop) streamLLM(ctx context.Context, sllm port.StreamingLLM, req port.CompletionRequest) (*port.CompletionResponse, error) {
	iter, err := sllm.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var fullContent string
	var toolCalls []port.ToolCall
	var usage port.TokenUsage
	var stopReason string

	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if chunk.Delta != "" {
			fullContent += chunk.Delta
			if l.IO != nil {
				l.IO.Send(ctx, port.OutputMessage{
					Type:    port.OutputStream,
					Content: chunk.Delta,
				})
			}
		}

		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}

		if chunk.Done {
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
			stopReason = "end_turn"
			if len(toolCalls) > 0 {
				stopReason = "tool_use"
			}
			if l.IO != nil {
				l.IO.Send(ctx, port.OutputMessage{Type: port.OutputStreamEnd})
			}
			break
		}
	}

	msg := port.Message{
		Role:      port.RoleAssistant,
		Content:   fullContent,
		ToolCalls: toolCalls,
	}

	return &port.CompletionResponse{
		Message:    msg,
		ToolCalls:  toolCalls,
		Usage:      usage,
		StopReason: stopReason,
	}, nil
}

func (l *AgentLoop) executeToolCalls(ctx context.Context, sess *session.Session, calls []port.ToolCall) error {
	for _, call := range calls {
		spec, handler, ok := l.Tools.Get(call.Name)
		if !ok {
			result := port.ToolResult{
				CallID:  call.ID,
				Content: fmt.Sprintf("tool %q not found", call.Name),
				IsError: true,
			}
			sess.AppendMessage(port.Message{Role: port.RoleTool, ToolResults: []port.ToolResult{result}})
			continue
		}

		// UserIO: 通知工具开始
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputToolStart,
				Content: call.Name,
				Meta:    map[string]any{"call_id": call.ID},
			})
		}

		// BeforeToolCall middleware
		if err := l.runMiddleware(ctx, middleware.BeforeToolCall, sess, &spec, call.Arguments, nil); err != nil {
			result := port.ToolResult{
				CallID:  call.ID,
				Content: err.Error(),
				IsError: true,
			}
			sess.AppendMessage(port.Message{Role: port.RoleTool, ToolResults: []port.ToolResult{result}})
			continue
		}

		// 执行工具
		output, err := handler(ctx, call.Arguments)

		var result port.ToolResult
		if err != nil {
			result = port.ToolResult{
				CallID:  call.ID,
				Content: err.Error(),
				IsError: true,
			}
		} else {
			result = port.ToolResult{
				CallID:  call.ID,
				Content: string(output),
			}
		}

		// AfterToolCall middleware
		l.runMiddleware(ctx, middleware.AfterToolCall, sess, &spec, nil, output)

		// UserIO: 通知工具结果
		if l.IO != nil {
			l.IO.Send(ctx, port.OutputMessage{
				Type:    port.OutputToolResult,
				Content: result.Content,
				Meta:    map[string]any{"call_id": call.ID, "tool": call.Name, "is_error": result.IsError},
			})
		}

		sess.AppendMessage(port.Message{Role: port.RoleTool, ToolResults: []port.ToolResult{result}})
	}
	return nil
}

func (l *AgentLoop) toolSpecs() []port.ToolSpec {
	tools := l.Tools.List()
	specs := make([]port.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = port.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return specs
}

func (l *AgentLoop) runMiddleware(ctx context.Context, phase middleware.Phase, sess *session.Session, t *tool.ToolSpec, input, result []byte) error {
	if l.Chain == nil {
		return nil
	}
	mc := &middleware.Context{
		Session: sess,
		Tool:    t,
		Input:   input,
		Result:  result,
		IO:      l.IO,
	}
	return l.Chain.Run(ctx, phase, mc)
}

func (l *AgentLoop) runErrorMiddleware(ctx context.Context, sess *session.Session, err error) {
	if l.Chain == nil {
		return
	}
	mc := &middleware.Context{
		Session: sess,
		Error:   err,
		IO:      l.IO,
	}
	l.Chain.Run(ctx, middleware.OnError, mc)
}

func (l *AgentLoop) fail(sess *session.Session, usage port.TokenUsage, err error) *SessionResult {
	sess.Status = session.StatusFailed
	return &SessionResult{
		SessionID:  sess.ID,
		Success:    false,
		Steps:      sess.Budget.UsedSteps,
		TokensUsed: usage,
		Error:      err.Error(),
	}
}
