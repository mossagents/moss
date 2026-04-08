# Kernel 设计

`kernel\` 的目标不是承载完整产品，而是提供 **稳定、可组合、可观测的 Agent Runtime 原语**。当前内核已经不再只围绕“Loop + Tool + Session”展开，而是同时把执行、任务协作、观测和扩展桥接纳入最小稳定边界。

## Kernel 负责什么

`kernel.Kernel` 当前组合的核心能力包括：

- `model.LLM`
- `io.UserIO`
- `tool.Registry`
- `session.Manager`
- `middleware.Chain`
- `workspace.Workspace`
- `workspace.Executor`
- `task.TaskRuntime`
- `task.Mailbox`
- `workspace.WorkspaceIsolation`
- repo state / patch apply / patch revert / worktree snapshots
- checkpoint store
- observer
- extension bridge

也就是说，今天的 Kernel 不只是“调用模型 + 调工具”，而是 **围绕一次完整 agent run 的状态、执行面、协作面和生命周期管理** 组织起来的。

## 核心运行流

```text
Boot
  -> validate LLM / UserIO
  -> boot extension hooks

NewSession
  -> create session
  -> extend system prompt
  -> emit lifecycle(created)

Run
  -> begin run supervisor context
  -> apply optional timeout
  -> loop.AgentLoop.Run(...)
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
| `kernel\tool` | 工具规范、注册表与风险级别 |
| `kernel\session` | 会话、状态、预算、持久化接口 |
| `kernel\middleware` | 拦截链与 builtins policy / logger 等 |
| `kernel\loop` | Agent 执行循环 |
| `kernel\workspace` | `Workspace` / `Executor` / isolation / snapshot 边界 |
| `kernel\task` | task runtime、mailbox 等协作抽象 |
| `kernel\checkpoint` | 会话检查点抽象 |
| `kernel\observe` | observer 事件、归一化指标、release gates |
| `kernel\retry` | retry 与 breaker 原语 |
| `kernel\prompt` | prompt registry |
| `kernel\errors` | 结构化错误 |

## 为什么引入 `Workspace` / `Executor`

旧式“直接把文件与命令执行绑在 sandbox 上”的模型不够表达当前需求。现在的内核把这两个概念拆开：

- `Workspace`：读写/列举/删除文件
- `Executor`：执行结构化命令请求

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

## 生命周期扩展桥

Kernel 通过 `ExtensionBridge` 暴露扩展接入点：

- `OnBoot`
- `OnShutdown`
- `OnSystemPrompt`
- `OnSessionLifecycle`
- `OnToolLifecycle`

这套机制的意义是：**让扩展在不污染内核 API 的情况下参与运行时生命周期。**

## 运行时可观测性

Kernel 现在支持：

- session / tool / LLM / error observer 事件
- normalized metrics snapshot
- active run 计数
- shutting-down 状态
- release gate 校验

这些能力分别被 `appkit\serve.Health*` 和 `testing\arch_guard.ps1` 等上层入口消费。

## 设计取舍

### 保留在内核中的能力

以下内容已经足够基础，适合留在 `kernel\`：

- 运行循环
- 会话生命周期
- 中间件
- 工具注册
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

> 一个可嵌入的 agent runtime substrate，负责稳定地运行、约束、观测和扩展 agent 会话，但不直接定义具体产品形态。
