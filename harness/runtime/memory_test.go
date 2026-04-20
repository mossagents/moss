package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	memstore "github.com/mossagents/moss/harness/runtime/memory"
	"github.com/mossagents/moss/harness/sandbox"
	kt "github.com/mossagents/moss/harness/testing"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

func bootMemoryTestKernel(t *testing.T, ws workspace.Workspace, store memstore.ExtendedMemoryStore, taskRuntime taskrt.TaskRuntime) *kernel.Kernel {
	t.Helper()
	opts := []kernel.Option{
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
	}
	if taskRuntime != nil {
		opts = append(opts, kernel.WithTaskRuntime(taskRuntime))
	}
	if ws != nil {
		opts = append(opts, WithMemoryWorkspace(ws))
	}
	if store != nil {
		opts = append(opts, WithMemoryStore(store))
	}
	k := kernel.New(opts...)
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() {
		_ = k.Shutdown(ctx)
	})
	return k
}

type runtimeMockEmbedder struct{}

func (runtimeMockEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	return []float64{float64(len(text)), 1}, nil
}

func (runtimeMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		out[i] = []float64{float64(len(text)), float64(i + 1)}
	}
	return out, nil
}

func (runtimeMockEmbedder) Dimension() int { return 2 }

func bootMemoryTestRegistry(t *testing.T, ws workspace.Workspace, store memstore.ExtendedMemoryStore, taskRuntime taskrt.TaskRuntime) tool.Registry {
	t.Helper()
	return bootMemoryTestKernel(t, ws, store, taskRuntime).ToolRegistry()
}

func bootMemoryTestRegistryWithDocuments(t *testing.T, ws workspace.Workspace, store memstore.ExtendedMemoryStore, taskRuntime taskrt.TaskRuntime) tool.Registry {
	t.Helper()
	opts := []kernel.Option{
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryEmbedder(runtimeMockEmbedder{}),
		WithMemoryDocumentTools(),
	}
	if taskRuntime != nil {
		opts = append(opts, kernel.WithTaskRuntime(taskRuntime))
	}
	if store != nil {
		opts = append(opts, WithMemoryStore(store))
	}
	k := kernel.New(opts...)
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() {
		_ = k.Shutdown(ctx)
	})
	return k.ToolRegistry()
}

func TestRegisterMemoryTools_RoundTrip(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistry(t, ws, nil, nil)

	ctx := context.Background()
	writeTool, ok := reg.Get("write_memory")
	if !ok {
		t.Fatal("write_memory not registered")
	}
	writeHandler := writeTool.Execute
	readTool, ok := reg.Get("read_memory")
	if !ok {
		t.Fatal("read_memory not registered")
	}
	readHandler := readTool.Execute
	listTool, ok := reg.Get("list_memories")
	if !ok {
		t.Fatal("list_memories not registered")
	}
	listHandler := listTool.Execute
	deleteTool, ok := reg.Get("delete_memory")
	if !ok {
		t.Fatal("delete_memory not registered")
	}
	deleteHandler := deleteTool.Execute

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

func TestRegisterMemoryTools_ExecutionMetadata(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistryWithDocuments(t, ws, nil, nil)
	cases := []struct {
		name       string
		effect     tool.Effect
		sideEffect tool.SideEffectClass
		approval   tool.ApprovalClass
	}{
		{"read_memory", tool.EffectReadOnly, tool.SideEffectNone, tool.ApprovalClassNone},
		{"write_memory", tool.EffectWritesMemory, tool.SideEffectMemory, tool.ApprovalClassExplicitUser},
		{"ingest_memory_trace", tool.EffectWritesMemory, tool.SideEffectMemory, tool.ApprovalClassPolicyGuarded},
		{"ingest_document", tool.EffectWritesMemory, tool.SideEffectMemory, tool.ApprovalClassExplicitUser},
		{"knowledge_search", tool.EffectReadOnly, tool.SideEffectNone, tool.ApprovalClassNone},
	}
	for _, tc := range cases {
		tl, ok := reg.Get(tc.name)
		if !ok {
			t.Fatalf("tool %q not registered", tc.name)
		}
		spec := tl.Spec()
		if effects := spec.EffectiveEffects(); len(effects) != 1 || effects[0] != tc.effect {
			t.Fatalf("%s effects = %v", tc.name, effects)
		}
		if spec.SideEffectClass != tc.sideEffect {
			t.Fatalf("%s side_effect_class = %q", tc.name, spec.SideEffectClass)
		}
		if spec.ApprovalClass != tc.approval {
			t.Fatalf("%s approval_class = %q", tc.name, spec.ApprovalClass)
		}
	}
}

func TestUnifiedMemoryTools_IngestAndSearchDocuments(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistryWithDocuments(t, ws, nil, nil)
	ctx := context.Background()

	ingestTool, ok := reg.Get("ingest_document")
	if !ok {
		t.Fatal("ingest_document not registered")
	}
	searchTool, ok := reg.Get("knowledge_search")
	if !ok {
		t.Fatal("knowledge_search not registered")
	}
	listTool, ok := reg.Get("knowledge_list")
	if !ok {
		t.Fatal("knowledge_list not registered")
	}

	if _, err := ingestTool.Execute(ctx, mustJSON(t, map[string]any{
		"id":         "readme",
		"source":     "README.md",
		"text":       "Moss uses sqlite for state and memory indexing.\nPersistent memories are runtime-owned.",
		"chunk_size": 40,
	})); err != nil {
		t.Fatalf("ingest_document: %v", err)
	}

	searchRaw, err := searchTool.Execute(ctx, mustJSON(t, map[string]any{
		"query": "sqlite memory indexing",
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("knowledge_search: %v", err)
	}
	var searchResp struct {
		Count   int `json:"count"`
		Results []struct {
			DocID    string  `json:"doc_id"`
			Source   string  `json:"source"`
			Text     string  `json:"text"`
			Score    float64 `json:"score"`
			ChunkIdx int     `json:"chunk_index"`
			Path     string  `json:"path"`
		} `json:"results"`
	}
	if err := json.Unmarshal(searchRaw, &searchResp); err != nil {
		t.Fatalf("decode knowledge_search: %v", err)
	}
	if searchResp.Count == 0 || len(searchResp.Results) == 0 {
		t.Fatalf("expected knowledge search hits, got %+v", searchResp)
	}
	if searchResp.Results[0].DocID != "readme" || searchResp.Results[0].Source != "README.md" {
		t.Fatalf("unexpected top result: %+v", searchResp.Results[0])
	}

	listRaw, err := listTool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("knowledge_list: %v", err)
	}
	var listResp struct {
		Documents []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Chunks int    `json:"chunks"`
		} `json:"documents"`
		TotalDocs   int `json:"total_docs"`
		TotalChunks int `json:"total_chunks"`
	}
	if err := json.Unmarshal(listRaw, &listResp); err != nil {
		t.Fatalf("decode knowledge_list: %v", err)
	}
	if listResp.TotalDocs != 1 || len(listResp.Documents) != 1 || listResp.Documents[0].ID != "readme" {
		t.Fatalf("unexpected knowledge list: %+v", listResp)
	}
}

func TestWithMemoryWorkspace_AutoCapturesExplicitPreferences(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	mock := &kt.MockLLM{Responses: []model.CompletionResponse{{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("收到")}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryStore(store),
	)
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { _ = k.Shutdown(ctx) })

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "capture preferences",
		Mode:         "oneshot",
		SystemPrompt: "base system",
		MaxSteps:     4,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("以后请用中文回复，简洁一点。")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("memory-auto"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes: []memstore.MemoryScope{memstore.MemoryScopeUser},
		UserID: currentMemoryUserID(),
		Kinds:  []string{autoMemoryPreferenceKind},
		Limit:  8,
	})
	if err != nil {
		t.Fatalf("SearchExtended: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected captured preference memories, got %+v", items)
	}
	joined := items[0].Fingerprint + "\n"
	for i := 1; i < len(items); i++ {
		joined += items[i].Fingerprint + "\n"
	}
	for _, want := range []string{"language-zh", "response-style-concise"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected preference %q in %+v", want, items)
		}
	}
}

