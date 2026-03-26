package logging

import (
	"io"
	"log/slog"
	"os"
)

var defaultLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

// GetLogger 返回全局 slog 实例。
// 默认输出到 stderr，使用 text 格式，级别为 Info。
func GetLogger() *slog.Logger {
	return defaultLogger
}

// SetLogger 设置全局 slog 实例（用于测试或自定义配置）。
func SetLogger(l *slog.Logger) {
	if l != nil {
		defaultLogger = l
	}
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

	defaultLogger = slog.New(handler)
	return nil
}
