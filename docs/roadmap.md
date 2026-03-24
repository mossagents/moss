# 🗺️ 开发路线图 (Roadmap)

---

## 当前状态

Moss 已完成核心 Kernel 实现，包含完整的 Agent Loop、工具系统、技能系统、配置管理和库友好 API。

### 已完成 ✅

| 模块 | 状态 | 说明 |
|---|---|---|
| Kernel Core | ✅ | 5 概念 + 2 Port，零外部依赖 |
| Agent Loop | ✅ | think→act→observe + streaming |
| Tool System | ✅ | Registry + 6 内置工具 |
| Session | ✅ | 对话历史 + 状态 + 预算管理 |
| Middleware | ✅ | 洋葱模型 + PolicyCheck/EventEmitter/Logger |
| Sandbox | ✅ | LocalSandbox + 路径逃逸保护 |
| LLM Adapters | ✅ | Claude + OpenAI (含 BaseURL 自定义) |
| TUI | ✅ | Bubble Tea 交互式终端 |
| Skill System | ✅ | BuiltinTool + MCPServer + Skill |
| Config | ✅ | ~/.moss/config.yaml 统一配置 |
| Library API | ✅ | SetupWithDefaults + 标准 UserIO + Boot 验证 |

---

## 短期规划 (P1)

### NoOp Sandbox

提供一个无操作的 Sandbox 实现，用于不需要文件系统操作的纯对话场景。

```go
k := kernel.New(
    kernel.WithLLM(myLLM),
    kernel.WithUserIO(&port.NoOpIO{}),
    // 无需 WithSandbox — 在纯对话模式下不需要
)
```

**目标**：允许库用户在不提供 Sandbox 的情况下启动 Kernel（对话模式）。

### 使用示例 (Examples)

在 `examples/` 目录下提供可运行的集成示例：

```
examples/
├── basic/          # 最简集成示例
├── custom-tool/    # 自定义工具注册
├── websocket/      # WebSocket UserIO 适配器
├── mcp-skill/      # MCP 技能配置示例
└── middleware/      # 自定义 Middleware 示例
```

### API 文档自动生成

- 确保所有导出类型的 GoDoc 注释完整
- 配置 pkg.go.dev 自动索引

---

## 中期规划 (P2)

### 多 Agent 编排

在 Kernel 之上实现 Agent 编排层，支持 Manager → Worker 模式：

```go
// Manager Agent 通过 Session 创建子任务
subSess, _ := k.NewSession(ctx, session.SessionConfig{
    Goal: "Research the topic",
})
result, _ := k.Run(ctx, subSess)

// 或通过 SessionManager.Notify 跨 Session 通信
k.SessionManager().Notify(otherSessionID, port.Message{...})
```

### Docker Sandbox

基于 Docker 容器的 Sandbox 实现，提供更强的执行隔离：

```go
dockerSb, _ := docker.NewSandbox(docker.Config{
    Image: "golang:1.24",
    Mounts: []docker.Mount{{Src: "/workspace", Dst: "/workspace"}},
})
k := kernel.New(kernel.WithSandbox(dockerSb))
```

### 持久化 Session

基于 Event Sourcing 的 Session 持久化：

```go
// EventEmitter MW → 持久化到存储
k.OnEvent("*", func(e builtins.Event) {
    store.Append(e)
})

// 从事件流恢复 Session
sess, _ := k.SessionManager().Restore(sessionID, events)
```

### 更多 LLM Adapter

| Adapter | 说明 |
|---|---|
| Ollama | 本地模型调用 |
| Azure OpenAI | Azure 端点支持 |
| Gemini | Google AI |
| Failover LLM | 主备切换 |

---

## 长期方向 (P3)

### 分布式部署

- EventEmitter → 消息队列（Kafka/NATS）
- 分布式 SessionManager
- 水平扩展 Agent Worker

### 技能市场

- 在线 Skill 注册中心
- `moss install <skill>` 命令
- 版本管理与依赖解析

### Web IDE 集成

- VS Code 扩展
- Web 编辑器嵌入
- Jupyter Notebook 集成

### 可观测性

- OpenTelemetry 集成
- 结构化日志（slog）
- 执行轨迹（Trace）可视化

---

## 贡献

欢迎参与 Moss 开发：

1. **Issue**: 报告 Bug 或提出功能需求
2. **Pull Request**: 参照现有代码风格，附带测试
3. **Skill 贡献**: 创建 MCPServer 或 Skill（SKILL.md）

### 开发环境

```bash
# 克隆并构建
git clone https://github.com/mossagi/moss.git
cd moss
go build ./...

# 运行测试
go test ./... -count=1

# 运行 TUI
go run ./cmd/moss/ --provider openai --model gpt-4o
```
