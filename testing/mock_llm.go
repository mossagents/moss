package testing

import (
	"context"
	mdl "github.com/mossagents/moss/kernel/model"
	"io"
)

// MockLLM 是可编程的 LLM 测试桩。
type MockLLM struct {
	Responses []mdl.CompletionResponse
	Calls     []mdl.CompletionRequest
	index     int
}

// Complete 按预设顺序返回响应。
func (m *MockLLM) Complete(_ context.Context, req mdl.CompletionRequest) (*mdl.CompletionResponse, error) {
	m.Calls = append(m.Calls, req)
	if m.index >= len(m.Responses) {
		return &mdl.CompletionResponse{
			Message:    mdl.Message{Role: mdl.RoleAssistant, ContentParts: []mdl.ContentPart{mdl.TextPart("done")}},
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
	Chunks [][]mdl.StreamChunk
	sIndex int
}

// Stream 返回预设的 chunk 序列。
func (m *MockStreamingLLM) Stream(_ context.Context, req mdl.CompletionRequest) (mdl.StreamIterator, error) {
	m.Calls = append(m.Calls, req)
	if m.sIndex >= len(m.Chunks) {
		return &mockIterator{chunks: []mdl.StreamChunk{{Done: true}}}, nil
	}
	chunks := m.Chunks[m.sIndex]
	m.sIndex++
	return &mockIterator{chunks: chunks}, nil
}

type mockIterator struct {
	chunks []mdl.StreamChunk
	index  int
}

func (it *mockIterator) Next() (mdl.StreamChunk, error) {
	if it.index >= len(it.chunks) {
		return mdl.StreamChunk{}, io.EOF
	}
	c := it.chunks[it.index]
	it.index++
	return c, nil
}

func (it *mockIterator) Close() error { return nil }
