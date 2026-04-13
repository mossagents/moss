package harness

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/knowledge"
	"github.com/mossagents/moss/scheduler"
	kt "github.com/mossagents/moss/testing"
)

func TestRuntimeCapabilityFeatures_RegisterExpectedTools(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		feature func(*testing.T) Feature
		tools   []string
	}{
		{
			name: "planning",
			feature: func(*testing.T) Feature {
				return Planning()
			},
			tools: []string{"write_todos"},
		},
		{
			name: "context-offload",
			feature: func(t *testing.T) Feature {
				store, err := session.NewFileStore(t.TempDir())
				if err != nil {
					t.Fatalf("NewFileStore: %v", err)
				}
				return ContextOffload(store)
			},
			tools: []string{"offload_context"},
		},
		{
			name: "context-management",
			feature: func(t *testing.T) Feature {
				store, err := session.NewFileStore(t.TempDir())
				if err != nil {
					t.Fatalf("NewFileStore: %v", err)
				}
				return ContextManagement(store)
			},
			tools: []string{"compact_conversation"},
		},
		{
			name: "scheduling",
			feature: func(*testing.T) Feature {
				return Scheduling(scheduler.New())
			},
			tools: []string{"schedule_task", "list_schedules", "cancel_schedule"},
		},
		{
			name: "knowledge",
			feature: func(*testing.T) Feature {
				return Knowledge(knowledge.NewMemoryStore(), nil)
			},
			tools: []string{"ingest_document", "knowledge_search", "knowledge_list"},
		},
		{
			name: "persistent-memories",
			feature: func(t *testing.T) Feature {
				return PersistentMemories(filepath.Join(t.TempDir(), "memories"))
			},
			tools: []string{"read_memory", "write_memory", "list_memories", "delete_memory"},
		},
		{
			name: "persistent-memories-sqlite",
			feature: func(t *testing.T) Feature {
				root := t.TempDir()
				return PersistentMemoriesSQLite(filepath.Join(root, "memories"), filepath.Join(root, "sqlite", "memory.db"))
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
			if err := h.Install(ctx, tt.feature(t)); err != nil {
				_ = h.Kernel().Shutdown(ctx)
				t.Fatalf("Install: %v", err)
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
