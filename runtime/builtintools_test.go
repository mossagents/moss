package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mossagents/moss/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
)

// ── mock sandbox ─────────────────────────────────────

type mockSandbox struct {
	root        string
	files       map[string]string // path → content (absolute paths)
	lastExecReq workspace.ExecRequest
}

func newMockSandbox(root string, files map[string]string) *mockSandbox {
	originalRoot := root
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	normalized := make(map[string]string, len(files))
	for path, content := range files {
		switch {
		case strings.HasPrefix(path, originalRoot+"/"):
			normalized[root+path[len(originalRoot):]] = content
		case strings.HasPrefix(path, originalRoot+"\\"):
			normalized[root+path[len(originalRoot):]] = content
		case filepath.IsAbs(path):
			normalized[path] = content
		default:
			normalized[filepath.Join(root, path)] = content
		}
	}
	return &mockSandbox{root: root, files: normalized}
}

func (m *mockSandbox) ResolvePath(path string) (string, error) {
	if path == "." {
		return m.root, nil
	}
	if strings.HasPrefix(path, m.root) {
		return path, nil
	}
	return m.root + "/" + path, nil
}

func (m *mockSandbox) ListFiles(pattern string) ([]string, error) {
	var result []string
	for p := range m.files {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockSandbox) ReadFile(path string) ([]byte, error) {
	resolved, _ := m.ResolvePath(path)
	if content, ok := m.files[resolved]; ok {
		return []byte(content), nil
	}
	return nil, &notFoundError{path: path}
}

func (m *mockSandbox) WriteFile(path string, content []byte) error {
	resolved, _ := m.ResolvePath(path)
	m.files[resolved] = string(content)
	return nil
}

func (m *mockSandbox) Execute(_ context.Context, req workspace.ExecRequest) (sandbox.Output, error) {
	m.lastExecReq = req
	return sandbox.Output{
		Stdout:   "mock output for: " + req.Command + " " + strings.Join(req.Args, " "),
		ExitCode: 0,
	}, nil
}

func (m *mockSandbox) Limits() sandbox.ResourceLimits {
	return sandbox.ResourceLimits{}
}

type notFoundError struct{ path string }

func (e *notFoundError) Error() string { return "file not found: " + e.path }

// ── mock user IO ─────────────────────────────────────

type mockUserIO struct {
	lastQuestion string
	response     string
	lastReq      io.InputRequest
	formResponse map[string]any
}

func (m *mockUserIO) Send(_ context.Context, _ io.OutputMessage) error { return nil }

func (m *mockUserIO) Ask(_ context.Context, req io.InputRequest) (io.InputResponse, error) {
	m.lastQuestion = req.Prompt
	m.lastReq = req
	if req.Type == io.InputForm {
		if m.formResponse != nil {
			return io.InputResponse{Form: m.formResponse}, nil
		}
		return io.InputResponse{Form: map[string]any{}}, nil
	}
	return io.InputResponse{Value: m.response}, nil
}

// ── mock workspace ───────────────────────────────────

type mockWorkspace struct {
	files map[string]string
}

func (m *mockWorkspace) ReadFile(_ context.Context, path string) ([]byte, error) {
	if content, ok := m.files[path]; ok {
		return []byte(content), nil
	}
	return nil, &notFoundError{path: path}
}

func (m *mockWorkspace) WriteFile(_ context.Context, path string, content []byte) error {
	m.files[path] = string(content)
	return nil
}

func (m *mockWorkspace) ListFiles(_ context.Context, _ string) ([]string, error) {
	var result []string
	for p := range m.files {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockWorkspace) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return workspace.FileInfo{Name: path, Size: int64(len(m.files[path]))}, nil
	}
	return workspace.FileInfo{}, &notFoundError{path: path}
}

func (m *mockWorkspace) DeleteFile(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}

// ── mock executor ────────────────────────────────────

type mockExecutor struct {
	lastReq workspace.ExecRequest
}

func (m *mockExecutor) Execute(_ context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	m.lastReq = req
	return workspace.ExecOutput{
		Stdout:   "exec: " + req.Command + " " + strings.Join(req.Args, " "),
		ExitCode: 0,
	}, nil
}

