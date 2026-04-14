package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/mossagents/moss/internal/stringutil"

	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
)

type contractRegistry struct {
	parent   tool.Registry
	contract taskrt.TaskContract
}

func withTaskContract(parent tool.Registry, contract taskrt.TaskContract) tool.Registry {
	return &contractRegistry{parent: parent, contract: contract}
}

func (r *contractRegistry) Register(t tool.Tool) error {
	return fmt.Errorf("contract registry is read-only: cannot register tool %q", t.Name())
}

func (r *contractRegistry) Unregister(name string) error {
	return fmt.Errorf("contract registry is read-only: cannot unregister tool %q", name)
}

func (r *contractRegistry) Get(name string) (tool.Tool, bool) {
	t, ok := r.parent.Get(name)
	if !ok {
		return nil, false
	}
	spec := t.Spec()
	if err := validateContractSpec(spec, r.contract); err != nil {
		return tool.NewRawTool(spec, func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, err
		}), true
	}
	return r.wrapTool(t), true
}

func (r *contractRegistry) List() []tool.Tool {
	out := make([]tool.Tool, 0)
	for _, t := range r.parent.List() {
		if validateContractSpec(t.Spec(), r.contract) == nil {
			out = append(out, t)
		}
	}
	return out
}

func (r *contractRegistry) ListByCapability(cap string) []tool.Tool {
	out := make([]tool.Tool, 0)
	for _, t := range r.parent.ListByCapability(cap) {
		if validateContractSpec(t.Spec(), r.contract) == nil {
			out = append(out, t)
		}
	}
	return out
}

func (r *contractRegistry) wrapTool(t tool.Tool) tool.Tool {
	spec := t.Spec()
	return tool.NewRawTool(spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if err := validateContractInput(spec, r.contract, input); err != nil {
			return nil, err
		}
		return t.Execute(ctx, input)
	})
}

func normalizeTaskContract(contract taskrt.TaskContract, taskID string, goal string, scoped tool.Registry, cfg AgentConfig) taskrt.TaskContract {
	contract.TaskID = strings.TrimSpace(stringutil.FirstNonEmpty(contract.TaskID, taskID))
	contract.Goal = strings.TrimSpace(stringutil.FirstNonEmpty(contract.Goal, goal))
	contract.InputContext = strings.TrimSpace(contract.InputContext)
	contract.MemoryScope = strings.TrimSpace(contract.MemoryScope)
	contract.WritableScopes = normalizeScopeList(contract.WritableScopes)
	contract.ReturnArtifacts = normalizeStringList(contract.ReturnArtifacts)
	if len(contract.ReturnArtifacts) == 0 {
		contract.ReturnArtifacts = []string{"summary"}
	}
	if contract.Budget.MaxSteps <= 0 {
		contract.Budget.MaxSteps = cfg.MaxSteps
	}
	if contract.ApprovalCeiling == "" {
		contract.ApprovalCeiling = maxApprovalForRegistry(scoped)
	}
	if len(contract.AllowedEffects) == 0 {
		contract.AllowedEffects = deriveEffectsForRegistry(scoped)
	}
	return contract
}

func validateContractSpec(spec tool.ToolSpec, contract taskrt.TaskContract) error {
	effects := spec.EffectiveEffects()
	if len(contract.AllowedEffects) > 0 {
		allowed := make(map[tool.Effect]struct{}, len(contract.AllowedEffects))
		for _, effect := range contract.AllowedEffects {
			if effect != "" {
				allowed[effect] = struct{}{}
			}
		}
		for _, effect := range effects {
			if _, ok := allowed[effect]; !ok {
				return fmt.Errorf("tool %q violates child task contract: effect %q is not allowed", spec.Name, effect)
			}
		}
	}
	if contract.ApprovalCeiling != "" && approvalRank(spec.EffectiveApprovalClass()) > approvalRank(contract.ApprovalCeiling) {
		return fmt.Errorf("tool %q violates child task contract: approval class %q exceeds ceiling %q", spec.Name, spec.EffectiveApprovalClass(), contract.ApprovalCeiling)
	}
	return nil
}

func validateContractInput(spec tool.ToolSpec, contract taskrt.TaskContract, input json.RawMessage) error {
	if len(contract.WritableScopes) > 0 && affects(spec, tool.EffectWritesWorkspace) {
		targetPath, ok := extractInputPath(input)
		if !ok {
			return fmt.Errorf("tool %q violates child task contract: writable scope requires a concrete path", spec.Name)
		}
		actual := "workspace:" + normalizeScopedPath(targetPath)
		if !matchesAnyScope(contract.WritableScopes, actual) {
			return fmt.Errorf("tool %q violates child task contract: path %q is outside writable scopes", spec.Name, targetPath)
		}
	}
	if contract.MemoryScope != "" && affects(spec, tool.EffectWritesMemory) {
		targetPath, ok := extractInputPath(input)
		if !ok {
			return fmt.Errorf("tool %q violates child task contract: memory scope requires a concrete path", spec.Name)
		}
		if prefix := memoryScopePrefix(contract); prefix != "" {
			actual := normalizeScopedPath(targetPath)
			if actual != prefix && !strings.HasPrefix(actual, prefix+"/") {
				return fmt.Errorf("tool %q violates child task contract: memory path %q is outside scope %q", spec.Name, targetPath, contract.MemoryScope)
			}
		}
	}
	return nil
}

