package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
)

const planningStateKey kernel.ServiceKey = "planning.state"
const planningTodosStateKey = "planning.todos"

type planningState struct {
	manager session.Manager
}

// PlanningTodoItem 是 write_todos 的单条任务项。
type PlanningTodoItem struct {
	ID          string `json:"id,omitempty"`
	Title       string `json:"title"`
	Status      string `json:"status,omitempty"`
	Description string `json:"description,omitempty"`
}

func WithPlanningSessionManager(m session.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensurePlanningState(k).manager = m
	}
}

func RegisterPlanningTools(reg tool.Registry, manager session.Manager) error {
	if manager == nil {
		return fmt.Errorf("session manager is nil")
	}
	if _, ok := reg.Get("write_todos"); ok {
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
		meta, ok := toolctx.ToolCallContextFromContext(ctx)
		if !ok || strings.TrimSpace(meta.SessionID) == "" {
			return nil, fmt.Errorf("write_todos requires session context")
		}
		sess, exists := manager.Get(meta.SessionID)
		if !exists || sess == nil {
			return nil, fmt.Errorf("session %q not found", meta.SessionID)
		}
		var in struct {
			Todos   []PlanningTodoItem `json:"todos"`
			Replace *bool              `json:"replace"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if len(in.Todos) == 0 {
			return nil, fmt.Errorf("todos is required")
		}

		next := normalizePlanningTodos(in.Todos)
		replace := true
		if in.Replace != nil {
			replace = *in.Replace
		}
		if !replace {
			next = mergePlanningTodos(readPlanningTodos(sess), next)
		}
		sess.SetState(planningTodosStateKey, next)
		return json.Marshal(map[string]any{
			"status":     "ok",
			"session_id": sess.ID,
			"count":      len(next),
			"todos":      next,
		})
	}
	return reg.Register(tool.NewRawTool(spec, handler))
}

func WithPlanningDefaults() kernel.Option {
	return WithPlanningSessionManager(nil)
}

// ensurePlanningState owns the planning substrate slot on the kernel service registry.
func ensurePlanningState(k *kernel.Kernel) *planningState {
	actual, loaded := k.Services().LoadOrStore(planningStateKey, &planningState{})
	st := actual.(*planningState)
	if loaded {
		return st
	}
	if err := k.Stages().OnBoot(125, func(_ context.Context, k *kernel.Kernel) error {
		if st.manager == nil {
			st.manager = k.SessionManager()
		}
		if st.manager == nil {
			return nil
		}
		return RegisterPlanningTools(k.ToolRegistry(), st.manager)
	}); err != nil {
		log.Printf("planning: register boot hook: %v", err)
	}
	if err := k.Prompts().Add(225, func(_ *kernel.Kernel) string {
		if st.manager == nil {
			return ""
		}
		return "Use write_todos to keep an explicit task list with statuses: pending, in_progress, completed."
	}); err != nil {
		log.Printf("planning: register prompt hook: %v", err)
	}
	return st
}

func normalizePlanningTodos(in []PlanningTodoItem) []PlanningTodoItem {
	out := make([]PlanningTodoItem, 0, len(in))
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
		out = append(out, PlanningTodoItem{
			ID:          strings.TrimSpace(item.ID),
			Title:       title,
			Status:      status,
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func readPlanningTodos(sess *session.Session) []PlanningTodoItem {
	raw, ok := sess.GetState(planningTodosStateKey)
	if !ok || raw == nil {
		return nil
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []PlanningTodoItem
	if err := json.Unmarshal(blob, &out); err != nil {
		return nil
	}
	return normalizePlanningTodos(out)
}

func mergePlanningTodos(existing, updates []PlanningTodoItem) []PlanningTodoItem {
	byID := make(map[string]PlanningTodoItem, len(existing))
	ordered := make([]PlanningTodoItem, 0, len(existing)+len(updates))
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