func TestWithMemoryWorkspace_AutoCapturesEnvironmentConstraints(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	mock := &kt.MockLLM{Responses: []model.CompletionResponse{{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("收到")}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryStore(store),
	)
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { _ = k.Shutdown(ctx) })

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "capture environment constraints",
		Mode:         "oneshot",
		SystemPrompt: "base system",
		MaxSteps:     4,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("请先看一下当前项目。")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("memory-auto"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes: []memstore.MemoryScope{memstore.MemoryScopeUser},
		UserID: currentMemoryUserID(),
		Kinds:  []string{autoMemoryConstraintKind},
		Limit:  8,
	})
	if err != nil {
		t.Fatalf("SearchExtended: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected environment constraint memory to be captured")
	}
	joined := strings.ToLower(items[0].Summary)
	for i := 1; i < len(items); i++ {
		joined += "\n" + strings.ToLower(items[i].Summary)
	}
	if goruntime.GOOS == "windows" {
		for _, want := range []string{"windows", "avoid bash"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("expected environment constraint summary to mention %q in %q", want, joined)
			}
		}
	} else if !strings.Contains(joined, goruntime.GOOS) {
		t.Fatalf("expected environment constraint summary to mention current os %q in %q", goruntime.GOOS, joined)
	}
}

func TestWithMemoryWorkspace_AutoRecallInjectsStructuredMemory(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	ctx := context.Background()
	if _, err := store.UpsertExtended(ctx, memstore.ExtendedMemoryRecord{
		Path:        "constraints/environment-windows-powershell.md",
		Content:     "# Operational Constraint\n\nThe current environment is Windows. Prefer PowerShell or cmd syntax by default. Do not start with bash-style commands unless the user explicitly asks for bash or a POSIX shell.",
		Summary:     "Environment constraint: current OS is Windows; prefer PowerShell or cmd; avoid bash-first.",
		Tags:        []string{"environment", "constraint", "windows", "powershell", "cmd", "avoid-bash"},
		Scope:       memstore.MemoryScopeUser,
		UserID:      currentMemoryUserID(),
		Kind:        autoMemoryConstraintKind,
		Fingerprint: "environment:os:windows:shell:powershell",
		Confidence:  1.0,
		Stage:       memstore.MemoryStagePromoted,
		Status:      memstore.MemoryStatusActive,
	}); err != nil {
		t.Fatalf("Upsert constraint: %v", err)
	}
	if _, err := store.UpsertExtended(ctx, memstore.ExtendedMemoryRecord{
		Path:        "preferences/language-zh.md",
		Content:     "# User Preference\n\nReply in Chinese by default unless the user asks for another language.",
		Summary:     "Reply in Chinese by default.",
		Tags:        []string{"preference", "language", "zh", "中文"},
		Scope:       memstore.MemoryScopeUser,
		UserID:      currentMemoryUserID(),
		Kind:        autoMemoryPreferenceKind,
		Fingerprint: "language-zh",
		Confidence:  1.0,
		Stage:       memstore.MemoryStagePromoted,
		Status:      memstore.MemoryStatusActive,
	}); err != nil {
		t.Fatalf("Upsert preference: %v", err)
	}
	if _, err := store.UpsertExtended(ctx, memstore.ExtendedMemoryRecord{
		Path:        "auto/tool_outcomes/http_request-weather-wttr-in.md",
		Content:     "Prefer wttr.in for weather/天气 requests: GET returned 200 successfully.",
		Summary:     "Prefer wttr.in for weather/天气 requests: GET returned 200 successfully.",
		Tags:        []string{"auto", "tool-outcome", "weather", "天气", "wttr.in", "success"},
		Scope:       memstore.MemoryScopeRepo,
		Kind:        "trace_consolidated",
		Fingerprint: "http_request:weather:wttr.in",
		Confidence:  0.9,
		Stage:       memstore.MemoryStageConsolidated,
		Status:      memstore.MemoryStatusActive,
	}); err != nil {
		t.Fatalf("Upsert repo memory: %v", err)
	}
	mock := &kt.MockLLM{Responses: []model.CompletionResponse{{
		Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("好的")}},
		StopReason: "end_turn",
	}}}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryStore(store),
	)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { _ = k.Shutdown(ctx) })

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "recall weather memory",
		Mode:         "oneshot",
		SystemPrompt: "base system",
		MaxSteps:     4,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("上海天气如何？")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("memory-auto"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("calls=%d, want 1", len(mock.Calls))
	}
	requestPrompt := model.ContentPartsToPlainText(mock.Calls[0].Messages[0].ContentParts)
	for _, want := range []string{"<memory_context>", "<operational_constraints>", "<user_preferences>", "<proven_lessons>", "Reply in Chinese by default.", "wttr.in", "avoid bash-first"} {
		if !strings.Contains(requestPrompt, want) {
			t.Fatalf("request prompt missing %q in %q", want, requestPrompt)
		}
	}
	constraintIdx := strings.Index(requestPrompt, "<operational_constraints>")
	prefIdx := strings.Index(requestPrompt, "<user_preferences>")
	lessonIdx := strings.Index(requestPrompt, "<proven_lessons>")
	if constraintIdx == -1 || prefIdx == -1 || lessonIdx == -1 || !(constraintIdx < prefIdx && prefIdx < lessonIdx) {
		t.Fatalf("expected layered memory sections in prompt, got %q", requestPrompt)
	}
	sessionPrompt := model.ContentPartsToPlainText(sess.Messages[0].ContentParts)
	if strings.Contains(sessionPrompt, "<memory_context>") {
		t.Fatalf("session prompt should remain request-local, got %q", sessionPrompt)
	}
}

