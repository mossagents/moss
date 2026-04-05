package session

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/logging"
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
		Messages: []port.Message{
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("hello")}},
			{Role: port.RoleAssistant, ContentParts: []port.ContentPart{port.TextPart("world")}},
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

func TestFileStoreLoadMigratesLegacyContentFields(t *testing.T) {
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

	loaded, err := store.Load(context.Background(), "legacy-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := port.ContentPartsToPlainText(loaded.Messages[0].ContentParts); got != "hello from legacy" {
		t.Fatalf("message content migration failed: %q", got)
	}
	if got := port.ContentPartsToPlainText(loaded.Messages[1].ToolResults[0].ContentParts); got != "legacy tool result" {
		t.Fatalf("tool result content migration failed: %q", got)
	}
}

func TestFileStoreLoadPrefersContentPartsWhenBothFieldsPresent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	prev := logging.GetLogger()
	logging.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { logging.SetLogger(prev) })

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
	loaded, err := store.Load(context.Background(), "legacy-2")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := port.ContentPartsToPlainText(loaded.Messages[0].ContentParts); got != "new" {
		t.Fatalf("expected content_parts to win, got %q", got)
	}
	if !strings.Contains(buf.String(), "migrated legacy message content field") {
		t.Fatalf("expected migration warning log, got: %s", buf.String())
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
	if err == nil || !strings.Contains(err.Error(), "legacy content must be string") {
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
	if err == nil || !strings.Contains(err.Error(), "legacy content must be string") {
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
		Messages: []port.Message{
			{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart("hello")}},
			{Role: port.RoleTool, ToolResults: []port.ToolResult{{CallID: "tc1", ContentParts: []port.ContentPart{port.TextPart("ok")}}}},
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
		Messages: []port.Message{
			{
				Role: port.RoleAssistant,
				ContentParts: []port.ContentPart{
					port.ReasoningPart("internal scratchpad"),
					port.TextPart("visible reply"),
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
	if got := port.ContentPartsToReasoningText(loaded.Messages[0].ContentParts); got != "" {
		t.Fatalf("expected reasoning to be stripped, got %q", got)
	}
	if got := port.ContentPartsToPlainText(loaded.Messages[0].ContentParts); got != "visible reply" {
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
	if got := port.ContentPartsToReasoningText(loaded.Messages[0].ContentParts); got != "" {
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
