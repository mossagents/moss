# 生产准备度

Moss 当前已经具备 **单节点、可审计、可回放、可治理** 的产品基础；真正需要继续补强的，主要是分布式部署、统一发布包装和更完整的托管运维面。

## 1. 当前已经落地的生产能力

### 1.1 执行姿态与权限控制

当前产品面（尤其是 `examples\mosscode`）已经把以下概念接到运行时：

- `trust`：`trusted` / `restricted`
- `approval mode`
- `profile`
- `ExecutionPolicy`
- 结构化 `PolicyContext`

这意味着“是否允许执行命令、是否允许 HTTP、是否需要审批、是否只能软降级执行”都不再依赖 prompt 约定，而是走明确的运行时策略面。

### 1.2 结构化执行边界

命令执行不再只是 `(cmd, args)`，而是 `workspace.ExecRequest`：

- working directory
- timeout
- allow paths
- env
- network policy
- isolation level

配合 `ExecOutput.Enforcement` / `Degraded`，上层可以区分：

- 正常执行
- 因策略被硬阻断
- 因宿主环境限制而降级

### 1.3 状态持久化

`presets\deepagent` 默认已经接入：

- session store
- checkpoint store
- task runtime
- persistent memories
- workspace isolation root

`mosscode` 还额外把这些能力用于：

- 恢复 thread
- 管理 checkpoint
- 记录 patch apply / rollback
- review 与 changes 状态追踪

### 1.4 变更安全

当前代码库里已经有与代码变更相关的正式能力面：

- repo state capture
- patch apply
- patch revert
- worktree snapshot
- checkpoint replay / fork

这使“先保存状态，再应用改动，再审计或回滚”成为运行时能力，而不是临时脚本。

### 1.5 运行时观测

`kernel\observe` 已提供：

- `LLMCallEvent`
- `ToolCallEvent`
- `SessionEvent`
- `ErrorEvent`
- `NormalizedMetricsSnapshot`
- `ReleaseGateMeter`

目前默认 release gates 包括：

| Gate | 默认阈值 |
|---|---|
| `success_rate` | `>= 0.95` |
| `llm_latency_avg` | `<= 10000ms` |
| `tool_latency_avg` | `<= 5000ms` |
| `tool_error_rate` | `<= 0.05` |

### 1.6 健康面

`appkit\serve.go` 已提供统一健康输出：

- `Health(...)`
- `HealthJSON(...)`
- `HealthText(...)`

当前健康快照包含：

- `status`
- `active_runs`
- `llm_latency_avg_ms`
- `tool_latency_avg_ms`
- `success_rate`
- `tool_error_rate`
- `total_runs`

这已经足够作为 CLI / gateway / HTTP 外壳的基础健康面。

## 2. 现阶段适合的部署形态

### 推荐：单节点产品实例

最适合当前仓库现状的，是下面这类部署：

- 单个 `mosscode` / `mossresearch` / `mosswriter` 实例
- 本地或挂载卷上的会话、检查点、任务与记忆目录
- 明确的 trust / approval / profile
- 有 operator 审批与日志审计

### 可行但仍需更多工程化：服务化入口

`mossclaw` 已经演示了：

- scheduler
- knowledge
- gateway serve
- session routing

它说明 Moss 已经能做服务化入口，但还没有把“统一 HTTP 服务壳、进程管理、部署模板、认证/配额”抽成一个官方稳定产品面。

## 3. 发布门禁与审计

仓库当前提供 `testing\arch_guard.ps1` 作为发布前守门脚本：

```powershell
pwsh .\testing\arch_guard.ps1 -Environment prod
```

若需人工覆盖，可记录原因：

```powershell
pwsh .\testing\arch_guard.ps1 -Environment prod -OverrideReason "temporary incident mitigation"
```

覆盖记录会写入：

```text
docs\release-overrides.log
```

这份日志是**当前保留的正式审计轨迹**，不再依赖旧的 `docs\v1\` 目录。

## 4. 仍需继续补强的部分

### 4.1 分布式状态与锁

当前代码已经有 `distributed\` 包和 `WorkspaceLock` 抽象，但默认实现仍以单进程/本地文件为主。要支撑多实例生产，还需要：

- 分布式 session / checkpoint / task store
- 分布式 workspace lock
- 更清晰的多租户隔离策略

### 4.2 统一服务壳

虽然 `gateway`、`serve`、`examples\mossclaw` 已经提供基础能力，但仓库还缺一个“官方统一 API server 包装层”，用于：

- HTTP health / readiness / metrics
- authn / authz
- request admission / quotas
- worker lifecycle orchestration

### 4.3 发布与包装

当前 examples 已经可以直接运行，但还缺少统一的：

- 发布工件策略
- 版本打包约定
- 平台安装分发入口
- 对 README / docs / examples 的统一产品叙述

## 5. 是否已经“生产可用”

### 可以说“是”的部分

对 **单节点、人工监管、工作区明确、对安全边界敏感** 的 agent 产品，当前代码已经具备生产所需的关键机制：

- 执行策略
- 审批
- 审计
- 回滚
- 持久化
- 健康检查
- 门禁验证

### 不能说“完全完成”的部分

对 **大规模多租户、强分布式、高可用托管** 场景，当前仓库还没有给出最终形态。

## 6. 建议的上线清单

如果你打算基于当前代码上线一个单实例产品，至少确认：

1. 明确 `trusted` / `restricted` 与 approval mode 默认值
2. 配置 session / checkpoint / task / memory 持久化目录
3. 打开审计日志和价格/治理观察器
4. 在部署脚本中加入 `go test ./...`、`go build ./...` 与 `testing\arch_guard.ps1`
5. 为 operator 提供 checkpoint、review、rollback 的操作路径

做到这些后，当前仓库已经能支撑“谨慎上线、可回滚、可审计”的 agent 产品。
