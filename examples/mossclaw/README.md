# mossclaw

mossclaw 是一个个人 AI 助理示例，对标 [OpenClaw](https://github.com/openclaw/openclaw)。

在 Moss 框架上构建，演示如何将 Agent 打造为全能个人助理。

## 功能

- 网络访问工具：`fetch_url` / `extract_links`
- 知识库：语义检索 + 文档摄入
- 定时任务调度器
- Bootstrap 上下文（AGENTS.md / SOUL.md / TOOLS.md）
- 交互式 REPL 模式

## 运行

```bash
cd examples/mossclaw
go run .
```

常用参数：

```bash
go run . --provider openai --model gpt-4o
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
```

## 自定义 Bootstrap 上下文

在工作区根目录或 `~/.mossclaw/` 放置以下文件即可定制 Agent 行为：

| 文件 | 作用 |
|------|------|
| `AGENTS.md` | Agent 行为指令 |
| `SOUL.md` | 性格 / 沟通风格 |
| `TOOLS.md` | 工具使用建议 |
| `IDENTITY.md` | Agent 身份标识 |
| `USER.md` | 用户画像 |

搜索路径（优先级从高到低）：

1. `.agents/` — 项目级
2. `.mossclaw/` — 项目级
3. `~/.mossclaw/` — 全局级

## 配置

- 全局配置目录：`~/.mossclaw`
- 全局配置文件：`~/.mossclaw/config.yaml`

## System Prompt 模板覆盖

- 项目级（优先）：`./.mossclaw/system_prompt.tmpl`
- 全局级：`~/.mossclaw/system_prompt.tmpl`

默认模板文件：`templates/system_prompt.tmpl`
