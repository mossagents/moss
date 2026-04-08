package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/interaction"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
	providers "github.com/mossagents/moss/providers"
	"github.com/mossagents/moss/sandbox"
	"golang.org/x/net/websocket"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"
)

// ── 游戏状态 ────────────────────────────────────────

type GameState string

const (
	StateLobby GameState = "lobby"
	StateEnded GameState = "ended"
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
	Avatar  string // 头像 emoji
	Intro   string // 一句话简介
}

// Room 是一个游戏房间，拥有独立的 Kernel 和 Session。
type Room struct {
	Code           string
	players        map[string]*Player        // user_id → Player
	virtualPlayers map[string]*VirtualPlayer // name → VirtualPlayer
	history        []HistoryMsg
	state          GameState
	ScriptID       string
	Topic          string          // 陪聊主题 ID（仅 chat 剧本）
	selectedChars  []string        // 用户选定的角色名列表
	gameContext    json.RawMessage // 持久化游戏上下文（角色扮演进度）

	k    *kernel.Kernel
	sess *session.Session
	io   *RoomIO

	// 自主对话循环
	lastUserMsg   time.Time   // 上次真实用户发言时间
	autoTimer     *time.Timer // 自主对话计时器
	autoTurnCount int         // 已执行的自主对话轮次

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
		infos = append(infos, PlayerInfo{
			ID: "v_" + vp.Name, Name: vp.Name, Online: true, IsVirtual: true,
			Avatar: vp.Avatar, Intro: vp.Intro,
		})
	}
	return infos
}

// AddVirtualPlayer 添加一个虚拟角色到房间。
func (r *Room) AddVirtualPlayer(name, persona, avatar, intro string) {
	r.mu.Lock()
	if r.virtualPlayers == nil {
		r.virtualPlayers = make(map[string]*VirtualPlayer)
	}
	r.virtualPlayers[name] = &VirtualPlayer{Name: name, Persona: persona, Avatar: avatar, Intro: intro}
	r.mu.Unlock()

	r.addHistory("系统", name+" 加入了房间", MsgSystem)
	r.broadcast(ServerMsg{Type: MsgUserJoined, Content: name, Users: r.playerInfos()})
}

// ChatAs 以虚拟角色身份在房间中发送消息。
func (r *Room) ChatAs(name, content string) {
	r.addHistory(name, content, "chat")
	r.broadcast(ServerMsg{Type: MsgChatBcast, From: name, Content: content})
}

// AskChoice 以虚拟角色身份发送选择题卡片。
func (r *Room) AskChoice(name, question string, options []string) {
	r.addHistory(name, question, "choice")
	r.broadcast(ServerMsg{
		Type:    MsgChoiceCard,
		From:    name,
		Choices: &ChoiceCardInfo{Question: question, Options: options},
	})
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

// SelectTopic 用户选择陪聊主题后，发送角色列表供用户选择。
func (r *Room) SelectTopic(topicID string, p *Player) {
	r.mu.Lock()
	r.Topic = topicID
	r.mu.Unlock()

	// 发送该主题下的角色列表，由用户选择 3-5 个
	chars := CharactersForTheme(topicID)
	charInfos := make([]CharInfo, len(chars))
	for i, c := range chars {
		charInfos[i] = CharInfo{Name: c.Name, Avatar: c.Avatar, Intro: c.Intro, Gender: c.Gender}
	}
	p.send(ServerMsg{Type: MsgCharList, CharList: charInfos})
}

// SelectCharacters 用户选择角色后，启动陪聊。
func (r *Room) SelectCharacters(charNames []string, p *Player) {
	// 验证角色数量
	if len(charNames) < 3 {
		charNames = charNames[:0]
		// 自动补齐到 3 个
		chars := CharactersForTheme(r.Topic)
		for _, c := range chars {
			charNames = append(charNames, c.Name)
			if len(charNames) >= 3 {
				break
			}
		}
	}
	if len(charNames) > 5 {
		charNames = charNames[:5]
	}

	// 保存选定角色名
	r.mu.Lock()
	r.selectedChars = charNames
	r.mu.Unlock()

	go r.triggerChatStart(p)
}

// triggerChatStart 在陪聊剧本中自动触发 Agent 初始化虚拟角色。
func (r *Room) triggerChatStart(p *Player) {
	// 构建选定角色的详细信息
	r.mu.RLock()
	charNames := r.selectedChars
	r.mu.RUnlock()

	var charDetails []string
	for _, name := range charNames {
		if c, ok := CharacterByName(name); ok {
			charDetails = append(charDetails, fmt.Sprintf("- %s（%s）：%s。人设：%s", c.Name, c.Avatar, c.Intro, c.Persona))
		}
	}

	initMsg := fmt.Sprintf("[系统]: 玩家 %s 进入了聊天室，选择的主题是【%s】。\n\n用户指定的角色（请严格使用这些角色）：\n%s\n\n请立刻为每个角色调用 add_virtual_player 注册，然后让它们做自我介绍。",
		p.Name, r.Topic, strings.Join(charDetails, "\n"))
	r.sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart(initMsg)}})

	// 记录用户活动时间（初始化也算活动）
	r.mu.Lock()
	r.lastUserMsg = time.Now()
	r.mu.Unlock()

	if !r.runSession("chat auto-start error", "自动启动失败", true) {
		return
	}

	// 初始化完成后启动自主对话循环
	r.startAutoLoop()
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

	// 2. 记录用户活动时间 & 重置自主对话
	r.mu.Lock()
	r.lastUserMsg = time.Now()
	r.autoTurnCount = 0
	r.mu.Unlock()

	// 3. 拼接为 "[玩家名]: 内容" 作为用户消息交给 Agent
	userMsg := fmt.Sprintf("[%s]: %s", pm.player.Name, pm.content)
	r.sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart(userMsg)}})

	// 4. 运行 Agent Loop（串行，当前消息处理完才处理下一条）
	if !r.runSession("agent error", "Agent 出错", true) {
		return
	}

	// 5. 用户发言后重启自主对话计时
	if r.ScriptID == "chat" {
		r.startAutoLoop()
	}
}

