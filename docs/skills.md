# 🧩 技能系统 (Skills)

Moss 的技能系统支持三种类型的扩展：**Core Tool Skill**（默认内置工具技能）、**MCP Provider**（`mcp` 包中的外部 MCP 服务器集成）、**Skill**（系统提示词注入）。

---

## 概览

```
┌─────────────────────────────────────────┐
│            Skill Manager                 │
│  ┌───────────┐ ┌─────────┐ ┌────────┐  │
│  │Core Tool  │ │   MCP   │ │ Skill  │  │
│  │ (6 tools) │ │(MCP srv)│ │(SKILL) │  │
│  └───────────┘ └─────────┘ └────────┘  │
├─────────────────────────────────────────┤
│  Kernel: ToolRegistry + Middleware       │
└─────────────────────────────────────────┘
```

## Skill 接口

所有 Skill 实现统一接口：

```go
type Provider interface {
    Metadata() Metadata
    Init(ctx context.Context, deps Deps) error
    Shutdown(ctx context.Context) error
}

type Metadata struct {
    Name        string
    Version     string
    Description string
    Tools       []string    // 提供的工具名称列表
    Prompts     []string    // 系统提示词片段
}

type Deps struct {
    ToolRegistry tool.Registry
    Middleware   *middleware.Chain
    Sandbox      sandbox.Sandbox
    UserIO       port.UserIO
}
```

---

## Core Tool Skill

内置核心工具集，提供文件操作、命令执行和用户交互能力。

**注册方式**：通过 `defaults.Setup` 自动装配（推荐）：

```go
import "github.com/mossagents/moss/extensions/defaults"

defaults.Setup(ctx, k, workspaceDir)
```

**提供的 6 个工具**：

| 工具 | 风险等级 | 参数 | 说明 |
|---|---|---|---|
| `read_file` | Low | `path` | 读取文件内容 |
| `write_file` | High | `path`, `content` | 写入文件，自动创建父目录 |
| `list_files` | Low | `pattern` | Glob 模式列出文件，支持 `**` 递归 |
| `search_text` | Low | `pattern`, `glob`, `max_results` | 正则搜索文件内容 |
| `run_command` | High | `command`, `args` | 执行 shell 命令 |
| `ask_user` | Medium | `prompt`, `type`, `options` | 向用户请求输入 |

---

## MCP Provider（mcp 包）

通过 [MCP 协议](https://modelcontextprotocol.io/) 连接外部工具服务器。实现位于 `github.com/mossagents/moss/mcp` 包（`mcp.NewMCPServer(...)`），并由 `extensions/defaults.Setup` 统一装配。

### 配置

在 `~/.moss/config.yaml` 或项目级 `moss.yaml` 中添加：

```yaml
skills:
  # stdio 传输方式
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/user"]

  # SSE 传输方式
  - name: remote-tools
    transport: sse
    url: http://localhost:8080/mcp

  # 带环境变量
  - name: github
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_xxx"

  # 禁用某个 skill
  - name: disabled-skill
    transport: stdio
    command: some-cmd
    enabled: false
```

### 配置合并

Moss 自动合并全局和项目配置：

| 配置文件 | 路径 | 优先级 |
|---|---|---|
| 全局配置 | `~/.moss/config.yaml` | 低 |
| 项目配置 | `./moss.yaml` | 高（覆盖同名 skill） |

```go
// 手动加载和合并
globalCfg, _ := skill.LoadConfig(skill.DefaultGlobalConfigPath())
projectCfg, _ := skill.LoadConfig(skill.DefaultProjectConfigPath(workspaceDir))
merged := skill.MergeConfigs(globalCfg, projectCfg)
```

---

## Skill

通过 `SKILL.md` 文件注入系统提示词，兼容 [skills.sh](https://skills.sh) 格式。

### SKILL.md 格式

```markdown
---
name: my-skill
description: A skill that adds domain knowledge
---

# My Skill

You are an expert in XYZ domain.

## Rules
- Always follow best practices
- Use idiomatic Go patterns
```

YAML frontmatter 定义元数据，Markdown 正文成为系统提示词注入。

### 发现路径

Moss 按以下优先级自动发现 SKILL.md（项目级 > 全局）：

| 路径 | 级别 |
|---|---|
| `.agents/skills/SKILL.md` | 项目 |
| `.agents/skills/*/SKILL.md` | 项目（多 skill） |
| `.moss/skills/SKILL.md` | 项目 |
| `.moss/skills/*/SKILL.md` | 项目（多 skill） |
| `~/.copilot/skills/SKILL.md` | 全局 |
| `~/.moss/skills/SKILL.md` | 全局 |
| `~/.config/agents/skills/SKILL.md` | 全局 (Unix) |

### 手动加载

```go
ps, err := skill.ParseSkillMD("/path/to/SKILL.md")
skillsx.Manager(k).Register(ctx, ps, skillsx.Deps(k))
```

---

## Skill Manager

统一管理所有 Skill 的生命周期：

```go
manager := skill.NewManager()

// 注册
manager.Register(ctx, mySkill, deps)

// 查询
list := manager.List()              // 所有已注册 skill 的元数据
s, ok := manager.Get("skill-name")  // 按名称查找

// 系统提示词聚合（PromptSkill 的内容）
// 系统提示词聚合（Skill 的内容）
additions := manager.SystemPromptAdditions()

// 卸载
manager.Unregister(ctx, "skill-name")

// 关闭所有
manager.ShutdownAll(ctx)
```

---

## defaults.Setup

推荐使用 `defaults.Setup` 一键装配所有标准技能：

```go
// 默认行为：注册 Core Tool Skill + 加载 MCP Servers + 发现 Skills
defaults.Setup(ctx, k, workspaceDir)

// 选择性禁用
defaults.Setup(ctx, k, workspaceDir,
  defaults.WithoutBuiltin(),        // 不注册内置 6 工具
  defaults.WithoutMCPServers(),     // 不加载 MCP 配置
  defaults.WithoutSkills(),         // 不发现 SKILL.md
)
```

加载过程中的警告会通过 slog 输出到 stderr，日志级别为 `WARN`。可通过 `logging.ConfigureLogging()` 调整日志级别。
