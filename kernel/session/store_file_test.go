package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/model"
)

func TestFileStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 空列表
	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}

	// Save
	sess := &Session{
		ID:     "test-1",
		Status: StatusCompleted,
		Config: SessionConfig{
			Goal: "test goal",
			Mode: "test",
		},
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hello")}},
			{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("world")}},
		},
		Budget:    Budget{MaxSteps: 10, UsedSteps: 3},
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}

	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load
	loaded, err := store.Load(ctx, "test-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.ID != "test-1" {
		t.Fatalf("expected ID test-1, got %s", loaded.ID)
	}
	if loaded.Config.Goal != "test goal" {
		t.Fatalf("expected goal 'test goal', got %s", loaded.Config.Goal)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.Messages))
	}

	// Load non-existent
	missing, err := store.Load(ctx, "no-such-id")
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing session")
	}

	// List
	summaries, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Goal != "test goal" {
		t.Fatalf("expected goal 'test goal', got %s", summaries[0].Goal)
	}

	// Delete
	if err := store.Delete(ctx, "test-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify deleted
	loaded, err = store.Load(ctx, "test-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestFileStoreSanitizeID(t *testing.T) {
	// 确保路径遍历攻击被阻止
	id := sanitizeID("../../etc/passwd")
	if id == "../../etc/passwd" {
		t.Fatal("sanitizeID should prevent path traversal")
	}

	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 即使传入恶意 ID，文件也应该在 store.dir 内
	path := store.path("../../etc/passwd")
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	// 应该在 dir 内（不以 .. 开头）
	if filepath.IsAbs(rel) || rel[:2] == ".." {
		t.Fatalf("path traversal not prevented: %s", path)
	}
}

func TestFileStoreOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess := &Session{
		ID:     "ow-1",
		Status: StatusRunning,
		Config: SessionConfig{Goal: "v1"},
	}
	if err := store.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// Update and save again
	sess.Status = StatusCompleted
	sess.Config.Goal = "v2"
	if err := store.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(ctx, "ow-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", loaded.Status)
	}
	if loaded.Config.Goal != "v2" {
		t.Fatalf("expected v2, got %s", loaded.Config.Goal)
	}

	// 确认只有一个文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
}

