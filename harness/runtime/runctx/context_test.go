package runctx

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rt "github.com/mossagents/moss/harness/runtime"
	memstore "github.com/mossagents/moss/harness/runtime/memory"
	rstate "github.com/mossagents/moss/harness/runtime/state"
	"github.com/mossagents/moss/harness/sandbox"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

type executionObserverRecorder struct {
	execution []observe.ExecutionEvent
}

func (o *executionObserverRecorder) OnLLMCall(context.Context, observe.LLMCallEvent)      {}
func (o *executionObserverRecorder) OnToolCall(context.Context, observe.ToolCallEvent)    {}
func (o *executionObserverRecorder) OnApproval(context.Context, io.ApprovalEvent)         {}
func (o *executionObserverRecorder) OnSessionEvent(context.Context, observe.SessionEvent) {}
func (o *executionObserverRecorder) OnError(context.Context, observe.ErrorEvent)          {}
func (o *executionObserverRecorder) OnExecutionEvent(_ context.Context, e observe.ExecutionEvent) {
	o.execution = append(o.execution, e)
}

type stubRepoStateCapture struct {
	state *workspace.RepoState
}

func (s stubRepoStateCapture) Capture(context.Context) (*workspace.RepoState, error) {
	return s.state, nil
}

func applyContextMemoryService(k *kernel.Kernel) {
	k.Apply(WithContextMemoryService(rt.NewContextMemoryService(k)))
}

func TestConfigureContextWithoutStoreDoesNotRequireMemoryService(t *testing.T) {
	ctx := context.Background()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
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
}

func TestCompactConversationPreservesHistoryAndPersistsSnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ws := sandbox.NewMemoryWorkspace()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{Responses: []model.CompletionResponse{{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("summary")}},
			StopReason: "end_turn",
		}}}),
		kernel.WithUserIO(&io.NoOpIO{}),
		rt.WithMemoryWorkspace(ws),
		WithContextSessionStore(store),
		ConfigureContext(
			WithKeepRecent(2),
			WithContextPromptBudget(200),
			WithContextTriggerTokens(120),
			WithContextStartupBudget(0),
		),
	)
	applyContextMemoryService(k)
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
	compactTool, ok := k.ToolRegistry().Get("compact_conversation")
	if !ok {
		t.Fatal("compact_conversation not registered")
	}
	raw, err := compactTool.Execute(ctx, mustJSON(t, map[string]any{
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
	recordPath, _ := out["memory_record_path"].(string)
	if strings.TrimSpace(recordPath) == "" {
		t.Fatalf("expected memory_record_path in tool output: %+v", out)
	}
	record, err := rt.ExtendedMemoryStoreOf(k).GetByPathExtended(ctx, recordPath)
	if err != nil {
		t.Fatalf("GetByPathExtended: %v", err)
	}
	if record == nil || record.Stage != memstore.MemoryStageSnapshot || record.SourceKind != "context_summary" {
		t.Fatalf("unexpected memory record: %+v", record)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		raw, err := ws.ReadFile(ctx, "raw_memories.md")
		return err == nil && strings.Contains(string(raw), recordPath)
	})
	prompt := session.PromptMessages(sess)
	if got := model.ContentPartsToPlainText(prompt[0].ContentParts); !strings.Contains(got, "Use compact_conversation") {
		t.Fatalf("unexpected prompt baseline: %q", got)
	}
	var hasSummary bool
	for _, msg := range prompt {
		if strings.Contains(model.ContentPartsToPlainText(msg.ContentParts), "<context_summary>") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Fatalf("expected compact summary in prompt: %+v", prompt)
	}
}

func TestContextToolsExposeExecutionMetadata(t *testing.T) {
	spec := runtimeContextToolSpec(tool.ToolSpec{Name: "compact_conversation"})
	if len(spec.Effects) != 2 || spec.Effects[0] != tool.EffectGraphMutation || spec.Effects[1] != tool.EffectWritesMemory {
		t.Fatalf("effects = %v", spec.Effects)
	}
	if spec.SideEffectClass != tool.SideEffectTaskGraph {
		t.Fatalf("side_effect_class = %q", spec.SideEffectClass)
	}
	if spec.ApprovalClass != tool.ApprovalClassPolicyGuarded {
		t.Fatalf("approval_class = %q", spec.ApprovalClass)
	}
	if spec.PlannerVisibility != tool.PlannerVisibilityVisibleWithConstraints {
		t.Fatalf("planner_visibility = %q", spec.PlannerVisibility)
	}
}

func TestSkillCatalogAndMentionedSkillFragments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	skillFile := filepath.Join(root, ".agents", "skills", "jina", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "---\nname: jina\ndescription: Extract content from URLs\n---\n\nUse this skill to fetch web pages.\n"
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ws, err := sandbox.NewLocalWorkspace(root)
	if err != nil {
		t.Fatalf("NewLocalWorkspace: %v", err)
	}
	k := kernel.New(kernel.WithWorkspace(ws))
	sess := &session.Session{
		ID:       "sess-skill",
		Messages: []model.Message{{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("Please use @jina now")}}},
		Budget:   session.Budget{MaxSteps: 4},
	}

	catalog := buildSkillCatalogStartupFragment(k)
	if !strings.Contains(catalog.Text, "@jina") {
		t.Fatalf("catalog missing skill listing: %q", catalog.Text)
	}
	fragments := buildMentionedSkillFragments(k, sess)
	if len(fragments) != 1 {
		t.Fatalf("fragments=%d, want 1", len(fragments))
	}
	if !strings.Contains(fragments[0].Text, "fetch web pages") {
		t.Fatalf("skill body not injected: %q", fragments[0].Text)
	}
	_ = ctx
}