type mockExecutorLarge struct {
	stdout string
}

func (m *mockExecutorLarge) Execute(_ context.Context, _ workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{
		Stdout:   m.stdout,
		ExitCode: 0,
	}, nil
}

// ── tests ────────────────────────────────────────────

func TestRegisterBuiltinTools(t *testing.T) {
	reg := tool.NewRegistry()
	sb := newMockSandbox("/ws", nil)
	io := &mockUserIO{}

	if err := RegisterBuiltinTools(reg, sb, io, nil, nil); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	expected := []string{"read_file", "write_file", "edit_file", "glob", "ls", "grep", "run_command", "http_request", "datetime", "ask_user"}
	specs := reg.List()
	if len(specs) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(specs))
	}

	names := make(map[string]bool)
	for _, s := range specs {
		names[s.Name()] = true
	}
	for _, e := range expected {
		if !names[e] {
			t.Errorf("missing tool: %s", e)
		}
	}
}

func newBuiltinToolsKernel(sb sandbox.Sandbox, userIO io.UserIO, ws workspace.Workspace, exec workspace.Executor) *kernel.Kernel {
	opts := make([]kernel.Option, 0, 4)
	if userIO != nil {
		opts = append(opts, kernel.WithUserIO(userIO))
	}
	if sb != nil {
		opts = append(opts, kernel.WithSandbox(sb))
	}
	if ws != nil {
		opts = append(opts, kernel.WithWorkspace(ws))
	}
	if exec != nil {
		opts = append(opts, kernel.WithExecutor(exec))
	}
	return kernel.New(opts...)
}

func newKernelWithToolPolicy(t *testing.T, policy ToolPolicy) *kernel.Kernel {
	t.Helper()
	k := kernel.New()
	payload, err := EncodeToolPolicyMetadata(policy)
	if err != nil {
		t.Fatalf("EncodeToolPolicyMetadata: %v", err)
	}
	policystate.Ensure(k).Set(payload, session.EncodeToolPolicySummary(SummarizeToolPolicy(policy)), nil)
	return k
}

func RegisterBuiltinTools(reg tool.Registry, sb sandbox.Sandbox, userIO io.UserIO, ws workspace.Workspace, exec workspace.Executor) error {
	return RegisterBuiltinToolsForKernel(newBuiltinToolsKernel(sb, userIO, ws, exec), reg)
}

func readFileHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return readFileHandlerPort(k.Workspace())
}

func writeFileHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return writeFileHandlerPort(k.Workspace())
}

func editFileHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return editFileHandlerPort(k.Workspace())
}

func globHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return globHandlerPort(k.Workspace(), sandboxRoot(k.Sandbox()))
}

func listFilesHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return listFilesHandlerPort(k.Workspace(), sandboxRoot(k.Sandbox()))
}

func grepHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return grepHandlerPort(k.Workspace(), sandboxRoot(k.Sandbox()))
}

func runCommandHandler(sb sandbox.Sandbox) tool.ToolHandler {
	k := newBuiltinToolsKernel(sb, nil, nil, nil)
	return runCommandHandlerWithExecutor(k, k.Executor(), k.Workspace(), sandboxRoot(k.Sandbox()))
}

func runCommandHandlerWithPolicy(k *kernel.Kernel, sb sandbox.Sandbox) tool.ToolHandler {
	sandboxKernel := newBuiltinToolsKernel(sb, nil, nil, nil)
	return runCommandHandlerWithExecutor(k, sandboxKernel.Executor(), sandboxKernel.Workspace(), sandboxRoot(sandboxKernel.Sandbox()))
}

func globHandlerWS(ws workspace.Workspace) tool.ToolHandler {
	return globHandlerPort(ws, "")
}

func listFilesHandlerWS(ws workspace.Workspace) tool.ToolHandler {
	return listFilesHandlerPort(ws, "")
}

func grepHandlerWS(ws workspace.Workspace) tool.ToolHandler {
	return grepHandlerPort(ws, "")
}

