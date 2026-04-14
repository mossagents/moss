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
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"fmt"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/runtime/events"
	"golang.org/x/net/websocket"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed index.html
var staticFS embed.FS

func main() {
	flags := appkit.ParseAppFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	token, err := websocketAccessToken()
	if err != nil {
		logging.GetLogger().Error("token generation failed", slog.Any("error", err))
		return
	}
	listenAddr := "127.0.0.1:8090"
	listenURL := "http://" + listenAddr + "/?token=" + token

	appkit.PrintBannerWithHint("websocket", map[string]string{
		"Provider": flags.Provider,
		"Model":    flags.Model,
		"Listen":   listenURL,
	}, "只在本机浏览器打开上面的地址开始对话")

	// 每个 WebSocket 连接创建独立的 Kernel + Session
	http.Handle("/ws", websocket.Server{
		Handshake: func(cfg *websocket.Config, req *http.Request) error {
			if err := validateWebSocketOrigin(req); err != nil {
				return err
			}
			if !validWebSocketToken(req, token) {
				return fmt.Errorf("invalid websocket token")
			}
			return nil
		},
		Handler: func(conn *websocket.Conn) {
			handleConnection(ctx, flags, conn)
		},
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, _ := staticFS.ReadFile("index.html")
		page := strings.ReplaceAll(string(data), "__MOSS_WS_TOKEN__", token)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})

	srv := &http.Server{Addr: listenAddr}
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
			userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(msg.Content)}}
			sess.AppendMessage(userMsg)
			result, err := kernel.CollectRunAgentResult(connCtx, k, kernel.RunAgentRequest{
				Session:     sess,
				Agent:       k.BuildLLMAgent("websocket"),
				UserContent: &userMsg,
			})
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

func websocketAccessToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("MOSS_WS_TOKEN")); token != "" {
		return token, nil
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validWebSocketToken(req *http.Request, expected string) bool {
	if req == nil {
		return false
	}
	token := strings.TrimSpace(req.URL.Query().Get("token"))
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func validateWebSocketOrigin(req *http.Request) error {
	if req == nil {
		return fmt.Errorf("missing request")
	}
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return fmt.Errorf("origin header is required")
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid origin: %w", err)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	switch host {
	case "127.0.0.1", "localhost":
		return nil
	default:
		return fmt.Errorf("origin %q is not allowed", origin)
	}
}

// ── WebSocket 消息协议 ──────────────────────────────

type wsMsg struct {
	Type     string              `json:"type"`               // user, assistant, system, error, stream, stream_end, tool_start, tool_result, ask
	Content  string              `json:"content"`            // 消息内容
	AskType  string              `json:"ask_type,omitempty"` // confirm, select, free_text
	Options  []string            `json:"options,omitempty"`  // select 选项
	Meta     map[string]any      `json:"meta,omitempty"`
	Approval *io.ApprovalRequest `json:"approval,omitempty"`
}

// ── WebSocket UserIO 实现 ───────────────────────────

// WebSocketIO 实现 io.UserIO，通过 WebSocket 双向通信。
type WebSocketIO struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

var _ io.UserIO = (*WebSocketIO)(nil)

func (w *WebSocketIO) Send(_ context.Context, msg io.OutputMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	ev := events.FromOutputMessage(msg, time.Now().UTC())
	wsType := string(ev.Type)
	if wsType == string(events.EventAssistantMessage) {
		wsType = "assistant"
	}
	return websocket.JSON.Send(w.conn, wsMsg{Type: wsType, Content: ev.Content, Meta: ev.Meta})
}

func (w *WebSocketIO) Ask(_ context.Context, req io.InputRequest) (io.InputResponse, error) {
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
		return io.InputResponse{}, err
	}

	// 阻塞等待客户端回复
	var reply wsMsg
	if err := websocket.JSON.Receive(w.conn, &reply); err != nil {
		return io.InputResponse{}, err
	}

	resp := io.InputResponse{Value: reply.Content}
	if req.Type == io.InputConfirm {
		resp.Approved = reply.Content == "y" || reply.Content == "yes"
		if req.Approval != nil {
			resp.Decision = &io.ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  resp.Approved,
				Source:    "websocket",
			}
		}
	}
	return resp, nil
}
