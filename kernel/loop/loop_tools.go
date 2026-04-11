package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
)

func (l *AgentLoop) executeToolCalls(ctx context.Context, sess *session.Session, calls []model.ToolCall) error {
	if len(calls) == 0 {
		return nil
	}
	plan, err := buildExecutionPlan(calls, l.Tools)
	if err != nil {
		l.emitExecutionPlanRejected(ctx, sess, calls, err)
		return err
	}
	l.emitExecutionPlanValidated(ctx, sess, plan)
	for _, batch := range l.admitToolCallBatches(calls) {
		if l.Config.ParallelToolCall && len(batch) > 1 {
			if err := l.executeToolCallsParallel(ctx, sess, batch); err != nil {
				return err
			}
			continue
		}
		if err := l.executeToolCallsSerial(ctx, sess, batch); err != nil {
			return err
		}
	}
	return nil
}

func (l *AgentLoop) emitExecutionPlanValidated(ctx context.Context, sess *session.Session, plan ExecutionPlan) {
	event := l.executionEventBase(sess, observe.ExecutionEventType("execution.plan_validated"), "planning", "runtime", "execution_plan")
	event.Data = map[string]any{
		"call_count": len(plan.Calls),
		"call_ids":   executionPlanCallIDs(plan),
		"calls":      executionPlanPayload(plan),
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) emitExecutionPlanRejected(ctx context.Context, sess *session.Session, calls []model.ToolCall, err error) {
	event := l.executionEventBase(sess, observe.ExecutionEventType("execution.plan_invalid"), "planning", "runtime", "execution_plan")
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	event.Error = err.Error()
	event.Data = map[string]any{
		"call_count": len(calls),
		"tool_names": names,
	}
	observe.ObserveExecutionEvent(ctx, l.observer(), event)
}

func (l *AgentLoop) executeToolCallsSerial(ctx context.Context, sess *session.Session, calls []model.ToolCall) error {
	for _, call := range calls {
		result := l.executeSingleToolCall(ctx, sess, call)
		msg := model.Message{Role: model.RoleTool, ToolResults: []model.ToolResult{result}}
		sess.AppendMessage(msg)
		// Yield tool result event in real-time.
		l.emitAgentEvent(&session.Event{
			Type:      session.EventTypeToolResult,
			Author:    l.AgentName,
			Content:   &msg,
			TurnID:    l.currentTurn.TurnID,
			Timestamp: time.Now().UTC(),
		})
	}
	return nil
}

func (l *AgentLoop) maxConcurrentTools() int {
	if l.Config.MaxConcurrentTools > 0 {
		return l.Config.MaxConcurrentTools
	}
	return 8
}

type toolAdmissionCandidate struct {
	call model.ToolCall
	spec tool.ToolSpec
	ok   bool
}

func (l *AgentLoop) admitToolCallBatches(calls []model.ToolCall) [][]model.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	if !l.Config.ParallelToolCall || len(calls) == 1 {
		return [][]model.ToolCall{append([]model.ToolCall(nil), calls...)}
	}
	batches := make([][]model.ToolCall, 0, len(calls))
	current := make([]toolAdmissionCandidate, 0, min(len(calls), l.maxConcurrentTools()))
	flush := func() {
		if len(current) == 0 {
			return
		}
		batch := make([]model.ToolCall, 0, len(current))
		for _, candidate := range current {
			batch = append(batch, candidate.call)
		}
		batches = append(batches, batch)
		current = current[:0]
	}
	for _, call := range calls {
		candidate := l.describeToolCallForAdmission(call)
		if len(current) > 0 && (len(current) >= l.maxConcurrentTools() || conflictsWithBatch(candidate, current)) {
			flush()
		}
		current = append(current, candidate)
	}
	flush()
	return batches
}

func (l *AgentLoop) describeToolCallForAdmission(call model.ToolCall) toolAdmissionCandidate {
	t, ok := l.Tools.Get(call.Name)
	var spec tool.ToolSpec
	if ok {
		spec = t.Spec()
	}
	return toolAdmissionCandidate{
		call: call,
		spec: spec,
		ok:   ok,
	}
}

func conflictsWithBatch(candidate toolAdmissionCandidate, batch []toolAdmissionCandidate) bool {
	for _, existing := range batch {
		if toolCallsConflict(candidate, existing) {
			return true
		}
	}
	return false
}