// runSession 运行一次 Agent 会话，并按需要记录/广播错误。
func (r *Room) runSession(logMsg, userErrPrefix string, notifyUsers bool) bool {
	_, err := r.k.Run(r.ctx, r.sess)
	if err == nil {
		return true
	}
	if r.ctx.Err() != nil {
		return false
	}

	logging.GetLogger().ErrorContext(r.ctx, logMsg,
		slog.String("room", r.Code),
		slog.Any("error", err),
	)
	if notifyUsers {
		r.broadcast(ServerMsg{Type: MsgError, Content: userErrPrefix + ": " + err.Error()})
	}
	return false
}

// ── 自主对话循环 ────────────────────────────────────

const (
	autoMaxTurns   = 6  // 最大自主对话轮次
	autoWindowMins = 10 // 自主对话窗口（分钟）
)

// autoDelay 返回第 n 轮自主对话的延迟时间（递增）。
func autoDelay(turn int) time.Duration {
	delays := []time.Duration{
		45 * time.Second,
		60 * time.Second,
		90 * time.Second,
		120 * time.Second,
		150 * time.Second,
		180 * time.Second,
	}
	if turn < len(delays) {
		return delays[turn]
	}
	return 180 * time.Second
}

// startAutoLoop 启动或重置自主对话计时器。
func (r *Room) startAutoLoop() {
	r.mu.Lock()
	// 停掉旧的计时器
	if r.autoTimer != nil {
		r.autoTimer.Stop()
	}
	delay := autoDelay(r.autoTurnCount)
	r.autoTimer = time.AfterFunc(delay, r.fireAutoTurn)
	r.mu.Unlock()
}

// fireAutoTurn 触发一次自主对话。
func (r *Room) fireAutoTurn() {
	r.mu.RLock()
	lastMsg := r.lastUserMsg
	turnCount := r.autoTurnCount
	state := r.state
	r.mu.RUnlock()

	elapsed := time.Since(lastMsg)

	// 检查是否超出窗口或轮次限制
	if state == StateEnded || turnCount >= autoMaxTurns || elapsed > autoWindowMins*time.Minute {
		return
	}

	prompt := fmt.Sprintf(`[系统-自主对话]: 距离上次用户发言已过 %d 秒，虚拟角色可以自主闲聊或互动。请自然地让1-2个角色说点什么（闲聊、接之前的话题、互相调侃等），但控制篇幅简短。如果觉得没什么好说的，回复"skip"即可。`,
		int(elapsed.Seconds()))

	r.sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart(prompt)}})

	if !r.runSession("auto-turn error", "", false) {
		return
	}

	// 递增轮次并安排下一轮
	r.mu.Lock()
	r.autoTurnCount++
	r.mu.Unlock()

	r.startAutoLoop()
}

// ── RoomIO ──────────────────────────────────────────

// RoomIO 实现 intr.UserIO，将 Agent 输出广播到房间所有玩家。
type RoomIO struct {
	room *Room
}

var _ intr.UserIO = (*RoomIO)(nil)

func (io *RoomIO) Send(_ context.Context, msg intr.OutputMessage) error {
	isChatScript := io.room.ScriptID == "chat"
	switch msg.Type {
	case intr.OutputStream:
		if isChatScript {
			return nil // 陡聊剧本不显示主持人流式输出
		}
		io.room.broadcast(ServerMsg{Type: MsgStream, Content: msg.Content, From: "主持人"})
	case intr.OutputStreamEnd:
		if isChatScript {
			return nil
		}
		io.room.broadcast(ServerMsg{Type: MsgStreamEnd, From: "主持人"})
	case intr.OutputText:
		if isChatScript {
			return nil // 陡聊剧本主持人隐身，不广播直接文本
		}
		io.room.addHistory("主持人", msg.Content, "agent")
		io.room.broadcast(ServerMsg{Type: MsgAgent, Content: msg.Content, From: "主持人"})
	case intr.OutputToolStart, intr.OutputToolResult:
		// 工具调用内部过程不广播给玩家
	default:
		io.room.broadcast(ServerMsg{Type: MsgSystem, Content: msg.Content})
	}
	return nil
}

func (io *RoomIO) Ask(_ context.Context, _ intr.InputRequest) (intr.InputResponse, error) {
	// 游戏 Agent 的工具调用自动批准
	return intr.InputResponse{Approved: true, Value: "y"}, nil
}

// ── RoomManager ─────────────────────────────────────

// RoomManager 管理所有房间的创建和查找。
type RoomManager struct {
	mu    sync.Mutex
	rooms map[string]*Room
	flags *appkit.AppFlags
}

// NewRoomManager 创建房间管理器。
func NewRoomManager(flags *appkit.AppFlags) *RoomManager {
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
	llm, err := providers.BuildLLM(
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
		kernel.WithWorkspace(sandbox.NewMemoryWorkspace()),
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

	logging.GetLogger().InfoContext(context.Background(), "room created",
		slog.String("code", code),
	)
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
