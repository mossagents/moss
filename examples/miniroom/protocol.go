package main

// ── 客户端 → 服务端消息类型 ─────────────────────────

const (
	MsgCreateRoom = "create_room" // 创建房间
	MsgJoinRoom   = "join_room"   // 加入房间
	MsgChat       = "chat"        // 发送消息
	MsgLeaveRoom  = "leave_room"  // 离开房间
)

// ── 服务端 → 客户端消息类型 ─────────────────────────

const (
	MsgRoomCreated = "room_created" // 房间已创建
	MsgRoomJoined  = "room_joined"  // 成功加入房间
	MsgUserJoined  = "user_joined"  // 有人加入
	MsgUserLeft    = "user_left"    // 有人离开
	MsgUsers       = "users"        // 完整用户列表
	MsgChatBcast   = "chat_bcast"   // 玩家消息广播
	MsgAgent       = "agent"        // Agent 完整回复
	MsgStream      = "stream"       // Agent 流式片段
	MsgStreamEnd   = "stream_end"   // 流式结束
	MsgWhisper     = "whisper"      // 私信
	MsgSystem      = "system"       // 系统消息
	MsgGameState   = "game_state"   // 游戏状态
	MsgScripts     = "scripts"      // 可用剧本列表
	MsgChatTopics  = "chat_topics"  // 陪聊主题列表
	MsgSelectTopic = "select_topic" // 选择陪聊主题
	MsgChoiceCard  = "choice_card"  // 选择题卡片
	MsgError       = "error"        // 错误
)

// ClientMsg 是客户端发送到服务端的消息。
type ClientMsg struct {
	Type     string `json:"type"`
	Room     string `json:"room,omitempty"`
	Content  string `json:"content,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	Script   string `json:"script,omitempty"` // 创建房间时选择的剧本 ID
	Topic    string `json:"topic,omitempty"`  // 选择的陪聊主题 ID
}

// ServerMsg 是服务端发送到客户端的消息。
type ServerMsg struct {
	Type       string          `json:"type"`
	Room       string          `json:"room,omitempty"`
	Content    string          `json:"content,omitempty"`
	From       string          `json:"from,omitempty"`
	Users      []PlayerInfo    `json:"users,omitempty"`
	History    []HistoryMsg    `json:"history,omitempty"`
	State      string          `json:"state,omitempty"`
	Scripts    []ScriptInfo    `json:"scripts,omitempty"`     // 可用剧本列表
	ScriptID   string          `json:"script_id,omitempty"`   // 房间使用的剧本 ID
	ChatTopics []ChatTopicInfo `json:"chat_topics,omitempty"` // 陪聊主题列表
	Choices    *ChoiceCardInfo `json:"choices,omitempty"`     // 选择题卡片数据
}

// PlayerInfo 是玩家摘要信息。
type PlayerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Online    bool   `json:"online"`
	IsVirtual bool   `json:"is_virtual,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	Intro     string `json:"intro,omitempty"`
	Gender    string `json:"gender,omitempty"`
}

// ChatTopicInfo 用于向客户端发送陪聊主题列表。
type ChatTopicInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Emoji       string `json:"emoji"`
	Description string `json:"description"`
}

// ChoiceCardInfo 包含选择题卡片数据。
type ChoiceCardInfo struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// HistoryMsg 是房间历史消息。
type HistoryMsg struct {
	From    string `json:"from"` // 玩家名 / "主持人" / "系统"
	Content string `json:"content"`
	Type    string `json:"type"` // "chat", "agent", "system", "whisper"
}
