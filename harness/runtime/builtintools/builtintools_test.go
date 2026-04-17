package builtintools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	policypack "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/harness/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

// ── mock workspace (sandbox-style with root) ────────

type mockSandboxWS struct {
	root        string
	files       map[string]string // path → content (absolute paths)
	lastExecReq workspace.ExecRequest
}

func newMockSandboxWS(root string, files map[string]string) *mockSandboxWS {
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
	return &mockSandboxWS{root: root, files: normalized}
}

func (m *mockSandboxWS) ResolvePath(path string) (string, error) {
	if path == "." {
		return m.root, nil
	}
	if strings.HasPrefix(path, m.root) {
		return path, nil
	}
	return m.root + "/" + path, nil
}

func (m *mockSandboxWS) ListFiles(_ context.Context, _ string) ([]string, error) {
	var result []string
	for p := range m.files {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockSandboxWS) ReadFile(_ context.Context, path string) ([]byte, error) {
	resolved, _ := m.ResolvePath(path)
	if content, ok := m.files[resolved]; ok {
		return []byte(content), nil
	}
	return nil, &notFoundError{path: path}
}

func (m *mockSandboxWS) WriteFile(_ context.Context, path string, content []byte) error {
	resolved, _ := m.ResolvePath(path)
	m.files[resolved] = string(content)
	return nil
}

func (m *mockSandboxWS) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	resolved, _ := m.ResolvePath(path)
	if content, ok := m.files[resolved]; ok {
		return workspace.FileInfo{Name: path, Size: int64(len(content))}, nil
	}
	return workspace.FileInfo{}, &notFoundError{path: path}
}

func (m *mockSandboxWS) DeleteFile(_ context.Context, path string) error {
	resolved, _ := m.ResolvePath(path)
	delete(m.files, resolved)
	return nil
}

func (m *mockSandboxWS) Execute(_ context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	m.lastExecReq = req
	return workspace.ExecOutput{
		Stdout:   "mock output for: " + req.Command + " " + strings.Join(req.Args, " "),
		ExitCode: 0,
	}, nil
}

func (m *mockSandboxWS) Capabilities() workspace.Capabilities {
	return workspace.Capabilities{}
}

func (m *mockSandboxWS) Policy() workspace.SecurityPolicy {
	return workspace.SecurityPolicy{}
}

func (m *mockSandboxWS) Limits() workspace.ResourceLimits {
	return workspace.ResourceLimits{}
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

// ── mock workspace (simple, no root) ─────────────────

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

func (m *mockWorkspace) Execute(_ context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{
		Stdout:   "exec: " + req.Command + " " + strings.Join(req.Args, " "),
		ExitCode: 0,
	}, nil
}

func (m *mockWorkspace) ResolvePath(_ string) (string, error) { return "", nil }
func (m *mockWorkspace) Capabilities() workspace.Capabilities { return workspace.Capabilities{} }
func (m *mockWorkspace) Policy() workspace.SecurityPolicy     { return workspace.SecurityPolicy{} }
func (m *mockWorkspace) Limits() workspace.ResourceLimits     { return workspace.ResourceLimits{} }

// ── mock workspace (large output) ────────────────────

type mockWorkspaceLarge struct {
	mockWorkspace
	stdout string
}

type mockPatchApply struct {
	lastReq workspace.PatchApplyRequest
	result  *workspace.PatchApplyResult
	err     error
}

func (m *mockPatchApply) Apply(_ context.Context, req workspace.PatchApplyRequest) (*workspace.PatchApplyResult, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &workspace.PatchApplyResult{Applied: true}, nil
}

func (m *mockWorkspaceLarge) Execute(_ context.Context, _ workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{
		Stdout:   m.stdout,
		ExitCode: 0,
	}, nil
}

// ── tests ────────────────────────────────────────────

func TestRegisterBuiltinTools(t *testing.T) {
	reg := tool.NewRegistry()
	ws := newMockSandboxWS("/ws", nil)
	uio := &mockUserIO{}

	k := kernel.New(kernel.WithWorkspace(ws), kernel.WithUserIO(uio))
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
	}

	expected := []string{"read_file", "write_file", "edit_file", "glob", "ls", "list_files", "grep", "run_command", "http_request", "datetime", "ask_user"}
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

func newTestKernel(ws workspace.Workspace, userIO io.UserIO) *kernel.Kernel {
	opts := make([]kernel.Option, 0, 2)
	if userIO != nil {
		opts = append(opts, kernel.WithUserIO(userIO))
	}
	if ws != nil {
		opts = append(opts, kernel.WithWorkspace(ws))
	}
	return kernel.New(opts...)
}

func newKernelWithToolPolicy(t *testing.T, policy policypack.ToolPolicy) *kernel.Kernel {
	t.Helper()
	k := kernel.New()
	payload, err := policypack.EncodeToolPolicyMetadata(policy)
	if err != nil {
		t.Fatalf("policypack.EncodeToolPolicyMetadata: %v", err)
	}
	policystate.Ensure(k).Set(payload, session.EncodeToolPolicySummary(policypack.SummarizeToolPolicy(policy)), nil)
	return k
}

func testReadFileHandler(ws workspace.Workspace) tool.ToolHandler {
	return readFileHandlerPort(ws)
}

func testWriteFileHandler(ws workspace.Workspace) tool.ToolHandler {
	return writeFileHandlerPort(ws)
}

func testEditFileHandler(ws workspace.Workspace) tool.ToolHandler {
	return editFileHandlerPort(ws)
}

func testGlobHandler(ws workspace.Workspace, root string) tool.ToolHandler {
	return globHandlerPort(ws, root)
}

func testListFilesHandler(ws workspace.Workspace, root string) tool.ToolHandler {
	return listFilesHandlerPort(ws, root)
}

func testGrepHandler(ws workspace.Workspace, root string) tool.ToolHandler {
	return grepHandlerPort(ws, root)
}

func testRunCommandHandler(ws workspace.Workspace, root string) tool.ToolHandler {
	k := newTestKernel(ws, nil)
	return runCommandHandler(k, ws, root)
}

func testRunCommandHandlerWithPolicy(k *kernel.Kernel, ws workspace.Workspace, root string) tool.ToolHandler {
	return runCommandHandler(k, ws, root)
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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/hello.txt": "Hello, World!",
	})
	handler := testReadFileHandler(ws)

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

