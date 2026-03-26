# mossroom — Agent 驱动的多人海龟汤游戏

🐢 **海龟汤**是一种横向思维解谜游戏。Agent 作为主持人准备谜题，玩家通过提问推理出隐藏的真相。

## 特性

- **多人实时** — 通过 WebSocket 多人同时在线，每个房间独立
- **Agent 主持** — AI 自动生成谜题、判断回答、掌控节奏
- **房间隔离** — 每个房间拥有独立的 Kernel + Session，数据完全隔离
- **断线重连** — 身份信息保存在浏览器本地，重连自动恢复
- **历史回放** — 重新进入房间可看到之前的所有消息

## 用法

```bash
go run . --provider openai --model gpt-4o
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
```

打开浏览器访问 http://localhost:8091

## 游戏流程

1. **创建/加入房间** — 创建后获得 4 位房间号，分享给朋友加入
2. **等待玩家** — 在大厅聊天，人齐后输入 **"开始"**
3. **解谜阶段** — 主持人公布汤面，玩家提是非题，主持人回答"是/不是/不相关"
4. **揭晓真相** — 猜中后主持人公布汤底，可输入 **"再来一局"** 继续

## 架构

```
Browser ──WebSocket──┐
Browser ──WebSocket──┼──→ Room ──→ RoomIO ──→ Kernel ──→ LLM
Browser ──WebSocket──┘         ↕
                          GameTools
                     (whisper, announce,
                      get_players, set_game_state)
```

| 组件 | 职责 |
|------|------|
| `RoomManager` | 创建/查找房间，管理房间生命周期 |
| `Room` | 持有 Kernel + Session，串行处理消息队列 |
| `RoomIO` | 实现 `port.UserIO`，将 Agent 输出广播到房间 |
| `GameTools` | 4 个自定义工具让 Agent 与游戏交互 |

## 自定义工具

| 工具 | 描述 |
|------|------|
| `get_players` | 获取当前房间在线玩家列表 |
| `whisper` | 给指定玩家发送私密消息 |
| `announce` | 以系统公告形式广播消息 |
| `set_game_state` | 设置游戏状态 (lobby/playing/ended) |