func TestReadFile(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/hello.txt": "Hello, World!",
	})
	handler := readFileHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"path": "hello.txt"}))
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}

	var content string
	if err := json.Unmarshal(result, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", content)
	}
}

func TestReadFileNotFound(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{})
	handler := readFileHandler(sb)

	_, err := handler(context.Background(), toJSON(t, map[string]string{"path": "missing.txt"}))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteFile(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{})
	handler := writeFileHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"path":    "out.txt",
		"content": "new content",
	}))
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %q", resp["status"])
	}
	outPath, _ := sb.ResolvePath("out.txt")
	if sb.files[outPath] != "new content" {
		t.Errorf("file not written, files: %v", sb.files)
	}
}

func TestEditFile(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/doc.txt": "hello moss",
	})
	handler := editFileHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"path":       "doc.txt",
		"old_string": "moss",
		"new_string": "world",
	}))
	if err != nil {
		t.Fatalf("editFile: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	docPath, _ := sb.ResolvePath("doc.txt")
	if sb.files[docPath] != "hello world" {
		t.Errorf("unexpected edited content: %q", sb.files[docPath])
	}
}

func TestEditFileRequireReplaceAll(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/doc.txt": "moss moss",
	})
	handler := editFileHandler(sb)

	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"path":       "doc.txt",
		"old_string": "moss",
		"new_string": "x",
	}))
	if err == nil {
		t.Fatal("expected replace_all guidance error")
	}
}

func TestGlobTool(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/a.go":        "",
		"/ws/sub/b.go":    "",
		"/ws/sub/readme":  "",
		"/ws/notes/test":  "",
		"/ws/notes/x.txt": "",
	})
	handler := globHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"pattern": "**/*.go",
		"path":    ".",
	}))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	var files []string
	if err := json.Unmarshal(result, &files); err != nil {
		t.Fatalf("unmarshal files: %v", err)
	}
	if len(files) != 5 {
		t.Errorf("expected 5 files from mock list, got %d", len(files))
	}
}

func TestListFiles(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/a.go":      "",
		"/ws/b.go":      "",
		"/ws/dir/c.txt": "",
	})
	handler := listFilesHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "*"}))
	if err != nil {
		t.Fatalf("listFiles: %v", err)
	}

	var files []string
	if err := json.Unmarshal(result, &files); err != nil {
		t.Fatalf("unmarshal files: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(files), files)
	}
}

func TestListFilesExcludesHiddenByDefault(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/a.go":         "",
		"/ws/.git/config":  "",
		"/ws/.moss/x.json": "",
	})
	handler := listFilesHandler(sb)
	result, err := handler(context.Background(), toJSON(t, map[string]any{"pattern": "**/*"}))
	if err != nil {
		t.Fatalf("listFiles hidden filter: %v", err)
	}
	var files []string
	_ = json.Unmarshal(result, &files)
	if len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("expected only a.go, got %v", files)
	}
}

func TestListFilesMaxResults(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/a.go": "", "/ws/b.go": "", "/ws/c.go": "",
	})
	handler := listFilesHandler(sb)
	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"pattern":     "**/*",
		"max_results": 2,
	}))
	if err != nil {
		t.Fatalf("listFiles max_results: %v", err)
	}
	var files []string
	_ = json.Unmarshal(result, &files)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d (%v)", len(files), files)
	}
}

func TestSearchText(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/main.go":  "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"/ws/other.go": "package other\n\nfunc other() {}\n",
	})
	handler := grepHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "main"}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}

	var matches []searchMatch
	if err := json.Unmarshal(result, &matches); err != nil {
		t.Fatalf("unmarshal matches: %v", err)
	}

	// Should find "package main" and "func main()" in main.go
	found := 0
	for _, m := range matches {
		if m.File == "main.go" {
			found++
		}
	}
	if found < 2 {
		t.Errorf("expected at least 2 matches in main.go, got %d. All matches: %+v", found, matches)
	}
}

