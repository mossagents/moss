package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const planningServiceKey kernel.ServiceKey = "planning.service"
const planningSessionStateKey = "planning.state"

type planningState struct {
	manager session.Manager
}

// Item is the unified planning item model. The same item powers both the
// plan view and the execution/todo view.
type Item struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Group       string   `json:"group,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Acceptance  string   `json:"acceptance,omitempty"`
	Order       int      `json:"order"`
}

// State is the unified planning source of truth for a session.
type State struct {
	Goal         string    `json:"goal,omitempty"`
	Explanation  string    `json:"explanation,omitempty"`
	CurrentFocus string    `json:"current_focus,omitempty"`
	Items        []Item    `json:"items"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UpdateInput struct {
	Goal         string `json:"goal,omitempty"`
	Explanation  string `json:"explanation,omitempty"`
	CurrentFocus string `json:"current_focus,omitempty"`
	Items        []Item `json:"items"`
}

func WithPlanningSessionManager(m session.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensurePlanningState(k).manager = m
	}
}

func WithPlanningDefaults() kernel.Option {
	return WithPlanningSessionManager(nil)
}

func RegisterPlanningTools(reg tool.Registry, manager session.Manager) error {
	if manager == nil {
		return fmt.Errorf("session manager is nil")
	}
	if _, ok := reg.Get("update_plan"); ok {
		return nil
	}
	spec := tool.ToolSpec{
		Name:        "update_plan",
		Description: "Replace the current session planning state with a unified plan/todo graph.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"goal":{"type":"string"},
				"explanation":{"type":"string"},
				"current_focus":{"type":"string"},
				"items":{
					"type":"array",
					"items":{
						"type":"object",
						"properties":{
							"id":{"type":"string"},
							"title":{"type":"string"},
							"description":{"type":"string"},
							"status":{"type":"string","description":"pending|in_progress|completed|blocked"},
							"depends_on":{"type":"array","items":{"type":"string"}},
							"group":{"type":"string"},
							"notes":{"type":"string"},
							"acceptance":{"type":"string"}
						},
						"required":["title"]
					}
				}
			},
			"required":["items"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"planning"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		meta, ok := tool.ToolCallContextFromContext(ctx)
		if !ok || strings.TrimSpace(meta.SessionID) == "" {
			return nil, fmt.Errorf("update_plan requires session context")
		}
		sess, exists := manager.Get(meta.SessionID)
		if !exists || sess == nil {
			return nil, fmt.Errorf("session %q not found", meta.SessionID)
		}
		var in UpdateInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		next, err := NormalizeState(in)
		if err != nil {
			return nil, err
		}
		sess.SetState(planningSessionStateKey, next)
		return json.Marshal(map[string]any{
			"status":        "ok",
			"session_id":    sess.ID,
			"current_focus": next.CurrentFocus,
			"item_count":    len(next.Items),
			"pending_count": countItemsByStatus(next.Items, "pending"),
			"blocked_count": countItemsByStatus(next.Items, "blocked"),
			"plan":          next,
			"plan_markdown": RenderPlanMarkdown(next),
			"todo_markdown": RenderTodoMarkdown(next),
		})
	}
	return reg.Register(tool.NewRawTool(spec, handler))
}

func ReadSessionPlan(sess *session.Session) (State, bool) {
	if sess == nil {
		return State{}, false
	}
	raw, ok := sess.GetState(planningSessionStateKey)
	if !ok || raw == nil {
		return State{}, false
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return State{}, false
	}
	var out State
	if err := json.Unmarshal(blob, &out); err != nil {
		return State{}, false
	}
	return out, true
}

