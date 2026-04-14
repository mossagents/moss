package harness

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	kt "github.com/mossagents/moss/testing"
)

type recordingSessionStore struct {
	mu    sync.Mutex
	kinds []string
}

func (s *recordingSessionStore) Save(_ context.Context, sess *session.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind := ""
	if raw, ok := sess.GetMetadata(session.MetadataThreadLastActivityKind); ok {
		kind, _ = raw.(string)
	}
	s.kinds = append(s.kinds, kind)
	return nil
}

func (s *recordingSessionStore) Load(context.Context, string) (*session.Session, error) {
	return nil, nil
}

func (s *recordingSessionStore) List(context.Context) ([]session.SessionSummary, error) {
	return nil, nil
}

func (s *recordingSessionStore) Delete(context.Context, string) error {
	return nil
}

func (s *recordingSessionStore) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.kinds...)
}

func TestFeature_SessionPersistence_PersistsLifecycle(t *testing.T) {
	h := newTestHarness()
	h.Kernel().Apply(
		kernel.WithLLM(&kt.MockLLM{
			Responses: []model.CompletionResponse{
				{
					Message: model.Message{
						Role:         model.RoleAssistant,
						ContentParts: []model.ContentPart{model.TextPart("")},
						ToolCalls: []model.ToolCall{{
							ID:        "c1",
							Name:      "greet",
							Arguments: json.RawMessage(`{"name":"world"}`),
						}},
					},
					ToolCalls:  []model.ToolCall{{ID: "c1", Name: "greet", Arguments: json.RawMessage(`{"name":"world"}`)}},
					StopReason: "tool_use",
					Usage:      model.TokenUsage{TotalTokens: 5},
				},
				{
					Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
					StopReason: "end_turn",
					Usage:      model.TokenUsage{TotalTokens: 3},
				},
			},
		}),
		kernel.WithUserIO(&io.NoOpIO{}),
	)
	store := &recordingSessionStore{}
	if err := h.Kernel().ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{Name: "greet", Description: "Greet someone"}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"hello world"`), nil
	})); err != nil {
		t.Fatalf("register greet: %v", err)
	}

	if err := h.Install(context.Background(), SessionPersistence(store)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := h.Kernel().Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	sess, err := h.Kernel().NewSession(context.Background(), session.SessionConfig{
		Goal:     "persist harness session lifecycle",
		MaxSteps: 4,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("persist lifecycle")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(context.Background(), h.Kernel(), kernel.RunAgentRequest{
		Session:     sess,
		Agent:       h.Kernel().BuildLLMAgent("harness"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	want := []string{"created", "started", "tool:greet", "completed"}
	if got := store.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved kinds = %v, want %v", got, want)
	}
}