func TestSearchTextRegex(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/main.go": "func main() {}\n",
		"/ws/lib.go":  "func helper() {}\n",
	})
	handler := grepHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": `^func main\(`}))
	if err != nil {
		t.Fatalf("grep regex: %v", err)
	}

	var matches []searchMatch
	if err := json.Unmarshal(result, &matches); err != nil {
		t.Fatalf("unmarshal matches: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 regex match, got %d: %+v", len(matches), matches)
	}
	if matches[0].File != "main.go" {
		t.Fatalf("expected main.go match, got %+v", matches[0])
	}
}

func TestSearchTextInvalidRegex(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/main.go": "func main() {}\n",
	})
	handler := grepHandler(sb)

	_, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "("}))
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
	if !strings.Contains(err.Error(), "invalid regex pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchTextMaxResults(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/data.txt": "aaa\naaa\naaa\naaa\naaa\naaa\naaa\naaa\naaa\naaa\n",
	})
	handler := grepHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"pattern":     "aaa",
		"max_results": 3,
	}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}

	var matches []searchMatch
	if err := json.Unmarshal(result, &matches); err != nil {
		t.Fatalf("unmarshal matches: %v", err)
	}
	if len(matches) != 3 {
		t.Errorf("expected 3 matches with max_results=3, got %d", len(matches))
	}
}

func TestRunCommand(t *testing.T) {
	sb := newMockSandbox("/ws", nil)
	handler := runCommandHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}

	var output sandbox.Output
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !strings.Contains(output.Stdout, "echo") {
		t.Errorf("expected stdout to contain command, got %q", output.Stdout)
	}
	if output.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", output.ExitCode)
	}
}

func TestRunCommandPolicyForwardingToSandbox(t *testing.T) {
	sb := newMockSandbox("/ws", nil)
	k := newKernelWithToolPolicy(t, ResolveToolPolicyForWorkspace("/ws", "restricted", "confirm"))
	handler := runCommandHandlerWithPolicy(k, sb)

	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if sb.lastExecReq.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", sb.lastExecReq.Timeout)
	}
	if sb.lastExecReq.WorkingDir != "." {
		t.Fatalf("working dir = %q, want .", sb.lastExecReq.WorkingDir)
	}
	if len(sb.lastExecReq.AllowedPaths) != 1 || sb.lastExecReq.AllowedPaths[0] != sb.root {
		t.Fatalf("allowed paths = %#v, want [%s]", sb.lastExecReq.AllowedPaths, sb.root)
	}
	if sb.lastExecReq.Network.Mode != workspace.ExecNetworkDisabled {
		t.Fatalf("network mode = %q, want %q", sb.lastExecReq.Network.Mode, workspace.ExecNetworkDisabled)
	}
}

func TestAskUser(t *testing.T) {
	io := &mockUserIO{response: "yes, proceed"}
	handler := askUserHandler(io)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"question": "Continue?"}))
	if err != nil {
		t.Fatalf("askUser: %v", err)
	}

	var answer string
	if err := json.Unmarshal(result, &answer); err != nil {
		t.Fatalf("unmarshal answer: %v", err)
	}
	if answer != "yes, proceed" {
		t.Errorf("expected 'yes, proceed', got %q", answer)
	}
	if io.lastQuestion != "Continue?" {
		t.Errorf("expected question 'Continue?', got %q", io.lastQuestion)
	}
}

func TestAskUserNilIO(t *testing.T) {
	handler := askUserHandler(nil)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"question": "hello?"}))
	if err != nil {
		t.Fatalf("askUser with nil IO: %v", err)
	}

	var answer string
	if err := json.Unmarshal(result, &answer); err != nil {
		t.Fatalf("unmarshal answer: %v", err)
	}
	if !strings.Contains(answer, "no user IO") {
		t.Errorf("expected fallback message, got %q", answer)
	}
}

