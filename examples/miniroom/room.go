package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"sync"

	"github.com/mossagi/moss/adapters"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
	"golang.org/x/net/websocket"
)

// ── 游戏状态 ────────────────────────────────────────

type GameState string

const (
	StateLobby   GameState = "lobby"
	StatePlaying GameState = "playing"
	StateEnded   GameState = "ended"
)

// ── Player ──────────────────────────────────────────

// Player 代表房间中的一位玩家。
type Player struct {
	ID     string
	Name   string
	conn   *websocket.Conn
	mu     sync.Mutex
	online bool
}

// send 向该玩家发送一条消息。
func (p *Player) send(msg ServerMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.online && p.conn != nil {
		_ = websocket.JSON.Send(p.conn, msg)
	}
}

// ── Room ────────────────────────────────────────────

// VirtualPlayer 代表由 Agent 扮演的虚拟角色。
type VirtualPlayer struct {
	Name    string
	Persona string // 角色设定简述
}

// Room 是一个游戏房间，拥有独立的 Kernel 和 Session。
type Room struct {
	Code           string
	players        map[string]*Player        // user_id → Player
	virtualPlayers map[string]*VirtualPlayer // name → VirtualPlayer
	history        []HistoryMsg
	state          GameState
	ScriptID       string
	Topic          string // 陪聊主题 ID（仅 chat 剧本）

	k    *kernel.Kernel
	sess *session.Session
	io   *RoomIO

	msgCh  chan playerMessage
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

type playerMessage struct {
	player  *Player
	content string
}

// addHistory 追加一条历史记录。
func (r *Room) addHistory(from, content, msgType string) {
	r.mu.Lock()
	r.history = append(r.history, HistoryMsg{From: from, Content: content, Type: msgType})
	r.mu.Unlock()
}

// broadcast 向房间内所有在线玩家广播消息。
func (r *Room) broadcast(msg ServerMsg) {
	r.mu.RLock()
	players := make([]*Player, 0, len(r.players))
	for _, p := range r.players {
		players = append(players, p)
	}
	r.mu.RUnlock()
	for _, p := range players {
		p.send(msg)
	}
}

// playerInfos 返回当前所有玩家信息（含虚拟角色）。
func (r *Room) playerInfos() []PlayerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]PlayerInfo, 0, len(r.players)+len(r.virtualPlayers))
	for _, p := range r.players {
		infos = append(infos, PlayerInfo{ID: p.ID, Name: p.Name, Online: p.online})
	}
	for _, vp := range r.virtualPlayers {
		infos = append(infos, PlayerInfo{ID: "v_" + vp.Name, Name: vp.Name, Online: true, IsVirtual: true})
	}
	return infos
}

// AddVirtualPlayer 添加一个虚拟角色到房间。
func (r *Room) AddVirtualPlayer(name, persona string) {
	r.mu.Lock()
	if r.virtualPlayers == nil {
		r.virtualPlayers = make(map[string]*VirtualPlayer)
	}
	r.virtualPlayers[name] = &VirtualPlayer{Name: name, Persona: persona}
	r.mu.Unlock()

	r.addHistory("系统", name+" 加入了房间", MsgSystem)
	r.broadcast(ServerMsg{Type: MsgUserJoined, Content: name, Users: r.playerInfos()})
}

// ChatAs 以虚拟角色身份在房间中发送消息。
func (r *Room) ChatAs(name, content string) {
	r.addHistory(name, content, "chat")
	r.broadcast(ServerMsg{Type: MsgChatBcast, From: name, Content: content})
}

