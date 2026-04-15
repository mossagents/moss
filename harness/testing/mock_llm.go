package testing

import (
	"context"
	"github.com/mossagents/moss/kernel/model"
	"iter"
)

// MockLLM 是可编程的 LLM 测试桩。
// 按预设顺序返回响应，每次调用 GenerateContent 产出单个完整 chunk。
type MockLLM struct {
	Responses []model.CompletionResponse
	Calls     []model.CompletionRequest
	index     int
}

// GenerateContent 按预设顺序返回响应（通过 ResponseToSeq 转换为流式 chunk）。
func (m *MockLLM) GenerateContent(_ context.Context, req model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	m.Calls = append(m.Calls, req)
	if m.index >= len(m.Responses) {
		return model.ResponseToSeq(&model.CompletionResponse{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
			StopReason: "end_turn",
		})
	}
	resp := m.Responses[m.index]
	m.index++
	return model.ResponseToSeq(&resp)
}

// MockStreamingLLM 是可编程的流式 LLM 测试桩，逐 chunk 产出。
type MockStreamingLLM struct {
	MockLLM
	Chunks [][]model.StreamChunk
	sIndex int
}

// GenerateContent 返回预设的 chunk 序列。
func (m *MockStreamingLLM) GenerateContent(_ context.Context, req model.CompletionRequest) iter.Seq2[model.StreamChunk, error] {
	m.Calls = append(m.Calls, req)
	var chunks []model.StreamChunk
	if m.sIndex < len(m.Chunks) {
		chunks = m.Chunks[m.sIndex]
		m.sIndex++
	} else {
		chunks = []model.StreamChunk{{Done: true}}
	}
	return func(yield func(model.StreamChunk, error) bool) {
		for _, chunk := range chunks {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}
