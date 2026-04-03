package runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
	kt "github.com/mossagents/moss/testing"
)

func TestRegisterMemoryTools_RoundTrip(t *testing.T) {
	reg := tool.NewRegistry()
	ws := sandbox.NewMemoryWorkspace()

	if err := RegisterMemoryToolsCompat(reg, ws); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	ctx := context.Background()
	_, writeHandler, ok := reg.Get("write_memory")
	if !ok {
		t.Fatal("write_memory not registered")
	}
	_, readHandler, ok := reg.Get("read_memory")
	if !ok {
		t.Fatal("read_memory not registered")
	}
	_, listHandler, ok := reg.Get("list_memories")
	if !ok {
		t.Fatal("list_memories not registered")
	}
	_, deleteHandler, ok := reg.Get("delete_memory")
	if !ok {
		t.Fatal("delete_memory not registered")
	}

	writeInput, _ := json.Marshal(map[string]string{
		"path":    "team/context.txt",
		"content": "remember this",
	})
	if _, err := writeHandler(ctx, writeInput); err != nil {
		t.Fatalf("write_memory failed: %v", err)
	}

	readInput, _ := json.Marshal(map[string]string{"path": "team/context.txt"})
	readRaw, err := readHandler(ctx, readInput)
	if err != nil {
		t.Fatalf("read_memory failed: %v", err)
	}
	var content string
	if err := json.Unmarshal(readRaw, &content); err != nil {
		t.Fatalf("decode read_memory: %v", err)
	}
	if content != "remember this" {
		t.Fatalf("unexpected memory content: %q", content)
	}

	listRaw, err := listHandler(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list_memories failed: %v", err)
	}
	var files []string
	if err := json.Unmarshal(listRaw, &files); err != nil {
		t.Fatalf("decode list_memories: %v", err)
	}
	if len(files) != 1 || files[0] != "team/context.txt" {
		t.Fatalf("unexpected memory file list: %+v", files)
	}

	deleteInput, _ := json.Marshal(map[string]string{"path": "team/context.txt"})
	if _, err := deleteHandler(ctx, deleteInput); err != nil {
		t.Fatalf("delete_memory failed: %v", err)
	}
	if _, err := readHandler(ctx, readInput); err == nil {
		t.Fatal("expected read_memory to fail after delete")
	}
}

func TestRegisterMemoryTools_NilWorkspace(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterMemoryToolsCompat(reg, nil); err == nil {
		t.Fatal("expected nil workspace error")
	}
}

func TestWithWorkspace_BootAndPrompt(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&port.NoOpIO{}),
		WithMemoryWorkspace(sandbox.NewMemoryWorkspace()),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	if _, _, ok := k.ToolRegistry().Get("read_memory"); !ok {
		t.Fatal("read_memory should be registered after boot")
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "test"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(sess.Messages) == 0 {
		t.Fatal("expected system prompt message")
	}
	if got := port.ContentPartsToPlainText(sess.Messages[0].ContentParts); !strings.Contains(got, "persistent memory tools") {
		t.Fatalf("expected memory prompt hint, got %q", got)
	}
}

