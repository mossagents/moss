package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"strings"
)

const contextStateKey kernel.ServiceKey = "context.state"
const offloadStateKey kernel.ServiceKey = "compact.state"

type contextFragmentKind string

const (
	contextFragmentDialog      contextFragmentKind = "dialog"
	contextFragmentAgentsMD    contextFragmentKind = "agents_md"
	contextFragmentSkill       contextFragmentKind = "skill"
	contextFragmentEnvironment contextFragmentKind = "environment"
	contextFragmentSubagent    contextFragmentKind = "subagent_notification"
	contextFragmentShell       contextFragmentKind = "user_shell_command"
	contextFragmentAborted     contextFragmentKind = "turn_aborted"
	contextFragmentOther       contextFragmentKind = "other"
)

type contextState struct {
	store                 session.SessionStore
	manager               session.Manager
	memory                contextMemoryService
	triggerDialog         int
	keepRecent            int
	triggerTokens         int
	maxPromptTokens       int
	startupTokens         int
	compactToolRegistered bool
	autoHookRegistered    bool
}

type offloadState struct {
	store session.SessionStore
}

// ContextOption 配置上下文管理行为。
type ContextOption func(*contextState)

func WithTriggerDialogCount(n int) ContextOption {
	return func(st *contextState) {
		if n > 0 {
			st.triggerDialog = n
		}
	}
}

func WithKeepRecent(n int) ContextOption {
	return func(st *contextState) {
		if n > 0 {
			st.keepRecent = n
		}
	}
}

func WithContextTriggerTokens(n int) ContextOption {
	return func(st *contextState) {
		if n > 0 {
			st.triggerTokens = n
		}
	}
}

func WithContextPromptBudget(n int) ContextOption {
	return func(st *contextState) {
		if n > 0 {
			st.maxPromptTokens = n
		}
	}
}

func WithContextStartupBudget(n int) ContextOption {
	return func(st *contextState) {
		if n >= 0 {
			st.startupTokens = n
		}
	}
}

func WithContextSessionStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureContextState(k).store = store
	}
}

func WithContextSessionManager(manager session.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureContextState(k).manager = manager
	}
}

func ConfigureContext(opts ...ContextOption) kernel.Option {
	return func(k *kernel.Kernel) {
		st := ensureContextState(k)
		for _, opt := range opts {
			if opt != nil {
				opt(st)
			}
		}
	}
}

func WithOffloadSessionStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureOffloadState(k).store = store
	}
}

func RegisterOffloadTools(reg tool.Registry, store session.SessionStore, manager session.Manager) error {
	if _, exists := reg.Get("offload_context"); exists {
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
	spec = runtimeContextToolSpec(spec)

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
		out, err := compactSessionContext(ctx, store, nil, nil, sess, in.KeepRecent, in.Note, nil, false)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}

	return reg.Register(tool.NewRawTool(spec, handler))
}

func ensureOffloadState(k *kernel.Kernel) *offloadState {
	actual, loaded := k.Services().LoadOrStore(offloadStateKey, &offloadState{})
	st := actual.(*offloadState)
	if loaded {
		return st
	}
	k.Prompts().Add(230, func(_ *kernel.Kernel) string {
		if st.store == nil {
			return ""
		}
		return "Use offload_context to compact long conversations and persist an offload snapshot."
	})
	return st
}

func ensureContextState(k *kernel.Kernel) *contextState {
	actual, loaded := k.Services().LoadOrStore(contextStateKey, &contextState{
		keepRecent:      20,
		triggerTokens:   3000,
		maxPromptTokens: 4000,
		startupTokens:   900,
	})
	st := actual.(*contextState)
	if loaded {
		return st
	}
	k.Stages().OnBoot(130, func(_ context.Context, k *kernel.Kernel) error {
		memState := ensureMemoryState(k)
		if st.manager == nil {
			st.manager = k.SessionManager()
		}
		st.memory = newContextMemoryService(memState)
		if st.store == nil || st.manager == nil {
			return nil
		}
		if err := registerCompactConversationTool(k.ToolRegistry(), st, k.LLM()); err != nil {
			return err
		}
		if !st.autoHookRegistered {
			k.InstallPlugin(kernel.Plugin{
				Name:      "auto-compact",
				BeforeLLM: AutoCompactHook(k),
			})
			st.autoHookRegistered = true
		}
		return nil
	})
	k.Prompts().Add(235, func(_ *kernel.Kernel) string {
		if st.store == nil {
			return ""
		}
		return "Use compact_conversation to keep prompt-visible context within budget with structured summaries, startup context, and snapshot-backed compaction."
	})
	return st
}

