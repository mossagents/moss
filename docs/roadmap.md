# Roadmap

当前路线图以 **现有主线代码** 为准：优先巩固 library-first runtime、examples 产品面和单节点生产能力，再推进分布式与托管化能力。

## 当前状态

已经完成的主线能力：

- 最小可嵌入 Kernel
- `appkit` 官方装配路径
- `presets\deepagent` 产品级预设
- builtin / MCP / `SKILL.md` / subagent 统一加载
- profile / trust / approval / execution policy
- session / checkpoint / task / memory 持久化
- workspace isolation、repo state、patch apply / rollback
- health surface 与 release gates
- `mosscode` 产品面和多个参考 examples

## 近期重点

### 1. 继续收敛文档与产品叙述

当前仓库的真实入口已经是 `examples\` + `appkit`，后续仍需要继续减少：

- README 与 examples 之间的漂移
- library API 与产品命令面的命名差异
- operator 文档与运行时实际行为的偏差

### 2. 把 gate/observer 接到更完整的运行时管线

现在已经有 normalized metrics 和 release gates，下一步重点是让这些能力更自然地进入：

- 长期运行实例
- 服务化 health / readiness
- 自动化发布流程

### 3. 稳定多实例协作边界

当前已具备 `WorkspaceLock`、task runtime、mailbox、gateway、distributed 等基础块；后续重点是：

- 分布式状态存储
- 多实例 workspace 协作
- 更清晰的多租户隔离模型

## 中期方向

### 服务化与托管化

补齐面向服务部署的统一能力层，例如：

- HTTP / RPC 服务壳
- 认证鉴权
- 配额和 admission control
- 更标准的 metrics / health / audit 接口

### Knowledge 与 scheduling 的产品化接线

当前 `knowledge\` 和 `scheduler\` 已可用，但还更多停留在“由 example 证明可组合”的阶段。中期目标是把它们变成：

- 更稳定的 appkit 扩展组合
- 更清晰的 operator 配置面
- 更明确的持久化/回放策略

### 更完整的发布包装

包括但不限于：

- 统一二进制/分发策略
- 示例应用的安装方式
- 平台打包与部署模板

## 长期方向

### 分布式 runtime

将当前单节点能力推广到多实例场景：

- 分布式 session / checkpoint / task runtime
- 分布式锁
- 跨实例 worktree / mailbox / event routing

### 更强的治理与观测

在现有 observer、metrics、gate 的基础上继续推进：

- 更细粒度成本治理
- 更稳定的 failover 策略
- 长周期运行质量回归面板

### 面向更多产品形态的适配层

当前 examples 已覆盖 coding、research、writer、assistant、desktop、realtime 等模式；长期方向是把这些共性继续抽象为更稳定的应用层构件，而不是让每个产品各自复制拼装逻辑。
