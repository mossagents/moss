package testing

import (
	"context"
	"github.com/mossagents/moss/kernel/model"
	"io"
)

// MockLLM 是可编程的 LLM 测试桩。
type MockLLM struct {
	Responses []model.CompletionResponse
	Calls     []model.CompletionRequest
	index     int
}

// Complete 按预设顺序返回响应。
func (m *MockLLM) Complete(_ context.Context, req model.CompletionRequest) (*model.CompletionResponse, error) {
	m.Calls = append(m.Calls, req)
	if m.index >= len(m.Responses) {
		return &model.CompletionResponse{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
			StopReason: "end_turn",
		}, nil
	}
	resp := m.Responses[m.index]
	m.index++
	return &resp, nil
}

// MockStreamingLLM 是可编程的 StreamingLLM 测试桩。
type MockStreamingLLM struct {
	MockLLM
	Chunks [][]model.StreamChunk
	sIndex int
}

// Stream 返回预设的 chunk 序列。
func (m *MockStreamingLLM) Stream(_ context.Context, req model.CompletionRequest) (model.StreamIterator, error) {
	m.Calls = append(m.Calls, req)
	if m.sIndex >= len(m.Chunks) {
		return &mockIterator{chunks: []model.StreamChunk{{Done: true}}}, nil
	}
	chunks := m.Chunks[m.sIndex]
	m.sIndex++
	return &mockIterator{chunks: chunks}, nil
}

type mockIterator struct {
	chunks []model.StreamChunk
	index  int
}

func (it *mockIterator) Next() (model.StreamChunk, error) {
	if it.index >= len(it.chunks) {
		return model.StreamChunk{}, io.EOF
	}
	c := it.chunks[it.index]
	it.index++
	return c, nil
}

func (it *mockIterator) Close() error { return nil }
