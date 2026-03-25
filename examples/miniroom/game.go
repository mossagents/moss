package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/tool"
)

//go:embed templates/*.tmpl
var promptFS embed.FS

// ── 剧本注册表 ──────────────────────────────────────

// Script 定义一个游戏剧本。
type Script struct {
	ID          string // 唯一标识
	Name        string // 显示名称
	Emoji       string // 代表 emoji
	Description string // 简短描述
	Template    string // 模板文件内容
}

// ScriptInfo 用于向客户端发送可用剧本列表。
type ScriptInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Emoji       string `json:"emoji"`
	Description string `json:"description"`
}

var scriptRegistry map[string]*Script

func init() {
	scriptRegistry = make(map[string]*Script)
	files := []struct {
		id, name, emoji, desc, filename string
	}{
		{"turtle_soup", "海龟汤", "🐢", "横向思维解谜：通过是非题推理隐藏真相", "templates/game_prompt.tmpl"},
		{"spy", "谁是卧底", "🕵️", "身份推理：找出拿到不同词语的卧底玩家", "templates/spy_prompt.tmpl"},
		{"chat", "陡聊室", "🎭", "AI 扮演多个虚拟角色与你聊天互动", "templates/chat_prompt.tmpl"},
	}
	for _, f := range files {
		data, err := promptFS.ReadFile(f.filename)
		if err != nil {
			panic("load script template " + f.filename + ": " + err.Error())
		}
		scriptRegistry[f.id] = &Script{
			ID:          f.id,
			Name:        f.name,
			Emoji:       f.emoji,
			Description: f.desc,
			Template:    string(data),
		}
	}
}

// GetScript 获取指定 ID 的剧本。
func GetScript(id string) (*Script, bool) {
	s, ok := scriptRegistry[id]
	return s, ok
}

// ListScripts 返回所有可用剧本信息。
func ListScripts() []ScriptInfo {
	list := make([]ScriptInfo, 0, len(scriptRegistry))
	// 保证顺序
	for _, id := range []string{"turtle_soup", "spy", "chat"} {
		if s, ok := scriptRegistry[id]; ok {
			list = append(list, ScriptInfo{
				ID:          s.ID,
				Name:        s.Name,
				Emoji:       s.Emoji,
				Description: s.Description,
			})
		}
	}
	return list
}

// buildSystemPrompt 渲染指定剧本的系统提示词。
func buildSystemPrompt(workspace string, script *Script) string {
	return appkit.RenderSystemPrompt(workspace, script.Template, nil)
}

// ── 陪聊主题 ────────────────────────────────────────

var chatTopics = []ChatTopicInfo{
	{ID: "emotion", Name: "深夜情感", Emoji: "🌃", Description: "谈心事、聊感情、倾诉烦恼"},
	{ID: "acg", Name: "二次元宅聊", Emoji: "🎮", Description: "动漫、游戏、cosplay、番剧推荐"},
	{ID: "daily", Name: "日常闲聊", Emoji: "🍵", Description: "轻松话题、分享日常、吐槽生活"},
	{ID: "literature", Name: "文艺沙龙", Emoji: "📚", Description: "文学、音乐、电影、诗歌"},
	{ID: "party", Name: "派对嗨聊", Emoji: "🎉", Description: "潮流、八卦、搞笑、段子"},
}

// ListChatTopics 返回所有可用的陪聊主题。
func ListChatTopics() []ChatTopicInfo {
	return chatTopics
}

// ── 工具定义 ─────────────────────────────────────────

func registerGameTools(reg tool.Registry, room *Room) {
	_ = reg.Register(getPlayersSpec, getPlayersHandler(room))
	_ = reg.Register(whisperSpec, whisperHandler(room))
	_ = reg.Register(announceSpec, announceHandler(room))
	_ = reg.Register(setGameStateSpec, setGameStateHandler(room))
	_ = reg.Register(addVirtualPlayerSpec, addVirtualPlayerHandler(room))
	_ = reg.Register(chatAsSpec, chatAsHandler(room))
	_ = reg.Register(getTimeSpec, getTimeHandler())
	_ = reg.Register(getWeatherSpec, getWeatherHandler())
	_ = reg.Register(setReminderSpec, setReminderHandler(room))
	_ = reg.Register(randomPickSpec, randomPickHandler())
}

// ── get_players ─────────────────────────────────────