func TestWithMemoryWorkspace_AutoDoesNotPromoteSingleAPIFailure(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	mock := &kt.MockLLM{Responses: []model.CompletionResponse{
		{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID:        "call-bad-api",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://bad.example/aqi"}`),
			}}},
			ToolCalls: []model.ToolCall{{
				ID:        "call-bad-api",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://bad.example/aqi"}`),
			}},
			StopReason: "tool_use",
		},
		{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("bad api failed")}},
			StopReason: "end_turn",
		},
	}}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryStore(store),
	)
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name:          "http_request",
		Description:   "HTTP request",
		Risk:          tool.RiskMedium,
		Capabilities:  []string{"network"},
		Effects:       []tool.Effect{tool.EffectExternalSideEffect},
		ApprovalClass: tool.ApprovalClassNone,
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"url":"https://bad.example/aqi","method":"GET","status_code":503,"body":"unavailable"}`), nil
	})); err != nil {
		t.Fatalf("Register tool: %v", err)
	}
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { _ = k.Shutdown(ctx) })

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "single api failure",
		Mode:         "oneshot",
		SystemPrompt: "base system",
		MaxSteps:     6,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("查询上海空气质量")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("memory-auto"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	repoItems, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes: []memstore.MemoryScope{memstore.MemoryScopeRepo},
		Kinds:  []string{autoMemoryExecutionLessonKind, "trace_consolidated"},
		Limit:  8,
	})
	if err != nil {
		t.Fatalf("SearchExtended: %v", err)
	}
	for _, item := range repoItems {
		if strings.Contains(item.Summary, "bad.example") {
			t.Fatalf("expected single api failure to remain local, got repo memory %+v", item)
		}
	}
}

func TestWithMemoryWorkspace_AutoPromotesAPIFallbackLesson(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	mock := &kt.MockLLM{Responses: []model.CompletionResponse{
		{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID:        "call-bad-api",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://bad.example/aqi"}`),
			}}},
			ToolCalls: []model.ToolCall{{
				ID:        "call-bad-api",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://bad.example/aqi"}`),
			}},
			StopReason: "tool_use",
		},
		{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID:        "call-good-api",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://good.example/aqi"}`),
			}}},
			ToolCalls: []model.ToolCall{{
				ID:        "call-good-api",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://good.example/aqi"}`),
			}},
			StopReason: "tool_use",
		},
		{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("空气质量已获取")}},
			StopReason: "end_turn",
		},
	}}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryStore(store),
	)
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name:          "http_request",
		Description:   "HTTP request",
		Risk:          tool.RiskMedium,
		Capabilities:  []string{"network"},
		Effects:       []tool.Effect{tool.EffectExternalSideEffect},
		ApprovalClass: tool.ApprovalClassNone,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, err
		}
		if strings.Contains(params.URL, "bad.example") {
			return json.RawMessage(`{"url":"https://bad.example/aqi","method":"GET","status_code":503,"body":"unavailable"}`), nil
		}
		return json.RawMessage(`{"url":"https://good.example/aqi","method":"GET","status_code":200,"body":"ok"}`), nil
	})); err != nil {
		t.Fatalf("Register tool: %v", err)
	}
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { _ = k.Shutdown(ctx) })

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "promote api fallback lesson",
		Mode:         "oneshot",
		SystemPrompt: "base system",
		MaxSteps:     8,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("查询上海空气质量")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("memory-auto"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	waitForCondition(t, 5*time.Second, func() bool {
		items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
			Scopes:    []memstore.MemoryScope{memstore.MemoryScopeSession},
			SessionID: sess.ID,
			Kinds:     []string{autoMemoryToolOutcomeKind},
			Limit:     8,
		})
		return err == nil && len(items) >= 2
	})
	sessionItems, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes:    []memstore.MemoryScope{memstore.MemoryScopeSession},
		SessionID: sess.ID,
		Kinds:     []string{autoMemoryToolOutcomeKind},
		Limit:     8,
	})
	if err != nil {
		t.Fatalf("SearchExtended session outcomes: %v", err)
	}

	var lessonItems []memstore.ExtendedMemoryRecord
	foundLesson := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
			Scopes: []memstore.MemoryScope{memstore.MemoryScopeRepo},
			Kinds:  []string{autoMemoryExecutionLessonKind},
			Limit:  8,
		})
		if err == nil && len(items) > 0 {
			lessonItems = items
			foundLesson = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !foundLesson {
		t.Fatalf("expected repo lesson to be created; session outcomes=%+v", sessionItems)
	}
	if lessonItems[0].Kind != autoMemoryExecutionLessonKind || lessonItems[0].Scope != memstore.MemoryScopeRepo {
		t.Fatalf("unexpected lesson record shape: %+v", lessonItems[0])
	}
	if !strings.Contains(lessonItems[0].Summary, "good.example") || !strings.Contains(lessonItems[0].Summary, "bad.example") {
		t.Fatalf("expected fallback lesson to mention both failed and preferred hosts, got %+v", lessonItems[0])
	}
}