func TestAskUserWithRequestedSchema(t *testing.T) {
	mockIO := &mockUserIO{
		formResponse: map[string]any{
			"database": "PostgreSQL",
			"cache":    true,
			"features": []string{"A", "C"},
		},
	}
	handler := askUserHandler(mockIO)
	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"question": "Choose options",
		"requestedSchema": map[string]any{
			"properties": map[string]any{
				"database": map[string]any{
					"type":  "string",
					"title": "Database",
					"enum":  []string{"PostgreSQL", "MySQL"},
				},
				"cache": map[string]any{
					"type":  "boolean",
					"title": "Enable Cache",
				},
				"features": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": []string{"A", "B", "C"},
					},
				},
			},
			"required": []string{"database"},
		},
	}))
	if err != nil {
		t.Fatalf("askUser with schema: %v", err)
	}
	if mockIO.lastReq.Type != io.InputForm {
		t.Fatalf("expected InputForm request, got %s", mockIO.lastReq.Type)
	}
	if len(mockIO.lastReq.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(mockIO.lastReq.Fields))
	}
	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out["database"] != "PostgreSQL" {
		t.Fatalf("unexpected database value: %v", out["database"])
	}
}

func TestAskUserWithRequestedSchemaStringRequired(t *testing.T) {
	mockIO := &mockUserIO{
		formResponse: map[string]any{
			"database": "PostgreSQL",
		},
	}
	handler := askUserHandler(mockIO)
	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"question": "Choose options",
		"requestedSchema": map[string]any{
			"properties": map[string]any{
				"database": map[string]any{
					"type": "string",
					"enum": []string{"PostgreSQL", "MySQL"},
				},
			},
			"required": []string{"database"},
		},
	}))
	if err != nil {
		t.Fatalf("askUser with string required: %v", err)
	}
	if len(mockIO.lastReq.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(mockIO.lastReq.Fields))
	}
	if !mockIO.lastReq.Fields[0].Required {
		t.Fatal("expected required field")
	}
}

func TestAskUserInputRetryUnexpectedEOF(t *testing.T) {
	mockIO := &mockUserIO{response: "ok"}
	handler := askUserHandler(mockIO)
	// Missing trailing brace should be auto-repaired for ask_user input.
	_, err := handler(context.Background(), json.RawMessage(`{"question":"Continue?","requestedSchema":{"properties":{"db":{"type":"string","enum":["a","b"]}},"required":["db"]}`))
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if mockIO.lastReq.Type != io.InputForm {
		t.Fatalf("expected InputForm after retry, got %s", mockIO.lastReq.Type)
	}
}

func TestToolRiskLevels(t *testing.T) {
	cases := []struct {
		name string
		risk tool.RiskLevel
	}{
		{"read_file", tool.RiskLow},
		{"write_file", tool.RiskHigh},
		{"edit_file", tool.RiskHigh},
		{"glob", tool.RiskLow},
		{"ls", tool.RiskLow},
		{"grep", tool.RiskLow},
		{"run_command", tool.RiskHigh},
		{"http_request", tool.RiskHigh},
		{"ask_user", tool.RiskLow},
	}

	reg := tool.NewRegistry()
	sb := newMockSandbox("/ws", nil)
	io := &mockUserIO{}
	if err := RegisterBuiltinTools(reg, sb, io, nil, nil); err != nil {
		t.Fatalf("RegisterBuiltinTools: %v", err)
	}

	for _, c := range cases {
		tl, ok := reg.Get(c.name)
		if !ok {
			t.Errorf("tool %q not found", c.name)
			continue
		}
		spec := tl.Spec()
		if spec.Risk != c.risk {
			t.Errorf("tool %q: expected risk %v, got %v", c.name, c.risk, spec.Risk)
		}
	}
}