func runtimeContextToolSpec(spec tool.ToolSpec) tool.ToolSpec {
	spec.Effects = []tool.Effect{tool.EffectGraphMutation, tool.EffectWritesMemory}
	spec.ResourceScope = []string{"graph:conversation", "memory:session"}
	spec.LockScope = []string{"graph:conversation", "memory:session"}
	spec.SideEffectClass = tool.SideEffectTaskGraph
	spec.ApprovalClass = tool.ApprovalClassPolicyGuarded
	spec.PlannerVisibility = tool.PlannerVisibilityVisibleWithConstraints
	spec.CommutativityClass = tool.CommutativityNonCommutative
	return spec
}

func registerCompactConversationTool(reg tool.Registry, st *contextState, llm model.LLM) error {
	if st.compactToolRegistered {
		return nil
	}
	if _, exists := reg.Get("compact_conversation"); exists {
		st.compactToolRegistered = true
		return nil
	}
	spec := tool.ToolSpec{
		Name:        "compact_conversation",
		Description: "Summarize older conversation into a short note and offload full history snapshot into session store.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"session_id":{"type":"string"},
				"keep_recent":{"type":"integer"},
				"note":{"type":"string"}
			}
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"context"},
	}
	spec = runtimeContextToolSpec(spec)
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if st.store == nil || st.manager == nil {
			return nil, fmt.Errorf("context compaction requires session store and manager")
		}
		var in struct {
			SessionID  string `json:"session_id"`
			KeepRecent int    `json:"keep_recent"`
			Note       string `json:"note"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if strings.TrimSpace(in.SessionID) == "" {
			if meta, ok := toolctx.ToolCallContextFromContext(ctx); ok {
				in.SessionID = meta.SessionID
			}
		}
		if strings.TrimSpace(in.SessionID) == "" {
			return nil, fmt.Errorf("session_id is required")
		}
		keep := in.KeepRecent
		if keep <= 0 {
			keep = st.keepRecent
		}
		if keep <= 0 {
			keep = 20
		}
		out, err := compactWithSummary(ctx, st.store, st.memory, st.manager, in.SessionID, keep, in.Note, llm)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
	if err := reg.Register(tool.NewRawTool(spec, handler)); err != nil {
		return err
	}
	st.compactToolRegistered = true
	return nil
}

func countDialogMessages(msgs []model.Message) int {
	count := 0
	for _, m := range msgs {
		if m.Role != model.RoleSystem {
			count++
		}
	}
	return count
}

func buildSummary(ctx context.Context, llm model.LLM, msgs []model.Message) string {
	if llm == nil {
		return ""
	}
	reqMsgs := []model.Message{
		{
			Role:         model.RoleSystem,
			ContentParts: []model.ContentPart{model.TextPart("Summarize the earlier conversation in <=120 words, focusing on decisions, open tasks, and constraints.")},
		},
	}
	for _, m := range msgs {
		if !includeMessageInMemorySummary(m) {
			continue
		}
		reqMsgs = append(reqMsgs, m)
	}
	resp, err := model.Complete(ctx, llm, model.CompletionRequest{
		Messages: reqMsgs,
		Config:   model.ModelConfig{Temperature: 0},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(model.ContentPartsToPlainText(resp.Message.ContentParts))
}

func includeMessageInMemorySummary(msg model.Message) bool {
	kind := classifyContextFragment(msg)
	switch kind {
	case contextFragmentAgentsMD, contextFragmentSkill:
		return false
	default:
		return true
	}
}

func classifyContextFragment(msg model.Message) contextFragmentKind {
	content := strings.TrimSpace(strings.ToLower(model.ContentPartsToPlainText(msg.ContentParts)))
	if msg.Role != model.RoleSystem {
		return contextFragmentDialog
	}
	switch {
	case strings.Contains(content, "<agents_md>"):
		return contextFragmentAgentsMD
	case strings.Contains(content, "<skill>"):
		return contextFragmentSkill
	case strings.Contains(content, "<environment_context>"):
		return contextFragmentEnvironment
	case strings.Contains(content, "<subagent_notification>"):
		return contextFragmentSubagent
	case strings.Contains(content, "<user_shell_command>"):
		return contextFragmentShell
	case strings.Contains(content, "<turn_aborted>"):
		return contextFragmentAborted
	default:
		return contextFragmentOther
	}
}

func compactWithSummary(
	ctx context.Context,
	store session.SessionStore,
	memory contextMemoryService,
	manager session.Manager,
	sessionID string,
	keepRecent int,
	note string,
	llm model.LLM,
) (map[string]any, error) {
	sess, ok := manager.Get(sessionID)
	if !ok || sess == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	return memory.CompactSessionContext(ctx, store, sess, keepRecent, note, llm, true)
}

// AutoCompactHook 在 BeforeLLM 阶段按 token 预算刷新 prompt 上下文。
func AutoCompactHook(k *kernel.Kernel) hooks.Hook[hooks.LLMEvent] {
	return func(ctx context.Context, ev *hooks.LLMEvent) error {
		if ev.Session == nil {
			return nil
		}
		st := ensureContextState(k)
		if _, _, _, err := preparePromptContext(ctx, k, st, ev.Session); err != nil {
			return err
		}
		return nil
	}
}
