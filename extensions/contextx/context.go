package contextx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const stateKey kernel.ExtensionStateKey = "contextx.state"

type state struct {
	store                    session.SessionStore
	manager                  session.Manager
	triggerDialog            int
	keepRecent               int
	compactToolRegistered    bool
	autoMiddlewareRegistered bool
}

// Option 配置 contextx 行为。
type Option func(*state)

// WithTriggerDialogCount 设置自动压缩触发阈值（按非 system 消息数）。
func WithTriggerDialogCount(n int) Option {
	return func(st *state) {
		if n > 0 {
			st.triggerDialog = n
		}
	}
}

// WithKeepRecent 设置自动压缩保留的最近对话数。
func WithKeepRecent(n int) Option {
	return func(st *state) {
		if n > 0 {
			st.keepRecent = n
		}
	}
}

// WithSessionStore 设置上下文快照持久化存储。
func WithSessionStore(store session.SessionStore) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).store = store
	}
}

// WithSessionManager 设置会话管理器。
func WithSessionManager(manager session.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).manager = manager
	}
}

// Configure 配置 contextx 扩展参数。
func Configure(opts ...Option) kernel.Option {
	return func(k *kernel.Kernel) {
		st := ensureState(k)
		for _, opt := range opts {
			if opt != nil {
				opt(st)
			}
		}
	}
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{
		triggerDialog: 100,
		keepRecent:    20,
	})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnBoot(130, func(_ context.Context, k *kernel.Kernel) error {
		if st.manager == nil {
			st.manager = k.SessionManager()
		}
		if st.store == nil || st.manager == nil {
			return nil
		}
		if err := registerCompactConversationTool(k.ToolRegistry(), st, k.LLM()); err != nil {
			return err
		}
		if !st.autoMiddlewareRegistered {
			k.Middleware().Use(AutoCompactMiddleware(k))
			st.autoMiddlewareRegistered = true
		}
		return nil
	})
	bridge.OnSystemPrompt(235, func(_ *kernel.Kernel) string {
		if st.store == nil {
			return ""
		}
		return "Use compact_conversation to summarize and offload older context when conversation gets long."
	})
	return st
}

func registerCompactConversationTool(reg tool.Registry, st *state, llm port.LLM) error {
	if st.compactToolRegistered {
		return nil
	}
	if _, _, exists := reg.Get("compact_conversation"); exists {
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
			if meta, ok := port.ToolCallContextFromContext(ctx); ok {
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
		out, err := compactWithSummary(ctx, st.store, st.manager, in.SessionID, keep, in.Note, llm)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
	if err := reg.Register(spec, handler); err != nil {
		return err
	}
	st.compactToolRegistered = true
	return nil
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

func buildSummary(ctx context.Context, llm port.LLM, msgs []port.Message) string {
	if llm == nil {
		return ""
	}
	reqMsgs := []port.Message{
		{
			Role:    port.RoleSystem,
			Content: "Summarize the earlier conversation in <=120 words, focusing on decisions, open tasks, and constraints.",
		},
	}
	for _, m := range msgs {
		if m.Role == port.RoleSystem {
			continue
		}
		reqMsgs = append(reqMsgs, m)
	}
	resp, err := llm.Complete(ctx, port.CompletionRequest{
		Messages: reqMsgs,
		Config:   port.ModelConfig{Temperature: 0},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(resp.Message.Content)
}

func compactWithSummary(
	ctx context.Context,
	store session.SessionStore,
	manager session.Manager,
	sessionID string,
	keepRecent int,
	note string,
	llm port.LLM,
) (map[string]any, error) {
	sess, ok := manager.Get(sessionID)
	if !ok || sess == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	dialogCount := countDialogMessages(sess.Messages)
	if dialogCount <= keepRecent {
		return map[string]any{
			"status":       "noop",
			"session_id":   sess.ID,
			"dialog_count": dialogCount,
			"keep_recent":  keepRecent,
		}, nil
	}
	original := append([]port.Message(nil), sess.Messages...)
	snapshotID := fmt.Sprintf("%s_summary_%d", sess.ID, time.Now().UnixNano())
	summaryText := buildSummary(ctx, llm, original)
	if summaryText == "" {
		summaryText = "Earlier context compacted and offloaded."
	}
	if strings.TrimSpace(note) != "" {
		summaryText += " Note: " + strings.TrimSpace(note)
	}
	snapshot := &session.Session{
		ID:       snapshotID,
		Status:   session.StatusCompleted,
		Config:   sess.Config,
		Messages: original,
		State: map[string]any{
			"offload_of": sess.ID,
			"note":       note,
		},
		Budget:    sess.Budget,
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	if err := store.Save(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("save summary snapshot: %w", err)
	}
	notice := fmt.Sprintf("[Context summarized/offloaded to %s]\n%s", snapshotID, summaryText)
	sess.Messages = session.BuildCompactedMessages(sess.Messages, keepRecent, notice)
	sess.SetState("last_context_snapshot", snapshotID)
	sess.SetState("last_context_summary", summaryText)
	sess.SetState("last_context_offload_at", time.Now().Format(time.RFC3339))
	if err := store.Save(ctx, sess); err != nil {
		return nil, fmt.Errorf("save compacted session: %w", err)
	}
	return map[string]any{
		"status":            "offloaded",
		"session_id":        sess.ID,
		"snapshot_session":  snapshotID,
		"dialog_before":     dialogCount,
		"kept_recent":       keepRecent,
		"message_count_now": len(sess.Messages),
		"summary":           summaryText,
	}, nil
}

// AutoCompactMiddleware 在 BeforeLLM 阶段按阈值自动触发压缩。
func AutoCompactMiddleware(k *kernel.Kernel) middleware.Middleware {
	return func(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
		if mc.Phase != middleware.BeforeLLM || mc.Session == nil {
			return next(ctx)
		}
		st := ensureState(k)
		if st.store == nil || st.manager == nil {
			return next(ctx)
		}
		dialog := countDialogMessages(mc.Session.Messages)
		if dialog < st.triggerDialog {
			return next(ctx)
		}
		if _, err := compactWithSummary(ctx, st.store, st.manager, mc.Session.ID, st.keepRecent, "auto compact", k.LLM()); err != nil {
			return err
		}
		return next(ctx)
	}
}
