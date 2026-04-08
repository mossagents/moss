package mcp

import (
	"context"
	"encoding/json"
	"errors"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	config "github.com/mossagents/moss/config"
	kerrors "github.com/mossagents/moss/kernel/errors"
	intr "github.com/mossagents/moss/kernel/interaction"
	"github.com/mossagents/moss/sandbox"
	"os"
	"strings"
	"testing"
)

type fakeMCPClient struct {
	callToolFn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

type guardRecorder struct {
	inputCalls  int
	outputCalls int
	lastTool    string
	inputErr    error
	outputErr   error
}

func (g *guardRecorder) ValidateInput(_ context.Context, tool string, _ []byte) error {
	g.inputCalls++
	g.lastTool = tool
	return g.inputErr
}

func (g *guardRecorder) ValidateOutput(_ context.Context, tool string, _ []byte) error {
	g.outputCalls++
	g.lastTool = tool
	return g.outputErr
}

func (f *fakeMCPClient) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}
func (f *fakeMCPClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, nil
}
func (f *fakeMCPClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if f.callToolFn != nil {
		return f.callToolFn(ctx, req)
	}
	return &mcp.CallToolResult{}, nil
}
func (f *fakeMCPClient) Ping(context.Context) error                    { return nil }
func (f *fakeMCPClient) Close() error                                  { return nil }
func (f *fakeMCPClient) AddRoots(context.Context, []mcp.Root) error    { return nil }
func (f *fakeMCPClient) RemoveRoots(context.Context, []mcp.Root) error { return nil }
func (f *fakeMCPClient) ListPrompts(context.Context, mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return &mcp.ListPromptsResult{}, nil
}
func (f *fakeMCPClient) GetPrompt(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{}, nil
}
func (f *fakeMCPClient) ListResources(context.Context, mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return &mcp.ListResourcesResult{}, nil
}
func (f *fakeMCPClient) ListResourcesByPage(context.Context, mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return &mcp.ListResourcesResult{}, nil
}
func (f *fakeMCPClient) ListResourceTemplates(context.Context, mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	return &mcp.ListResourceTemplatesResult{}, nil
}
func (f *fakeMCPClient) ListResourceTemplatesByPage(context.Context, mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	return &mcp.ListResourceTemplatesResult{}, nil
}
func (f *fakeMCPClient) ReadResource(context.Context, mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{}, nil
}
func (f *fakeMCPClient) Subscribe(context.Context, mcp.SubscribeRequest) error {
	return nil
}
func (f *fakeMCPClient) Unsubscribe(context.Context, mcp.UnsubscribeRequest) error {
	return nil
}
func (f *fakeMCPClient) Complete(context.Context, mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return &mcp.CompleteResult{}, nil
}
func (f *fakeMCPClient) ListPromptsByPage(context.Context, mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return &mcp.ListPromptsResult{}, nil
}
func (f *fakeMCPClient) ListToolsByPage(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, nil
}
func (f *fakeMCPClient) SetLevel(context.Context, mcp.SetLevelRequest) error {
	return nil
}
func (f *fakeMCPClient) OnNotification(func(mcp.JSONRPCNotification)) {}

var _ mcpclient.MCPClient = (*fakeMCPClient)(nil)

func TestValidateMCPInput_SizeAndShape(t *testing.T) {
	oversized := make([]byte, maxMCPToolInputBytes+1)
	if err := validateMCPInput(oversized); err == nil {
		t.Fatal("expected oversized input validation error")
	}
	if err := validateMCPInput(json.RawMessage(`[]`)); err != nil {
		t.Fatalf("array should pass low-level validation and be rejected by handler shape check: %v", err)
	}
	if err := validateMCPInput(json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatalf("valid input should pass: %v", err)
	}
}

func TestValidateMCPOutput_Size(t *testing.T) {
	oversized := make([]byte, maxMCPToolOutputBytes+1)
	if err := validateMCPOutput(oversized); err == nil {
		t.Fatal("expected oversized output validation error")
	}
	if err := validateMCPOutput(json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("valid output should pass: %v", err)
	}
}

func TestClassifyMCPCallError(t *testing.T) {
	timeoutErr := classifyMCPCallError("demo", context.DeadlineExceeded)
	if code := kerrors.GetCode(timeoutErr); code != kerrors.ErrLLMTimeout {
		t.Fatalf("expected timeout code, got %s", code)
	}
	cancelErr := classifyMCPCallError("demo", context.Canceled)
	if code := kerrors.GetCode(cancelErr); code != kerrors.ErrInternal {
		t.Fatalf("expected internal code for cancel, got %s", code)
	}
	remoteErr := classifyMCPCallError("demo", errors.New("boom"))
	if code := kerrors.GetCode(remoteErr); code != kerrors.ErrInternal {
		t.Fatalf("expected internal code for remote error, got %s", code)
	}
}