func TestEffectivePromptBudgetPrefersModelConfig(t *testing.T) {
	st := &contextState{maxPromptTokens: 4000, triggerTokens: 3000}
	sess := &session.Session{Config: session.SessionConfig{ModelConfig: model.ModelConfig{ContextWindow: 12000, AutoCompactTokenLimit: 9000}}}
	if got := effectivePromptBudget(sess, st); got != 12000 {
		t.Fatalf("prompt budget=%d, want 12000", got)
	}
	if got := effectiveTriggerTokens(sess, st); got != 9000 {
		t.Fatalf("trigger tokens=%d, want 9000", got)
	}
}

func TestEffectivePromptBudgetFallsBackToContextDefaults(t *testing.T) {
	st := &contextState{maxPromptTokens: 4000, triggerTokens: 3000}
	sess := &session.Session{}
	if got := effectivePromptBudget(sess, st); got != 4000 {
		t.Fatalf("prompt budget=%d, want 4000", got)
	}
	if got := effectiveTriggerTokens(sess, st); got != 3000 {
		t.Fatalf("trigger tokens=%d, want 3000", got)
	}
}

func TestPreparePromptContextEmitsCompactionEvent(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	observer := &executionObserverRecorder{}
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{Responses: []model.CompletionResponse{{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("summary")}},
			StopReason: "end_turn",
		}}}),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithObserver(observer),
		rt.WithMemoryWorkspace(sandbox.NewMemoryWorkspace()),
		WithContextSessionStore(store),
		ConfigureContext(
			WithKeepRecent(2),
			WithContextPromptBudget(120),
			WithContextTriggerTokens(80),
			WithContextStartupBudget(0),
		),
	)
	applyContextMemoryService(k)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "observe compaction"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	appendDialog(sess,
		strings.Repeat("first user context ", 12),
		strings.Repeat("first assistant context ", 12),
		strings.Repeat("second user context ", 12),
		strings.Repeat("second assistant context ", 12),
	)
	if _, _, _, err := preparePromptContext(ctx, k, ensureContextState(k), sess); err != nil {
		t.Fatalf("preparePromptContext: %v", err)
	}
	var found bool
	for _, event := range observer.execution {
		if event.Type != observe.ExecutionContextCompacted {
			continue
		}
		found = true
		if event.Metadata["snapshot_id"] == "" {
			t.Fatalf("expected snapshot_id metadata, got %+v", event.Metadata)
		}
		break
	}
	if !found {
		t.Fatal("expected context compaction execution event")
	}
}

