package testing

import (
	"context"
	"io"

	"github.com/mossagents/moss/kernel/port"
)

// MockLLM 是可编程的 LLM 测试桩。
type MockLLM struct {
	Responses []port.CompletionResponse
	Calls     []port.CompletionRequest
	index     int
}

// Complete 按预设顺序返回响应。
func (m *MockLLM) Complete(_ context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
	m.Calls = append(m.Calls, req)
	if m.index >= len(m.Responses) {
		return &port.CompletionResponse{
			Message:    port.Message{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("done")}},
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
	Chunks [][]port.StreamChunk
	sIndex int
}

// Stream 返回预设的 chunk 序列。
func (m *MockStreamingLLM) Stream(_ context.Context, req port.CompletionRequest) (port.StreamIterator, error) {
	m.Calls = append(m.Calls, req)
	if m.sIndex >= len(m.Chunks) {
		return &mockIterator{chunks: []port.StreamChunk{{Done: true}}}, nil
	}
	chunks := m.Chunks[m.sIndex]
	m.sIndex++
	return &mockIterator{chunks: chunks}, nil
}

type mockIterator struct {
	chunks []port.StreamChunk
	index  int
}

func (it *mockIterator) Next() (port.StreamChunk, error) {
	if it.index >= len(it.chunks) {
		return port.StreamChunk{}, io.EOF
	}
	c := it.chunks[it.index]
	it.index++
	return c, nil
}

func (it *mockIterator) Close() error { return nil }
