package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

var defaultLogger atomic.Pointer[slog.Logger]

func init() {
	l := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	defaultLogger.Store(l)
}

// GetLogger 返回全局 slog 实例。
// 默认输出到 stderr，使用 text 格式，级别为 Info。
func GetLogger() *slog.Logger {
	return defaultLogger.Load()
}

// SetLogger 设置全局 slog 实例（用于测试或自定义配置）。
func SetLogger(l *slog.Logger) {
	if l != nil {
		defaultLogger.Store(l)
	}
}

// DebugEnabled reports whether debug logging is enabled for the current process.
func DebugEnabled() bool {
	return os.Getenv("MOSS_DEBUG") == "1"
}

// EnableDebugFromArgs promotes --debug from CLI args into MOSS_DEBUG=1 before
// the main flag parser runs so debug.log can be configured during startup.
func EnableDebugFromArgs(args []string) bool {
	if DebugEnabled() {
		return true
	}
	if !debugFlagEnabled(args) {
		return false
	}
	_ = os.Setenv("MOSS_DEBUG", "1")
	return true
}

// ConfigureLogging 配置全局日志处理器。
//
// level: 日志级别（debug, info, warn, error）
// format: 输出格式（"text" 或 "json"）
// w: 输出目标，默认为 os.Stderr
func ConfigureLogging(level slog.Level, format string, w io.Writer) error {
	if w == nil {
		w = os.Stderr
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	l := slog.New(handler)
	defaultLogger.Store(l)
	slog.SetDefault(l)
	return nil
}

// ConfigureDebugFileWhenEnabled 在 MOSS_DEBUG=1 时启用应用目录 debug.log 落盘。
// 返回值：
//   - enabled: 是否已启用文件日志
//   - path: 日志文件路径（仅 enabled=true 时非空）
//   - closer: 调用方可在退出时关闭
func ConfigureDebugFileWhenEnabled(appDir string) (enabled bool, path string, closer io.Closer, err error) {
	if !DebugEnabled() {
		return false, "", nil, nil
	}
	if appDir == "" {
		return false, "", nil, os.ErrInvalid
	}
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return false, "", nil, err
	}
	path = filepath.Join(appDir, "debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, "", nil, err
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(handler)
	defaultLogger.Store(l)
	slog.SetDefault(l)
	return true, path, f, nil
}

func debugFlagEnabled(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--debug" {
			return true
		}
		if !strings.HasPrefix(arg, "--debug=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(arg, "--debug="))
		if value == "" {
			return true
		}
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return false
		}
		return enabled
	}
	return false
}
