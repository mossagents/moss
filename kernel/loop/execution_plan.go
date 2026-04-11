package loop

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/tool"
)

type ExecutionPlan struct {
	Calls []ExecutionPlanCall `json:"calls"`
}

type ExecutionPlanCall struct {
	CallID          string          `json:"call_id"`
	ToolName        string          `json:"tool_name"`
	Arguments       json.RawMessage `json:"arguments"`
	DeclaredEffects []tool.Effect   `json:"declared_effects"`
	ResourceScope   []string        `json:"resource_scope"`
	LockScope       []string        `json:"lock_scope"`
	DependsOn       []string        `json:"depends_on"`
	ParallelGroup   string          `json:"parallel_group,omitempty"`
	Rationale       string          `json:"rationale,omitempty"`
}

func buildExecutionPlan(calls []model.ToolCall, reg tool.Registry) (ExecutionPlan, error) {
	plan := ExecutionPlan{Calls: make([]ExecutionPlanCall, 0, len(calls))}
	for _, call := range calls {
		planCall := ExecutionPlanCall{
			CallID:          strings.TrimSpace(call.ID),
			ToolName:        strings.TrimSpace(call.Name),
			Arguments:       append(json.RawMessage(nil), call.Arguments...),
			DeclaredEffects: []tool.Effect{},
			ResourceScope:   []string{},
			LockScope:       []string{},
			DependsOn:       []string{},
		}
		if reg != nil {
			if t, ok := reg.Get(call.Name); ok {
				spec := t.Spec()
				planCall.DeclaredEffects = append([]tool.Effect(nil), spec.EffectiveEffects()...)
				planCall.ResourceScope = append([]string(nil), spec.ResourceScope...)
				planCall.LockScope = append([]string(nil), spec.LockScope...)
			}
		}
		plan.Calls = append(plan.Calls, planCall)
	}
	return plan, plan.Validate()
}

func (p ExecutionPlan) Validate() error {
	seen := make(map[string]struct{}, len(p.Calls))
	for _, call := range p.Calls {
		if strings.TrimSpace(call.CallID) == "" {
			return fmt.Errorf("execution plan call_id is required")
		}
		if strings.TrimSpace(call.ToolName) == "" {
			return fmt.Errorf("execution plan tool_name is required for %q", call.CallID)
		}
		if _, ok := seen[call.CallID]; ok {
			return fmt.Errorf("duplicate execution plan call_id %q", call.CallID)
		}
		seen[call.CallID] = struct{}{}
		if strings.TrimSpace(call.ParallelGroup) != "" && strings.TrimSpace(call.Rationale) == "" {
			return fmt.Errorf("execution plan parallel_group requires rationale for %q", call.CallID)
		}
		if err := validateExecutionPlanEffects(call); err != nil {
			return err
		}
		if err := validateExecutionPlanScopes(call.ResourceScope, "resource_scope", call.CallID); err != nil {
			return err
		}
		if err := validateExecutionPlanScopes(call.LockScope, "lock_scope", call.CallID); err != nil {
			return err
		}
	}
	for _, call := range p.Calls {
		for _, dep := range call.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				return fmt.Errorf("execution plan depends_on cannot contain empty id for %q", call.CallID)
			}
			if dep == call.CallID {
				return fmt.Errorf("execution plan call %q cannot depend on itself", call.CallID)
			}
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf("execution plan dependency %q not found for %q", dep, call.CallID)
			}
		}
	}
	if hasExecutionPlanCycle(p.Calls) {
		return fmt.Errorf("execution plan contains dependency cycle")
	}
	return nil
}

func executionPlanPayload(plan ExecutionPlan) []map[string]any {
	payload := make([]map[string]any, 0, len(plan.Calls))
	for _, call := range plan.Calls {
		payload = append(payload, map[string]any{
			"call_id":          call.CallID,
			"tool_name":        call.ToolName,
			"declared_effects": effectsToStrings(call.DeclaredEffects),
			"resource_scope":   append([]string(nil), call.ResourceScope...),
			"lock_scope":       append([]string(nil), call.LockScope...),
			"depends_on":       append([]string(nil), call.DependsOn...),
			"parallel_group":   call.ParallelGroup,
			"rationale":        call.Rationale,
		})
	}
	return payload
}

func validateExecutionPlanEffects(call ExecutionPlanCall) error {
	for _, effect := range call.DeclaredEffects {
		switch effect {
		case tool.EffectReadOnly, tool.EffectWritesWorkspace, tool.EffectWritesMemory, tool.EffectExternalSideEffect, tool.EffectGraphMutation:
		default:
			return fmt.Errorf("execution plan contains unknown effect %q for %q", effect, call.CallID)
		}
	}
	return nil
}

func validateExecutionPlanScopes(scopes []string, field string, callID string) error {
	for _, scope := range scopes {
		root, target := splitNormalizedScope(scope)
		switch root {
		case "workspace", "memory", "network", "process", "graph", "":
		default:
			return fmt.Errorf("execution plan contains unknown %s root %q for %q", field, root, callID)
		}
		if root != "" && strings.TrimSpace(target) == "" {
			return fmt.Errorf("execution plan %s target is required for %q", field, callID)
		}
	}
	return nil
}

func hasExecutionPlanCycle(calls []ExecutionPlanCall) bool {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	graph := make(map[string][]string, len(calls))
	for _, call := range calls {
		graph[call.CallID] = append([]string(nil), call.DependsOn...)
	}
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
	for _, call := range calls {
		if visit(call.CallID) {
			return true
		}
	}
	return false
}

func executionPlanCallIDs(plan ExecutionPlan) []string {
	ids := make([]string, 0, len(plan.Calls))
	for _, call := range plan.Calls {
		if !slices.Contains(ids, call.CallID) {
			ids = append(ids, call.CallID)
		}
	}
	return ids
}
