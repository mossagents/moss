# Kernel 设计

`kernel\` 的目标不是承载完整产品，而是提供 **稳定、可组合、可观测的 Agent Runtime 原语**。Kernel 围绕 Agent 接口、request-shaped `RunAgent(...)`、Session、Event、Tool、Plugin 六个核心概念展开。

## Kernel 核心概念

### Agent 接口

Agent 是最小执行单元，定义为 Go 接口：

```go
type Agent interface {
    Name() string
    Run(ctx *InvocationContext) iter.Seq2[*Event, error]
}
```

`CustomAgent` 是把用户自定义回调包装成 Agent 的轻量入口；当 custom agent 需要调用子 Agent 时，推荐通过 `InvocationContext.RunChild(...)` 获取统一的 branch-local child-run + event materialization 语义，而不是直接手写 session clone / event commit 逻辑。

### RunAgent

`Kernel.RunAgent(...)` 是顶层 canonical 执行入口，负责 request-scoped `Session` / `Agent` / `UserContent` / `IO` / `Tools` 绑定，以及 root 级 generic event 的 session materialization。对已经由底层执行器写入 session 的事件（例如 `AgentLoop` 产出的 LLM / tool event），canonical run path 会根据 `event.Actions.MaterializedIn` 与当前 session domain 是否一致来避免重复提交；如果事件继续向更外层 domain 传播，仍可再次提交。

### Event

统一的事件类型，由 Agent 的 `Run()` 方法通过 `iter.Seq2[*Event, error]` 迭代器产生。AgentLoop 通过 `EventYield` 回调实时推送事件到上层。

### Tool

类型安全的工具系统：

```go
type Tool interface {
    Name() string
    Description() string
    Schema() *jsonschema.Schema
    Execute(ctx *ToolContext, args json.RawMessage) (json.RawMessage, error)
}

// 泛型构造
tool := kernel.NewFunctionTool("greet", "Say hello",
    func(ctx *kernel.ToolContext, args struct{ Name string }) (string, error) {
        return "Hello, " + args.Name, nil
    })
```

### Plugin

Plugin 是将多个生命周期 Hook/Interceptor 聚合到一个单元的接口：

```go
type Plugin interface {
    Name() string   // 唯一标识
    Order() int     // 执行优先级（越小越先执行）
    Install(reg *hooks.Registry)
}
```

`Group` 是为单阶段或简单多阶段插件提供的流式构建器，无需手写接口实现：

```go
g := plugin.NewGroup("my-plugin", 10)
g.OnBeforeLLM(func(ctx context.Context, ev *hooks.LLMEvent) error {
    // 在每次 LLM 调用前执行
    return nil
})
g.InterceptToolLifecycle(func(ctx context.Context, ev *hooks.ToolEvent, next func() error) error {
    // 拦截工具调用
    return next()
})
kernel.New(kernel.WithPlugin(g))
```

复杂插件可直接实现 `Plugin` 接口，并在 `Install` 中手动注册多个 hook/interceptor。
`plugin/shorthand.go` 提供了 `BeforeLLMHook`、`ToolLifecycleHook` 等快捷构造函数。

## Kernel 组合的能力

`kernel.Kernel` 当前组合的核心能力包括：

- `model.LLM`
- `io.UserIO`
- `tool.Registry`（注册 `Tool` 接口）
- `session.Manager`
- `Plugin` 系统
- `workspace.Workspace`
- `task.TaskRuntime`
- `task.Mailbox`
- `workspace.WorkspaceIsolation`
- repo state / patch apply / patch revert / worktree snapshots
- checkpoint store
- observer
- prompt / stage / service registries

Kernel 围绕 **一次完整 agent run 的状态、执行面、协作面和生命周期管理** 组织。

## 核心运行流

```text
Boot
  -> validate LLM / UserIO
  -> boot extension hooks

NewSession
  -> create session
  -> extend system prompt
  -> emit lifecycle(created)

RunAgent
  -> begin run supervisor context
  -> apply optional timeout
  -> bind RunAgentRequest
  -> current Agent.Run(...)
  -> emit session/tool lifecycle events

Shutdown
  -> reject new runs
  -> wait active runs
  -> shutdown extension hooks
