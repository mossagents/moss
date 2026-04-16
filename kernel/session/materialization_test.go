package session

import (
	"testing"

	"github.com/mossagents/moss/kernel/model"
)

func TestMaterializeEvent_TracksDomainPerSession(t *testing.T) {
	parent := &Session{ID: "parent", State: ScopedState{}}
	child := parent.Clone()
	event := &Event{
		Type: EventTypeCustom,
		Content: &model.Message{
			Role:         model.RoleAssistant,
			ContentParts: []model.ContentPart{model.TextPart("done")},
		},
		Actions: EventActions{
			StateDelta: map[string]any{"shared": "yes"},
		},
	}

	MaterializeEvent(child, event)
	if got, want := event.Actions.MaterializedIn, child.MaterializationDomain(); got != want {
		t.Fatalf("after child materialization domain = %q, want %q", got, want)
	}
	if msgs := child.CopyMessages(); len(msgs) != 1 {
		t.Fatalf("child messages len = %d, want 1", len(msgs))
	}

	MaterializeEvent(child, event)
	if msgs := child.CopyMessages(); len(msgs) != 1 {
		t.Fatalf("child messages len after duplicate materialization = %d, want 1", len(msgs))
	}

	MaterializeEvent(parent, event)
	if got, want := event.Actions.MaterializedIn, parent.MaterializationDomain(); got != want {
		t.Fatalf("after parent materialization domain = %q, want %q", got, want)
	}
	if msgs := parent.CopyMessages(); len(msgs) != 1 {
		t.Fatalf("parent messages len = %d, want 1", len(msgs))
	}
	if state, ok := parent.GetState("shared"); !ok || state != "yes" {
		t.Fatalf("parent shared state = %v, %v; want yes, true", state, ok)
	}
}
