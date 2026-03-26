// miniroom 是一个 Agent 驱动的多人文字解密游戏（海龟汤）。
//
// 通过 WebSocket 接入，每个房间拥有独立的 Kernel 和 Session，
// Agent 作为游戏主持人引导玩家通过是非题推理出隐藏的真相。
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
//
// 然后在浏览器打开 http://localhost:8091
package main

import (
	"context"
	"embed"
	"log/slog"
	"net/http"

	"github.com/mossagents/moss/agentkit"
	appconfig "github.com/mossagents/moss/kernel/config"
	"github.com/mossagents/moss/kernel/logging"
	"golang.org/x/net/websocket"
)

//go:embed index.html
var staticFS embed.FS

func main() {
	appconfig.SetAppName("miniroom")
	_ = appconfig.EnsureAppDir()

	flags := agentkit.ParseAppFlags()

	ctx, cancel := agentkit.ContextWithSignal(context.Background())
	defer cancel()

	agentkit.PrintBannerWithHint("miniroom", map[string]string{
		"Provider": flags.Provider,
		"Model":    flags.Model,
		"Scripts":  "🐢 海龟汤 / 🕵️ 谁是卧底",
		"Listen":   "http://localhost:8091",
	}, "在浏览器中打开 http://localhost:8091 创建或加入房间")

	mgr := NewRoomManager(flags)

	http.Handle("/ws", websocket.Handler(func(conn *websocket.Conn) {
		handleWS(ctx, mgr, conn)
	}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, _ := staticFS.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	srv := &http.Server{Addr: ":8091"}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	logger := logging.GetLogger()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server error", slog.Any("error", err))
	}
}

// handleWS 处理一个 WebSocket 连接的完整生命周期。
func handleWS(ctx context.Context, mgr *RoomManager, conn *websocket.Conn) {
	defer conn.Close()

	var currentRoom *Room
	var currentUserID string

	defer func() {
		if currentRoom != nil && currentUserID != "" {
			currentRoom.leave(currentUserID)
		}
	}()

	// 连接建立后立即发送可用剧本列表
	websocket.JSON.Send(conn, ServerMsg{Type: MsgScripts, Scripts: ListScripts()})

	for {
		var msg ClientMsg
		if err := websocket.JSON.Receive(conn, &msg); err != nil {
			return // 连接关闭
		}

		switch msg.Type {
		case MsgCreateRoom:
			if currentRoom != nil {
				currentRoom.leave(currentUserID)
			}

			scriptID := msg.Script
			if scriptID == "" {
				scriptID = "turtle_soup" // 默认剧本
			}

			room, err := mgr.CreateRoom(ctx, scriptID)
			if err != nil {
				websocket.JSON.Send(conn, ServerMsg{Type: MsgError, Content: "创建房间失败: " + err.Error()})
				continue
			}

			// 生成用户身份（使用客户端传来的用户名，否则随机）
			userID := generateUserID()
			userName := msg.UserName
			if userName == "" {
				userName = generateUserName()
			}

			// 先发 room_created 让 JS 记录用户名，再 join 触发 room_joined
			websocket.JSON.Send(conn, ServerMsg{
				Type: MsgRoomCreated,
				Room: room.Code,
			})

			player := &Player{ID: userID, Name: userName, conn: conn}
			room.join(player)

			currentRoom = room
			currentUserID = userID

		case MsgJoinRoom:
			if msg.Room == "" {
				websocket.JSON.Send(conn, ServerMsg{Type: MsgError, Content: "请输入房间号"})
				continue
			}
			room, ok := mgr.GetRoom(msg.Room)
			if !ok {
				websocket.JSON.Send(conn, ServerMsg{Type: MsgError, Content: "房间 " + msg.Room + " 不存在"})
				continue
			}

			if currentRoom != nil {
				currentRoom.leave(currentUserID)
			}

			// 使用客户端传来的身份（如果有），否则新生成
			userID := msg.UserID
			userName := msg.UserName
			if userID == "" {
				userID = generateUserID()
			}
			if userName == "" {
				userName = generateUserName()
			}

			player := &Player{ID: userID, Name: userName, conn: conn}
			room.join(player) // join() 内部已发送 room_joined（含用户名）

			currentRoom = room
			currentUserID = userID

		case MsgChat:
			if currentRoom == nil {
				websocket.JSON.Send(conn, ServerMsg{Type: MsgError, Content: "请先加入房间"})
				continue
			}
			if msg.Content == "" {
				continue
			}

			currentRoom.mu.RLock()
			player := currentRoom.players[currentUserID]
			currentRoom.mu.RUnlock()

			if player == nil {
				continue
			}

			// 投递到房间消息队列（非阻塞）
			select {
			case currentRoom.msgCh <- playerMessage{player: player, content: msg.Content}:
			default:
				websocket.JSON.Send(conn, ServerMsg{Type: MsgError, Content: "消息队列已满，请稍后"})
			}

		case MsgSelectTopic:
			if currentRoom == nil || currentRoom.ScriptID != "chat" {
				continue
			}
			if msg.Topic == "" {
				continue
			}
			currentRoom.mu.RLock()
			player := currentRoom.players[currentUserID]
			currentRoom.mu.RUnlock()
			if player != nil {
				currentRoom.SelectTopic(msg.Topic, player)
			}

		case MsgSelectChars:
			if currentRoom == nil || currentRoom.ScriptID != "chat" {
				continue
			}
			if len(msg.Chars) == 0 {
				continue
			}
			currentRoom.mu.RLock()
			player := currentRoom.players[currentUserID]
			currentRoom.mu.RUnlock()
			if player != nil {
				currentRoom.SelectCharacters(msg.Chars, player)
			}

		case MsgLeaveRoom:
			if currentRoom != nil {
				currentRoom.leave(currentUserID)
				currentRoom = nil
				currentUserID = ""
			}
		}
	}
}