var getPlayersSpec = tool.ToolSpec{
	Name:        "get_players",
	Description: "获取当前房间在线玩家列表",
	InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	Risk:        tool.RiskLow,
}

func getPlayersHandler(room *Room) tool.ToolHandler {
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(room.playerInfos())
	}
}

// ── whisper ─────────────────────────────────────────

var whisperSpec = tool.ToolSpec{
	Name:        "whisper",
	Description: "给指定玩家发送私密消息，其他玩家看不到",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"player_name": {"type": "string", "description": "目标玩家的名字"},
			"content": {"type": "string", "description": "私密消息内容"}
		},
		"required": ["player_name", "content"]
	}`),
	Risk: tool.RiskLow,
}

func whisperHandler(room *Room) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			PlayerName string `json:"player_name"`
			Content    string `json:"content"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}

		room.mu.RLock()
		var target *Player
		for _, p := range room.players {
			if p.Name == args.PlayerName {
				target = p
				break
			}
		}
		room.mu.RUnlock()

		if target == nil {
			return json.Marshal(map[string]string{"status": "error", "message": "玩家不存在"})
		}

		target.send(ServerMsg{Type: MsgWhisper, Content: args.Content, From: "主持人"})
		return json.Marshal(map[string]string{"status": "sent"})
	}
}

// ── announce ────────────────────────────────────────

var announceSpec = tool.ToolSpec{
	Name:        "announce",
	Description: "向房间所有人发送系统公告",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "公告内容"}
		},
		"required": ["content"]
	}`),
	Risk: tool.RiskLow,
}

func announceHandler(room *Room) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}

		room.addHistory("系统", args.Content, MsgSystem)
		room.broadcast(ServerMsg{Type: MsgSystem, Content: args.Content})
		return json.Marshal(map[string]string{"status": "announced"})
	}
}

// ── set_game_state ──────────────────────────────────

var setGameStateSpec = tool.ToolSpec{
	Name:        "set_game_state",
	Description: "设置游戏状态：lobby（等待中）、playing（进行中）、ended（已结束）",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"state": {
				"type": "string",
				"enum": ["lobby", "playing", "ended"],
				"description": "目标游戏状态"
			}
		},
		"required": ["state"]
	}`),
	Risk: tool.RiskLow,
}

func setGameStateHandler(room *Room) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}

		room.mu.Lock()
		room.state = GameState(args.State)
		room.mu.Unlock()

		label := map[string]string{
			"lobby":   "🏠 等待中",
			"playing": "🎮 游戏进行中",
			"ended":   "🏁 游戏结束",
		}[args.State]
		if label == "" {
			label = args.State
		}

		room.broadcast(ServerMsg{
			Type:    MsgGameState,
			State:   args.State,
			Content: fmt.Sprintf("游戏状态已更新：%s", label),
		})
		return json.Marshal(map[string]string{"status": "updated", "state": args.State})
	}
}

// ── add_virtual_player ──────────────────────────────

var addVirtualPlayerSpec = tool.ToolSpec{
	Name:        "add_virtual_player",
	Description: "添加一个虚拟角色到房间，虚拟角色会出现在在线列表中",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":    {"type": "string", "description": "虚拟角色的名字"},
			"persona": {"type": "string", "description": "角色设定简述（如：活泼可爱的高中生）"}
		},
		"required": ["name", "persona"]
	}`),
	Risk: tool.RiskLow,
}

func addVirtualPlayerHandler(room *Room) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Name    string `json:"name"`
			Persona string `json:"persona"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}

		room.AddVirtualPlayer(args.Name, args.Persona)
		return json.Marshal(map[string]string{"status": "added", "name": args.Name})
	}
}

// ── chat_as ─────────────────────────────────────────