func TestReadFileLineRange(t *testing.T) {
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/hello.txt": "one\ntwo\nthree\nfour\n",
	})
	handler := testReadFileHandler(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"path":       "hello.txt",
		"start_line": 2,
		"end_line":   3,
	}))
	if err != nil {
		t.Fatalf("readFile line range: %v", err)
	}

	var resp struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Total     int    `json:"total_lines"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal line range response: %v", err)
	}
	if resp.Path != "hello.txt" || resp.StartLine != 2 || resp.EndLine != 3 || resp.Total != 4 {
		t.Fatalf("unexpected line range response: %+v", resp)
	}
	if resp.Content != "two\nthree" {
		t.Fatalf("content = %q, want %q", resp.Content, "two\nthree")
	}
}

func TestReadFileNotFound(t *testing.T) {
	ws := newMockSandboxWS("/ws", map[string]string{})
	handler := testReadFileHandler(ws)

	_, err := handler(context.Background(), toJSON(t, map[string]string{"path": "missing.txt"}))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteFile(t *testing.T) {
	ws := newMockSandboxWS("/ws", map[string]string{})
	handler := testWriteFileHandler(ws)

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
	outPath, _ := ws.ResolvePath("out.txt")
	if ws.files[outPath] != "new content" {
		t.Errorf("file not written, files: %v", ws.files)
	}
}

func TestEditFile(t *testing.T) {
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/doc.txt": "hello moss",
	})
	handler := testEditFileHandler(ws)

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
	docPath, _ := ws.ResolvePath("doc.txt")
	if ws.files[docPath] != "hello world" {
		t.Errorf("unexpected edited content: %q", ws.files[docPath])
	}
}

func TestEditFileRequireReplaceAll(t *testing.T) {
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/doc.txt": "moss moss",
	})
	handler := testEditFileHandler(ws)

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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/a.go":        "",
		"/ws/sub/b.go":    "",
		"/ws/sub/readme":  "",
		"/ws/notes/test":  "",
		"/ws/notes/x.txt": "",
	})
	handler := testGlobHandler(ws, ws.root)

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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/a.go":      "",
		"/ws/b.go":      "",
		"/ws/dir/c.txt": "",
	})
	handler := testListFilesHandler(ws, ws.root)

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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/a.go":         "",
		"/ws/.git/config":  "",
		"/ws/.moss/x.json": "",
	})
	handler := testListFilesHandler(ws, ws.root)
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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/a.go": "", "/ws/b.go": "", "/ws/c.go": "",
	})
	handler := testListFilesHandler(ws, ws.root)
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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/main.go":  "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"/ws/other.go": "package other\n\nfunc other() {}\n",
	})
	handler := testGrepHandler(ws, ws.root)

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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/main.go": "func main() {}\n",
		"/ws/lib.go":  "func helper() {}\n",
	})
	handler := testGrepHandler(ws, ws.root)

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
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/main.go": "func main() {}\n",
	})
	handler := testGrepHandler(ws, ws.root)

	_, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "("}))
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
	if !strings.Contains(err.Error(), "invalid regex pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchTextMaxResults(t *testing.T) {
	ws := newMockSandboxWS("/ws", map[string]string{
		"/ws/data.txt": "aaa\naaa\naaa\naaa\naaa\naaa\naaa\naaa\naaa\naaa\n",
	})
	handler := testGrepHandler(ws, ws.root)

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
	ws := newMockSandboxWS("/ws", nil)
	handler := testRunCommandHandler(ws, ws.root)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}

	var output workspace.ExecOutput
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
	ws := newMockSandboxWS("/ws", nil)
	k := newKernelWithToolPolicy(t, policypack.ResolveToolPolicyForWorkspace("/ws", "restricted", "confirm"))
	handler := testRunCommandHandlerWithPolicy(k, ws, ws.root)

	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if ws.lastExecReq.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", ws.lastExecReq.Timeout)
	}
	if ws.lastExecReq.WorkingDir != "." {
		t.Fatalf("working dir = %q, want .", ws.lastExecReq.WorkingDir)
	}
	if len(ws.lastExecReq.AllowedPaths) != 1 || ws.lastExecReq.AllowedPaths[0] != ws.root {
		t.Fatalf("allowed paths = %#v, want [%s]", ws.lastExecReq.AllowedPaths, ws.root)
	}
	if ws.lastExecReq.Network.Mode != workspace.ExecNetworkDisabled {
		t.Fatalf("network mode = %q, want %q", ws.lastExecReq.Network.Mode, workspace.ExecNetworkDisabled)
	}
}

func TestAskUser(t *testing.T) {
	uio := &mockUserIO{response: "yes, proceed"}
	handler := askUserHandler(uio)

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
	if uio.lastQuestion != "Continue?" {
		t.Errorf("expected question 'Continue?', got %q", uio.lastQuestion)
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
	_, err := handler(context.Background(), json.RawMessage(`{"question":"Continue?","requestedSchema":{"properties":{"db":{"type":"string","enum":["a","b"]}},"required":["db"]}}`))
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
		{"list_files", tool.RiskLow},
		{"grep", tool.RiskLow},
		{"run_command", tool.RiskHigh},
		{"http_request", tool.RiskHigh},
		{"ask_user", tool.RiskLow},
	}

	reg := tool.NewRegistry()
	ws := newMockSandboxWS("/ws", nil)
	uio := &mockUserIO{}
	k := newTestKernel(ws, uio)
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
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
	ws := newMockSandboxWS("/ws", nil)
	k := newTestKernel(ws, &mockUserIO{})
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
	}
	cases := []struct {
		name           string
		effect         tool.Effect
		sideEffect     tool.SideEffectClass
		approvalClass  tool.ApprovalClass
		plannerVisible tool.PlannerVisibility
	}{
		{"read_file", tool.EffectReadOnly, tool.SideEffectNone, tool.ApprovalClassNone, tool.PlannerVisibilityVisible},
		{"list_files", tool.EffectReadOnly, tool.SideEffectNone, tool.ApprovalClassNone, tool.PlannerVisibilityVisible},
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
	ws := newMockSandboxWS("/ws", nil)
	cases := []struct {
		name    string
		handler tool.ToolHandler
	}{
		{"read_file", testReadFileHandler(ws)},
		{"write_file", testWriteFileHandler(ws)},
		{"edit_file", testEditFileHandler(ws)},
		{"glob", testGlobHandler(ws, ws.root)},
		{"ls", testListFilesHandler(ws, ws.root)},
		{"grep", testGrepHandler(ws, ws.root)},
		{"run_command", testRunCommandHandler(ws, ws.root)},
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
	k := newKernelWithToolPolicy(t, policypack.ResolveToolPolicyForWorkspace("/ws", "trusted", "full-auto"))
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

	k := newKernelWithToolPolicy(t, policypack.ResolveToolPolicyForWorkspace("/ws", "trusted", "full-auto"))
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

// ── Workspace handler tests ─────────────────────────

func TestRegisterAllWithWorkspace(t *testing.T) {
	reg := tool.NewRegistry()
	ws := &mockWorkspace{files: map[string]string{}}
	uio := &mockUserIO{}

	k := newTestKernel(ws, uio)
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
	}

	expected := []string{"read_file", "write_file", "edit_file", "glob", "ls", "list_files", "grep", "run_command", "http_request", "datetime", "ask_user"}
	specs := reg.List()
	if len(specs) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(specs))
	}
}

func TestListFilesAlias(t *testing.T) {
	reg := tool.NewRegistry()
	ws := &mockWorkspace{files: map[string]string{"a.txt": "a", "b.go": "b"}}
	k := newTestKernel(ws, &mockUserIO{})
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
	}
	listTool, ok := reg.Get("list_files")
	if !ok {
		t.Fatal("list_files not registered")
	}
	raw, err := listTool.Execute(context.Background(), toJSON(t, map[string]any{"pattern": "**/*"}))
	if err != nil {
		t.Fatalf("list_files: %v", err)
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		t.Fatalf("decode list_files: %v", err)
	}
	if len(paths) != 2 || paths[0] != "a.txt" || paths[1] != "b.go" {
		t.Fatalf("unexpected list_files response: %+v", paths)
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

func TestApplyPatchTool(t *testing.T) {
	reg := tool.NewRegistry()
	ws := &mockWorkspace{files: map[string]string{"a.txt": "before"}}
	applier := &mockPatchApply{result: &workspace.PatchApplyResult{PatchID: "p1", Applied: true, TargetFiles: []string{"a.txt"}}}
	k := kernel.New(
		kernel.WithWorkspace(ws),
		kernel.WithUserIO(&mockUserIO{}),
		kernel.WithPatchApply(applier),
	)
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
	}
	patchTool, ok := reg.Get("apply_patch")
	if !ok {
		t.Fatal("apply_patch not registered")
	}
	raw, err := patchTool.Execute(context.Background(), toJSON(t, map[string]any{
		"patch":     "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-before\n+after\n",
		"three_way": true,
		"source":    "user",
	}))
	if err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	if !applier.lastReq.ThreeWay || applier.lastReq.Source != workspace.PatchSourceUser {
		t.Fatalf("unexpected patch apply request: %+v", applier.lastReq)
	}
	var resp workspace.PatchApplyResult
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode apply_patch: %v", err)
	}
	if !resp.Applied || resp.PatchID != "p1" {
		t.Fatalf("unexpected patch response: %+v", resp)
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

func TestRunCommandViaWorkspace(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{}}
	handler := testRunCommandHandler(ws, "")

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand via workspace: %v", err)
	}

	var output workspace.ExecOutput
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !strings.Contains(output.Stdout, "echo") {
		t.Errorf("expected stdout to contain command, got %q", output.Stdout)
	}
}

func TestRunCommandViaWorkspacePolicyForwarding(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{}}
	k := newKernelWithToolPolicy(t, policypack.ResolveToolPolicyForWorkspace(".", "trusted", "confirm"))
	handler := testRunCommandHandlerWithPolicy(k, ws, "")

	_, err := handler(context.Background(), toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
}

func TestRunCommandOffloadLargeOutput(t *testing.T) {
	ws := &mockWorkspaceLarge{
		mockWorkspace: mockWorkspace{files: map[string]string{}},
		stdout:        strings.Repeat("x", maxInlineCommandOutput+256),
	}
	handler := testRunCommandHandler(ws, "")
	ctx := tool.WithToolCallContext(context.Background(), tool.ToolCallContext{
		SessionID: "sess-1",
		ToolName:  "run_command",
		CallID:    "call-1",
	})

	result, err := handler(ctx, toJSON(t, map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	}))
	if err != nil {
		t.Fatalf("runCommand: %v", err)
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
	// When Workspace is provided, it should be used for file operations
	ws := &mockWorkspace{files: map[string]string{"workspace.txt": "from workspace"}}

	reg := tool.NewRegistry()
	k := newTestKernel(ws, &mockUserIO{})
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
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
	ws := newMockSandboxWS("/ws", nil)
	uio := &mockUserIO{}
	k := newTestKernel(ws, uio)
	if err := RegisterBuiltinToolsForKernel(k, reg); err != nil {
		t.Fatalf("RegisterBuiltinToolsForKernel: %v", err)
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
