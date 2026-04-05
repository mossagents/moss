// websocket 演示如何实现 WebSocket UserIO 适配器。
//
// 运行后在 http://localhost:8090 打开内置页面进行对话。
// 展示了：
//   - 自定义 UserIO 实现（WebSocket 双向通信）
//   - 流式输出通过 WebSocket 实时推送到浏览器
//   - Ask 请求阻塞等待客户端 JSON 回复
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/runtime/events"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
	"golang.org/x/net/websocket"
)

//go:embed index.html
var staticFS embed.FS

func main() {
	flags := appkit.ParseAppFlags()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	appkit.PrintBannerWithHint("websocket", map[string]string{
		"Provider": flags.Provider,
		"Model":    flags.Model,
		"Listen":   "http://localhost:8090",
	}, "在浏览器中打开 http://localhost:8090 开始对话")

	// 每个 WebSocket 连接创建独立的 Kernel + Session
	http.Handle("/ws", websocket.Handler(func(conn *websocket.Conn) {
		handleConnection(ctx, flags, conn)
	}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, _ := staticFS.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	srv := &http.Server{Addr: ":8090"}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	logger := logging.GetLogger()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", slog.Any("error", err))
	}
}

func handleConnection(ctx context.Context, flags *appkit.AppFlags, conn *websocket.Conn) {
	defer conn.Close()

	wsIO := &WebSocketIO{conn: conn}

	k, err := appkit.BuildKernel(ctx, flags, wsIO)
	if err != nil {
		websocket.JSON.Send(conn, wsMsg{Type: "error", Content: err.Error()})
		return
	}

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	if err := k.Boot(connCtx); err != nil {
		websocket.JSON.Send(conn, wsMsg{Type: "error", Content: err.Error()})
		return
	}
	defer k.Shutdown(connCtx)

	sess, err := k.NewSession(connCtx, session.SessionConfig{
		Goal:         "interactive",
		Mode:         "interactive",
		TrustLevel:   flags.Trust,
		MaxSteps:     100,
		SystemPrompt: "You are a helpful assistant. Answer concisely.",
	})
	if err != nil {
		websocket.JSON.Send(conn, wsMsg{Type: "error", Content: err.Error()})
		return
	}

	websocket.JSON.Send(conn, wsMsg{Type: "system", Content: fmt.Sprintf("Connected to %s", flags.Provider)})

	// 读取用户消息并执行
	for {
		var msg wsMsg
		if err := websocket.JSON.Receive(conn, &msg); err != nil {
			return // 连接关闭
		}

		if msg.Type == "user" && msg.Content != "" {
			sess.AppendMessage(port.Message{Role: port.RoleUser, ContentParts: []port.ContentPart{port.TextPart(msg.Content)}})
			result, err := k.Run(connCtx, sess)
			if err != nil {
				if connCtx.Err() != nil {
					return
				}
				websocket.JSON.Send(conn, wsMsg{Type: "error", Content: err.Error()})
				continue
			}
			_ = result
		}
	}
}

// ── WebSocket 消息协议 ──────────────────────────────

type wsMsg struct {
	Type     string                `json:"type"`               // user, assistant, system, error, stream, stream_end, tool_start, tool_result, ask
	Content  string                `json:"content"`            // 消息内容
	AskType  string                `json:"ask_type,omitempty"` // confirm, select, free_text
	Options  []string              `json:"options,omitempty"`  // select 选项
	Meta     map[string]any        `json:"meta,omitempty"`
	Approval *port.ApprovalRequest `json:"approval,omitempty"`
}

// ── WebSocket UserIO 实现 ───────────────────────────

// WebSocketIO 实现 port.UserIO，通过 WebSocket 双向通信。
type WebSocketIO struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

var _ port.UserIO = (*WebSocketIO)(nil)

func (w *WebSocketIO) Send(_ context.Context, msg port.OutputMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	ev := events.FromOutputMessage(msg, time.Now().UTC())
	wsType := string(ev.Type)
	if wsType == string(events.EventAssistantMessage) {
		wsType = "assistant"
	}
	return websocket.JSON.Send(w.conn, wsMsg{Type: wsType, Content: ev.Content, Meta: ev.Meta})
}

func (w *WebSocketIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	w.mu.Lock()
	askType := string(req.Type)
	err := websocket.JSON.Send(w.conn, wsMsg{
		Type:     "ask",
		Content:  req.Prompt,
		AskType:  askType,
		Options:  req.Options,
		Meta:     req.Meta,
		Approval: req.Approval,
	})
	w.mu.Unlock()
	if err != nil {
		return port.InputResponse{}, err
	}

	// 阻塞等待客户端回复
	var reply wsMsg
	if err := websocket.JSON.Receive(w.conn, &reply); err != nil {
		return port.InputResponse{}, err
	}

	resp := port.InputResponse{Value: reply.Content}
	if req.Type == port.InputConfirm {
		resp.Approved = reply.Content == "y" || reply.Content == "yes"
		if req.Approval != nil {
			resp.Decision = &port.ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  resp.Approved,
				Source:    "websocket",
			}
		}
	}
	return resp, nil
}