func TestWithMemoryWorkspace_AutoCapturesToolOutcomeIntoMemory(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	taskRuntime := taskrt.NewMemoryTaskRuntime()
	mock := &kt.MockLLM{Responses: []model.CompletionResponse{
		{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID:        "call-weather",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://wttr.in/hangzhou?format=j1"}`),
			}}},
			ToolCalls: []model.ToolCall{{
				ID:        "call-weather",
				Name:      "http_request",
				Arguments: json.RawMessage(`{"url":"https://wttr.in/hangzhou?format=j1"}`),
			}},
			StopReason: "tool_use",
		},
		{
			Message:    model.Message{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("天气已获取")}},
			StopReason: "end_turn",
		},
	}}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&io.NoOpIO{}),
		kernel.WithTaskRuntime(taskRuntime),
		WithMemoryWorkspace(ws),
		WithMemoryStore(store),
	)
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name:          "http_request",
		Description:   "HTTP request",
		Risk:          tool.RiskMedium,
		Capabilities:  []string{"network"},
		Effects:       []tool.Effect{tool.EffectExternalSideEffect},
		ApprovalClass: tool.ApprovalClassNone,
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"url":"https://wttr.in/hangzhou?format=j1","method":"GET","status_code":200,"body":"ok"}`), nil
	})); err != nil {
		t.Fatalf("Register tool: %v", err)
	}
	ctx := context.Background()
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() { _ = k.Shutdown(ctx) })

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "capture tool outcome",
		Mode:         "oneshot",
		SystemPrompt: "base system",
		MaxSteps:     6,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("杭州天气怎么样")}}
	sess.AppendMessage(userMsg)
	if _, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("memory-auto"),
		UserContent: &userMsg,
	}); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}

	var sessionItems []memstore.ExtendedMemoryRecord
	waitForCondition(t, 2*time.Second, func() bool {
		items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
			Scopes:    []memstore.MemoryScope{memstore.MemoryScopeSession},
			SessionID: sess.ID,
			Kinds:     []string{autoMemoryToolOutcomeKind},
			Limit:     8,
		})
		if err != nil || len(items) == 0 {
			return false
		}
		sessionItems = items
		return true
	})
	if !strings.Contains(sessionItems[0].Summary, "wttr.in") {
		t.Fatalf("expected session memory to mention wttr.in, got %+v", sessionItems[0])
	}
	if sessionItems[0].Scope != memstore.MemoryScopeSession || sessionItems[0].Kind != autoMemoryToolOutcomeKind {
		t.Fatalf("unexpected session memory shape: %+v", sessionItems[0])
	}

	targetPath := buildToolOutcomeTargetPath("http_request", detectMemoryTopics("杭州天气怎么样", "https://wttr.in/hangzhou?format=j1", "wttr.in"), "wttr.in")
	var consolidated *memstore.ExtendedMemoryRecord
	waitForCondition(t, 2*time.Second, func() bool {
		record, err := store.GetByPathExtended(ctx, targetPath)
		if err != nil {
			return false
		}
		consolidated = record
		return consolidated.Stage == memstore.MemoryStageConsolidated
	})
	if consolidated == nil || !strings.Contains(consolidated.Content, "wttr.in") {
		t.Fatalf("expected consolidated repo memory for tool outcome, got %+v", consolidated)
	}
}

func TestAutomaticMemoryCapture_PromotesCommandStrategyFallbackLesson(t *testing.T) {
	ctx := context.Background()
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	capture := &automaticMemoryCapture{
		store:         store,
		workspaceRoot: ".",
		userID:        currentMemoryUserID(),
		toolContext: map[string]autoToolContext{
			"sess1:call-bash": {
				SessionID:  "sess1",
				CallID:     "call-bash",
				ToolName:   "run_command",
				Query:      "列出当前目录文件",
				Command:    "bash",
				Args:       []string{"-lc", "ls"},
				CapturedAt: time.Now().UTC(),
			},
			"sess1:call-powershell": {
				SessionID:  "sess1",
				CallID:     "call-powershell",
				ToolName:   "run_command",
				Query:      "列出当前目录文件",
				Command:    "powershell",
				Args:       []string{"-Command", "Get-ChildItem"},
				CapturedAt: time.Now().UTC(),
			},
		},
	}

	capture.captureExecutionEvent(ctx, observe.ExecutionEvent{
		Type:      observe.ExecutionToolCompleted,
		SessionID: "sess1",
		ToolName:  "run_command",
		CallID:    "call-bash",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"exit_code":      1,
			"stderr_preview": "bash: command not found",
		},
	})
	capture.captureExecutionEvent(ctx, observe.ExecutionEvent{
		Type:      observe.ExecutionToolCompleted,
		SessionID: "sess1",
		ToolName:  "run_command",
		CallID:    "call-powershell",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"exit_code":      0,
			"stdout_preview": "file1.go file2.go",
		},
	})

	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes: []memstore.MemoryScope{memstore.MemoryScopeRepo},
		Kinds:  []string{autoMemoryExecutionLessonKind},
		Limit:  8,
	})
	if err != nil {
		t.Fatalf("SearchExtended: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected command strategy lesson to be promoted")
	}
	if !strings.Contains(items[0].Summary, "PowerShell") || !strings.Contains(items[0].Summary, "bash-style") {
		t.Fatalf("expected strategy lesson summary to mention both strategies, got %+v", items[0])
	}
}

func TestReadMemory_ReconcilesProjectionIntoStore(t *testing.T) {
	ctx := context.Background()
	ws := sandbox.NewMemoryWorkspace()
	store := memstore.NewWorkspaceMemoryStore(ws)
	if err := ws.WriteFile(ctx, "team/legacy.txt", []byte("legacy memory")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reg := bootMemoryTestRegistry(t, ws, store, nil)
	readTool, ok := reg.Get("read_memory")
	if !ok {
		t.Fatal("read_memory not registered")
	}
	raw, err := readTool.Execute(ctx, json.RawMessage(`{"path":"team/legacy.txt"}`))
	if err != nil {
		t.Fatalf("read_memory: %v", err)
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode read_memory: %v", err)
	}
	if content != "legacy memory" {
		t.Fatalf("content = %q", content)
	}
	record, err := store.GetByPathExtended(ctx, "team/legacy.txt")
	if err != nil {
		t.Fatalf("GetByPath: %v", err)
	}
	if record == nil || record.SourceKind != "projection.reconciled" {
		t.Fatalf("unexpected reconciled record: %+v", record)
	}
}

func TestWithMemoryStoreWithoutWorkspace_BootFails(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryStore(memstore.NewWorkspaceMemoryStore(sandbox.NewMemoryWorkspace())),
	)
	err := k.Boot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "memory workspace is nil") {
		t.Fatalf("expected memory workspace error, got %v", err)
	}
}

func TestWithWorkspace_BootAndPrompt(t *testing.T) {
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(sandbox.NewMemoryWorkspace()),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	if _, ok := k.ToolRegistry().Get("read_memory"); !ok {
		t.Fatal("read_memory should be registered after boot")
	}

	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "test"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(sess.Messages) == 0 {
		t.Fatal("expected system prompt message")
	}
	if got := model.ContentPartsToPlainText(sess.Messages[0].ContentParts); !strings.Contains(got, "staged persistent memory tools") {
		t.Fatalf("expected memory prompt hint, got %q", got)
	}
}

func TestWithMemoryWorkspace_BootInitializesPipeline(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	k := kernel.New(
		kernel.WithLLM(&kt.MockLLM{}),
		kernel.WithUserIO(&io.NoOpIO{}),
		WithMemoryWorkspace(ws),
		WithMemoryStore(memstore.NewWorkspaceMemoryStore(ws)),
	)
	st := ensureMemoryState(k)
	if st.pipeline != nil {
		t.Fatal("expected memory pipeline to be nil before boot")
	}
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	t.Cleanup(func() {
		_ = k.Shutdown(context.Background())
	})
	if ensureMemoryState(k).pipeline == nil {
		t.Fatal("expected memory pipeline to be initialized on boot")
	}
	if _, ok := k.ToolRegistry().Get("read_memory"); !ok {
		t.Fatal("expected read_memory to remain registered")
	}
}

func TestStructuredMemoryTools_RecordAndSearch(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistry(t, ws, nil, nil)
	ctx := context.Background()
	wrTool, ok := reg.Get("write_memory_record")
	if !ok {
		t.Fatal("write_memory_record not registered")
	}
	writeRecord := wrTool.Execute
	rrTool, ok := reg.Get("read_memory_record")
	if !ok {
		t.Fatal("read_memory_record not registered")
	}
	readRecord := rrTool.Execute
	smTool, ok := reg.Get("search_memories")
	if !ok {
		t.Fatal("search_memories not registered")
	}
	searchMemories := smTool.Execute

	writeInput, _ := json.Marshal(map[string]any{
		"path":        "team/decision.md",
		"content":     "We decided to use sqlite backend for state queries.",
		"tags":        []string{"architecture", "state"},
		"scope":       "repo",
		"repo_id":     "repo-main",
		"kind":        "decision",
		"fingerprint": "state-backend",
		"confidence":  0.95,
		"expires_at":  time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
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
	},
	)
	if _, err := writeRecord(ctx, writeInput); err != nil {
		t.Fatalf("write_memory_record failed: %v", err)
	}

	recordRaw := waitForMemoryRecord(t, ctx, readRecord, "team/decision.md")
	var record memstore.ExtendedMemoryRecord
	if err := json.Unmarshal(recordRaw, &record); err != nil {
		t.Fatalf("decode read_memory_record: %v", err)
	}
	if record.Summary == "" {
		t.Fatalf("expected generated summary, got %+v", record)
	}
	if record.Scope != memstore.MemoryScopeRepo || record.RepoID != "repo-main" || record.Kind != "decision" {
		t.Fatalf("expected typed fields to round-trip, got %+v", record)
	}
	if record.UsageCount < 1 {
		t.Fatalf("expected read to bump usage, got %+v", record)
	}

	searchInput, _ := json.Marshal(map[string]any{
		"query":          "backend state sqlite",
		"scopes":         []string{"repo"},
		"repo_id":        "repo-main",
		"kinds":          []string{"decision"},
		"fingerprint":    "state-backend",
		"min_confidence": 0.9,
		"not_expired_at": time.Now().UTC().Format(time.RFC3339),
		"sort_by":        "score",
		"limit":          5,
	})
	searchRaw, err := searchMemories(ctx, searchInput)
	if err != nil {
		t.Fatalf("search_memories failed: %v", err)
	}
	var searchResp struct {
		Count int                             `json:"count"`
		Items []memstore.ExtendedMemoryRecord `json:"items"`
	}
	if err := json.Unmarshal(searchRaw, &searchResp); err != nil {
		t.Fatalf("decode search_memories: %v", err)
	}
	if searchResp.Count != 1 || len(searchResp.Items) != 1 {
		t.Fatalf("unexpected search result: %+v", searchResp)
	}
	if searchResp.Items[0].UsageCount < 2 {
		t.Fatalf("expected search to bump usage, got %+v", searchResp.Items[0])
	}
}

func TestStructuredMemoryTools_SearchMemories_MatchesMixedLanguageKeywords(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistry(t, ws, nil, nil)
	ctx := context.Background()
	wrTool, ok := reg.Get("write_memory_record")
	if !ok {
		t.Fatal("write_memory_record not registered")
	}
	smTool, ok := reg.Get("search_memories")
	if !ok {
		t.Fatal("search_memories not registered")
	}
	writeRecord := wrTool.Execute
	searchMemories := smTool.Execute

	writeInput, _ := json.Marshal(map[string]any{
		"path":        "auto/tool_outcomes/http_request-weather-wttr-in.md",
		"content":     "Prefer wttr.in weather API for 天气 requests.",
		"summary":     "Prefer wttr.in for weather/天气 API requests.",
		"tags":        []string{"weather", "天气", "wttr.in", "api"},
		"scope":       "repo",
		"repo_id":     "repo-main",
		"kind":        "trace_consolidated",
		"fingerprint": "http_request:weather:wttr.in",
		"confidence":  0.9,
		"stage":       "consolidated",
		"status":      "active",
	})
	if _, err := writeRecord(ctx, writeInput); err != nil {
		t.Fatalf("write_memory_record failed: %v", err)
	}

	searchInput, _ := json.Marshal(map[string]any{
		"query":   "天气 API weather",
		"repo_id": "repo-main",
		"limit":   5,
	})
	searchRaw, err := searchMemories(ctx, searchInput)
	if err != nil {
		t.Fatalf("search_memories failed: %v", err)
	}
	var searchResp struct {
		Count int                             `json:"count"`
		Items []memstore.ExtendedMemoryRecord `json:"items"`
	}
	if err := json.Unmarshal(searchRaw, &searchResp); err != nil {
		t.Fatalf("decode search_memories: %v", err)
	}
	if searchResp.Count == 0 || len(searchResp.Items) == 0 {
		t.Fatalf("expected mixed-language search to find weather memory, got %+v", searchResp)
	}
	if searchResp.Items[0].Path != "auto/tool_outcomes/http_request-weather-wttr-in.md" {
		t.Fatalf("unexpected top search result: %+v", searchResp.Items[0])
	}
}

func TestStructuredMemoryTools_IngestMemoryTrace(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistry(t, ws, nil, nil)
	ctx := context.Background()
	itTool, ok := reg.Get("ingest_memory_trace")
	if !ok {
		t.Fatal("ingest_memory_trace not registered")
	}
	ingestTrace := itTool.Execute
	rrTool, ok := reg.Get("read_memory_record")
	if !ok {
		t.Fatal("read_memory_record not registered")
	}
	readRecord := rrTool.Execute

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
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	if resp.Status != "queued" || resp.JobID == "" {
		t.Fatalf("unexpected ingest response: %+v", resp)
	}

	recordRaw := waitForMemoryRecord(t, ctx, readRecord, "team/memory/trace-summary.md")
	var record memstore.ExtendedMemoryRecord
	if err := json.Unmarshal(recordRaw, &record); err != nil {
		t.Fatalf("decode read_memory_record: %v", err)
	}
	if record.Stage != memstore.MemoryStageConsolidated {
		t.Fatalf("expected consolidated stage, got %+v", record)
	}
	if len(record.Citation.MemoryPaths) == 0 {
		t.Fatalf("expected snapshot citations, got %+v", record.Citation)
	}
	memorySummary, err := ws.ReadFile(ctx, "memory_summary.md")
	waitForCondition(t, 2*time.Second, func() bool {
		memorySummary, err = ws.ReadFile(ctx, "memory_summary.md")
		return err == nil && strings.Contains(string(memorySummary), "team/memory/trace-summary.md")
	})
}

func TestStructuredMemoryTools_PromotesCorroboratedFacts(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistry(t, ws, nil, nil)
	ctx := context.Background()
	itTool, ok := reg.Get("ingest_memory_trace")
	if !ok {
		t.Fatal("ingest_memory_trace not registered")
	}
	ingestTrace := itTool.Execute
	rrTool, ok := reg.Get("read_memory_record")
	if !ok {
		t.Fatal("read_memory_record not registered")
	}
	readRecord := rrTool.Execute
	targetPath := "team/memory/promoted-fact.md"
	for idx, trace := range []string{
		`{"type":"message","role":"user","content":"Use sqlite for runtime state"}`,
		`{"type":"message","role":"assistant","content":"Confirmed sqlite remains the runtime state backend"}`,
	} {
		input, _ := json.Marshal(map[string]any{
			"source_path": fmt.Sprintf("trace/session-%d.jsonl", idx+1),
			"trace":       trace,
			"target_path": targetPath,
			"tags":        []string{"trace", "decision"},
		})
		if _, err := ingestTrace(ctx, input); err != nil {
			t.Fatalf("ingest_memory_trace(%d): %v", idx+1, err)
		}
	}
	recordRaw := waitForMemoryRecord(t, ctx, readRecord, "promoted/promoted-fact.md")
	var record memstore.ExtendedMemoryRecord
	if err := json.Unmarshal(recordRaw, &record); err != nil {
		t.Fatalf("decode promoted record: %v", err)
	}
	if record.Stage != memstore.MemoryStagePromoted || record.SourceKind != "promotion" {
		t.Fatalf("unexpected promoted record: %+v", record)
	}
	if !strings.Contains(record.Content, "confidence: 1.0") {
		t.Fatalf("expected promoted confidence in content, got %q", record.Content)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		registry, err := ws.ReadFile(ctx, "MEMORY.md")
		return err == nil && strings.Contains(string(registry), "promoted/promoted-fact.md")
	})
}

func TestWriteMemory_SyncsDerivedArtifacts(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	reg := bootMemoryTestRegistry(t, ws, nil, nil)
	ctx := context.Background()
	wmTool, ok := reg.Get("write_memory")
	if !ok {
		t.Fatal("write_memory not registered")
	}
	writeMemory := wmTool.Execute
	if _, err := writeMemory(ctx, mustJSON(t, map[string]any{
		"path":    "team/manual-decision.md",
		"content": "Use sqlite for state queries.",
	})); err != nil {
		t.Fatalf("write_memory: %v", err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		registry, err := ws.ReadFile(ctx, "MEMORY.md")
		if err != nil || !strings.Contains(string(registry), "team/manual-decision.md") {
			return false
		}
		summary, err := ws.ReadFile(ctx, "memory_summary.md")
		return err == nil && strings.Contains(string(summary), "team/manual-decision.md")
	})
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

	_, err = store.UpsertExtended(ctx, memstore.ExtendedMemoryRecord{
		Path:    "team/decision.md",
		Content: "Use sqlite memory backend",
		Tags:    []string{"state", "sqlite"},
	})
	if err != nil {
		t.Fatalf("UpsertExtended: %v", err)
	}

	got, err := store.GetByPathExtended(ctx, "team/decision.md")
	if err != nil {
		t.Fatalf("GetByPathExtended: %v", err)
	}
	if got.Summary == "" {
		t.Fatal("expected summary to be generated")
	}
	if got.Metadata != nil {
		t.Fatalf("expected empty metadata by default, got %+v", got.Metadata)
	}
	if got.Stage != memstore.MemoryStageManual || got.Status != memstore.MemoryStatusActive {
		t.Fatalf("expected default stage/status, got %+v", got)
	}
	if got.Scope != memstore.MemoryScopeRepo {
		t.Fatalf("expected default repo scope, got %+v", got)
	}
	if got.Fingerprint != "team/decision.md" {
		t.Fatalf("expected default fingerprint to match path, got %+v", got)
	}
	if err := store.RecordUsage(ctx, []string{"team/decision.md"}, time.Now().UTC()); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	got, err = store.GetByPathExtended(ctx, "team/decision.md")
	if err != nil {
		t.Fatalf("GetByPathExtended after usage: %v", err)
	}
	if got.UsageCount != 1 || got.LastUsedAt.IsZero() {
		t.Fatalf("expected usage tracking, got %+v", got)
	}

	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{Query: "sqlite", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(items) != 1 || !strings.HasSuffix(strings.ReplaceAll(items[0].Path, "\\", "/"), "team/decision.md") {
		t.Fatalf("unexpected search result: %+v", items)
	}

	if err := store.DeleteByPath(ctx, "team/decision.md"); err != nil {
		t.Fatalf("DeleteByPath: %v", err)
	}
	if _, err := store.GetByPathExtended(ctx, "team/decision.md"); err == nil {
		t.Fatal("expected GetByPath to fail after delete")
	}
}

func TestSQLiteMemoryStore_OpensLegacySchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory-legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE memory_records (
  path TEXT PRIMARY KEY,
  id TEXT NOT NULL,
  content TEXT NOT NULL,
  summary TEXT NOT NULL,
  tags_json TEXT NOT NULL,
  citation_json TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  stage TEXT NOT NULL,
  status TEXT NOT NULL,
  group_key TEXT NOT NULL,
  workspace TEXT NOT NULL,
  cwd TEXT NOT NULL,
  git_branch TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  source_id TEXT NOT NULL,
  source_path TEXT NOT NULL,
  source_updated_at TEXT NOT NULL,
  usage_count INTEGER NOT NULL,
  last_used_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO memory_records(
  path,id,content,summary,tags_json,citation_json,metadata_json,stage,status,group_key,workspace,cwd,git_branch,
  source_kind,source_id,source_path,source_updated_at,usage_count,last_used_at,created_at,updated_at
) VALUES (
  'legacy/note.md','legacy-id','legacy content','legacy summary','[]','[]','{}','manual','active','','','','','','','','',0,'','',''
);
`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore with legacy schema: %v", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	ctx := context.Background()
	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("SearchExtended legacy schema: %v", err)
	}
	if len(items) != 1 || items[0].Path != "legacy/note.md" {
		t.Fatalf("unexpected legacy records after migration: %+v", items)
	}
	if _, err := store.UpsertExtended(ctx, memstore.ExtendedMemoryRecord{
		Path:      "repo/new-note.md",
		Content:   "new content",
		Scope:     memstore.MemoryScopeRepo,
		RepoID:    "repo-main",
		Kind:      "manual_note",
		Stage:     memstore.MemoryStageManual,
		Status:    memstore.MemoryStatusActive,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertExtended after migration: %v", err)
	}
	filtered, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes: []memstore.MemoryScope{memstore.MemoryScopeRepo},
		RepoID: "repo-main",
		Kinds:  []string{"manual_note"},
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("SearchExtended typed query after migration: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Path != "repo/new-note.md" {
		t.Fatalf("unexpected typed query results after migration: %+v", filtered)
	}
}