func TestFileStorePersistsResolvedSessionSpecAndSummary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess := &Session{
		ID:     "spec-1",
		Status: StatusRunning,
		Config: SessionConfig{
			Goal: "ship typed session persistence",
			SessionSpec: &SessionSpec{
				Workspace: SessionWorkspace{Trust: "trusted"},
				Intent: SessionIntent{
					CollaborationMode: "plan",
					PromptPack:        "planner",
				},
				Runtime: SessionRuntime{
					RunMode:           "foreground",
					PermissionProfile: "workspace-write",
					SessionPolicy:     "deep-work",
					ModelProfile:      "gpt-5.4-default",
				},
				Origin: SessionOrigin{Preset: "planner-safe"},
			},
			ResolvedSessionSpec: &ResolvedSessionSpec{
				Workspace: ResolvedWorkspace{Trust: "trusted"},
				Intent: ResolvedIntent{
					CollaborationMode: "plan",
					PromptPack:        PromptPackRef{ID: "planner", Source: "builtin:planner"},
					CapabilityCeiling: []string{"read_workspace", "mutate_graph"},
				},
				Runtime: ResolvedRuntime{
					RunMode:           "foreground",
					PermissionProfile: "workspace-write",
					PermissionPolicy:  json.RawMessage(`{"approval_mode":"confirm"}`),
					SessionPolicyName: "deep-work",
					SessionPolicy:     SessionPolicySpec{MaxSteps: 128, MaxTokens: 64000},
					ModelProfile:      "gpt-5.4-default",
					Provider:          "openai",
					ModelConfig:       model.ModelConfig{Model: "gpt-5.4"},
					ReasoningEffort:   "high",
					Verbosity:         "medium",
					RouterLane:        "coding",
				},
				Prompt: ResolvedPrompt{
					BasePackID:            "planner",
					RenderedPromptVersion: "v1",
					SnapshotRef:           "snapshot-1",
				},
				Origin: ResolvedOrigin{Preset: "planner-safe"},
			},
			PromptSnapshot: &PromptSnapshot{
				Ref:            "snapshot-1",
				Layers:         []ResolvedPromptLayer{{ID: "system/base", Source: "builtin", Content: "You are the planner."}},
				RenderedPrompt: "You are the planner.",
				Version:        "v1",
			},
		},
		CreatedAt: time.Now().UTC(),
	}

	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.Config.ResolvedSessionSpec == nil {
		t.Fatalf("expected resolved session spec to round-trip, got %+v", loaded)
	}
	if loaded.Config.ResolvedSessionSpec.Intent.PromptPack.ID != "planner" {
		t.Fatalf("prompt pack = %q, want planner", loaded.Config.ResolvedSessionSpec.Intent.PromptPack.ID)
	}
	if loaded.Config.PromptSnapshot == nil || loaded.Config.PromptSnapshot.Ref != "snapshot-1" {
		t.Fatalf("prompt snapshot = %+v, want snapshot-1", loaded.Config.PromptSnapshot)
	}

	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.Mode != "foreground" {
		t.Fatalf("mode = %q, want foreground", summary.Mode)
	}
	if summary.Preset != "planner-safe" {
		t.Fatalf("preset = %q, want planner-safe", summary.Preset)
	}
	if summary.Profile != "planner-safe" {
		t.Fatalf("profile fallback = %q, want planner-safe", summary.Profile)
	}
	if summary.CollaborationMode != "plan" {
		t.Fatalf("collaboration_mode = %q, want plan", summary.CollaborationMode)
	}
	if summary.EffectiveApproval != "confirm" {
		t.Fatalf("effective_approval = %q, want confirm", summary.EffectiveApproval)
	}
	if summary.TaskMode != "plan" {
		t.Fatalf("task_mode fallback = %q, want plan", summary.TaskMode)
	}
	if summary.WorkspaceTrust != "trusted" {
		t.Fatalf("workspace_trust = %q, want trusted", summary.WorkspaceTrust)
	}
	if summary.PermissionProfile != "workspace-write" {
		t.Fatalf("permission_profile = %q, want workspace-write", summary.PermissionProfile)
	}
	if summary.SessionPolicy != "deep-work" {
		t.Fatalf("session_policy = %q, want deep-work", summary.SessionPolicy)
	}
	if summary.ModelProfile != "gpt-5.4-default" {
		t.Fatalf("model_profile = %q, want gpt-5.4-default", summary.ModelProfile)
	}
	if summary.PromptPack != "planner" {
		t.Fatalf("prompt_pack = %q, want planner", summary.PromptPack)
	}
	if thread := ThreadRefFromSession(loaded); thread.PromptPack != "planner" || thread.CollaborationMode != "plan" {
		t.Fatalf("unexpected thread ref: %+v", thread)
	}
}

func TestFileStoreSave_NormalizesInvalidToolCallArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess := &Session{
		ID:     "invalid-tool-args",
		Status: StatusCompleted,
		Config: SessionConfig{Goal: "persist malformed tool call"},
		Messages: []model.Message{
			{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{
					{
						ID:        "call-1",
						Name:      "read_file",
						Arguments: json.RawMessage(`D:/Codes/qiulin/moss/apps/mosscode/README.md`),
					},
				},
			},
		},
	}

	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || len(loaded.Messages) != 1 || len(loaded.Messages[0].ToolCalls) != 1 {
		t.Fatalf("unexpected loaded session: %+v", loaded)
	}
	args := loaded.Messages[0].ToolCalls[0].Arguments
	if !json.Valid(args) {
		t.Fatalf("expected persisted args to be valid json, got %s", args)
	}
	var decoded string
	if err := json.Unmarshal(args, &decoded); err != nil {
		t.Fatalf("unmarshal persisted args: %v", err)
	}
	if decoded != `D:/Codes/qiulin/moss/apps/mosscode/README.md` {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestFileStore_PersistsBudgetPolicy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess := &Session{
		ID:     "budget-policy",
		Status: StatusCreated,
		Config: SessionConfig{
			Goal:         "policy check",
			BudgetPolicy: BudgetPolicyObserveOnly,
		},
	}
	if err := store.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Config.BudgetPolicy != BudgetPolicyObserveOnly {
		t.Fatalf("budget policy lost after roundtrip: %+v", loaded)
	}
}

