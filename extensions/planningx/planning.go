package planningx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const stateKey kernel.ExtensionStateKey = "planningx.state"
const todosStateKey = "planning.todos"

type state struct {
	manager session.Manager
}

// TodoItem 是 write_todos 的单条任务项。
type TodoItem struct {
	ID          string `json:"id,omitempty"`
	Title       string `json:"title"`
	Status      string `json:"status,omitempty"`
	Description string `json:"description,omitempty"`
}

// WithSessionManager 设置 planning 工具使用的 SessionManager。
func WithSessionManager(m session.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).manager = m
	}
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnBoot(125, func(_ context.Context, k *kernel.Kernel) error {
		if st.manager == nil {
			st.manager = k.SessionManager()
		}
		if st.manager == nil {
			return nil
		}
		return RegisterTools(k.ToolRegistry(), st.manager)
	})
	bridge.OnSystemPrompt(225, func(_ *kernel.Kernel) string {
		if st.manager == nil {
			return ""
		}
		return "Use write_todos to keep an explicit task list with statuses: pending, in_progress, completed."
	})
	return st
}

// RegisterTools 注册 write_todos 工具。
func RegisterTools(reg tool.Registry, manager session.Manager) error {
	if manager == nil {
		return fmt.Errorf("session manager is nil")
	}
	if _, _, ok := reg.Get("write_todos"); ok {
		return nil
	}
	spec := tool.ToolSpec{
		Name:        "write_todos",
		Description: "Write or update a structured todo list for the current session.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"todos":{
					"type":"array",
					"items":{
						"type":"object",
						"properties":{
							"id":{"type":"string"},
							"title":{"type":"string"},
							"status":{"type":"string","description":"pending|in_progress|completed"},
							"description":{"type":"string"}
						},
						"required":["title"]
					}
				},
				"replace":{"type":"boolean","description":"Whether to replace all existing todos (default: true)"}
			},
			"required":["todos"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"planning"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		meta, ok := port.ToolCallContextFromContext(ctx)
		if !ok || strings.TrimSpace(meta.SessionID) == "" {
			return nil, fmt.Errorf("write_todos requires session context")
		}
		sess, exists := manager.Get(meta.SessionID)
		if !exists || sess == nil {
			return nil, fmt.Errorf("session %q not found", meta.SessionID)
		}
		var in struct {
			Todos   []TodoItem `json:"todos"`
			Replace *bool      `json:"replace"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if len(in.Todos) == 0 {
			return nil, fmt.Errorf("todos is required")
		}

		next := normalizeTodos(in.Todos)
		replace := true
		if in.Replace != nil {
			replace = *in.Replace
		}
		if !replace {
			next = mergeTodos(readTodos(sess), next)
		}
		sess.SetState(todosStateKey, next)
		return json.Marshal(map[string]any{
			"status":     "ok",
			"session_id": sess.ID,
			"count":      len(next),
			"todos":      next,
		})
	}
	return reg.Register(spec, handler)
}

func normalizeTodos(in []TodoItem) []TodoItem {
	out := make([]TodoItem, 0, len(in))
	for i, item := range in {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = fmt.Sprintf("todo-%d", i+1)
		}
		status := strings.ToLower(strings.TrimSpace(item.Status))
		switch status {
		case "", "pending":
			status = "pending"
		case "in_progress", "completed":
		default:
			status = "pending"
		}
		out = append(out, TodoItem{
			ID:          strings.TrimSpace(item.ID),
			Title:       title,
			Status:      status,
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func readTodos(sess *session.Session) []TodoItem {
	raw, ok := sess.GetState(todosStateKey)
	if !ok || raw == nil {
		return nil
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []TodoItem
	if err := json.Unmarshal(blob, &out); err != nil {
		return nil
	}
	return normalizeTodos(out)
}

func mergeTodos(existing, updates []TodoItem) []TodoItem {
	byID := make(map[string]TodoItem, len(existing))
	ordered := make([]TodoItem, 0, len(existing)+len(updates))
	for _, it := range existing {
		key := strings.TrimSpace(it.ID)
		if key == "" {
			ordered = append(ordered, it)
			continue
		}
		byID[key] = it
		ordered = append(ordered, it)
	}
	for _, up := range updates {
		key := strings.TrimSpace(up.ID)
		if key == "" {
			ordered = append(ordered, up)
			continue
		}
		_, exists := byID[key]
		byID[key] = up
		if !exists {
			ordered = append(ordered, up)
		}
	}
	for i := range ordered {
		key := strings.TrimSpace(ordered[i].ID)
		if key == "" {
			continue
		}
		if latest, ok := byID[key]; ok {
			ordered[i] = latest
		}
	}
	return ordered
}