func TestSQLiteMemoryStore_PersistsMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory-meta.db")
	store, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore: %v", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	ctx := context.Background()
	_, err = store.UpsertExtended(ctx, memstore.ExtendedMemoryRecord{
		Path:       "knowledge/readme/chunk-0000.md",
		Content:    "runtime-owned memory",
		SourceKind: "knowledge.document",
		Metadata: map[string]any{
			"doc_id":    "readme",
			"embedding": []float64{1, 2, 3},
		},
	})
	if err != nil {
		t.Fatalf("UpsertExtended: %v", err)
	}
	got, err := store.GetByPathExtended(ctx, "knowledge/readme/chunk-0000.md")
	if err != nil {
		t.Fatalf("GetByPathExtended: %v", err)
	}
	if got.Metadata == nil || got.Metadata["doc_id"] != "readme" {
		t.Fatalf("expected metadata to round-trip, got %+v", got.Metadata)
	}
	embedding, ok := got.Metadata["embedding"].([]any)
	if !ok || len(embedding) != 3 {
		t.Fatalf("expected embedding metadata to round-trip, got %+v", got.Metadata["embedding"])
	}
}

func TestSQLiteMemoryStore_SearchRanksByUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory-rank.db")
	store, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore: %v", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	ctx := context.Background()
	for _, record := range []memstore.ExtendedMemoryRecord{
		{Path: "team/alpha.md", Content: "sqlite backend decision alpha"},
		{Path: "team/beta.md", Content: "sqlite backend decision beta"},
	} {
		if _, err := store.UpsertExtended(ctx, record); err != nil {
			t.Fatalf("Upsert %s: %v", record.Path, err)
		}
	}
	if err := store.RecordUsage(ctx, []string{"team/beta.md"}, time.Now().UTC()); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := store.RecordUsage(ctx, []string{"team/beta.md"}, time.Now().UTC().Add(time.Millisecond)); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{Query: "sqlite backend", Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(items) != 2 || memstore.NormalizePath(items[0].Path) != "team/beta.md" {
		t.Fatalf("expected usage-ranked result first, got %+v", items)
	}
}

