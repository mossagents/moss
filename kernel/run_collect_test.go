package kernel

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

	kio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
)

func TestRunAgent_PopulatesUserContentAndSyncWrapsIO(t *testing.T) {
	var gotText string
	var gotSyncIO bool
	agent := NewCustomAgent(CustomAgentConfig{
		Name: "probe",
		Run: func(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				if ctx.UserContent() != nil {
					gotText = model.ContentPartsToPlainText(ctx.UserContent().ContentParts)
				}
				_, gotSyncIO = ctx.IO().(*kio.SyncIO)
			}
		},
	})
	k := New()
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "probe"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	msg := &model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("hello")},
	}
	for _, err := range k.RunAgent(context.Background(), RunAgentRequest{
		Session:     sess,
		Agent:       agent,
		UserContent: msg,
		IO:          kt.NewRecorderIO(),
	}) {
		if err != nil {
			t.Fatalf("RunAgent: %v", err)
		}
	}
	if gotText != "hello" {
		t.Fatalf("expected user content hello, got %q", gotText)
	}
	if !gotSyncIO {
		t.Fatal("expected RunAgent to normalize IO to *io.SyncIO")
	}
}

func TestRunAgent_RejectsToolOverrideForCustomAgent(t *testing.T) {
	agent := NewCustomAgent(CustomAgentConfig{
		Name: "custom",
		Run: func(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	k := New()
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "tool override"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	var runErr error
	for _, err := range k.RunAgent(context.Background(), RunAgentRequest{
		Session: sess,
		Agent:   agent,
		Tools:   tool.NewRegistry(),
	}) {
		runErr = err
	}
	if runErr == nil {
		t.Fatal("expected RunAgent to reject unsupported tool override")
	}
	if !strings.Contains(runErr.Error(), "request-scoped tool override") {
		t.Fatalf("unexpected error: %v", runErr)
	}
}

func TestCollectRunAgentResult_RequiresAuthoritativeResult(t *testing.T) {
	agent := NewCustomAgent(CustomAgentConfig{
		Name: "custom",
		Run: func(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(&session.Event{Type: session.EventTypeCustom}, nil)
			}
		},
	})
	k := New()
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "collect"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	result, err := CollectRunAgentResult(context.Background(), k, RunAgentRequest{
		Session: sess,
		Agent:   agent,
	})
	if err == nil {
		t.Fatal("expected collector to fail without authoritative result")
	}
	if result != nil {
		t.Fatalf("expected nil result on unsupported collection, got %+v", result)
	}
	if !strings.Contains(err.Error(), "did not produce a lifecycle result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCollectRunAgentResult_UsesLLMAgentLifecycleResult(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{{
			Message: model.Message{
				Role:         model.RoleAssistant,
				ContentParts: []model.ContentPart{model.TextPart("done")},
			},
			StopReason: "end_turn",
			Usage:      model.TokenUsage{PromptTokens: 2, CompletionTokens: 5, TotalTokens: 7},
		}},
	}
	k := New(WithLLM(mock), WithUserIO(kt.NewRecorderIO()))
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "collect"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	msg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("say done")},
	}
	sess.AppendMessage(msg)

	result, err := CollectRunAgentResult(context.Background(), k, RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &msg,
	})
	if err != nil {
		t.Fatalf("CollectRunAgentResult: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Output != "done" {
		t.Fatalf("expected output done, got %q", result.Output)
	}
	if result.Steps != 1 {
		t.Fatalf("expected steps 1, got %d", result.Steps)
	}
	if result.TokensUsed.TotalTokens != 7 {
		t.Fatalf("expected 7 tokens, got %+v", result.TokensUsed)
	}
}

func TestCollectRunAgentResult_RebindsScopedToolsForLLMAgent(t *testing.T) {
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("use scoped tool")},
					ToolCalls: []model.ToolCall{{
						ID:        "call-1",
						Name:      "scoped_tool",
						Arguments: json.RawMessage(`{}`),
					}},
				},
				ToolCalls: []model.ToolCall{{
					ID:        "call-1",
					Name:      "scoped_tool",
					Arguments: json.RawMessage(`{}`),
				}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{TotalTokens: 3},
			},
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("scoped ok")},
				},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 2},
			},
		},
	}
	k := New(WithLLM(mock), WithUserIO(kt.NewRecorderIO()))
	scoped := tool.NewRegistry()
	if err := scoped.Register(tool.NewRawTool(tool.ToolSpec{
		Name:        "scoped_tool",
		Description: "scoped",
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"ok"`), nil
	})); err != nil {
		t.Fatalf("register scoped tool: %v", err)
	}
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "scoped"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	msg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("use scoped tool")},
	}
	sess.AppendMessage(msg)

	result, err := CollectRunAgentResult(context.Background(), k, RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &msg,
		Tools:       scoped,
	})
	if err != nil {
		t.Fatalf("CollectRunAgentResult: %v", err)
	}
	if result.Output != "scoped ok" {
		t.Fatalf("expected scoped ok output, got %+v", result)
	}
}
