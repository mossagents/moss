package compactx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const stateKey kernel.ExtensionStateKey = "compactx.state"

type state struct {
	store session.SessionStore
}

// WithSessionStore 注入 offload 使用的 SessionStore。
func WithSessionStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).store = store
	}
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnSystemPrompt(230, func(_ *kernel.Kernel) string {
		if st.store == nil {
			return ""
		}
		return "Use offload_context to compact long conversations and persist an offload snapshot."
	})
	return st
}

// RegisterTools 注册上下文压缩工具。
func RegisterTools(reg tool.Registry, store session.SessionStore, manager session.Manager) error {
	if _, _, exists := reg.Get("offload_context"); exists {
		return nil
	}
	if store == nil {
		return fmt.Errorf("session store is required for offload_context")
	}
	if manager == nil {
		return fmt.Errorf("session manager is required for offload_context")
	}

	spec := tool.ToolSpec{
		Name:        "offload_context",
		Description: "Persist older dialog context to session store and compact in-memory conversation.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"session_id":{"type":"string","description":"Session ID to compact"},
				"keep_recent":{"type":"integer","description":"How many recent dialog messages to keep in memory (default: 20)"},
				"note":{"type":"string","description":"Optional note for the offload snapshot"}
			},
			"required":["session_id"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"context"},
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			SessionID  string `json:"session_id"`
			KeepRecent int    `json:"keep_recent"`
			Note       string `json:"note"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		in.SessionID = strings.TrimSpace(in.SessionID)
		if in.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}
		if in.KeepRecent <= 0 {
			in.KeepRecent = 20
		}

		sess, ok := manager.Get(in.SessionID)
		if !ok || sess == nil {
			return nil, fmt.Errorf("session %q not found", in.SessionID)
		}
		original := append([]port.Message(nil), sess.Messages...)
		dialogCount := countDialogMessages(sess.Messages)
		if dialogCount <= in.KeepRecent {
			return json.Marshal(map[string]any{
				"status":       "noop",
				"session_id":   sess.ID,
				"dialog_count": dialogCount,
				"keep_recent":  in.KeepRecent,
			})
		}

		offloadID := fmt.Sprintf("%s_offload_%d", sess.ID, time.Now().UnixNano())
		snapshot := &session.Session{
			ID:       offloadID,
			Status:   session.StatusCompleted,
			Config:   sess.Config,
			Messages: append([]port.Message(nil), original...),
			State: map[string]any{
				"offload_of": sess.ID,
				"note":       in.Note,
			},
			Budget:    sess.Budget,
			CreatedAt: time.Now(),
			EndedAt:   time.Now(),
		}
		if err := store.Save(ctx, snapshot); err != nil {
			return nil, fmt.Errorf("save offload snapshot: %w", err)
		}

		notice := fmt.Sprintf("[Context offloaded to snapshot %s; kept recent %d dialog messages]", offloadID, in.KeepRecent)
		sess.Messages = session.BuildCompactedMessages(sess.Messages, in.KeepRecent, notice)
		sess.SetState("last_offload_snapshot", offloadID)
		sess.SetState("last_offload_at", time.Now().Format(time.RFC3339))

		if err := store.Save(ctx, sess); err != nil {
			return nil, fmt.Errorf("save compacted session: %w", err)
		}

		return json.Marshal(map[string]any{
			"status":            "offloaded",
			"session_id":        sess.ID,
			"snapshot_session":  offloadID,
			"dialog_before":     dialogCount,
			"kept_recent":       in.KeepRecent,
			"message_count_now": len(sess.Messages),
		})
	}

	return reg.Register(spec, handler)
}

func countDialogMessages(msgs []port.Message) int {
	count := 0
	for _, m := range msgs {
		if m.Role != port.RoleSystem {
			count++
		}
	}
	return count
}