func TestSQLiteMemoryStore_SearchByTypedFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory-typed.db")
	store, err := NewSQLiteMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMemoryStore: %v", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	ctx := context.Background()
	now := time.Now().UTC()
	for _, record := range []memstore.ExtendedMemoryRecord{
		{
			Path:        "session/weather-a.md",
			Content:     "Prefer wttr for weather lookups.",
			Scope:       memstore.MemoryScopeSession,
			SessionID:   "sess-1",
			RepoID:      "repo-main",
			Kind:        "tool_strategy",
			Fingerprint: "weather-api",
			Confidence:  0.9,
			ExpiresAt:   now.Add(2 * time.Hour),
		},
		{
			Path:        "repo/weather-b.md",
			Content:     "Prefer internal API.",
			Scope:       memstore.MemoryScopeRepo,
			RepoID:      "repo-main",
			Kind:        "tool_strategy",
			Fingerprint: "weather-api",
			Confidence:  0.8,
			ExpiresAt:   now.Add(2 * time.Hour),
		},
		{
			Path:        "repo/weather-expired.md",
			Content:     "Old weather choice.",
			Scope:       memstore.MemoryScopeRepo,
			RepoID:      "repo-main",
			Kind:        "tool_strategy",
			Fingerprint: "weather-api-old",
			Confidence:  0.95,
			ExpiresAt:   now.Add(-2 * time.Hour),
		},
	} {
		if _, err := store.UpsertExtended(ctx, record); err != nil {
			t.Fatalf("Upsert %s: %v", record.Path, err)
		}
	}

	items, err := store.SearchExtended(ctx, memstore.ExtendedMemoryQuery{
		Scopes:        []memstore.MemoryScope{memstore.MemoryScopeSession},
		SessionID:     "sess-1",
		RepoID:        "repo-main",
		Kinds:         []string{"tool_strategy"},
		Fingerprint:   "weather-api",
		MinConfidence: 0.85,
		NotExpiredAt:  now,
		SortBy:        memstore.MemorySortByScore,
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("SearchExtended: %v", err)
	}
	if len(items) != 1 || items[0].Path != "session/weather-a.md" {
		t.Fatalf("unexpected typed query results: %+v", items)
	}
}

