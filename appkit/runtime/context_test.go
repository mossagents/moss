package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
	kt "github.com/mossagents/moss/testing"
)

type stubRepoStateCapture struct {
	state *port.RepoState
}

func (s stubRepoStateCapture) Capture(context.Context) (*port.RepoState, error) {
	return s.state, nil
}

func TestCompactConversationPreservesHistoryAndPersistsSnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{Responses: []port.CompletionResponse{{
			Message:    port.Message{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("summary")}},
			StopReason: "end_turn",
		}}}),
		kernel.WithUserIO(&port.NoOpIO{}),
		WithContextSessionStore(store),
		ConfigureContext(
			WithKeepRecent(2),
			WithContextPromptBudget(200),
			WithContextTriggerTokens(120),
			WithContextStartupBudget(0),
		),
	)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "context compaction"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	appendDialog(sess,
		strings.Repeat("first user ", 10),
		strings.Repeat("first assistant ", 10),
		strings.Repeat("second user ", 10),
		strings.Repeat("second assistant ", 10),
		strings.Repeat("third user ", 10),
	)
	before := len(sess.Messages)
	_, handler, ok := k.ToolRegistry().Get("compact_conversation")
	if !ok {
		t.Fatal("compact_conversation not registered")
	}
	raw, err := handler(ctx, mustJSON(t, map[string]any{
		"session_id":  sess.ID,
		"keep_recent": 2,
		"note":        "manual compact",
	}))
	if err != nil {
		t.Fatalf("compact_conversation: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode tool output: %v", err)
	}
	if got := strings.TrimSpace(out["status"].(string)); got != "offloaded" {
		t.Fatalf("status=%q", got)
	}
	if len(sess.Messages) != before {
		t.Fatalf("session history mutated: before=%d after=%d", before, len(sess.Messages))
	}
	state := session.ReadPromptContextState(sess)
	if state.LastSnapshotID == "" || state.CompactedDialogCount == 0 {
		t.Fatalf("unexpected prompt context state: %+v", state)
	}
	if len(state.DynamicFragments) != 1 {
		t.Fatalf("expected summary fragment, got %+v", state.DynamicFragments)
	}
	snapshot, err := store.Load(ctx, state.LastSnapshotID)
	if err != nil {
		t.Fatalf("Load snapshot: %v", err)
	}
	if snapshot == nil || len(snapshot.Messages) != before {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	prompt := session.PromptMessages(sess)
	if got := port.ContentPartsToPlainText(prompt[0].ContentParts); !strings.Contains(got, "Use compact_conversation") {
		t.Fatalf("unexpected prompt baseline: %q", got)
	}
	var hasSummary bool
	for _, msg := range prompt {
		if strings.Contains(port.ContentPartsToPlainText(msg.ContentParts), "<context_summary>") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Fatalf("expected compact summary in prompt: %+v", prompt)
	}
}

