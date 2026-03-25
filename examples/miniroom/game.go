package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

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
	// 保证顺序：turtle_soup 在前
	for _, id := range []string{"turtle_soup", "spy"} {
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

// ── 工具定义 ─────────────────────────────────────────

func registerGameTools(reg tool.Registry, room *Room) {
	_ = reg.Register(getPlayersSpec, getPlayersHandler(room))
	_ = reg.Register(whisperSpec, whisperHandler(room))
	_ = reg.Register(announceSpec, announceHandler(room))
	_ = reg.Register(setGameStateSpec, setGameStateHandler(room))
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
