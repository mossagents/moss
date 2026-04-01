package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	kerrors "github.com/mossagents/moss/kernel/errors"
)

type fakeMCPClient struct {
	callToolFn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
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
func (f *fakeMCPClient) Ping(context.Context) error                                  { return nil }
func (f *fakeMCPClient) Close() error                                                 { return nil }
func (f *fakeMCPClient) AddRoots(context.Context, []mcp.Root) error                  { return nil }
func (f *fakeMCPClient) RemoveRoots(context.Context, []mcp.Root) error               { return nil }
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