func toolCallsConflict(a, b toolAdmissionCandidate) bool {
	if !a.ok || !b.ok {
		return true
	}
	if a.spec.IsReadOnly() && b.spec.IsReadOnly() {
		return false
	}
	scopesA := normalizedAdmissionScopes(a.spec)
	scopesB := normalizedAdmissionScopes(b.spec)
	if hasEffect(a.spec, tool.EffectExternalSideEffect) || hasEffect(b.spec, tool.EffectExternalSideEffect) {
		return externalSideEffectConflict(a.spec, b.spec, scopesA, scopesB)
	}
	if hasEffect(a.spec, tool.EffectGraphMutation) && hasEffect(b.spec, tool.EffectGraphMutation) {
		return admissionScopesConflict(scopesA, scopesB)
	}
	return admissionScopesConflict(scopesA, scopesB)
}

func normalizedAdmissionScopes(spec tool.ToolSpec) []string {
	raw := make([]string, 0, len(spec.ResourceScope)+len(spec.LockScope)+2)
	raw = append(raw, spec.ResourceScope...)
	raw = append(raw, spec.LockScope...)
	if len(raw) == 0 {
		switch spec.EffectiveSideEffectClass() {
		case tool.SideEffectWorkspace:
			raw = append(raw, "workspace:*")
		case tool.SideEffectMemory:
			raw = append(raw, "memory:*")
		case tool.SideEffectNetwork:
			raw = append(raw, "network:*")
		case tool.SideEffectProcess:
			raw = append(raw, "process:*")
		case tool.SideEffectTaskGraph:
			raw = append(raw, "graph:*")
		case tool.SideEffectNone:
			if !spec.IsReadOnly() {
				raw = append(raw, "runtime:*")
			}
		}
	}
	out := make([]string, 0, len(raw))
	for _, scope := range raw {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "" {
			continue
		}
		if !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	return out
}

func admissionScopesConflict(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, left := range a {
		for _, right := range b {
			if normalizedScopeOverlap(left, right) {
				return true
			}
		}
	}
	return false
}

func normalizedScopeOverlap(a, b string) bool {
	rootA, targetA := splitNormalizedScope(a)
	rootB, targetB := splitNormalizedScope(b)
	if rootA == "" || rootB == "" || rootA != rootB {
		return false
	}
	if targetA == "*" || targetB == "*" {
		return true
	}
	return targetA == targetB ||
		strings.HasPrefix(targetA, targetB+"/") ||
		strings.HasPrefix(targetB, targetA+"/")
}

func splitNormalizedScope(scope string) (string, string) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		return "", ""
	}
	root, target, ok := strings.Cut(scope, ":")
	if !ok {
		return scope, "*"
	}
	root = strings.TrimSpace(root)
	target = strings.TrimSpace(target)
	if target == "" {
		target = "*"
	}
	return root, target
}

func externalSideEffectConflict(a, b tool.ToolSpec, scopesA, scopesB []string) bool {
	if a.EffectiveCommutativityClass() == tool.CommutativityFullyCommutative &&
		b.EffectiveCommutativityClass() == tool.CommutativityFullyCommutative {
		return false
	}
	if a.Idempotent && b.Idempotent &&
		a.EffectiveCommutativityClass() == tool.CommutativityTargetSafe &&
		b.EffectiveCommutativityClass() == tool.CommutativityTargetSafe &&
		!admissionScopesConflict(scopesA, scopesB) {
		return false
	}
	return admissionScopesConflict(scopesA, scopesB)
}

func hasEffect(spec tool.ToolSpec, want tool.Effect) bool {
	for _, effect := range spec.EffectiveEffects() {
		if effect == want {
			return true
		}
	}
	return false
}