func renderTaskContractPrompt(contract taskrt.TaskContract) string {
	var b strings.Builder
	b.WriteString("\n\n<child_task_contract>\n")
	if contract.TaskID != "" {
		b.WriteString("task_id: " + contract.TaskID + "\n")
	}
	if contract.Goal != "" {
		b.WriteString("goal: " + contract.Goal + "\n")
	}
	if contract.InputContext != "" {
		b.WriteString("input_context: " + contract.InputContext + "\n")
	}
	if contract.Budget.MaxSteps > 0 {
		b.WriteString(fmt.Sprintf("max_steps: %d\n", contract.Budget.MaxSteps))
	}
	if contract.Budget.MaxTokens > 0 {
		b.WriteString(fmt.Sprintf("max_tokens: %d\n", contract.Budget.MaxTokens))
	}
	if contract.Budget.TimeoutSec > 0 {
		b.WriteString(fmt.Sprintf("timeout_sec: %d\n", contract.Budget.TimeoutSec))
	}
	if contract.ApprovalCeiling != "" {
		b.WriteString("approval_ceiling: " + string(contract.ApprovalCeiling) + "\n")
	}
	if len(contract.AllowedEffects) > 0 {
		parts := make([]string, 0, len(contract.AllowedEffects))
		for _, effect := range contract.AllowedEffects {
			parts = append(parts, string(effect))
		}
		b.WriteString("allowed_effects: " + strings.Join(parts, ", ") + "\n")
	}
	if len(contract.WritableScopes) > 0 {
		b.WriteString("writable_scopes: " + strings.Join(contract.WritableScopes, ", ") + "\n")
	}
	if contract.MemoryScope != "" {
		b.WriteString("memory_scope: " + contract.MemoryScope + "\n")
	}
	if len(contract.ReturnArtifacts) > 0 {
		b.WriteString("return_artifacts: " + strings.Join(contract.ReturnArtifacts, ", ") + "\n")
	}
	b.WriteString("Do not widen this contract. If you cannot complete the task within these bounds, stop and report the constraint.\n")
	b.WriteString("</child_task_contract>")
	return b.String()
}

func maxApprovalForRegistry(reg tool.Registry) tool.ApprovalClass {
	maxRank := approvalRank(tool.ApprovalClassNone)
	maxApproval := tool.ApprovalClassNone
	for _, t := range reg.List() {
		spec := t.Spec()
		if rank := approvalRank(spec.EffectiveApprovalClass()); rank > maxRank {
			maxRank = rank
			maxApproval = spec.EffectiveApprovalClass()
		}
	}
	return maxApproval
}

func deriveEffectsForRegistry(reg tool.Registry) []tool.Effect {
	seen := make(map[tool.Effect]struct{})
	out := make([]tool.Effect, 0)
	for _, t := range reg.List() {
		for _, effect := range t.Spec().EffectiveEffects() {
			if _, ok := seen[effect]; ok {
				continue
			}
			seen[effect] = struct{}{}
			out = append(out, effect)
		}
	}
	return out
}

func approvalRank(class tool.ApprovalClass) int {
	switch class {
	case tool.ApprovalClassPolicyGuarded:
		return 1
	case tool.ApprovalClassExplicitUser:
		return 2
	case tool.ApprovalClassSupervisorOnly:
		return 3
	default:
		return 0
	}
}

func affects(spec tool.ToolSpec, effect tool.Effect) bool {
	for _, actual := range spec.EffectiveEffects() {
		if actual == effect {
			return true
		}
	}
	return false
}

func extractInputPath(input json.RawMessage) (string, bool) {
	if len(strings.TrimSpace(string(input))) == 0 {
		return "", false
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", false
	}
	for _, key := range []string{"path", "target_path"} {
		if raw, ok := payload[key]; ok {
			if value, ok := raw.(string); ok && strings.TrimSpace(value) != "" {
				return value, true
			}
		}
	}
	return "", false
}

func normalizeScopeList(scopes []string) []string {
	return normalizeStringList(scopes)
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeScopedPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	return strings.TrimPrefix(value, "/")
}

func matchesAnyScope(patterns []string, actual string) bool {
	for _, pattern := range patterns {
		if matchScope(pattern, actual) {
			return true
		}
	}
	return false
}

func matchScope(pattern string, actual string) bool {
	pattern = normalizeScopedPath(pattern)
	actual = normalizeScopedPath(actual)
	if pattern == "" {
		return false
	}
	if pattern == "*" || pattern == actual {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "**")
		return strings.HasPrefix(actual, prefix)
	}
	if matched, err := path.Match(pattern, actual); err == nil && matched {
		return true
	}
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		offset := 0
		for _, part := range parts {
			if part == "" {
				continue
			}
			idx := strings.Index(actual[offset:], part)
			if idx < 0 {
				return false
			}
			offset += idx + len(part)
		}
		return true
	}
	return false
}

func memoryScopePrefix(contract taskrt.TaskContract) string {
	switch strings.ToLower(strings.TrimSpace(contract.MemoryScope)) {
	case "task":
		if contract.TaskID == "" {
			return ""
		}
		return "tasks/" + normalizeScopedPath(contract.TaskID)
	default:
		return ""
	}
}

func applyContractTimeout(ctx context.Context, contract taskrt.TaskContract) (context.Context, context.CancelFunc) {
	if contract.Budget.TimeoutSec <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(contract.Budget.TimeoutSec)*time.Second)
}