// join 将一位玩家加入/重连到房间。
func (r *Room) join(p *Player) {
	r.mu.Lock()
	existing, ok := r.players[p.ID]
	if ok {
		// 重连：更新连接
		existing.mu.Lock()
		existing.conn = p.conn
		existing.online = true
		existing.mu.Unlock()
		p = existing
	} else {
		p.online = true
		r.players[p.ID] = p
	}
	r.mu.Unlock()

	// 返回历史和用户列表
	r.mu.RLock()
	hist := make([]HistoryMsg, len(r.history))
	copy(hist, r.history)
	r.mu.RUnlock()

	p.send(ServerMsg{
		Type:     MsgRoomJoined,
		Room:     r.Code,
		Content:  p.Name,
		Users:    r.playerInfos(),
		History:  hist,
		State:    string(r.state),
		ScriptID: r.ScriptID,
	})

	if !ok {
		// 新玩家加入通知
		r.addHistory("系统", p.Name+" 加入了房间", MsgSystem)
		r.broadcast(ServerMsg{Type: MsgUserJoined, Content: p.Name, Users: r.playerInfos()})

		// 陪聊剧本：第一个玩家加入时发送主题列表，等用户选择
		if r.ScriptID == "chat" && r.sess != nil && len(r.players) == 1 {
			p.send(ServerMsg{Type: MsgChatTopics, ChatTopics: ListChatTopics()})
		}
	} else {
		// 重连通知
		r.broadcast(ServerMsg{Type: MsgUsers, Users: r.playerInfos()})
	}
}

// leave 标记玩家离线。
func (r *Room) leave(userID string) {
	r.mu.Lock()
	p, ok := r.players[userID]
	if ok {
		p.mu.Lock()
		p.online = false
		p.conn = nil
		p.mu.Unlock()
	}
	r.mu.Unlock()

	if ok {
		r.addHistory("系统", p.Name+" 离开了房间", MsgSystem)
		r.broadcast(ServerMsg{Type: MsgUserLeft, Content: p.Name, Users: r.playerInfos()})
	}
}

// run 是房间主循环（在独立 goroutine 运行）。
func (r *Room) run() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case pm := <-r.msgCh:
			r.handlePlayerMessage(pm)
		}
	}
}

// SelectTopic 用户选择陪聊主题后，存储主题并触发 Agent 初始化。
func (r *Room) SelectTopic(topicID string, p *Player) {
	r.mu.Lock()
	r.Topic = topicID
	r.mu.Unlock()
	go r.triggerChatStart(p)
}

// triggerChatStart 在陪聊剧本中自动触发 Agent 初始化虚拟角色。
func (r *Room) triggerChatStart(p *Player) {
	initMsg := fmt.Sprintf("[系统]: 玩家 %s 进入了聊天室，选择的主题是【%s】，请立刻根据该主题初始化对应的角色并做自我介绍。", p.Name, r.Topic)
	r.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: initMsg})

	result, err := r.k.Run(r.ctx, r.sess)
	if err != nil {
		if r.ctx.Err() != nil {
			return
		}
		log.Printf("[room %s] chat auto-start error: %v", r.Code, err)
		r.broadcast(ServerMsg{Type: MsgError, Content: "自动启动失败: " + err.Error()})
		return
	}
	_ = result
}

// handlePlayerMessage 处理一条玩家消息。
func (r *Room) handlePlayerMessage(pm playerMessage) {
	// 1. 记录历史并广播
	r.addHistory(pm.player.Name, pm.content, "chat")
	r.broadcast(ServerMsg{
		Type:    MsgChatBcast,
		From:    pm.player.Name,
		Content: pm.content,
	})

	// 2. 拼接为 "[玩家名]: 内容" 作为用户消息交给 Agent
	userMsg := fmt.Sprintf("[%s]: %s", pm.player.Name, pm.content)
	r.sess.AppendMessage(port.Message{Role: port.RoleUser, Content: userMsg})

	// 3. 运行 Agent Loop（串行，当前消息处理完才处理下一条）
	result, err := r.k.Run(r.ctx, r.sess)
	if err != nil {
		if r.ctx.Err() != nil {
			return
		}
		log.Printf("[room %s] agent error: %v", r.Code, err)
		r.broadcast(ServerMsg{Type: MsgError, Content: "Agent 出错: " + err.Error()})
		return
	}
	_ = result
}

// ── RoomIO ──────────────────────────────────────────

// RoomIO 实现 port.UserIO，将 Agent 输出广播到房间所有玩家。
type RoomIO struct {
	room *Room
}

var _ port.UserIO = (*RoomIO)(nil)