```

## 主要子包

| 子包 | 作用 |
|---|---|
| `kernel\model` | LLM 接口、消息与 tool-call 数据结构 |
| `kernel\io` | `UserIO`、审批与结构化策略上下文 |
| `kernel\tool` | 工具规范、注册表、风险级别与 `ToolCallContext` |
| `kernel\session` | 会话、`ScopedState`、预算、持久化接口 |
| `kernel\hooks` | Hook/Interceptor pipeline、事件类型（LLMEvent、ToolEvent、ErrorEvent）与 builtins |
| `kernel\plugin` | Plugin 接口、`Group` 构建器与快捷构造函数 |
| `kernel\patterns` | Agent 编排原语（Sequential、Parallel、Loop、Supervisor、Research） |
| `kernel\loop` | Agent 执行循环 |
| `kernel\workspace` | `Workspace` / isolation / snapshot 边界 |
| `kernel\task` | task runtime、mailbox 等协作抽象 |
| `kernel\checkpoint` | 会话检查点抽象 |
| `kernel\artifact` | Artifact 存储接口与内存实现 |
| `kernel\observe` | observer 事件、归一化指标、release gates |
| `kernel\retry` | retry 与 breaker 原语 |
| `kernel\prompt` | prompt registry |
| `kernel\errors` | 结构化错误 |

## 为什么把执行面收进 `Workspace`

旧式“直接把文件与命令执行绑在 sandbox 上”的模型不够表达当前需求。当前实现把文件访问与结构化命令执行统一收进 `workspace.Workspace` 这个端口：

- 文件操作：读写/列举/删除文件
- 命令执行：通过 `Execute(ctx, ExecRequest)` 执行结构化命令请求

`ExecRequest` 目前已经包含：

- `Command` / `Args`
- `WorkingDir`
- `Timeout`
- `AllowedPaths`
- `Env`
- `Network`
- `IsolationLevel`

`ExecOutput` 也会显式返回：

- `Stdout` / `Stderr`
- `ExitCode`
- `Enforcement`
- `Degraded`
- `Details`

这使执行面可以表达“允许但降级”“需要隔离”“网络受限”等真实生产语义。

## Policy 与审批

策略输入不是只有 tool name，而是结构化的 `PolicyContext`：

```go
type PolicyContext struct {
	SessionID    string
	SessionState map[string]any
	Identity     *Identity
	Tool         tool.ToolSpec
	Input        json.RawMessage
}
```

策略输出也不是简单布尔值，而是：

- `Decision`
- `Enforcement`
- `Reason`
- `Meta`

这允许产品层在审批、审计、提示和降级执行之间共享统一语义。

## 运行时扩展接入点

当前 Kernel 主要通过几个显式注册面暴露扩展入口：

- `Stages()`：注册 boot / shutdown 生命周期逻辑
- `PromptLayers()`：拼装 system prompt layer
- `InstallPlugin()`：把 hook 接入 `hooks.Registry`
- `Services()`：挂载 owner package 持有的 substrate state

这套机制的意义是：**让扩展按职责参与运行时生命周期，而不是依赖一个宽泛、难约束的扩展总线。**

## 运行时可观测性

Kernel 现在支持：

- session / tool / LLM / error observer 事件
- normalized metrics snapshot
- active run 计数
- shutting-down 状态
- release gate 校验

这些能力分别被 `appkit\serve.Health*` 和 `harness\testing\arch_guard.ps1` 等上层入口消费。

## 设计取舍

### 保留在内核中的能力

以下内容已经足够基础，适合留在 `kernel\`：

- Agent 接口与组合器
- `RunAgent(...)` 执行入口与运行监督
- Event 流
- Plugin 系统
- 会话生命周期
- 工具注册（Tool 接口 + FunctionTool）
- 结构化执行面
- 任务与邮箱抽象
- 检查点与观测接口

### 不保留在内核中的能力

以下内容继续放在上层：

- builtin tools 的具体实现
- skill / MCP 的发现与装配
- knowledge / scheduling 的默认接线
- coding / research / writer 产品语义
- deepagent 预设能力组合

## 当前内核定位

可以把今天的 Kernel 理解成：

> 一个可嵌入的 agent runtime substrate，负责稳定地运行、约束、观测和扩展 agent 会话，但不直接定义具体产品形态。上层通过 `harness\` 的 Feature/Backend 组合模式将能力装配到 Kernel，应用层只需选择所需的 Feature。
