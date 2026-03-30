package sandbox

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/kernel/port"
)

// NoOp 是一个无操作的 Sandbox 实现，所有文件和命令操作均返回错误。
// 适用于纯对话场景，不需要文件系统访问。
type NoOp struct{}

var _ Sandbox = (*NoOp)(nil)

var errNoSandbox = fmt.Errorf("sandbox not available: this agent is running in conversation-only mode")

func (*NoOp) ResolvePath(string) (string, error) { return "", errNoSandbox }
func (*NoOp) ListFiles(string) ([]string, error) { return nil, errNoSandbox }
func (*NoOp) ReadFile(string) ([]byte, error)    { return nil, errNoSandbox }
func (*NoOp) WriteFile(string, []byte) error     { return errNoSandbox }
func (*NoOp) Execute(context.Context, port.ExecRequest) (port.ExecOutput, error) {
	return port.ExecOutput{}, errNoSandbox
}
func (*NoOp) Limits() ResourceLimits { return ResourceLimits{} }
