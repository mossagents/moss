package sandbox

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/workspace"
)

// NoOp 是一个无操作的 Workspace 实现，所有操作均返回错误。
// 适用于纯对话场景，不需要文件系统访问。
type NoOp struct{}

var _ workspace.Workspace = (*NoOp)(nil)

var errNoWorkspace = fmt.Errorf("workspace not available: this agent is running in conversation-only mode")

func (*NoOp) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return nil, errNoWorkspace
}
func (*NoOp) WriteFile(_ context.Context, _ string, _ []byte) error {
	return errNoWorkspace
}
func (*NoOp) ListFiles(_ context.Context, _ string) ([]string, error) {
	return nil, errNoWorkspace
}
func (*NoOp) Stat(_ context.Context, _ string) (workspace.FileInfo, error) {
	return workspace.FileInfo{}, errNoWorkspace
}
func (*NoOp) DeleteFile(_ context.Context, _ string) error {
	return errNoWorkspace
}
func (*NoOp) Execute(_ context.Context, _ workspace.ExecRequest) (workspace.ExecOutput, error) {
	return workspace.ExecOutput{}, errNoWorkspace
}
func (*NoOp) ResolvePath(_ string) (string, error) {
	return "", errNoWorkspace
}
func (*NoOp) Capabilities() workspace.Capabilities { return workspace.Capabilities{} }
func (*NoOp) Policy() workspace.SecurityPolicy     { return workspace.SecurityPolicy{} }
func (*NoOp) Limits() workspace.ResourceLimits     { return workspace.ResourceLimits{} }
