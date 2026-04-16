package harness

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/harness/scheduler"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	return []float64{float64(len(text)), 1}, nil
}

func (stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		out[i] = []float64{float64(len(text)), float64(i + 1)}
	}
	return out, nil
}

func (stubEmbedder) Dimension() int { return 2 }

var _ model.Embedder = stubEmbedder{}

func TestRuntimeCapabilityFeatures_RegisterExpectedTools(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		features func(*testing.T) []Feature
		tools    []string
	}{
		{
			name: "planning",
			features: func(*testing.T) []Feature {
				return []Feature{Planning()}
			},
			tools: []string{"write_todos"},
		},
		{
			name: "context-offload",
			features: func(t *testing.T) []Feature {
				store, err := session.NewFileStore(t.TempDir())
				if err != nil {
					t.Fatalf("NewFileStore: %v", err)
				}
				return []Feature{ContextOffload(store)}
			},
			tools: []string{"offload_context"},
		},
		{
			name: "context-management",
			features: func(t *testing.T) []Feature {
				store, err := session.NewFileStore(t.TempDir())
				if err != nil {
					t.Fatalf("NewFileStore: %v", err)
				}
				return []Feature{ContextManagement(store)}
			},
			tools: []string{"compact_conversation"},
		},
		{
			name: "scheduling",
			features: func(*testing.T) []Feature {
				return []Feature{Scheduling(scheduler.New())}
			},
			tools: []string{"schedule_task", "list_schedules", "cancel_schedule"},
		},
		{
			name: "knowledge",
			features: func(t *testing.T) []Feature {
				memDir := filepath.Join(t.TempDir(), "memories")
				return []Feature{
					PersistentMemories(memDir),
					Knowledge(stubEmbedder{}),
				}
			},
			tools: []string{"ingest_document", "knowledge_search", "knowledge_list"},
		},
		{
			name: "persistent-memories",
			features: func(t *testing.T) []Feature {
				return []Feature{PersistentMemories(filepath.Join(t.TempDir(), "memories"))}
			},
			tools: []string{"read_memory", "write_memory", "list_memories", "delete_memory"},
		},
		{
			name: "persistent-memories-sqlite",
			features: func(t *testing.T) []Feature {
				root := t.TempDir()
				return []Feature{PersistentMemoriesSQLite(filepath.Join(root, "memories"), filepath.Join(root, "sqlite", "memory.db"))}
			},
			tools: []string{"read_memory", "write_memory", "list_memories", "delete_memory"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHarness()
			h.Kernel().Apply(
				kernel.WithLLM(&kt.MockLLM{}),
				kernel.WithUserIO(&io.NoOpIO{}),
			)
			for _, feature := range tt.features(t) {
				if err := h.Install(ctx, feature); err != nil {
					_ = h.Kernel().Shutdown(ctx)
					t.Fatalf("Install: %v", err)
				}
			}
			if err := h.Kernel().Boot(ctx); err != nil {
				_ = h.Kernel().Shutdown(ctx)
				t.Fatalf("Boot: %v", err)
			}
			t.Cleanup(func() {
				_ = h.Kernel().Shutdown(ctx)
			})

			for _, name := range tt.tools {
				if _, ok := h.Kernel().ToolRegistry().Get(name); !ok {
					t.Fatalf("expected tool %q to be registered", name)
				}
			}
		})
	}
}