func TestMakeHandler_ValidatesInputObject(t *testing.T) {
	s := &MCPServer{
		client: &fakeMCPClient{
			callToolFn: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			},
		},
	}
	handler := s.makeHandler("echo")
	if _, err := handler(context.Background(), json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected object-shape validation error")
	}
	if _, err := handler(context.Background(), json.RawMessage(`{"v":"ok"}`)); err != nil {
		t.Fatalf("expected valid object input, got %v", err)
	}
}

func TestNewMCPServer_DefaultGuardIsSet(t *testing.T) {
	s := NewMCPServer(config.SkillConfig{Name: "demo", Transport: "stdio"})
	if s.guard == nil {
		t.Fatal("expected default guard to be set")
	}
}

func TestMakeHandler_UsesCustomGuardForInputAndOutput(t *testing.T) {
	guard := &guardRecorder{}
	s := &MCPServer{
		client: &fakeMCPClient{
			callToolFn: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			},
		},
		guard: guard,
	}

	handler := s.makeHandler("echo")
	if _, err := handler(context.Background(), json.RawMessage(`{"v":"ok"}`)); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if guard.inputCalls != 1 || guard.outputCalls != 1 {
		t.Fatalf("expected input/output guard called once, got input=%d output=%d", guard.inputCalls, guard.outputCalls)
	}
	if guard.lastTool != "echo" {
		t.Fatalf("expected guard tool echo, got %q", guard.lastTool)
	}
}

func TestMakeHandler_StopsWhenGuardRejectsInput(t *testing.T) {
	guard := &guardRecorder{inputErr: kerrors.New(kerrors.ErrValidation, "blocked input")}
	called := false
	s := &MCPServer{
		client: &fakeMCPClient{
			callToolFn: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				called = true
				return &mcp.CallToolResult{}, nil
			},
		},
		guard: guard,
	}
	handler := s.makeHandler("echo")
	if _, err := handler(context.Background(), json.RawMessage(`{"v":"ok"}`)); err == nil {
		t.Fatal("expected input guard error")
	}
	if called {
		t.Fatal("expected mcp call not invoked when input guard fails")
	}
}

type promptIO struct {
	last intr.InputRequest
	form map[string]any
}

func (p *promptIO) Send(context.Context, intr.OutputMessage) error { return nil }

func (p *promptIO) Ask(_ context.Context, req intr.InputRequest) (intr.InputResponse, error) {
	p.last = req
	return intr.InputResponse{Form: p.form}, nil
}

func TestResolveMCPRequiredEnvPromptsForMissingValues(t *testing.T) {
	io := &promptIO{form: map[string]any{"API_TOKEN": "secret"}}
	env, err := resolveMCPRequiredEnv(context.Background(), io, "demo", map[string]string{"STATIC": "1"}, []string{"API_TOKEN"})
	if err != nil {
		t.Fatalf("resolveMCPRequiredEnv: %v", err)
	}
	if env["API_TOKEN"] != "secret" || env["STATIC"] != "1" {
		t.Fatalf("unexpected resolved env: %+v", env)
	}
	if io.last.Type != intr.InputForm || !strings.Contains(io.last.Prompt, "demo") {
		t.Fatalf("expected prompt for missing env, got %+v", io.last)
	}
}

func TestBuildEnvUsesAllowlistedInheritedEnvironment(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("TMP", t.TempDir())
	t.Setenv("UNSAFE_SECRET", "do-not-inherit")
	s := &MCPServer{
		cfg: config.SkillConfig{
			Name:        "demo",
			Transport:   "stdio",
			Env:         map[string]string{"STATIC": "1"},
			RequiredEnv: []string{"API_TOKEN"},
		},
	}
	t.Setenv("API_TOKEN", "secret")
	env, err := s.buildEnv(context.Background(), &intr.NoOpIO{})
	if err != nil {
		t.Fatalf("buildEnv: %v", err)
	}
	got := map[string]string{}
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			got[parts[0]] = parts[1]
		}
	}
	if got["STATIC"] != "1" || got["API_TOKEN"] != "secret" {
		t.Fatalf("unexpected resolved env: %+v", got)
	}
	if _, ok := got["UNSAFE_SECRET"]; ok {
		t.Fatalf("unexpected inherited secret in env: %+v", got)
	}
	for key, value := range sandbox.SafeInheritedEnvironment() {
		if got[key] != value {
			t.Fatalf("missing inherited env %q", key)
		}
	}
}
