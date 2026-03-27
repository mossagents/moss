package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
)

// ── mock sandbox ─────────────────────────────────────

type mockSandbox struct {
	root  string
	files map[string]string // path → content (absolute paths)
}

func newMockSandbox(root string, files map[string]string) *mockSandbox {
	return &mockSandbox{root: root, files: files}
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

func (m *mockSandbox) Execute(_ context.Context, cmd string, args []string) (sandbox.Output, error) {
	return sandbox.Output{
		Stdout:   "mock output for: " + cmd + " " + strings.Join(args, " "),
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
}

func (m *mockUserIO) Send(_ context.Context, _ port.OutputMessage) error { return nil }

func (m *mockUserIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	m.lastQuestion = req.Prompt
	return port.InputResponse{Value: m.response}, nil
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

func (m *mockWorkspace) Stat(_ context.Context, path string) (port.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return port.FileInfo{Name: path, Size: int64(len(m.files[path]))}, nil
	}
	return port.FileInfo{}, &notFoundError{path: path}
}

func (m *mockWorkspace) DeleteFile(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}

// ── mock executor ────────────────────────────────────

type mockExecutor struct{}

func (m *mockExecutor) Execute(_ context.Context, cmd string, args []string) (port.ExecOutput, error) {
	return port.ExecOutput{
		Stdout:   "exec: " + cmd + " " + strings.Join(args, " "),
		ExitCode: 0,
	}, nil
}

type mockExecutorLarge struct {
	stdout string
}

func (m *mockExecutorLarge) Execute(_ context.Context, _ string, _ []string) (port.ExecOutput, error) {
	return port.ExecOutput{
		Stdout:   m.stdout,
		ExitCode: 0,
	}, nil
}

// ── tests ────────────────────────────────────────────

func TestRegisterAll(t *testing.T) {
	reg := tool.NewRegistry()
	sb := newMockSandbox("/ws", nil)
	io := &mockUserIO{}

	if err := RegisterAll(reg, sb, io, nil, nil); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	expected := []string{"read_file", "write_file", "edit_file", "glob", "list_files", "search_text", "run_command", "ask_user"}
	specs := reg.List()
	if len(specs) != len(expected) {
		t.Fatalf("expected %d tools, got %d", len(expected), len(specs))
	}

	names := make(map[string]bool)
	for _, s := range specs {
		names[s.Name] = true
	}
	for _, e := range expected {
		if !names[e] {
			t.Errorf("missing tool: %s", e)
		}
	}
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
	json.Unmarshal(result, &content)
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
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %q", resp["status"])
	}
	if sb.files["/ws/out.txt"] != "new content" {
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
	json.Unmarshal(result, &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	if sb.files["/ws/doc.txt"] != "hello world" {
		t.Errorf("unexpected edited content: %q", sb.files["/ws/doc.txt"])
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
	json.Unmarshal(result, &files)
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
	json.Unmarshal(result, &files)
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(files), files)
	}
}

func TestSearchText(t *testing.T) {
	sb := newMockSandbox("/ws", map[string]string{
		"/ws/main.go":  "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"/ws/other.go": "package other\n\nfunc other() {}\n",
	})
	handler := searchTextHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "main"}))
	if err != nil {
		t.Fatalf("searchText: %v", err)
	}

	var matches []searchMatch
	json.Unmarshal(result, &matches)

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
	handler := searchTextHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": `^func main\(`}))
	if err != nil {
		t.Fatalf("searchText regex: %v", err)
	}

	var matches []searchMatch
	json.Unmarshal(result, &matches)
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
	handler := searchTextHandler(sb)

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
	handler := searchTextHandler(sb)

	result, err := handler(context.Background(), toJSON(t, map[string]any{
		"pattern":     "aaa",
		"max_results": 3,
	}))
	if err != nil {
		t.Fatalf("searchText: %v", err)
	}

	var matches []searchMatch
	json.Unmarshal(result, &matches)
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
	json.Unmarshal(result, &output)
	if !strings.Contains(output.Stdout, "echo") {
		t.Errorf("expected stdout to contain command, got %q", output.Stdout)
	}
	if output.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", output.ExitCode)
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
	json.Unmarshal(result, &answer)
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
	json.Unmarshal(result, &answer)
	if !strings.Contains(answer, "no user IO") {
		t.Errorf("expected fallback message, got %q", answer)
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
		{"list_files", tool.RiskLow},
		{"search_text", tool.RiskLow},
		{"run_command", tool.RiskHigh},
		{"ask_user", tool.RiskLow},
	}

	reg := tool.NewRegistry()
	sb := newMockSandbox("/ws", nil)
	io := &mockUserIO{}
	RegisterAll(reg, sb, io, nil, nil)

	for _, c := range cases {
		spec, _, ok := reg.Get(c.name)
		if !ok {
			t.Errorf("tool %q not found", c.name)
			continue
		}
		if spec.Risk != c.risk {
			t.Errorf("tool %q: expected risk %v, got %v", c.name, c.risk, spec.Risk)
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
		{"list_files", listFilesHandler(sb)},
		{"search_text", searchTextHandler(sb)},
		{"run_command", runCommandHandler(sb)},
		{"ask_user", askUserHandler(&mockUserIO{})},
	}

	for _, c := range cases {
		_, err := c.handler(context.Background(), json.RawMessage(`{invalid json`))
		if err == nil {
			t.Errorf("%s: expected error for invalid JSON", c.name)
		}
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

	if err := RegisterAll(reg, nil, io, ws, exec); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	expected := []string{"read_file", "write_file", "edit_file", "glob", "list_files", "search_text", "run_command", "ask_user"}
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
	json.Unmarshal(result, &content)
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
	json.Unmarshal(result, &resp)
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
	json.Unmarshal(result, &resp)
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
	json.Unmarshal(result, &files)
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(files), files)
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
	json.Unmarshal(result, &files)
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestSearchTextWS(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"main.go":  "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"other.go": "package other\n\nfunc other() {}\n",
	}}
	handler := searchTextHandlerWS(ws)

	result, err := handler(context.Background(), toJSON(t, map[string]string{"pattern": "main"}))
	if err != nil {
		t.Fatalf("searchTextWS: %v", err)
	}

	var matches []searchMatch
	json.Unmarshal(result, &matches)
	if len(matches) < 2 {
		t.Errorf("expected at least 2 matches, got %d: %+v", len(matches), matches)
	}
}

