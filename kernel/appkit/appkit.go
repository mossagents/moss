// Package appkit 提供构建 MOSS 应用的公共脚手架工具。
//
// 包含信号处理、REPL 引擎、配置解析等在多个示例中重复出现的能力，
// 下沉到框架层以简化上层应用的构建。
package appkit

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// ContextWithSignal 创建一个会被 SIGINT/SIGTERM 自动取消的 Context。
//
// 用法：
//
//	ctx, cancel := appkit.ContextWithSignal(context.Background())
//	defer cancel()
func ContextWithSignal(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "\nInterrupted.")
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()
	return ctx, cancel
}

// FirstNonEmpty 返回第一个非空字符串，若全为空则返回空串。
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
