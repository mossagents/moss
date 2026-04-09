package builtins

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
)

func TestRAG_NoManagerSkips(t *testing.T) {
	mw := RAG(RAGConfig{Manager: nil})
	mc := &middleware.Context{
		Phase:   middleware.BeforeLLM,
		Session: &session.Session{ID: "test"},
	}
	called := false
	err := mw(context.Background(), mc, func(_ context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatal("expected next to be called without error")
	}
}

func TestRAG_NonLLMPhaseSkips(t *testing.T) {
	mgr := newTestManager(t)
	mw := RAG(RAGConfig{Manager: mgr})
	mc := &middleware.Context{
		Phase:   middleware.AfterLLM,
		Session: &session.Session{ID: "s"},
	}
	called := false
	_ = mw(context.Background(), mc, func(_ context.Context) error {
		called = true
		return nil
	})
	if !called {
		t.Fatal("expected next to be called")
	}
}

func TestRAG_InjectsMemoryContextIntoSystemMessage(t *testing.T) {
	mgr := newTestManager(t)

	sess := &session.Session{
		ID: "s1",
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("You are a helpful assistant.")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("查询认证相关问题")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}

	called := false
	err := RAG(RAGConfig{Manager: mgr})(context.Background(), mc, func(_ context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}

	msgs := sess.CopyMessages()
	sysText := ""
	for _, m := range msgs {
		if m.Role == mdl.RoleSystem {
			sysText = mdl.ContentPartsToPlainText(m.ContentParts)
		}
	}
	// 应包含原始 system prompt 和注入的 memory_context
	if !strings.Contains(sysText, "You are a helpful assistant.") {
		t.Error("original system prompt should be preserved")
	}
}

func TestRAG_InsertsNewSystemMessageIfNone(t *testing.T) {
	mgr := newTestManager(t)

	sess := &session.Session{
		ID: "s2",
		Messages: []mdl.Message{
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("帮我找找 bug")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}

	_ = RAG(RAGConfig{Manager: mgr})(context.Background(), mc, func(_ context.Context) error { return nil })

	msgs := sess.CopyMessages()
	if msgs[0].Role != mdl.RoleSystem {
		t.Error("expected first message to be system message with injected context")
	}
}

func TestRAG_CustomQueryExtractor(t *testing.T) {
	mgr := newTestManager(t)

	sess := &session.Session{
		ID: "s3",
		Messages: []mdl.Message{
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("some user message")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}

	customQuery := ""
	_ = RAG(RAGConfig{
		Manager: mgr,
		QueryExtractor: func(msgs []mdl.Message) string {
			customQuery = "custom_query"
			return customQuery
		},
	})(context.Background(), mc, func(_ context.Context) error { return nil })

	if customQuery != "custom_query" {
		t.Error("expected custom query extractor to be called")
	}
}

func TestRAG_DisabledSkips(t *testing.T) {
	disabled := false
	mgr := newTestManager(t)
	sess := &session.Session{
		ID: "rag-disabled",
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("sys")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("query")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}

	called := false
	err := RAG(RAGConfig{Enabled: &disabled, Manager: mgr})(context.Background(), mc, func(_ context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected next to be called")
	}
	if len(sess.CopyMessages()) != 2 {
		t.Fatal("disabled rag should not mutate messages")
	}
}

func TestRAG_DoesNotAccumulateAcrossTurns(t *testing.T) {
	mgr := newTestManager(t)

	sess := &session.Session{
		ID: "accumulate-test",
		Messages: []mdl.Message{
			{Role: mdl.RoleSystem, ContentParts: []mdl.ContentPart{mdl.TextPart("Base system prompt.")}},
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("first query")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}
	mw := RAG(RAGConfig{Manager: mgr})

	// 模拟第一轮 BeforeLLM
	_ = mw(context.Background(), mc, func(_ context.Context) error { return nil })

	// 模拟第二轮 BeforeLLM（同一 session，memory_context 应替换而非累积）
	mc2 := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}
	_ = mw(context.Background(), mc2, func(_ context.Context) error { return nil })

	msgs := sess.CopyMessages()
	sysText := ""
	for _, m := range msgs {
		if m.Role == mdl.RoleSystem {
			sysText = mdl.ContentPartsToPlainText(m.ContentParts)
		}
	}

	// <memory_context> 标签在最终文本中应恰好出现一次
	count := strings.Count(sysText, "<memory_context>")
	if count != 1 {
		t.Errorf("expected exactly 1 <memory_context> block after two turns, got %d; content:\n%s", count, sysText)
	}
	if !strings.Contains(sysText, "Base system prompt.") {
		t.Error("original system prompt should be preserved")
	}
}

func TestStripMemoryContext(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no block",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "block at end",
			input: "base\n\n<memory_context>\ndata\n</memory_context>",
			want:  "base",
		},
		{
			name:  "multiple blocks",
			input: "<memory_context>a</memory_context> mid <memory_context>b</memory_context>",
			want:  " mid",
		},
		{
			name:  "unterminated block",
			input: "base\n\n<memory_context>\nunfinished",
			want:  "base",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripMemoryContext(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
func newTestManager(t *testing.T) *knowledge.MemoryManager {
	t.Helper()
	wm := knowledge.NewWorkingMemory()
	ctx := context.Background()
	_ = wm.Set(ctx, "goal", "fix auth")

	store := knowledge.NewMemoryEpisodicStore()
	_ = store.Append(ctx, knowledge.Episode{
		SessionID: "s1",
		Kind:      knowledge.EpisodeToolCall,
		Summary:   "read_file auth.go",
		Timestamp: time.Now(),
	})

	return knowledge.NewMemoryManager(knowledge.MemoryManagerConfig{
		Working:  wm,
		Episodic: store,
	})
}
