package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
)

func BenchmarkToolCallDispatch(b *testing.B) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:        "echo",
		Description: "Echo input back",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		return input, nil
	})); err != nil {
		b.Fatalf("register: %v", err)
	}

	l := &AgentLoop{Tools: reg, IO: kt.NewRecorderIO()}
	sess := &session.Session{
		ID:     "bench-tool",
		Status: session.StatusRunning,
		Budget: session.Budget{MaxSteps: 1000000},
	}
	call := model.ToolCall{
		ID:        "c1",
		Name:      "echo",
		Arguments: json.RawMessage(`{"msg":"hello"}`),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.executeSingleToolCall(context.Background(), sess, call)
	}
}

func BenchmarkHooksPipeline(b *testing.B) {
	chain := hooks.NewRegistry()
	for range 5 {
		chain.BeforeLLM.AddHook("", func(ctx context.Context, ev *hooks.LLMEvent) error {
			return nil
		}, 0)
	}

	reg := tool.NewRegistry()
	if err := reg.Register(tool.NewRawTool(tool.ToolSpec{
		Name:        "noop",
		Description: "No-op tool",
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})); err != nil {
		b.Fatalf("register: %v", err)
	}

	l := &AgentLoop{Tools: reg, Hooks: chain, IO: kt.NewRecorderIO()}
	sess := &session.Session{
		ID:     "bench-mw",
		Status: session.StatusRunning,
		Budget: session.Budget{MaxSteps: 1000000},
	}
	call := model.ToolCall{
		ID:        "c1",
		Name:      "noop",
		Arguments: json.RawMessage(`{}`),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.executeSingleToolCall(context.Background(), sess, call)
	}
}

func BenchmarkJSONRepair(b *testing.B) {
	samples := []struct {
		name string
		data json.RawMessage
	}{
		{"valid", json.RawMessage(`{"key":"value","num":42}`)},
		{"truncated_simple", json.RawMessage(`{"key":"val`)},
		{"truncated_nested", json.RawMessage(`{"a":{"b":[1,2`)},
		{"empty", json.RawMessage(``)},
		{"large_valid", json.RawMessage(`{"a":"` + largeString(500) + `","b":123,"c":true}`)},
	}

	for _, s := range samples {
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				repairToolArguments(s.data)
			}
		})
	}
}

func largeString(n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = 'x'
	}
	return string(buf)
}