var chatAsSpec = tool.ToolSpec{
	Name:        "chat_as",
	Description: "以指定虚拟角色的身份在房间中发送消息",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":    {"type": "string", "description": "虚拟角色的名字（需先 add_virtual_player）"},
			"content": {"type": "string", "description": "消息内容"}
		},
		"required": ["name", "content"]
	}`),
	Risk: tool.RiskLow,
}

func chatAsHandler(room *Room) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}

		room.mu.RLock()
		_, exists := room.virtualPlayers[args.Name]
		room.mu.RUnlock()

		if !exists {
			return json.Marshal(map[string]string{"status": "error", "message": "虚拟角色不存在: " + args.Name})
		}

		room.ChatAs(args.Name, args.Content)
		return json.Marshal(map[string]string{"status": "sent", "name": args.Name})
	}
}

// ── get_time ────────────────────────────────────────

var getTimeSpec = tool.ToolSpec{
	Name:        "get_time",
	Description: "获取当前日期和时间（北京时间）",
	InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	Risk:        tool.RiskLow,
}

func getTimeHandler() tool.ToolHandler {
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		loc, _ := time.LoadLocation("Asia/Shanghai")
		now := time.Now().In(loc)
		return json.Marshal(map[string]string{
			"datetime": now.Format("2006-01-02 15:04:05"),
			"weekday":  now.Weekday().String(),
		})
	}
}

// ── get_weather ─────────────────────────────────────

var getWeatherSpec = tool.ToolSpec{
	Name:        "get_weather",
	Description: "查询指定城市的天气（模拟数据，用于角色对话）",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {"type": "string", "description": "城市名称，如：杭州、北京、上海"}
		},
		"required": ["city"]
	}`),
	Risk: tool.RiskLow,
}

func getWeatherHandler() tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			City string `json:"city"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		// 模拟天气数据，虚拟角色场景不需要真实 API
		loc, _ := time.LoadLocation("Asia/Shanghai")
		hour := time.Now().In(loc).Hour()
		var weather, temp string
		switch {
		case hour >= 6 && hour < 12:
			weather, temp = "晴转多云", "18°C ~ 25°C"
		case hour >= 12 && hour < 18:
			weather, temp = "多云", "22°C ~ 28°C"
		default:
			weather, temp = "晴", "15°C ~ 20°C"
		}
		return json.Marshal(map[string]string{
			"city":    args.City,
			"weather": weather,
			"temp":    temp,
			"tip":     "适合外出，注意防晒",
		})
	}
}

// ── set_reminder ────────────────────────────────────

var setReminderSpec = tool.ToolSpec{
	Name:        "set_reminder",
	Description: "设定一个定时提醒，到时间后以指定虚拟角色的身份提醒某位玩家",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"minutes":     {"type": "number", "description": "几分钟后提醒"},
			"content":     {"type": "string", "description": "提醒内容"},
			"remind_as":   {"type": "string", "description": "以哪个虚拟角色身份提醒"},
			"target_name": {"type": "string", "description": "要提醒的玩家名字"}
		},
		"required": ["minutes", "content", "remind_as"]
	}`),
	Risk: tool.RiskLow,
}

func setReminderHandler(room *Room) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Minutes    float64 `json:"minutes"`
			Content    string  `json:"content"`
			RemindAs   string  `json:"remind_as"`
			TargetName string  `json:"target_name"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		if args.Minutes <= 0 || args.Minutes > 60 {
			return json.Marshal(map[string]string{"status": "error", "message": "提醒时间需在 1-60 分钟之间"})
		}

		dur := time.Duration(args.Minutes * float64(time.Minute))
		go func() {
			select {
			case <-time.After(dur):
				msg := args.Content
				if args.TargetName != "" {
					msg = fmt.Sprintf("@%s %s", args.TargetName, args.Content)
				}
				room.ChatAs(args.RemindAs, msg)
			case <-room.ctx.Done():
			}
		}()

		label := fmt.Sprintf("%.0f 分钟后提醒", args.Minutes)
		return json.Marshal(map[string]string{"status": "scheduled", "when": label})
	}
}

// ── random_pick ─────────────────────────────────────

var randomPickSpec = tool.ToolSpec{
	Name:        "random_pick",
	Description: "从给定选项中随机选一个（用于抽签、骰子等趣味互动）",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"options": {
				"type": "array",
				"items": {"type": "string"},
				"description": "候选选项列表"
			}
		},
		"required": ["options"]
	}`),
	Risk: tool.RiskLow,
}

func randomPickHandler() tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Options []string `json:"options"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, err
		}
		if len(args.Options) == 0 {
			return json.Marshal(map[string]string{"status": "error", "message": "选项不能为空"})
		}
		idx := time.Now().UnixNano() % int64(len(args.Options))
		return json.Marshal(map[string]string{
			"result": args.Options[idx],
			"total":  fmt.Sprintf("%d", len(args.Options)),
		})
	}
}