func TestIngestMemoryTrace_WithAtomicJobRuntime(t *testing.T) {
	ws := sandbox.NewMemoryWorkspace()
	taskRuntime := taskrt.NewMemoryTaskRuntime()
	if err := taskRuntime.UpsertJob(context.Background(), taskrt.AgentJob{
		ID:        "job-mem",
		AgentName: "worker",
		Goal:      "summarize",
		Status:    taskrt.JobPending,
	}); err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if _, err := taskRuntime.MarkJobItemRunning(context.Background(), "job-mem", "item-1", "exec-a"); err != nil {
		t.Fatalf("MarkJobItemRunning: %v", err)
	}
	reg := bootMemoryTestRegistry(t, ws, memstore.NewWorkspaceMemoryStore(ws), taskRuntime)
	ctx := context.Background()
	itTool, ok := reg.Get("ingest_memory_trace")
	if !ok {
		t.Fatal("ingest_memory_trace not registered")
	}
	ingestTrace := itTool.Execute
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
	var resp struct {
		Status string `json:"status"`
		JobID  string `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	if resp.Status != "queued" || resp.JobID == "" {
		t.Fatalf("expected queued response, got %s", string(raw))
	}

	waitForCondition(t, 2*time.Second, func() bool {
		items, err := taskRuntime.ListJobItems(ctx, taskrt.JobItemQuery{JobID: "job-mem"})
		if err != nil || len(items) != 1 {
			return false
		}
		return items[0].Status == taskrt.JobCompleted
	})
	items, err := taskRuntime.ListJobItems(ctx, taskrt.JobItemQuery{JobID: "job-mem"})
	if err != nil {
		t.Fatalf("ListJobItems: %v", err)
	}
	if items[0].Status != taskrt.JobCompleted {
		t.Fatalf("expected completed external job item, got %+v", items)
	}
}

func waitForMemoryRecord(t *testing.T, ctx context.Context, handler tool.ToolHandler, path string) json.RawMessage {
	t.Helper()
	var (
		lastErr error
		raw     json.RawMessage
	)
	waitForCondition(t, 2*time.Second, func() bool {
		input, _ := json.Marshal(map[string]string{"path": path})
		value, err := handler(ctx, input)
		if err != nil {
			lastErr = err
			return false
		}
		raw = value
		return true
	})
	if raw == nil {
		t.Fatalf("memory record %s not ready: %v", path, lastErr)
	}
	return raw
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}
