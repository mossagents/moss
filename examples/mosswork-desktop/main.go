// mosswork-desktop 是基于 Wails v3 的桌面端 AI 助手。
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
	"encoding/json"
	"flag"
	"fmt"
	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
	"github.com/wailsapp/wails/v3/pkg/application"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed templates/manager_system_prompt.tmpl
var defaultManagerPromptTemplate string

//go:embed templates/worker_system_prompt.tmpl
var defaultWorkerPromptTemplate string

func main() {
	if err := appkit.InitializeApp("mosswork-desktop", nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
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
	globalEmitter = newSerialEmitter(mainWindow)

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
	fs := flag.NewFlagSet("mosswork-desktop", flag.ContinueOnError)
	appkit.BindAppFlags(fs, common)
	fs.IntVar(&c.workers, "workers", 3, "Max parallel workers")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := appkit.InitializeApp("mosswork-desktop", common, "MOSSWORK_DESKTOP", "MOSS"); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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
	if dir != "" && dir != "." {
		return dir
	}
	// Default to a dedicated workspace directory under the app data folder.
	// This prevents the agent from operating on whatever directory the process
	// happened to be launched from (e.g. the user's home directory).
	appDir := appconfig.AppDir()
	if appDir != "" {
		ws := filepath.Join(appDir, "workspace")
		_ = os.MkdirAll(ws, 0700)
		return ws
	}
	// Last-resort fallback (should not happen in practice).
	return "."
}

// serialEmitter dispatches Wails custom events to the webview in strict FIFO order.
//
// Wails' app.Event.Emit spawns goroutines internally for each call, causing events
// emitted in rapid succession (e.g. LLM stream tokens) to race and arrive in the
// JS frontend out of order. By routing all emissions through a single buffered channel
// and calling window.ExecJS directly, we guarantee delivery order matches emission order.
type serialEmitter struct {
	ch chan serialEmitMsg
}

type serialEmitMsg struct {
	name string
	data any
}

func newSerialEmitter(window application.Window) *serialEmitter {
	e := &serialEmitter{ch: make(chan serialEmitMsg, 2048)}
	go e.run(window)
	return e
}

func (e *serialEmitter) run(window application.Window) {
	for msg := range e.ch {
		payload := map[string]any{
			"name":   msg.name,
			"data":   msg.data,
			"sender": "",
		}
		b, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		window.ExecJS(fmt.Sprintf(
			`if(window._wails&&window._wails.dispatchWailsEvent){window._wails.dispatchWailsEvent(%s);}`,
			string(b),
		))
	}
}

func (e *serialEmitter) emit(name string, data any) {
	e.ch <- serialEmitMsg{name: name, data: data}
}

// globalEmitter is initialised in main() once the window is created.
var globalEmitter *serialEmitter

// emitEvent dispatches a custom event to the webview via the serial emitter.
func emitEvent(name string, data any) {
	if globalEmitter != nil {
		globalEmitter.emit(name, data)
		return
	}
	// Fallback before window is ready (should not happen in normal flow).
	if app := application.Get(); app != nil {
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
