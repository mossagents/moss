# 🧩 技能系统 (Skills)

Moss 的技能系统支持三种类型的扩展：**CoreSkill**（内置工具）、**MCP Skill**（外部 MCP 服务器）、**PromptSkill**（系统提示词注入）。

---

## 概览

```
┌─────────────────────────────────────────┐
│            Skill Manager                 │
│  ┌───────────┐ ┌─────────┐ ┌────────┐  │
│  │ CoreSkill │ │MCPSkill │ │Prompt  │  │
│  │ (6 tools) │ │(MCP srv)│ │Skill   │  │
│  └───────────┘ └─────────┘ └────────┘  │
├─────────────────────────────────────────┤
│  Kernel: ToolRegistry + Middleware       │
└─────────────────────────────────────────┘
```

## Skill 接口

所有 Skill 实现统一接口：

```go
type Skill interface {
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

## CoreSkill

内置核心工具集，提供文件操作、命令执行和用户交互能力。

**注册方式**：通过 `SetupWithDefaults` 自动注册，或手动注册：

```go
import toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"

core := &toolbuiltins.CoreSkill{}
k.SkillManager().Register(ctx, core, k.SkillDeps())
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

## MCP Skill

通过 [MCP 协议](https://modelcontextprotocol.io/) 连接外部工具服务器。

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

## PromptSkill

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
k.SkillManager().Register(ctx, ps, k.SkillDeps())
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
additions := manager.SystemPromptAdditions()

// 卸载
manager.Unregister(ctx, "skill-name")

// 关闭所有
manager.ShutdownAll(ctx)
```

---

## SetupWithDefaults

推荐使用 `SetupWithDefaults` 一键注册所有标准技能：

```go
// 默认行为：注册 CoreSkill + 加载 MCP Skills + 发现 PromptSkills
k.SetupWithDefaults(ctx, workspaceDir)

// 选择性禁用
k.SetupWithDefaults(ctx, workspaceDir,
    kernel.WithoutCoreSkill(),      // 不注册内置 6 工具
    kernel.WithoutMCPSkills(),      // 不加载 MCP 配置
    kernel.WithoutPromptSkills(),   // 不发现 SKILL.md
    kernel.WithWarningWriter(os.Stderr),  // 输出加载警告
)
```

`WithWarningWriter` 默认不设置（静默模式），确保库用户不会收到意外的 stderr 输出。
