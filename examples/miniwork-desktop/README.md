# Miniwork Desktop

基于 [Wails v3](https://github.com/wailsapp/wails) 的桌面端 AI 助手 POC，验证 moss kernel 接入桌面端的能力。

## 功能

参考 [Claude Cowork](https://support.claude.com/en/articles/13345190-get-started-with-cowork) 核心功能实现：

- **对话式任务交互** — 流式输出、Markdown 渲染
- **Manager → Worker 委派** — 多 Agent 并行执行子任务
- **文件/图片上传** — 系统原生对话框 + workspace 管理
- **实时进度展示** — Worker 状态面板、进度指示器
- **Ask/Confirm 交互** — Agent 可向用户请求确认或输入

## 架构

```
┌─────────────────┐       Wails Events        ┌─────────────────┐
│   Frontend (JS) │  ◄──── chat:stream ──────  │    WailsUserIO  │
│   index.html    │  ◄──── chat:text ────────  │ (port.UserIO)   │
│   main.js       │  ◄──── chat:ask ─────────  │                 │
│   style.css     │  ────► RespondToAsk ─────► │                 │
└────────┬────────┘       Wails Bindings       └────────┬────────┘
         │                                              │
         │  Call.ByName(Service.Method)                 │  Send() / Ask()
         │                                              │
┌────────▼────────┐                            ┌────────▼────────┐
│   ChatService   │ ──── RunWithUserIO ──────► │   Moss Kernel   │
│   FileService   │                            │   Agent Loop    │
└─────────────────┘                            └─────────────────┘
```

## 前置要求

- Go 1.24+
- [Wails v3 CLI](https://v3alpha.wails.io/getting-started/installation/)

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

## 运行

```bash
# 开发模式（带热重载）
cd examples/miniwork-desktop
wails3 dev

# 或直接 go run
go run . --provider openai --model gpt-4o

# 使用自定义 API (如 OpenAI 兼容的本地模型)
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
```

## 构建

```bash
wails3 build
```

## 配置

| 参数 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `--provider` | `MOSS_PROVIDER` | `openai` | LLM 提供商 |
| `--model` | `MOSS_MODEL` | - | 模型名称 |
| `--api-key` | `MOSS_API_KEY` | - | API Key |
| `--base-url` | `MOSS_BASE_URL` | - | 自定义 API 端点 |
| `--workspace` | `MOSS_WORKSPACE` | `.` | 工作目录 |
| `--trust` | `MOSS_TRUST` | `trusted` | 信任级别 |
| `--workers` | - | `3` | 最大并行 Worker 数 |

## 项目结构

```
miniwork-desktop/
├── main.go            # Wails 应用入口、配置解析
├── chatservice.go     # ChatService — 桥接 kernel 和前端
├── wailsio.go         # WailsUserIO — port.UserIO 实现
├── fileservice.go     # FileService — 文件对话框和上传
├── tracker.go         # 任务编排状态追踪
├── go.mod
├── frontend/
│   ├── index.html     # HTML 结构
│   ├── style.css      # 暗色主题样式
│   └── main.js        # 前端逻辑与 Wails IPC
└── templates/
    ├── manager_system_prompt.tmpl
    └── worker_system_prompt.tmpl
```

## 注意

这是 POC 验证阶段，不考虑向后兼容性。Wails v3 仍处于 alpha 阶段，API 可能变更。
