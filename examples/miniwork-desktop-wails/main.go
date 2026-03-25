// miniwork-desktop-wails 是基于 Wails v3 的桌面端 AI 助手。
//
// 演示 moss kernel 接入桌面端的能力：
//   - 对话式任务交互（流式输出）
//   - Manager → Worker 多 Agent 委派执行
//   - 文件/图片上传与本地文件访问
//   - 实时执行进度展示
package main

import (
	"context"
	"embed"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mossagi/moss/kernel/appkit"
	appconfig "github.com/mossagi/moss/kernel/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed templates/manager_system_prompt.tmpl
var defaultManagerPromptTemplate string

//go:embed templates/worker_system_prompt.tmpl
var defaultWorkerPromptTemplate string

func main() {
	appconfig.SetAppName("miniwork-desktop")
	_ = appconfig.EnsureAppDir()

	cfg := parseFlags()

	app := application.New(application.Options{
		Name:        "Moss Desktop",
		Description: "AI Agent Desktop — Powered by Moss Kernel",
		LogLevel:    slog.LevelInfo,
		Services: []application.Service{
			application.NewService(NewChatService(cfg)),
			application.NewService(NewFileService(cfg)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Moss Desktop — AI Agent Workspace",
		Width:     1280,
		Height:    860,
		MinWidth:  900,
		MinHeight: 600,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	// 监听系统信号，实现优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Info("Received signal, triggering graceful shutdown", slog.String("signal", sig.String()))
		if app != nil {
			app.Quit()
		}
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}

	slog.Info("Application shutdown complete")
}

// ─── Config & Flags ─────────────────────────────────

type config struct {
	provider  string
	model     string
	workspace string
	trust     string
	apiKey    string
	baseURL   string
	workers   int
}

func parseFlags() config {
	common := &appkit.AppFlags{}
	c := config{
		workspace: ".",
		trust:     "trusted",
		workers:   3,
	}
	fs := flag.NewFlagSet("miniwork-desktop", flag.ContinueOnError)
	appkit.BindAppFlags(fs, common)
	fs.IntVar(&c.workers, "workers", 3, "Max parallel workers")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	common.MergeGlobalConfig()
	common.MergeEnv("MINIWORK_DESKTOP", "MOSS")

	c.provider = common.Provider
	c.model = common.Model
	c.workspace = common.Workspace
	c.trust = common.Trust
	c.apiKey = common.APIKey
	c.baseURL = common.BaseURL

	return c
}

func buildManagerPrompt(workspace string, maxWorkers int) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	ctx["MaxWorkers"] = maxWorkers
	return appconfig.RenderSystemPrompt(workspace, defaultManagerPromptTemplate, ctx)
}

func buildWorkerPrompt(workspace string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPrompt(workspace, defaultWorkerPromptTemplate, ctx)
}

func resolveWorkspace(dir string) string {
	if dir == "" || dir == "." {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		return wd
	}
	return dir
}

// emitEvent 通过 Wails alpha.74 API 发射事件。
func emitEvent(name string, data any) {
	app := application.Get()
	if app != nil {
		app.Event.Emit(name, data)
	}
}

func emitEventOnCtx(ctx context.Context, name string, data any) {
	select {
	case <-ctx.Done():
		return
	default:
		emitEvent(name, data)
	}
}
