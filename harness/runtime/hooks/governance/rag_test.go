package governance_test

import (
	"context"
	"testing"

	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/kernel/hooks"
	kernelmemory "github.com/mossagents/moss/kernel/memory"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

type recordingInjector struct {
	calls    []kernelmemory.ContextInjectConfig
	injected string
	err      error
}

func (r *recordingInjector) InjectContext(_ context.Context, cfg kernelmemory.ContextInjectConfig) (string, error) {
	r.calls = append(r.calls, cfg)
	return r.injected, r.err
}

func TestRAGInjectsOnlyIntoRequestLocalPrompt(t *testing.T) {
	injector := &recordingInjector{injected: "<memory_context>\nremember preferred weather api\n</memory_context>"}
	hook := governance.RAG(governance.RAGConfig{Manager: injector, MaxChars: 256})
	sess := &session.Session{
		ID: "rag-session",
		Messages: []model.Message{
			{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("base system")}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("杭州天气怎么样")}},
		},
	}
	req := &model.CompletionRequest{Messages: []model.Message{
		{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("base system")}},
		{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("杭州天气怎么样")}},
	}}
	ev := &hooks.LLMEvent{Session: sess, Request: req, PromptMessages: req.Messages}

	if err := hook(context.Background(), ev); err != nil {
		t.Fatalf("RAG hook: %v", err)
	}
	if len(injector.calls) != 1 {
		t.Fatalf("injector calls = %d, want 1", len(injector.calls))
	}
	if got := injector.calls[0].SessionID; got != "rag-session" {
		t.Fatalf("session id = %q, want rag-session", got)
	}
	if got := injector.calls[0].Query; got != "杭州天气怎么样" {
		t.Fatalf("query = %q, want 杭州天气怎么样", got)
	}
	if got := model.ContentPartsToPlainText(req.Messages[0].ContentParts); got != "base system\n\n<memory_context>\nremember preferred weather api\n</memory_context>" {
		t.Fatalf("request prompt = %q", got)
	}
	if got := model.ContentPartsToPlainText(ev.PromptMessages[0].ContentParts); got != "base system\n\n<memory_context>\nremember preferred weather api\n</memory_context>" {
		t.Fatalf("event prompt = %q", got)
	}
	if got := model.ContentPartsToPlainText(sess.CopyMessages()[0].ContentParts); got != "base system" {
		t.Fatalf("session prompt should remain unchanged, got %q", got)
	}
}
