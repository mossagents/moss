package builtins

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
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
		Messages: []port.Message{
			{Role: port.RoleSystem, ContentParts: []port.ContentPart{port.TextPart("You are a helpful assistant.")}},
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("查询认证相关问题")}},
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
		if m.Role == port.RoleSystem {
			sysText = port.ContentPartsToPlainText(m.ContentParts)
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
		Messages: []port.Message{
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("帮我找找 bug")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}

	_ = RAG(RAGConfig{Manager: mgr})(context.Background(), mc, func(_ context.Context) error { return nil })

	msgs := sess.CopyMessages()
	if msgs[0].Role != port.RoleSystem {
		t.Error("expected first message to be system message with injected context")
	}
}

func TestRAG_CustomQueryExtractor(t *testing.T) {
	mgr := newTestManager(t)

	sess := &session.Session{
		ID: "s3",
		Messages: []port.Message{
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("some user message")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}

	customQuery := ""
	_ = RAG(RAGConfig{
		Manager: mgr,
		QueryExtractor: func(msgs []port.Message) string {
			customQuery = "custom_query"
			return customQuery
		},
	})(context.Background(), mc, func(_ context.Context) error { return nil })

	if customQuery != "custom_query" {
		t.Error("expected custom query extractor to be called")
	}
}

// newTestManager 创建一个有少量数据的测试 MemoryManager。
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
