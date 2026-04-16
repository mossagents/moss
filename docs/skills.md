# Capabilities、Prompt Skills、MCP 与 Subagents

当前仓库里最容易混淆的，不是“有没有 skill”，而是 **不同能力是通过什么机制进入运行时**。在 Moss 里，至少要区分四件事：

1. **Builtin tools**：由 `harness.RuntimeSetup(...)`（或 `appkit.BuildKernel(...)` 的默认装配）注册的官方工具
2. **Prompt skills**：从 `SKILL.md` 发现并注入系统提示词
3. **MCP servers**：通过 `extensions\mcp\` 桥接进来的外部工具服务
4. **Subagents**：通过 `harness` 公开入口注册到 runtime-backed catalog 的委派代理

## 统一抽象：`capability.Provider`

虽然来源不同，这三类 provider（builtin / prompt skill / MCP）共享统一生命周期接口：

```go
type Provider interface {
	Metadata() Metadata
	Init(ctx context.Context, deps Deps) error
	Shutdown(ctx context.Context) error
}
```

`Deps` 里不只有 `ToolRegistry`，还包括当前 Kernel 暴露的关键依赖：

- `Kernel`
- `ToolRegistry`
- `Hooks`
- `Sandbox`
- `UserIO`
- `Workspace`
- `Executor`
- `TaskRuntime`
- `Mailbox`
- `SessionStore`

这里的 generic lifecycle 已经不再属于 prompt-skill 实现包，而是放在顶层 `capability\` 下；`extensions\skill\` 现在只负责 prompt skill (`SKILL.md`) 的发现、解析与实现。因此当前的能力文档应该围绕 **capability.Provider + harness.RuntimeSetup(...)** 叙述，而不是围绕旧的“单一 skill 系统”叙述。

## 1. Builtin tools

`harness.RuntimeSetup(...)` 默认会注册官方 builtin tools。当前基础工具包括：

| 工具 | 作用 |
|---|---|
| `datetime` | 返回当前时间 |
| `read_file` / `write_file` / `edit_file` | 文件读写与编辑 |
| `glob` / `ls` / `grep` | 文件发现与内容搜索 |
| `run_command` | 命令执行 |
| `http_request` | HTTP 调用 |
| `ask_user` | 结构化向用户询问输入 |

在此基础上，扩展还会继续注册更多工具组，例如：

- context：`offload_context`、`compact_conversation`
- planning / task：`update_plan`、`update_task`、`plan_task`、`claim_task`
- mailbox：`send_mail`、`read_mailbox`
- workspace isolation：`acquire_workspace`、`release_workspace`
- memory：`read_memory`、`write_memory`、`search_memories` 等
- knowledge：文档摄入与搜索
- scheduling：计划任务相关工具

## 2. Prompt skills (`SKILL.md`)

Prompt skill 只负责 **向 system prompt 注入额外上下文**，并不直接注册工具。

### 发现目录

项目级：

- `.agents\skills\`
- `.agent\skills\`
- `.<app>\skills\`
- 兼容 legacy：`.moss\skills\`

全局级：

- `~\.copilot\skills\`
- `~\.copilot\installed-plugins\**\skills\`
- `~\.agents\skills\`
- `~\.agent\skills\`
- `~\.<app>\skills\`
- `~\.config\agents\skills\`

### trust 规则

project skill 是否可见，取决于当前 trust：

- `trusted`：允许发现和加载项目级 skill
- `restricted`：跳过项目级 skill，只保留全局安全面

### progressive skills

若启用：

```go
err := h.Install(ctx,
	harness.RuntimeSetup(workspace, trust,
		harness.WithProgressiveSkills(true),
	),
)
```

运行时不会立刻注入全部 `SKILL.md` 正文，而是先暴露 skill manifest，并注册按需激活工具。这适合首轮上下文预算更敏感的场景。

## 3. MCP servers

MCP 通过配置文件声明，由 `extensions\mcp\` 包桥接到本地运行时。示例：

```yaml
skills:
  - name: github
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
```

加载流程由 `harness.RuntimeSetup(...)` 驱动的底层 capability assembly 完成：

1. 读取全局配置
2. 读取项目配置（受 trust 约束）
3. 对 project MCP 做审批确认
4. 按依赖顺序注册 server
5. 把远端工具注入本地 `ToolRegistry`

因此，`skills:` 配置块在今天更准确的含义是：**声明外部 capability provider，主要用于 MCP server**。

## 4. Subagents

Subagent 不是 `SKILL.md`，也不是 MCP。它是注册到 runtime-backed catalog 的受控委派代理，但**公开 owner 在 `harness`**，不是 `runtime`。

来源有两类：

- 代码里直接 `harness.RegisterSubagent(...)`
- 代码中通过 `harness.LoadSubagentsFromYAML(...)` 加载工作区中的 `subagents.yaml`

`subagents.yaml` 结构：

```yaml
researcher:
  description: Research-focused helper
  system_prompt: You investigate and summarize evidence.
  tools: [read_file, grep, http_request]
  max_steps: 40
  trust_level: restricted
```

若需要读取当前配置好的 subagent catalog，也应优先走 `harness.SubagentCatalogOf(...)`。

## 5. 默认加载行为

`harness.RuntimeSetup(...)` 默认会同时打开：

- builtin tools
- MCP servers
- prompt skills
- subagents

可以按需关闭：

```go
err := h.Install(ctx,
	harness.RuntimeSetup(workspace, trust,
		harness.WithMCPServers(false),
		harness.WithSkills(false),
		harness.WithAgents(false),
	),
)
```

## 6. 应该如何理解“skills”

在当前仓库语境里，**skills 不是单一技术点，而是一组 capability loading 机制中的 prompt-skill 子域**：

- 想要官方工具：看 builtin tools
- 想要 prompt augmentation：看 `SKILL.md` 与 `extensions\skill\`
- 想要外部能力：看 `extensions\mcp\`
- 想要可控委派：看 `harness` 下的 subagent surface

真正把 builtin / prompt skill / MCP 粘起来的是 `harness.RuntimeSetup(...)`、底层 capability assembly 与 `capability.Provider` 抽象；真正把 subagents 粘到运行时的是 `harness` 公开 surface 与底层 runtime-backed catalog。