func TestBuiltinToolExecutionMetadata(t *testing.T) {
	reg := tool.NewRegistry()
	sb := newMockSandbox("/ws", nil)
	if err := RegisterBuiltinTools(reg, sb, &mockUserIO{}, nil, nil); err != nil {
		t.Fatalf("RegisterBuiltinTools: %v", err)
	}
	cases := []struct {
		name           string
		effect         tool.Effect
		sideEffect     tool.SideEffectClass
		approvalClass  tool.ApprovalClass
		plannerVisible tool.PlannerVisibility
	}{
		{"read_file", tool.EffectReadOnly, tool.SideEffectNone, tool.ApprovalClassNone, tool.PlannerVisibilityVisible},
		{"write_file", tool.EffectWritesWorkspace, tool.SideEffectWorkspace, tool.ApprovalClassExplicitUser, tool.PlannerVisibilityVisibleWithConstraints},
		{"run_command", tool.EffectExternalSideEffect, tool.SideEffectProcess, tool.ApprovalClassExplicitUser, tool.PlannerVisibilityVisibleWithConstraints},
		{"http_request", tool.EffectExternalSideEffect, tool.SideEffectNetwork, tool.ApprovalClassExplicitUser, tool.PlannerVisibilityVisibleWithConstraints},
	}
	for _, tc := range cases {
		tl, ok := reg.Get(tc.name)
		if !ok {
			t.Fatalf("tool %q not found", tc.name)
		}
		spec := tl.Spec()
		if effects := spec.EffectiveEffects(); len(effects) != 1 || effects[0] != tc.effect {
			t.Fatalf("%s effects = %v", tc.name, effects)
		}
		if spec.SideEffectClass != tc.sideEffect {
			t.Fatalf("%s side_effect_class = %q", tc.name, spec.SideEffectClass)
		}
		if spec.ApprovalClass != tc.approvalClass {
			t.Fatalf("%s approval_class = %q", tc.name, spec.ApprovalClass)
		}
		if spec.PlannerVisibility != tc.plannerVisible {
			t.Fatalf("%s planner_visibility = %q", tc.name, spec.PlannerVisibility)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	sb := newMockSandbox("/ws", nil)
	cases := []struct {
		name    string
		handler tool.ToolHandler
	}{
		{"read_file", readFileHandler(sb)},
		{"write_file", writeFileHandler(sb)},
		{"edit_file", editFileHandler(sb)},
		{"glob", globHandler(sb)},
		{"ls", listFilesHandler(sb)},
		{"grep", grepHandler(sb)},
		{"run_command", runCommandHandler(sb)},
		{"http_request", httpRequestHandler()},
		{"ask_user", askUserHandler(&mockUserIO{})},
	}

	for _, c := range cases {
		_, err := c.handler(context.Background(), json.RawMessage(`{invalid json`))
		if err == nil {
			t.Errorf("%s: expected error for invalid JSON", c.name)
		}
	}
}

func TestHTTPRequestTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-Test") != "ok" {
			t.Fatalf("header X-Test = %q, want ok", r.Header.Get("X-Test"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	handler := httpRequestHandler()
	out, err := handler(context.Background(), toJSON(t, map[string]any{
		"url":     srv.URL,
		"method":  "POST",
		"headers": map[string]string{"X-Test": "ok"},
		"body":    `{"hello":"world"}`,
	}))
	if err != nil {
		t.Fatalf("http_request: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if int(resp["status_code"].(float64)) != http.StatusCreated {
		t.Fatalf("status_code = %v, want %d", resp["status_code"], http.StatusCreated)
	}
	if !strings.Contains(resp["body"].(string), `"ok":true`) {
		t.Fatalf("unexpected body: %s", resp["body"].(string))
	}
}

func TestHTTPRequestPolicyRejectsDisallowedMethod(t *testing.T) {
	k := newKernelWithToolPolicy(t, ResolveToolPolicyForWorkspace("/ws", "trusted", "full-auto"))
	handler := httpRequestHandlerWithPolicy(k)

	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"url":    "https://example.com",
		"method": "PUT",
	}))
	if err == nil {
		t.Fatal("expected method policy error")
	}
	if !strings.Contains(err.Error(), "not allowed by tool policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPRequestPolicyDisablesRedirectsByDefault(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirect.Close()

	k := newKernelWithToolPolicy(t, ResolveToolPolicyForWorkspace("/ws", "trusted", "full-auto"))
	handler := httpRequestHandlerWithPolicy(k)
	out, err := handler(context.Background(), toJSON(t, map[string]any{
		"url": redirect.URL,
	}))
	if err != nil {
		t.Fatalf("http_request: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if int(resp["status_code"].(float64)) != http.StatusFound {
		t.Fatalf("status_code = %v, want %d", resp["status_code"], http.StatusFound)
	}
	if followed, _ := resp["follow_redirects"].(bool); followed {
		t.Fatalf("expected follow_redirects=false, got %+v", resp)
	}
}

// ── helper ───────────────────────────────────────────

func toJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// ── Workspace/Executor handler tests ─────────────────

func TestRegisterAllWithWorkspace(t *testing.T) {
	reg := tool.NewRegistry()
	ws := &mockWorkspace{files: map[string]string{}}
	exec := &mockExecutor{}
	io := &mockUserIO{}

	if err := RegisterBuiltinTools(reg, nil, io, ws, exec); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	expected := []string{"read_file", "write_file", "edit_file", "glob", "ls", "grep", "run_command", "http_request", "datetime", "ask_user"}
	specs := reg.List()
	if len(specs) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(specs))
	}
}

func TestReadFileWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{"hello.txt": "Hello via Workspace!"}}
	handler := readFileHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"path": "hello.txt"}))
	if err != nil {
		t.Fatalf("readFileWS: %v", err)
	}

	var content string
	if err := json.Unmarshal(result, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content != "Hello via Workspace!" {
		t.Errorf("expected 'Hello via Workspace!', got %q", content)
	}
}

func TestWriteFileWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{}}
	handler := writeFileHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"path":    "out.txt",
		"content": "ws content",
	}))
	if err != nil {
		t.Fatalf("writeFileWS: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %q", resp["status"])
	}
	if ws.files["out.txt"] != "ws content" {
		t.Errorf("file not written via workspace")
	}
}

func TestEditFileWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{"a.txt": "hello moss"}}
	handler := editFileHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"path":       "a.txt",
		"old_string": "moss",
		"new_string": "team",
	}))
	if err != nil {
		t.Fatalf("editFileWS: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	if ws.files["a.txt"] != "hello team" {
		t.Errorf("unexpected content: %q", ws.files["a.txt"])
	}
}

func TestListFilesWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"a.go": "", "b.go": "", "dir/c.txt": "",
	}}
	handler := listFilesHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "*"}))
	if err != nil {
		t.Fatalf("listFilesWS: %v", err)
	}

	var files []string
	if err := json.Unmarshal(result, &files); err != nil {
		t.Fatalf("unmarshal files: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(files), files)
	}
}

func TestListFilesWSExcludesHiddenByDefault(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"a.go":         "",
		".git/config":  "",
		".moss/x.json": "",
	}}
	handler := listFilesHandlerWS(ws)
	result, err := handler(context.Background(), toJSON(t, map[string]any{"pattern": "**/*"}))
	if err != nil {
		t.Fatalf("listFilesWS hidden filter: %v", err)
	}
	var files []string
	if err := json.Unmarshal(result, &files); err != nil {
		t.Fatalf("unmarshal files: %v", err)
	}
	if len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("expected only a.go, got %v", files)
	}
}

func TestGlobWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"a.go": "",
		"b.go": "",
	}}
	handler := globHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"pattern": "*.go",
	}))
	if err != nil {
		t.Fatalf("globWS: %v", err)
	}

	var files []string
	if err := json.Unmarshal(result, &files); err != nil {
		t.Fatalf("unmarshal files: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestSearchTextWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"main.go":  "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"other.go": "package other\n\nfunc other() {}\n",
	}}
	handler := grepHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "main"}))
	if err != nil {
		t.Fatalf("grepWS: %v", err)
	}

	var matches []searchMatch
	if err := json.Unmarshal(result, &matches); err != nil {
		t.Fatalf("unmarshal matches: %v", err)
	}
	if len(matches) < 2 {
		t.Errorf("expected at least 2 matches, got %d: %+v", len(matches), matches)
	}
}

func TestSearchTextWSInvalidRegex(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"main.go": "func main() {}\n",
	}}
	handler := grepHandlerWS(ws)

	_, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "("}))
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
	if !strings.Contains(err.Error(), "invalid regex pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommandExec(t *testing.T) {
	exec := &mockExecutor{}
	handler := runCommandHandlerExec(exec, &mockWorkspace{files: map[string]string{}})

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommandExec: %v", err)
	}

	var output workspace.ExecOutput
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !strings.Contains(output.Stdout, "echo") {
		t.Errorf("expected stdout to contain command, got %q", output.Stdout)
	}
}