func TestAutoCompactMiddlewareInjectsStartupContext(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	catalog, err := NewStateCatalog(t.TempDir(), t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	ws := sandbox.NewMemoryWorkspace()
	if err := ws.WriteFile(ctx, "MEMORY.md", []byte("Use sqlite memory indexes.")); err != nil {
		t.Fatalf("WriteFile MEMORY.md: %v", err)
	}
	if err := ws.WriteFile(ctx, "README.md", []byte("Workspace readme")); err != nil {
		t.Fatalf("WriteFile README.md: %v", err)
	}
	llm := &kt.MockLLM{Responses: []port.CompletionResponse{{
		Message:    port.Message{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("done")}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(&port.NoOpIO{}),
		kernel.WithWorkspace(ws),
		kernel.WithRepoStateCapture(stubRepoStateCapture{state: &port.RepoState{
			RepoRoot:  "D:/Codes/qiulin/moss",
			Branch:    "main",
			IsDirty:   true,
			Untracked: []string{"notes.txt"},
		}}),
		WithStateCatalog(catalog),
		WithContextSessionStore(store),
		ConfigureContext(
			WithKeepRecent(2),
			WithContextPromptBudget(420),
			WithContextTriggerTokens(180),
			WithContextStartupBudget(180),
		),
	)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "auto compact"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := catalog.Upsert(StateEntry{
		Kind:      StateKindMemory,
		RecordID:  "memory/project",
		SessionID: sess.ID,
		Status:    "active",
		Title:     "Project memory",
		Summary:   "sqlite decision",
		SortTime:  sess.CreatedAt,
	}); err != nil {
		t.Fatalf("catalog upsert: %v", err)
	}
	appendDialog(sess,
		strings.Repeat("very long user context ", 12),
		strings.Repeat("very long assistant context ", 12),
		strings.Repeat("new user request ", 12),
		strings.Repeat("assistant reasoning ", 12),
	)
	before := len(sess.Messages)
	if _, err := k.Run(ctx, sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(llm.Calls) == 0 {
		t.Fatal("expected llm call")
	}
	call := llm.Calls[len(llm.Calls)-1]
	joined := flattenMessageText(call.Messages)
	if !strings.Contains(joined, "<context_summary>") {
		t.Fatalf("expected summary fragment in prompt: %s", joined)
	}
	if !strings.Contains(joined, "<startup_memory_context>") {
		t.Fatalf("expected startup memory fragment in prompt: %s", joined)
	}
	if !strings.Contains(joined, "<startup_repo_state>") {
		t.Fatalf("expected repo state fragment in prompt: %s", joined)
	}
	state := session.ReadPromptContextState(sess)
	if state.LastPromptTokens == 0 || len(state.StartupFragments) == 0 {
		t.Fatalf("unexpected prompt state: %+v", state)
	}
	if len(sess.Messages) != before+1 {
		t.Fatalf("expected full history to remain plus assistant reply: before=%d after=%d", before, len(sess.Messages))
	}
}

func TestPromptContextIncludesRealtimeEnvironmentChanges(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ws := sandbox.NewMemoryWorkspace()
	if err := ws.WriteFile(ctx, "README.md", []byte("initial")); err != nil {
		t.Fatalf("WriteFile README.md: %v", err)
	}
	repo := &port.RepoState{
		RepoRoot: "D:/Codes/qiulin/moss",
		Branch:   "main",
		IsDirty:  false,
	}
	llm := &kt.MockLLM{Responses: []port.CompletionResponse{
		{Message: port.Message{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("first")}}, StopReason: "end_turn"},
		{Message: port.Message{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("second")}}, StopReason: "end_turn"},
	}}
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(&port.NoOpIO{}),
		kernel.WithWorkspace(ws),
		kernel.WithRepoStateCapture(stubRepoStateCapture{state: repo}),
		WithContextSessionStore(store),
		ConfigureContext(
			WithContextPromptBudget(400),
			WithContextStartupBudget(160),
		),
	)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "watch env"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("first turn")}})
	if _, err := k.Run(ctx, sess); err != nil {
		t.Fatalf("Run first: %v", err)
	}
	repo.IsDirty = true
	repo.Untracked = []string{"notes.txt"}
	if err := ws.WriteFile(ctx, "NEW.txt", []byte("changed")); err != nil {
		t.Fatalf("WriteFile NEW.txt: %v", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("second turn")}})
	if _, err := k.Run(ctx, sess); err != nil {
		t.Fatalf("Run second: %v", err)
	}
	joined := flattenMessageText(llm.Calls[len(llm.Calls)-1].Messages)
	if !strings.Contains(joined, "<realtime_repo_changes>") {
		t.Fatalf("expected realtime repo fragment in prompt: %s", joined)
	}
	if !strings.Contains(joined, "<realtime_workspace_changes>") {
		t.Fatalf("expected realtime workspace fragment in prompt: %s", joined)
	}
}

func appendDialog(sess *session.Session, texts ...string) {
	roles := []port.Role{port.RoleUser, port.RoleAssistant}
	for i, text := range texts {
		sess.AppendMessage(port.Message{
			Role:         roles[i%len(roles)],
			ContentParts: []port.ContentPart{port.TextPart(text)},
		})
	}
}

func flattenMessageText(messages []port.Message) string {
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		lines = append(lines, port.ContentPartsToPlainText(msg.ContentParts))
	}
	return strings.Join(lines, "\n")
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