func RenderPlanMarkdown(state State) string {
	var b strings.Builder
	title := strings.TrimSpace(state.Goal)
	if title == "" {
		title = "Plan"
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n")
	if explanation := strings.TrimSpace(state.Explanation); explanation != "" {
		b.WriteString("\n")
		b.WriteString(explanation)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for _, item := range state.Items {
		b.WriteString(fmt.Sprintf("%d. [%s] %s", item.Order, item.Status, item.Title))
		if item.Description != "" {
			b.WriteString(" — ")
			b.WriteString(item.Description)
		}
		if len(item.DependsOn) > 0 {
			b.WriteString(" (depends on: ")
			b.WriteString(strings.Join(item.DependsOn, ", "))
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func RenderTodoMarkdown(state State) string {
	var b strings.Builder
	for _, item := range state.Items {
		marker := " "
		switch item.Status {
		case "completed":
			marker = "x"
		case "in_progress":
			marker = ">"
		case "blocked":
			marker = "!"
		}
		b.WriteString(fmt.Sprintf("- [%s] %s", marker, item.Title))
		if item.Notes != "" {
			b.WriteString(" — ")
			b.WriteString(item.Notes)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// NormalizeState validates and canonicalizes the unified plan/todo state.
func NormalizeState(in UpdateInput) (State, error) {
	if len(in.Items) == 0 {
		return State{}, fmt.Errorf("items is required")
	}
	items := make([]Item, 0, len(in.Items))
	seenIDs := make(map[string]int, len(in.Items))
	inProgressCount := 0
	for i, raw := range in.Items {
		item := normalizeItem(raw, i+1)
		item.ID = uniqueItemID(item.ID, item.Title, seenIDs)
		if item.Status == "in_progress" {
			inProgressCount++
		}
		items = append(items, item)
	}
	if inProgressCount > 1 {
		return State{}, fmt.Errorf("at most one item can be in_progress")
	}
	index := make(map[string]Item, len(items))
	for _, item := range items {
		index[item.ID] = item
	}
	for _, item := range items {
		for _, dep := range item.DependsOn {
			if dep == item.ID {
				return State{}, fmt.Errorf("item %q cannot depend on itself", item.ID)
			}
			if _, ok := index[dep]; !ok {
				return State{}, fmt.Errorf("item %q depends on unknown item %q", item.ID, dep)
			}
		}
	}
	if hasDependencyCycle(items) {
		return State{}, fmt.Errorf("planning items contain dependency cycle")
	}

	focus := normalizeIdentifier(in.CurrentFocus)
	if focus != "" {
		if _, ok := index[focus]; !ok {
			return State{}, fmt.Errorf("current_focus %q not found", focus)
		}
	} else {
		for _, item := range items {
			if item.Status == "in_progress" {
				focus = item.ID
				break
			}
		}
	}
	return State{
		Goal:         strings.TrimSpace(in.Goal),
		Explanation:  strings.TrimSpace(in.Explanation),
		CurrentFocus: focus,
		Items:        items,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

// ensurePlanningState owns the planning substrate slot on the kernel service registry.
func ensurePlanningState(k *kernel.Kernel) *planningState {
	actual, loaded := k.Services().LoadOrStore(planningServiceKey, &planningState{})
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
		return "Use update_plan to maintain the unified planning state. The same structured plan powers both the high-level plan and the execution todo list. Allowed statuses: pending, in_progress, completed, blocked."
	}); err != nil {
		log.Printf("planning: register prompt hook: %v", err)
	}
	return st
}

func normalizeItem(in Item, order int) Item {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = fmt.Sprintf("step-%d", order)
	}
	status := normalizeStatus(in.Status)
	deps := make([]string, 0, len(in.DependsOn))
	for _, dep := range in.DependsOn {
		dep = normalizeIdentifier(dep)
		if dep != "" && !slices.Contains(deps, dep) {
			deps = append(deps, dep)
		}
	}
	return Item{
		ID:          normalizeIdentifier(in.ID),
		Title:       title,
		Description: strings.TrimSpace(in.Description),
		Status:      status,
		DependsOn:   deps,
		Group:       strings.TrimSpace(in.Group),
		Notes:       strings.TrimSpace(in.Notes),
		Acceptance:  strings.TrimSpace(in.Acceptance),
		Order:       order,
	}
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "pending":
		return "pending"
	case "in_progress", "completed", "blocked":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "pending"
	}
}

func uniqueItemID(id string, title string, seen map[string]int) string {
	base := normalizeIdentifier(id)
	if base == "" {
		base = normalizeIdentifier(title)
	}
	if base == "" {
		base = "step"
	}
	count := seen[base]
	seen[base] = count + 1
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, count+1)
}

func normalizeIdentifier(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func hasDependencyCycle(items []Item) bool {
	graph := make(map[string][]string, len(items))
	for _, item := range items {
		graph[item.ID] = append([]string(nil), item.DependsOn...)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if visit(dep) {
				return true
			}
		}
		delete(visiting, id)
		visited[id] = true
		return false
	}
	for _, item := range items {
		if visit(item.ID) {
			return true
		}
	}
	return false
}

func countItemsByStatus(items []Item, status string) int {
	total := 0
	for _, item := range items {
		if item.Status == status {
			total++
		}
	}
	return total
}
