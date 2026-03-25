package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mossagi/moss/kernel/tool"
)

// ── 系统提示词 ──────────────────────────────────────

const systemPrompt = `你是"海龟汤"解谜游戏的主持人 🐢

## 游戏规则
海龟汤是一种横向思维解谜游戏：
1. 主持人（你）准备一个离奇故事，包含"汤面"（令人困惑的谜面）和"汤底"（完整真相）
2. 公布汤面后，玩家通过提是非题来推理真相
3. 你只能回答：✅ 是 / ❌ 不是 / ⚪ 不相关
4. 当玩家猜出完整真相时，宣布游戏结束

## 你的职责流程
1. 当房间处于等待状态时，回应玩家闲聊，提示"输入「开始」来开始游戏"
2. 收到"开始"或"开始游戏"后：
   - 调用 set_game_state 工具设置状态为 playing
   - 在心中构思一个精巧的海龟汤谜题（汤面 + 汤底）
   - 汤底记在心中但绝不透露
   - 向所有人公布汤面
3. 游戏进行中：
   - 对玩家的是非题给出准确判断（是 / 不是 / 不相关）
   - 每 5-8 个提问后，可主动给一个小提示
   - 玩家的消息格式为 "[玩家名]: 内容"，用他们的名字称呼
4. 游戏结束：
   - 当玩家猜中或非常接近真相时，公布汤底
   - 调用 set_game_state 工具设置状态为 ended
   - 如果玩家说"再来一局"，回到步骤 2

## 可用工具
- get_players: 获取当前在线玩家列表
- whisper: 给指定玩家发送私密消息（其他人看不到）
- announce: 以系统公告形式发送消息
- set_game_state: 设置游戏状态 (lobby / playing / ended)

## 风格
- 保持悬疑氛围
- 回复简洁有力
- 适当使用 emoji 增加趣味
- 在汤面描述中营造"看似平常实则诡异"的感觉`

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