func (io *RoomIO) Send(_ context.Context, msg port.OutputMessage) error {
	isChatScript := io.room.ScriptID == "chat"
	switch msg.Type {
	case port.OutputStream:
		if isChatScript {
			return nil // 陡聊剧本不显示主持人流式输出
		}
		io.room.broadcast(ServerMsg{Type: MsgStream, Content: msg.Content, From: "主持人"})
	case port.OutputStreamEnd:
		if isChatScript {
			return nil
		}
		io.room.broadcast(ServerMsg{Type: MsgStreamEnd, From: "主持人"})
	case port.OutputText:
		if isChatScript {
			return nil // 陡聊剧本主持人隐身，不广播直接文本
		}
		io.room.addHistory("主持人", msg.Content, "agent")
		io.room.broadcast(ServerMsg{Type: MsgAgent, Content: msg.Content, From: "主持人"})
	case port.OutputToolStart, port.OutputToolResult:
		// 工具调用内部过程不广播给玩家
	default:
		io.room.broadcast(ServerMsg{Type: MsgSystem, Content: msg.Content})
	}
	return nil
}

func (io *RoomIO) Ask(_ context.Context, _ port.InputRequest) (port.InputResponse, error) {
	// 游戏 Agent 的工具调用自动批准
	return port.InputResponse{Approved: true, Value: "y"}, nil
}

// ── RoomManager ─────────────────────────────────────

// RoomManager 管理所有房间的创建和查找。
type RoomManager struct {
	mu    sync.Mutex
	rooms map[string]*Room
	flags *appkit.CommonFlags
}

// NewRoomManager 创建房间管理器。
func NewRoomManager(flags *appkit.CommonFlags) *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Room),
		flags: flags,
	}
}

// CreateRoom 创建新房间，使用指定的剧本。
func (rm *RoomManager) CreateRoom(parentCtx context.Context, scriptID string) (*Room, error) {
	script, ok := GetScript(scriptID)
	if !ok {
		return nil, fmt.Errorf("未知剧本: %s", scriptID)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 生成不重复的 4 位房间号
	var code string
	for i := 0; i < 100; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10000))
		if err != nil {
			return nil, err
		}
		code = fmt.Sprintf("%04d", n.Int64())
		if _, exists := rm.rooms[code]; !exists {
			break
		}
	}
	if _, exists := rm.rooms[code]; exists {
		return nil, fmt.Errorf("无法生成可用的房间号")
	}

	ctx, cancel := context.WithCancel(parentCtx)

	room := &Room{
		Code:           code,
		ScriptID:       scriptID,
		players:        make(map[string]*Player),
		virtualPlayers: make(map[string]*VirtualPlayer),
		state:          StateLobby,
		msgCh:          make(chan playerMessage, 64),
		ctx:            ctx,
		cancel:         cancel,
	}

	roomIO := &RoomIO{room: room}
	room.io = roomIO

	// 构建该房间专属的 Kernel（无 sandbox，无内置文件工具）
	llm, err := adapters.BuildLLM(
		rm.flags.Provider, rm.flags.Model,
		rm.flags.APIKey, rm.flags.BaseURL,
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build llm: %w", err)
	}

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithUserIO(roomIO),
	)

	// 注册游戏专用工具
	registerGameTools(k.ToolRegistry(), room)

	if err := k.Boot(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("boot kernel: %w", err)
	}

	// 创建共享 Session
	prompt := buildSystemPrompt(rm.flags.Workspace, script)
	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         script.ID,
		Mode:         "interactive",
		MaxSteps:     200,
		SystemPrompt: prompt,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create session: %w", err)
	}

	room.k = k
	room.sess = sess

	rm.rooms[code] = room

	// 启动房间消息处理循环
	go room.run()

	log.Printf("[room %s] created", code)
	return room, nil
}

// GetRoom 获取已存在的房间。
func (rm *RoomManager) GetRoom(code string) (*Room, bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	r, ok := rm.rooms[code]
	return r, ok
}

// ── 工具函数 ────────────────────────────────────────

var chineseNames = []string{
	"小龙", "阿凤", "大白", "小黑", "阿蓝",
	"星辰", "明月", "清风", "细雨", "流云",
	"竹叶", "松果", "梅花", "桃子", "柳絮",
	"铁柱", "翠花", "阿福", "小鱼", "石头",
}

// generateUserID 生成随机用户 ID。
func generateUserID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "u_" + hex.EncodeToString(b)
}

// generateUserName 从候选列表中随机选择一个中文名。
func generateUserName() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chineseNames))))
	return chineseNames[n.Int64()]
}