func TestFileStoreListMarksRecoverableSessions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := store.Save(ctx, &Session{
		ID:        "paused",
		Status:    StatusPaused,
		Config:    SessionConfig{Goal: "resume me"},
		CreatedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, &Session{
		ID:        "completed",
		Status:    StatusCompleted,
		Config:    SessionConfig{Goal: "done"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].ID != "completed" {
		t.Fatalf("expected newest session first, got %q", summaries[0].ID)
	}
	var seenPaused, seenCompleted bool
	for _, summary := range summaries {
		switch summary.ID {
		case "paused":
			seenPaused = true
			if !summary.Recoverable {
				t.Fatal("paused session should be recoverable")
			}
		case "completed":
			seenCompleted = true
			if summary.Recoverable {
				t.Fatal("completed session should not be recoverable")
			}
		}
	}
	if !seenPaused || !seenCompleted {
		t.Fatalf("missing expected summaries: %+v", summaries)
	}
}

func TestFileStoreRouteKeyLookup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess := &Session{
		ID:     "route-1",
		Status: StatusPaused,
		Config: SessionConfig{Goal: "route"},
	}
	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.SaveRouteKey(ctx, "main", sess.ID); err != nil {
		t.Fatalf("SaveRouteKey: %v", err)
	}
	loaded, err := store.LoadByRouteKey(ctx, "main")
	if err != nil {
		t.Fatalf("LoadByRouteKey: %v", err)
	}
	if loaded == nil || loaded.ID != sess.ID {
		t.Fatalf("loaded route session = %+v", loaded)
	}
	if err := store.DeleteRouteKey(ctx, "main"); err != nil {
		t.Fatalf("DeleteRouteKey: %v", err)
	}
	loaded, err = store.LoadByRouteKey(ctx, "main")
	if err != nil {
		t.Fatalf("LoadByRouteKey after delete: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil route lookup after delete, got %+v", loaded)
	}
}

func TestFileStoreListSkipsHistoryHiddenSessions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	visible := &Session{
		ID:        "visible",
		Status:    StatusCompleted,
		Config:    SessionConfig{Goal: "visible"},
		CreatedAt: time.Now().Add(-time.Minute),
	}
	hidden := &Session{
		ID:        "hidden",
		Status:    StatusCompleted,
		Config:    SessionConfig{Goal: "hidden"},
		CreatedAt: time.Now(),
	}
	MarkHistoryHidden(hidden)

	if err := store.Save(ctx, visible); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, hidden); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 visible summary, got %d", len(summaries))
	}
	if summaries[0].ID != "visible" {
		t.Fatalf("expected visible summary, got %q", summaries[0].ID)
	}
}