func TestSearchTextWSInvalidRegex(t *testing.T) {
	ws := &mockWorkspace{files: map[string]string{
		"main.go": "func main() {}\n",
	}}
	handler := searchTextHandlerWS(ws)

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

	var output port.ExecOutput
	json.Unmarshal(result, &output)
	if !strings.Contains(output.Stdout, "echo") {
		t.Errorf("expected stdout to contain command, got %q", output.Stdout)
	}
}

func TestRunCommandExecOffloadLargeOutput(t *testing.T) {
	exec := &mockExecutorLarge{
		stdout: strings.Repeat("x", maxInlineCommandOutput+256),
	}
	ws := &mockWorkspace{files: map[string]string{}}
	handler := runCommandHandlerExec(exec, ws)
	ctx := port.WithToolCallContext(context.Background(), port.ToolCallContext{
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
	ws := &mockWorkspace{files: map[string]string{"ws.txt": "from workspace"}}
	sb := newMockSandbox("/ws", map[string]string{"/ws/ws.txt": "from sandbox"})

	reg := tool.NewRegistry()
	RegisterAll(reg, sb, &mockUserIO{}, ws, nil)

	_, handler, ok := reg.Get("read_file")
	if !ok {
		t.Fatal("read_file not registered")
	}

	result, err := handler(context.Background(), toJSON(t, map[string]string{"path": "ws.txt"}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	var content string
	json.Unmarshal(result, &content)
	if content != "from workspace" {
		t.Errorf("expected workspace content, got %q", content)
	}
}