func TestStructuredMemoryTools_RecordAndSearch(t *testing.T) {
	reg := tool.NewRegistry()
	ws := sandbox.NewMemoryWorkspace()
	if err := RegisterMemoryToolsCompat(reg, ws); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	ctx := context.Background()
	_, writeRecord, ok := reg.Get("write_memory_record")
	if !ok {
		t.Fatal("write_memory_record not registered")
	}
	_, readRecord, ok := reg.Get("read_memory_record")
	if !ok {
		t.Fatal("read_memory_record not registered")
	}
	_, searchMemories, ok := reg.Get("search_memories")
	if !ok {
		t.Fatal("search_memories not registered")
	}

	writeInput, _ := json.Marshal(map[string]any{
		"path":    "team/decision.md",
		"content": "We decided to use sqlite backend for state queries.",
		"tags":    []string{"architecture", "state"},
		"citation": map[string]any{
			"entries": []map[string]any{
				{
					"path":       "docs/decision.md",
					"line_start": 10,
					"line_end":   12,
					"note":       "decision source",
				},
			},
		},
	})
	if _, err := writeRecord(ctx, writeInput); err != nil {
		t.Fatalf("write_memory_record failed: %v", err)
	}

	readInput, _ := json.Marshal(map[string]string{"path": "team/decision.md"})
	recordRaw, err := readRecord(ctx, readInput)
	if err != nil {
		t.Fatalf("read_memory_record failed: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(recordRaw, &record); err != nil {
		t.Fatalf("decode read_memory_record: %v", err)
	}
	if record["summary"] == "" {
		t.Fatalf("expected generated summary, got %+v", record)
	}

	searchInput, _ := json.Marshal(map[string]any{
		"query": "sqlite backend",
		"limit": 5,
	})
	searchRaw, err := searchMemories(ctx, searchInput)
	if err != nil {
		t.Fatalf("search_memories failed: %v", err)
	}
	var searchResp struct {
		Count int               `json:"count"`
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(searchRaw, &searchResp); err != nil {
		t.Fatalf("decode search_memories: %v", err)
	}
	if searchResp.Count != 1 || len(searchResp.Items) != 1 {
		t.Fatalf("unexpected search result: %+v", searchResp)
	}
}

func TestStructuredMemoryTools_IngestMemoryTrace(t *testing.T) {
	reg := tool.NewRegistry()
	ws := sandbox.NewMemoryWorkspace()
	if err := RegisterMemoryToolsCompat(reg, ws); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	ctx := context.Background()
	_, ingestTrace, ok := reg.Get("ingest_memory_trace")
	if !ok {
		t.Fatal("ingest_memory_trace not registered")
	}
	_, readRecord, ok := reg.Get("read_memory_record")
	if !ok {
		t.Fatal("read_memory_record not registered")
	}

	trace := `{"type":"message","role":"user","content":"Need sqlite backend"}
{"type":"message","role":"assistant","content":"Will implement sqlite store"}`
	input, _ := json.Marshal(map[string]any{
		"source_path": "trace/session.jsonl",
		"trace":       trace,
		"target_path": "team/memory/trace-summary.md",
		"tags":        []string{"trace", "decision"},
	})
	raw, err := ingestTrace(ctx, input)
	if err != nil {
		t.Fatalf("ingest_memory_trace failed: %v", err)
	}
	var resp struct {
		Status string `json:"status"`
		Items  int    `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	if resp.Status != "ingested" || resp.Items != 2 {
		t.Fatalf("unexpected ingest response: %+v", resp)
	}

	recordRaw, err := readRecord(ctx, json.RawMessage(`{"path":"team/memory/trace-summary.md"}`))
	if err != nil {
		t.Fatalf("read_memory_record failed: %v", err)
	}
	var record port.MemoryRecord
	if err := json.Unmarshal(recordRaw, &record); err != nil {
		t.Fatalf("decode read_memory_record: %v", err)
	}
	if !strings.Contains(record.Content, "Need sqlite backend") {
		t.Fatalf("expected normalized trace content, got: %q", record.Content)
	}
	if len(record.Citation.Entries) != 1 || record.Citation.Entries[0].Path != "trace/session.jsonl" {
		t.Fatalf("unexpected citation: %+v", record.Citation)
	}
}

func TestSQLiteMemoryStore_BasicOperations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore: %v", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	ctx := context.Background()

	_, err = store.Upsert(ctx, port.MemoryRecord{
		Path:    "team/decision.md",
		Content: "Use sqlite memory backend",
		Tags:    []string{"state", "sqlite"},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.GetByPath(ctx, "team/decision.md")
	if err != nil {
		t.Fatalf("GetByPath: %v", err)
	}
	if got.Summary == "" {
		t.Fatal("expected summary to be generated")
	}

	items, err := store.Search(ctx, port.MemoryQuery{Query: "sqlite", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(items) != 1 || !strings.HasSuffix(strings.ReplaceAll(items[0].Path, "\\", "/"), "team/decision.md") {
		t.Fatalf("unexpected search result: %+v", items)
	}

	if err := store.DeleteByPath(ctx, "team/decision.md"); err != nil {
		t.Fatalf("DeleteByPath: %v", err)
	}
	if _, err := store.GetByPath(ctx, "team/decision.md"); err == nil {
		t.Fatal("expected GetByPath to fail after delete")
	}
}

func TestIngestMemoryTrace_WithAtomicJobRuntime(t *testing.T) {
	reg := tool.NewRegistry()
	ws := sandbox.NewMemoryWorkspace()
	taskRuntime := port.NewMemoryTaskRuntime()
	if err := taskRuntime.UpsertJob(context.Background(), port.AgentJob{
		ID:        "job-mem",
		AgentName: "worker",
		Goal:      "summarize",
		Status:    port.JobPending,
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if _, err := taskRuntime.MarkJobItemRunning(context.Background(), "job-mem", "item-1", "exec-a"); err != nil {
		t.Fatalf("MarkJobItemRunning: %v", err)
	}
	if err := RegisterMemoryToolsWithRuntime(reg, ws, NewWorkspaceMemoryStore(ws), taskRuntime); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	ctx := context.Background()
	_, ingestTrace, ok := reg.Get("ingest_memory_trace")
	if !ok {
		t.Fatal("ingest_memory_trace not registered")
	}
	trace := `[{"type":"message","role":"user","content":"save this"}]`
	input, _ := json.Marshal(map[string]any{
		"source_path": "trace/atomic.json",
		"trace":       trace,
		"target_path": "team/atomic.md",
		"job_id":      "job-mem",
		"item_id":     "item-1",
		"executor":    "exec-a",
	})
	raw, err := ingestTrace(ctx, input)
	if err != nil {
		t.Fatalf("ingest_memory_trace failed: %v", err)
	}
	if !strings.Contains(string(raw), `"atomic_updated":true`) {
		t.Fatalf("expected atomic update response, got %s", string(raw))
	}
	items, err := taskRuntime.ListJobItems(ctx, port.JobItemQuery{JobID: "job-mem"})
	if err != nil {
		t.Fatalf("ListJobItems: %v", err)
	}
	if len(items) != 1 || items[0].Status != port.JobCompleted {
		t.Fatalf("expected completed job item, got %+v", items)
	}
}