func TestFileStoreListIncludesThreadMetadataAndActivitySort(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	parent := &Session{
		ID:        "parent",
		Status:    StatusPaused,
		Config:    SessionConfig{Goal: "parent"},
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	RefreshThreadMetadata(parent, time.Now().Add(-30*time.Minute), "manual")
	SetThreadPreview(parent, "root summary")
	if err := store.Save(ctx, parent); err != nil {
		t.Fatal(err)
	}

	child := &Session{
		ID:        "child",
		Status:    StatusRunning,
		Config:    SessionConfig{Goal: "child"},
		CreatedAt: time.Now().Add(-time.Hour),
	}
	SetThreadSource(child, "delegated")
	SetThreadParent(child, "parent")
	SetThreadTaskID(child, "task-123")
	SetThreadArchived(child, true)
	RefreshThreadMetadata(child, time.Now(), "tool:task")
	SetThreadPreview(child, "latest delegated progress")
	if err := store.Save(ctx, child); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].ID != "child" {
		t.Fatalf("expected newest activity first, got %q", summaries[0].ID)
	}
	if summaries[0].Source != "delegated" || summaries[0].ParentID != "parent" {
		t.Fatalf("unexpected thread lineage: %+v", summaries[0])
	}
	if summaries[0].TaskID != "task-123" || summaries[0].ActivityKind != "tool:task" {
		t.Fatalf("unexpected thread task metadata: %+v", summaries[0])
	}
	if !summaries[0].Archived {
		t.Fatalf("expected archived delegated summary: %+v", summaries[0])
	}
	if summaries[0].Preview != "latest delegated progress" {
		t.Fatalf("unexpected preview %q", summaries[0].Preview)
	}
	if summaries[0].UpdatedAt == "" {
		t.Fatalf("expected updated_at in summary: %+v", summaries[0])
	}
}