func (l *AgentLoop) executeToolCallsParallel(ctx context.Context, sess *session.Session, calls []model.ToolCall) error {
	results := make([]model.ToolResult, len(calls))

	sem := make(chan struct{}, l.maxConcurrentTools())
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c model.ToolCall) {
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()
			results[idx] = l.executeSingleToolCall(ctx, sess, c)
		}(i, call)
	}
	wg.Wait()

	// 按顺序追加结果到 session（保持确定性）
	for _, result := range results {
		sess.AppendMessage(model.Message{Role: model.RoleTool, ToolResults: []model.ToolResult{result}})
	}
	// Yield all parallel tool results as a single aggregate event.
	l.emitAgentEvent(&session.Event{
		Type:      session.EventTypeToolResult,
		Author:    l.AgentName,
		Content:   &model.Message{Role: model.RoleTool, ToolResults: results},
		TurnID:    l.currentTurn.TurnID,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

func (l *AgentLoop) executeSingleToolCall(ctx context.Context, sess *session.Session, call model.ToolCall) model.ToolResult {
	repairedArgs := repairToolArguments(call.Arguments)
	l.emitToolLifecycle(ctx, session.ToolLifecycleEvent{
		Stage:     session.ToolLifecycleBefore,
		Session:   sess,
		ToolName:  call.Name,
		CallID:    call.ID,
		Arguments: repairedArgs,
		Timestamp: time.Now().UTC(),
	})
	if !l.toolAllowed(call.Name) {
		return l.handleMissingTool(ctx, sess, call, repairedArgs)
	}
	t, ok := l.Tools.Get(call.Name)
	if !ok {
		return l.handleMissingTool(ctx, sess, call, repairedArgs)
	}
	spec := t.Spec()

	// Validate required fields declared in the tool's input schema.
	// This is a best-effort guard against malformed or prompt-injected args.
	if err := validateRequiredToolArgs(spec, repairedArgs); err != nil {
		schemaErr := fmt.Errorf("tool %q argument validation failed: %w", call.Name, err)
		return buildToolResult(call.ID, nil, schemaErr)
	}

	l.emitToolStarted(ctx, sess, call, spec, repairedArgs)

	beforeErr := l.runBeforeToolCallHook(ctx, sess, spec, call.Arguments)
	if beforeErr != nil {
		return l.handleBeforeToolCallError(ctx, sess, call, spec, repairedArgs, beforeErr)
	}

	toolCtx := toolctx.WithToolCallContext(ctx, toolctx.ToolCallContext{
		SessionID: sess.ID,
		ToolName:  call.Name,
		CallID:    call.ID,
	})
	// 执行工具
	toolStart := time.Now()
	output, err := t.Execute(toolCtx, repairedArgs)
	toolDur := time.Since(toolStart)
	result := buildToolResult(call.ID, output, err)
	l.observeToolCompletion(ctx, sess, call, spec, toolStart, toolDur, result, output, err)
	l.runAfterToolCallHook(ctx, sess, spec, output)
	l.emitToolLifecycleAfter(ctx, sess, call, repairedArgs, spec, result, toolDur, err)
	l.sendToolResultIO(ctx, call, result, toolDur, err)
	return result
}

func buildToolResult(callID string, output []byte, err error) model.ToolResult {
	if err != nil {
		return model.ToolResult{
			CallID:       callID,
			ContentParts: []model.ContentPart{model.TextPart(err.Error())},
			IsError:      true,
		}
	}
	return model.ToolResult{
		CallID:       callID,
		ContentParts: []model.ContentPart{model.TextPart(string(output))},
	}
}

// validateRequiredToolArgs checks that all fields listed as "required" in the
// tool's input schema are present in the provided (repaired) arguments.
// It is a best-effort guard: if the schema cannot be parsed the call proceeds.
func validateRequiredToolArgs(spec tool.ToolSpec, args json.RawMessage) error {
	if len(spec.InputSchema) == 0 || len(args) == 0 {
		return nil
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil || len(schema.Required) == 0 {
		return nil // best-effort: unparseable schema or no required fields
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	for _, field := range schema.Required {
		if _, ok := obj[field]; !ok {
			return fmt.Errorf("missing required argument %q", field)
		}
	}
	return nil
}

func (l *AgentLoop) handleMissingTool(ctx context.Context, sess *session.Session, call model.ToolCall, repairedArgs json.RawMessage) model.ToolResult {
	err := fmt.Errorf("tool %q not found or not allowed in current turn", call.Name)
	result := buildToolResult(call.ID, nil, err)
	l.emitToolLifecycleAfter(ctx, sess, call, repairedArgs, tool.ToolSpec{}, result, 0, err)
	return result
}