func TestAutoCompactMiddlewareInjectsStartupContext(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	catalog, err := rstate.NewStateCatalog(t.TempDir(), t.TempDir(), true)
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
	llm := &kt.MockLLM{Responses: []model.CompletionResponse{{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("done")}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(ws),
		kernel.WithRepoStateCapture(stubRepoStateCapture{state: &workspace.RepoState{
			RepoRoot:  "D:/Codes/qiulin/moss",
			Branch:    "main",
			IsDirty:   true,
			Untracked: []string{"notes.txt"},
		}}),
		rt.WithStateCatalog(catalog),
		WithContextSessionStore(store),
		ConfigureContext(
			WithKeepRecent(2),
			WithContextPromptBudget(420),
			WithContextTriggerTokens(180),
			WithContextStartupBudget(180),
		),
	)
	applyContextMemoryService(k)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "auto compact"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := catalog.Upsert(rstate.StateEntry{
		Kind:      rstate.StateKindMemory,
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
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(strings.Repeat("new user request ", 12))}}
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("context"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
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
	repo := &workspace.RepoState{
		RepoRoot: "D:/Codes/qiulin/moss",
		Branch:   "main",
		IsDirty:  false,
	}
	llm := &kt.MockLLM{Responses: []model.CompletionResponse{
		{Message: model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("first")}}, StopReason: "end_turn"},
		{Message: model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("second")}}, StopReason: "end_turn"},
	}}
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithWorkspace(ws),
		kernel.WithRepoStateCapture(stubRepoStateCapture{state: repo}),
		WithContextSessionStore(store),
		ConfigureContext(
			WithContextPromptBudget(400),
			WithContextStartupBudget(160),
		),
	)
	applyContextMemoryService(k)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{Goal: "watch env"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	firstMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("first turn")}}
	sess.AppendMessage(firstMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("context"),
		UserContent: &firstMsg,
	}); err != nil {
		t.Fatalf("Run first: %v", err)
	}
	repo.IsDirty = true
	repo.Untracked = []string{"notes.txt"}
	if err := ws.WriteFile(ctx, "NEW.txt", []byte("changed")); err != nil {
		t.Fatalf("WriteFile NEW.txt: %v", err)
	}
	secondMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("second turn")}}
	sess.AppendMessage(secondMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("context"),
		UserContent: &secondMsg,
	}); err != nil {
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

func TestLightweightChatPromptSkipsStartupContext(t *testing.T) {
	ctx := context.Background()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	llm := &kt.MockLLM{Responses: []model.CompletionResponse{
		{Message: model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("你好！")}}, StopReason: "end_turn"},
	}}
	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithContextSessionStore(store),
		ConfigureContext(
			WithContextPromptBudget(400),
			WithContextStartupBudget(160),
		),
	)
	applyContextMemoryService(k)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "chat",
		SystemPrompt: "SYSTEM",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.AppendMessage(model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("帮我分析 README")}})
	sess.AppendMessage(model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("我先看看项目结构")}})
	latest := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("你好")}}
	sess.AppendMessage(latest)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("context"),
		UserContent: &latest,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	call := llm.Calls[len(llm.Calls)-1]
	joined := flattenMessageText(call.Messages)
	if !strings.Contains(joined, "SYSTEM") {
		t.Fatalf("expected system prompt to remain: %s", joined)
	}
	if !strings.Contains(joined, "你好") {
		t.Fatalf("expected lightweight chat turn in prompt: %s", joined)
	}
	if strings.Contains(joined, "帮我分析 README") || strings.Contains(joined, "我先看看项目结构") {
		t.Fatalf("expected prior raw dialog to be stripped: %s", joined)
	}
	if strings.Contains(joined, "<startup_") || strings.Contains(joined, "<realtime_") || strings.Contains(joined, "<context_summary>") {
		t.Fatalf("expected no startup or dynamic context for lightweight chat: %s", joined)
	}
	if len(call.Tools) != 0 {
		t.Fatalf("expected no tools for lightweight chat, got %d", len(call.Tools))
	}
}

func appendDialog(sess *session.Session, texts ...string) {
	roles := []model.Role{model.RoleUser, model.RoleAssistant}
	for i, text := range texts {
		sess.AppendMessage(model.Message{
			Role:         roles[i%len(roles)],
			ContentParts: []model.ContentPart{model.TextPart(text)},
		})
	}
}

func flattenMessageText(messages []model.Message) string {
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		lines = append(lines, model.ContentPartsToPlainText(msg.ContentParts))
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