func TestFileStoreLoadFailsOnLegacyContentFields(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{
  "id":"legacy-1",
  "status":"completed",
  "config":{"goal":"legacy"},
  "messages":[
    {"role":"user","content":"hello from legacy"},
    {"role":"tool","tool_results":[{"call_id":"tc1","content":"legacy tool result"}]}
  ],
  "budget":{"max_tokens":0,"max_steps":0,"used_tokens":0,"used_steps":0},
  "created_at":"2026-01-01T00:00:00Z"
}`
	if err := os.WriteFile(store.path("legacy-1"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	_, err = store.Load(context.Background(), "legacy-1")
	if err == nil || !strings.Contains(err.Error(), "legacy content field is no longer supported") {
		t.Fatalf("expected legacy content rejection, got %v", err)
	}
}

func TestFileStoreLoadFailsOnLegacyContentFieldEvenWhenContentPartsExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{
  "id":"legacy-2",
  "status":"completed",
  "config":{"goal":"legacy"},
  "messages":[
    {"role":"user","content_parts":[{"type":"text","text":"new"}],"content":"old"}
  ],
  "budget":{"max_tokens":0,"max_steps":0,"used_tokens":0,"used_steps":0},
  "created_at":"2026-01-01T00:00:00Z"
}`
	if err := os.WriteFile(store.path("legacy-2"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load(context.Background(), "legacy-2")
	if err == nil || !strings.Contains(err.Error(), "legacy content field is no longer supported") {
		t.Fatalf("expected legacy content rejection, got %v", err)
	}
}

func TestFileStoreLoadFailsOnInvalidLegacyMessageContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{
  "id":"legacy-bad-msg",
  "status":"completed",
  "config":{"goal":"legacy"},
  "messages":[{"role":"user","content":{"bad":"shape"}}],
  "budget":{"max_tokens":0,"max_steps":0,"used_tokens":0,"used_steps":0},
  "created_at":"2026-01-01T00:00:00Z"
}`
	if err := os.WriteFile(store.path("legacy-bad-msg"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load(context.Background(), "legacy-bad-msg")
	if err == nil || !strings.Contains(err.Error(), "legacy content field is no longer supported") {
		t.Fatalf("expected legacy content shape error, got %v", err)
	}
}

func TestFileStoreLoadFailsOnInvalidLegacyToolResultContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{
  "id":"legacy-bad-tool",
  "status":"completed",
  "config":{"goal":"legacy"},
  "messages":[{"role":"tool","tool_results":[{"call_id":"tc1","content":[1,2,3]}]}],
  "budget":{"max_tokens":0,"max_steps":0,"used_tokens":0,"used_steps":0},
  "created_at":"2026-01-01T00:00:00Z"
}`
	if err := os.WriteFile(store.path("legacy-bad-tool"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load(context.Background(), "legacy-bad-tool")
	if err == nil || !strings.Contains(err.Error(), "legacy content field is no longer supported") {
		t.Fatalf("expected legacy tool content shape error, got %v", err)
	}
}

func TestFileStoreSaveWritesNewSchemaOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		ID:     "new-schema",
		Status: StatusCompleted,
		Config: SessionConfig{Goal: "schema"},
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("hello")}},
			{Role: model.RoleTool, ToolResults: []model.ToolResult{{CallID: "tc1", ContentParts: []model.ContentPart{model.TextPart("ok")}}}},
		},
		CreatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(store.path("new-schema"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"content":`) {
		t.Fatalf("legacy content field must not be persisted: %s", string(data))
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal saved json: %v", err)
	}
}

func TestFileStoreSaveAppendsJSONLAndLoadReturnsLatest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		ID:     "jsonl-latest",
		Status: StatusRunning,
		Config: SessionConfig{Goal: "append"},
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("first")}},
		},
		CreatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	sess.Status = StatusCompleted
	sess.Messages = append(sess.Messages, model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("second")}})
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	raw, err := os.ReadFile(store.path(sess.ID))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected append-only jsonl with >=2 lines, got %d: %q", len(lines), string(raw))
	}
	loaded, err := store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.Status != StatusCompleted {
		t.Fatalf("unexpected loaded session: %+v", loaded)
	}
	if got := model.ContentPartsToPlainText(loaded.Messages[len(loaded.Messages)-1].ContentParts); got != "second" {
		t.Fatalf("latest message=%q, want second", got)
	}
}

func TestFileStoreStripsReasoningPartsOnSaveAndLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		ID:     "reasoning-stripped",
		Status: StatusCompleted,
		Config: SessionConfig{Goal: "strip reasoning"},
		Messages: []model.Message{
			{
				Role: model.RoleAssistant,
				ContentParts: []model.ContentPart{
					model.ReasoningPart("internal scratchpad"),
					model.TextPart("visible reply"),
				},
			},
		},
		CreatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(store.path(sess.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "internal scratchpad") {
		t.Fatalf("reasoning leaked into persisted session: %s", string(raw))
	}
	loaded, err := store.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || len(loaded.Messages) != 1 {
		t.Fatalf("unexpected loaded session: %+v", loaded)
	}
	if got := model.ContentPartsToReasoningText(loaded.Messages[0].ContentParts); got != "" {
		t.Fatalf("expected reasoning to be stripped, got %q", got)
	}
	if got := model.ContentPartsToPlainText(loaded.Messages[0].ContentParts); got != "visible reply" {
		t.Fatalf("plain text=%q", got)
	}
}

func TestFileStoreLoadRewritesExistingReasoningLeak(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{
  "id":"dirty-reasoning",
  "status":"completed",
  "config":{"goal":"legacy"},
  "messages":[{"role":"assistant","content_parts":[{"type":"reasoning","text":"fake user request"},{"type":"text","text":"visible"}]}],
  "budget":{"max_tokens":0,"max_steps":0,"used_tokens":0,"used_steps":0},
  "created_at":"2026-01-01T00:00:00Z"
}`
	if err := os.WriteFile(store.path("dirty-reasoning"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "dirty-reasoning")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected loaded session")
	}
	if got := model.ContentPartsToReasoningText(loaded.Messages[0].ContentParts); got != "" {
		t.Fatalf("expected in-memory reasoning to be stripped, got %q", got)
	}
	saved, err := os.ReadFile(store.path("dirty-reasoning"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "fake user request") {
		t.Fatalf("expected dirty file to be rewritten without reasoning: %s", string(saved))
	}
}
