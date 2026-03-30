package product

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
)

func AuditLogPath() string {
	return filepath.Join(appconfig.AppDir(), "audit.jsonl")
}

func DebugLogPath() string {
	return filepath.Join(appconfig.AppDir(), "debug.log")
}

func OpenAuditObserver() (port.Observer, io.Closer, error) {
	path := AuditLogPath()
	if path == "" {
		return nil, nil, fmt.Errorf("audit log path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create audit log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open audit log: %w", err)
	}
	return builtins.NewAuditLogger(f), f, nil
}
