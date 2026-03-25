# websocket — WebSocket UserIO 适配器示例

演示如何实现 `port.UserIO` 接口，通过 WebSocket 将 moss 集成到浏览器中。

## 特性

- **自定义 UserIO** — `WebSocketIO` 通过 WebSocket JSON 协议实现 `Send` / `Ask`
- **流式输出** — 流式 token 实时推送到浏览器
- **内置页面** — 使用 `embed.FS` 内嵌 HTML 客户端，无需额外静态文件
- **多连接** — 每个 WebSocket 连接创建独立 Kernel + Session

## 用法

```bash
go run . --provider openai --model gpt-4o
```

打开浏览器访问 http://localhost:8090 ，在输入框中对话。

## WebSocket 消息协议

客户端与服务端之间通过 JSON 消息通信：

| 方向 | type | 说明 |
|------|------|------|
| C→S | `user` | 用户输入 |
| S→C | `assistant` | 完整回复 |
| S→C | `stream` | 流式 token 片段 |
| S→C | `stream_end` | 流式结束 |
| S→C | `tool_start` | 工具调用开始 |
| S→C | `tool_result` | 工具调用结果 |
| S→C | `ask` | 请求用户确认/选择 |
| S→C | `error` | 错误信息 |

## 架构

```
Browser ←WebSocket→ WebSocketIO ←UserIO→ Kernel → LLM
```

每个 WebSocket 连接独立创建 `Kernel` 和 `Session`，互不干扰。
