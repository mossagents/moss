package builtins

import (
	"context"
	"testing"

	"github.com/mossagents/moss/kernel/middleware"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func TestAutoSummarize_DisabledSkips(t *testing.T) {
	disabled := false
	sess := &session.Session{
		ID: "sum-disabled",
		Messages: []mdl.Message{
			{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("hello")}},
		},
	}
	mc := &middleware.Context{Phase: middleware.BeforeLLM, Session: sess}
	mw := AutoSummarize(SummarizeConfig{Enabled: &disabled})

	called := false
	err := mw(context.Background(), mc, func(_ context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected next to be called")
	}
	if len(sess.CopyMessages()) != 1 {
		t.Fatal("disabled summarize should not mutate messages")
	}
}

