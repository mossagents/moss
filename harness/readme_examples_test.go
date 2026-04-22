package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/kernel/testing"
)

func TestReadmeHarnessExampleRuns(t *testing.T) {
	workspace := t.TempDir()
	ctx := context.Background()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{
			Responses: []model.CompletionResponse{{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("Hello from README")},
				},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{TotalTokens: 3},
			}},
		}),
		kernel.WithUserIO(&io.NoOpIO{}),
	)

	backend, err := harness.OpenLocalBackend(workspace)
	if err != nil {
		t.Fatalf("OpenLocalBackend: %v", err)
	}
	h := harness.New(k, backend)
	if err := h.ActivateBackend(ctx); err != nil {
		t.Fatalf("ActivateBackend: %v", err)
	}
	if err := h.Install(ctx,
		harness.BootstrapContext(workspace, "myapp", "trusted"),
		harness.LLMResilience(&retry.Config{
			MaxRetries:   3,
			InitialDelay: 500 * time.Millisecond,
		}, nil),
		harness.PatchToolCalls(),
	); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() {
		_ = k.Shutdown(ctx)
	})

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     "help me",
		MaxSteps: 50,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("Hello")},
	}
	sess.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &userMsg,
	})
	if err != nil {
		t.Fatalf("CollectRunAgentResult: %v", err)
	}
	if result.Output != "Hello from README" {
		t.Fatalf("Output = %q, want %q", result.Output, "Hello from README")
	}
}