func TestRunCommandExecPolicyForwarding(t *testing.T) {
	exec := &mockExecutor{}
	ws := &mockWorkspace{files: map[string]string{}}
	k := newKernelWithToolPolicy(t, ResolveToolPolicyForWorkspace(".", "trusted", "confirm"))
	handler := runCommandHandlerExecWithPolicy(k, exec, ws)

	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommandExec: %v", err)
	}
	if exec.lastReq.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", exec.lastReq.Timeout)
	}
	if exec.lastReq.WorkingDir != "." {
		t.Fatalf("working dir = %q, want .", exec.lastReq.WorkingDir)
	}
	if exec.lastReq.Network.Mode != workspace.ExecNetworkEnabled {
		t.Fatalf("network mode = %q, want %q", exec.lastReq.Network.Mode, workspace.ExecNetworkEnabled)
	}
}

func TestRunCommandExecOffloadLargeOutput(t *testing.T) {
	exec := &mockExecutorLarge{
		stdout: strings.Repeat("x", maxInlineCommandOutput+256),
	}
	ws := &mockWorkspace{files: map[string]string{}}
	handler := runCommandHandlerExec(exec, ws)
	ctx := toolctx.WithToolCallContext(context.Background(), toolctx.ToolCallContext{
		SessionID: "sess-1",
		ToolName:  "run_command",
		CallID:    "call-1",
	})

	result, err := handler(ctx, toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommandExec: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["offloaded"] != true {
		t.Fatalf("expected offloaded response, got %+v", out)
	}
	path, _ := out["path"].(string)
	if path == "" {
		t.Fatalf("expected non-empty offload path, got %+v", out)
	}
	if _, ok := ws.files[path]; !ok {
		t.Fatalf("expected offloaded file %q", path)
	}
}

func TestWorkspacePreferredOverSandbox(t *testing.T) {
	// When both Workspace and Sandbox are provided, Workspace should be used
	ws := &mockWorkspace{files: map[string]string{"workspace.txt": "from workspace"}}
	sb := newMockSandbox("/ws", map[string]string{"/ws/workspace.txt": "from sandbox"})

	reg := tool.NewRegistry()
	if err := RegisterBuiltinTools(reg, sb, &mockUserIO{}, ws, nil); err != nil {
		t.Fatalf("RegisterBuiltinTools: %v", err)
	}

	readTool, ok := reg.Get("read_file")
	if !ok {
		t.Fatal("read_file not registered")
	}

	result, err := readTool.Execute(context.Background(), toJSON(t, map[string]string{"path": "workspace.txt"}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	var content string
	if err := json.Unmarshal(result, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content != "from workspace" {
		t.Errorf("expected workspace content, got %q", content)
	}
}

func TestDatetimeTool(t *testing.T) {
	reg := tool.NewRegistry()
	sb := newMockSandbox("/ws", nil)
	io := &mockUserIO{}
	if err := RegisterBuiltinTools(reg, sb, io, nil, nil); err != nil {
		t.Fatalf("RegisterBuiltinTools: %v", err)
	}
	dtTool, ok := reg.Get("datetime")
	if !ok {
		t.Fatal("datetime not registered")
	}
	raw, err := dtTool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("datetime: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode datetime: %v", err)
	}
	iso, ok := resp["iso8601"].(string)
	if !ok || strings.TrimSpace(iso) == "" {
		t.Fatalf("missing iso8601: %+v", resp)
	}
	timezone, ok := resp["timezone_name"].(string)
	if !ok || strings.TrimSpace(timezone) == "" {
		t.Fatalf("missing timezone_name: %+v", resp)
	}
	offset, ok := resp["utc_offset"].(string)
	if !ok || strings.TrimSpace(offset) == "" {
		t.Fatalf("missing utc_offset: %+v", resp)
	}
	if _, ok := resp["unix_timestamp"].(float64); !ok {
		t.Fatalf("missing unix_timestamp: %+v", resp)
	}
}

