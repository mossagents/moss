# 🔧 Agent Harness 改进计划

> 基于 2026-04 架构评审的完整待办列表，按"设计先行、再实现"原则分优先级规划。

---

## 评审结论速览

| 子系统 | 完整度 | 核心问题 |
|--------|--------|----------|
| Model | ⭐⭐⭐⭐ | 缺成本聚合、Extra 参数不安全 |
| Context | ⭐⭐⭐ | 只有截断、无 summarization、无 cache hint |
| Agent/Subagents | ⭐⭐⭐⭐ | 委派协议弱、无优先级调度 |
| **Memory** | ⭐⭐ | **最薄弱**——无持久化、无层次、无 RAG pipeline |
| Skills | ⭐⭐⭐ | 无版本控制、无依赖拓扑、无模板变量 |
| Sandbox | ⭐⭐⭐ | 无容器隔离、网络限制软执行 |
| HITL | ⭐⭐⭐⭐ | 缺超时、缺异步审批 |
| Workspace | ⭐⭐⭐ | 与 Sandbox 边界模糊、无 VFS |
| Observability | ⭐⭐⭐ | Metrics 未内建、Span 边界不全 |
| **Evaluation** | ⭐ | **完全缺失** |
| Prompt Management | ⭐⭐ | 分散、无版本、无 A/B |
| Cost Governance | ⭐⭐ | 仅 Session 级，无跨 Agent 预算 |

---

## 规划原则

```
设计文档  →  接口定义  →  实现  →  测试  →  集成
```

每个优先级内部：**先完成所有设计任务，再开始实现任务**。
设计文档产出到 `docs/design/` 目录，实现对应 `plan/` 目录记录执行计划。

---

## P0 · 关键补全（优先级最高）

> 影响 agent 质量的根本性缺陷，阻塞生产可用性。

### 设计阶段（先完成）

#### P0-D1 · Memory 三层架构设计
- **问题**：`knowledge/memory.go` 仅有 in-memory 余弦相似度检索，无持久化、无层次、无 RAG pipeline
- **目标**：设计三层记忆体系 + 自动 RAG 注入
- **产出文档**：[`docs/design/memory-architecture.md`](design/memory-architecture.md)
- **包含内容**：
  - Working Memory：当前 session 激活状态（对话摘要、临时变量）
  - Episodic Memory：历史事件序列（含时间戳、重要性评分）
  - Semantic Memory：持久知识库（向量嵌入 + 全文索引）
  - `port.VectorStore` 接口定义
  - RAG pipeline：AgentLoop 每轮自动检索 → 注入 context
  - 遗忘策略预留接口（TTL、重要性衰减）

#### P0-D2 · Evaluation Harness 设计
- **问题**：完全缺失 evaluation 能力，无法测量 agent 行为质量
- **目标**：可用于回归测试和持续评估的 eval 框架
- **产出文档**：[`docs/design/evaluation-harness.md`](design/evaluation-harness.md)
- **包含内容**：
  - `eval.Case`：输入（messages/goal）+ 期望行为（assert 函数 or 期望输出模式）
  - `eval.Judge`：LLM-as-judge scorer + heuristic scorer 接口
  - `eval.Runner`：批量执行 + 打分 + 报告生成
  - 与 `go test` 集成方式（`testing/` 目录）
  - Baseline 对比与 regression 检测

#### P0-D3 · Context 压缩策略设计
- **问题**：`Session.TruncateMessages` 只有截断，长对话会丢失关键历史
- **目标**：可插拔的 context 压缩策略体系
- **产出文档**：[`docs/design/context-compression.md`](design/context-compression.md)
- **包含内容**：
  - `ContextCompressor` 接口（Summarize / Truncate / Sliding Window）
  - 基于 LLM 的摘要压缩实现
  - Prompt Cache Breakpoint 标记（兼容 Anthropic/Gemini cache API）
  - 触发阈值配置（`LoopConfig.ContextCompression`）

---

### 实现阶段（设计完成后）

#### P0-I1 · Memory 持久化 VectorStore Port + Adapters
- **依赖**：P0-D1 设计文档完成
- **文件**：`kernel/port/vector_store.go`、`knowledge/adapters/`
- **工作量**：定义 `port.VectorStore` 接口；实现 pgvector、Qdrant、in-memory 三个适配器
- **验收**：`knowledge/` 包通过 `go test`，in-memory 适配器覆盖基本 CRUD + 搜索

#### P0-I2 · Memory 三层实现
- **依赖**：P0-I1
- **文件**：`knowledge/working.go`、`knowledge/episodic.go`、`knowledge/semantic.go`
- **工作量**：实现三层接口，各层独立存储策略；提供统一 `MemoryManager` 聚合访问

#### P0-I3 · RAG Pipeline 内建
- **依赖**：P0-I2
- **文件**：`kernel/middleware/builtins/rag.go`
- **工作量**：实现 `RAGMiddleware`，在每轮 LLM 调用前自动检索并注入 memory chunk

#### P0-I4 · Evaluation Harness 实现
- **依赖**：P0-D2
- **文件**：`testing/eval/`
- **工作量**：实现 `eval.Case` + `eval.Judge` + `eval.Run()`，提供示例 eval suite

#### P0-I5 · Context Summarization Middleware
- **依赖**：P0-D3
- **文件**：`kernel/middleware/builtins/summarize.go`
- **工作量**：实现 `SummarizeMiddleware`，token 阈值触发，调用 LLM 压缩旧历史

---

## P1 · 高优先级改进

> 显著影响安全性、可扩展性、agent 协作能力。

### 设计阶段

#### P1-D1 · Prompt 管理系统设计
- **问题**：system prompt 拼接逻辑分散，无版本控制，无模板变量
- **产出文档**：[`docs/design/prompt-management.md`](design/prompt-management.md)
- **包含内容**：
  - `PromptBuilder`：统一拼接（system + skill additions + session context）
  - `VersionedPrompt`：版本化模板，semver 引用
  - 模板变量注入（运行时参数渲染：时间、用户信息、工具列表）
  - A/B 测试框架预留接口

#### P1-D2 · 跨 Agent 全局 Budget 治理设计
- **问题**：`Budget` 仅 Session 级，跨 agent 无法聚合 token 用量和成本
- **依赖**：P0-I2（Memory 实现后成本归因更完整）
- **产出文档**：[`docs/design/budget-governance.md`](design/budget-governance.md)
- **包含内容**：
  - `GlobalBudget`：跨 session/agent 的 token + 成本总限额
  - Cost 归因：哪个 tool / subagent / LLM call 消耗了多少
  - Budget 超额的 graceful degradation 策略（降级模型 → 截断 → 拒绝）
  - `BudgetReport` 结构化输出接口

#### P1-D3 · Container Sandbox 设计
- **问题**：当前仅本地文件系统隔离，高风险代码执行不安全
- **产出文档**：[`docs/design/container-sandbox.md`](design/container-sandbox.md)
- **包含内容**：
  - Docker 容器生命周期管理（创建/复用/销毁）
  - cgroup v2 资源限额（CPU/MEM/PID/文件描述符）
  - iptables/nftables 网络强制隔离
  - Overlay 文件系统挂载，支持 Workspace 映射
  - gVisor（runsc）可选 kernel-level 隔离

#### P1-D4 · Agent-to-Agent (A2A) 通信协议设计
- **问题**：`MailMessage.Content` 是纯字符串，父子 agent 间契约仅靠 LLM 理解
- **依赖**：P0-D1（Memory 设计先完成，共享上下文规范依赖它）
- **产出文档**：[`docs/design/a2a-protocol.md`](design/a2a-protocol.md)
- **包含内容**：
  - `TypedMailMessage`：InputSchema + OutputSchema + Priority
  - 任务委派契约（structured delegation spec）
  - Reply-to 路由标准
  - 与现有 `Mailbox` + `TaskTracker` 的兼容迁移路径

#### P1-D5 · 异步 HITL + 审批超时设计
- **问题**：审批同步阻塞无超时，无异步审批通道，无审批历史查询
- **产出文档**：[`docs/design/async-hitl.md`](design/async-hitl.md)
- **包含内容**：
  - `ApprovalRequest.Deadline`：超时自动降级策略（拒绝 / soft-limit）
  - 异步审批通道接口（Webhook 适配器：Slack / 邮件 / HTTP callback）
  - `port.ApprovalStore`：审批记录持久化与查询接口
  - 审批 SLA 告警接口

#### P1-D6 · Workspace 与 Sandbox 边界厘清
- **问题**：`Workspace`（文件操作）与 `Sandbox`（隔离执行）概念重叠，使用者易混淆
- **产出文档**：[`docs/design/workspace-sandbox-boundary.md`](design/workspace-sandbox-boundary.md)
- **包含内容**：
  - 职责边界重定义：Workspace = 数据层（文件 CRUD），Sandbox = 执行层（隔离 + 资源控制）
  - `port.VFS`（Virtual File System）抽象：统一本地 / 内存 / S3 / GCS
  - 多 Workspace namespace（跨 repo agent 操作的路径隔离）
  - 迁移指南：现有 `LocalSandbox` 拆分为 `LocalWorkspace + LocalExecutor`

---

### 实现阶段

#### P1-I1 · PromptBuilder + 模板变量注入
- **依赖**：P1-D1
- **文件**：`userio/prompting/builder.go`

#### P1-I2 · 跨 Agent Budget Governance 实现
- **依赖**：P1-D2
- **文件**：`kernel/session/global_budget.go`、`kernel/loop/` 消费路径修改

#### P1-I3 · Docker Sandbox
- **依赖**：P1-D3
- **文件**：`sandbox/docker.go`、`sandbox/docker_test.go`

#### P1-I4 · A2A 协议 typed message + 优先级调度
- **依赖**：P1-D4
- **文件**：`kernel/port/mailbox.go` 扩展、`agent/task.go` Priority 字段

#### P1-I5 · HITL 审批超时 + 审批记录存储
- **依赖**：P1-D5
- **文件**：`kernel/port/approval.go` 扩展、`kernel/port/approval_store.go`

#### P1-I6 · ModelConfig.Extra 参数 Schema 校验
- **依赖**：P1-D6（边界厘清后 adapter 结构更清晰）
- **文件**：各 `adapters/*/` 提供 typed config struct，Boot 阶段校验

---

## P2 · 中优先级完善

> 提升系统可维护性、可观测性、长期运营质量。

### 设计阶段

#### P2-D1 · Skill 版本控制与依赖拓扑设计
- **问题**：`skill.Metadata` 无版本字段，skill 间依赖靠加载顺序隐性保证
- **产出文档**：[`docs/design/skill-versioning.md`](design/skill-versioning.md)
- **包含内容**：
  - `Metadata.Version`（semver）+ `Metadata.Dependencies []SkillDep`
  - `Manager.Register` 拓扑排序 + 循环依赖检测
  - 热更新接口（reload 不重启进程）
  - 版本锁文件（`moss.lock`）

#### P2-D2 · Observability 全链路 Span 设计
- **问题**：OTel span 边界不标准，Metrics 未内建，slog 未统一
- **产出文档**：[`docs/design/observability-spans.md`](design/observability-spans.md)
- **包含内容**：
  - AgentLoop 各阶段 span 边界（LLM call / tool call / approval wait / context compression）
  - `MetricsObserver` 子接口（counter/histogram）
  - Prometheus exporter 实现规范
  - 结构化日志字段规范（session_id / run_id / tool_name / model）

#### P2-D3 · Memory 遗忘与合并策略设计
- **问题**：Memory 只增不减，无 TTL、无重要性衰减、无去重合并
- **依赖**：P0-I2
- **产出文档**：[`docs/design/memory-lifecycle.md`](design/memory-lifecycle.md)
- **包含内容**：
  - TTL 索引与过期清理
  - 重要性评分衰减函数（基于访问频率 + 时间距离）
  - 相似 chunk 去重合并算法（cosine threshold）
  - Consolidation 周期任务接口

#### P2-D4 · 远程 Workspace / VFS 设计
- **依赖**：P1-D6（边界厘清后开展）
- **产出文档**：[`docs/design/virtual-filesystem.md`](design/virtual-filesystem.md)
- **包含内容**：
  - `port.VFS` 接口（超集于现有 `port.Workspace`）
  - S3 适配器（AWS SDK v2）
  - GCS 适配器（Google Cloud Go）
  - 内存 VFS（测试用）
  - 多 namespace 隔离实现

---

### 实现阶段

#### P2-I1 · OTel Span 自动注入
- **依赖**：P2-D2
- **文件**：`contrib/telemetry/otel/span_observer.go`

#### P2-I2 · Metrics 内建层（Prometheus Exporter）
- **依赖**：P2-D2
- **文件**：`contrib/telemetry/prometheus/`

#### P2-I3 · Memory TTL + 重要性衰减 + Consolidator
- **依赖**：P2-D3
- **文件**：`knowledge/lifecycle.go`、`knowledge/consolidator.go`

#### P2-I4 · Skill 版本字段 + 依赖拓扑加载
- **依赖**：P2-D1
- **文件**：`skill/skill.go`、`skill/manager.go`

#### P2-I5 · S3 / GCS Workspace Adapter
- **依赖**：P2-D4
- **文件**：`workspace/s3.go`、`workspace/gcs.go`

---

## P3 · 长期方向

> 架构演进，非当前迭代目标。

### 设计阶段

#### P3-D1 · 分布式 Session / Task Runtime 设计
- **依赖**：P1-I2（全局预算治理）
- **产出文档**：[`docs/design/distributed-runtime.md`](design/distributed-runtime.md)
- **包含内容**：
  - NATS / Kafka 驱动的分布式 Session 路由
  - 跨实例 TaskTracker 同步（CRDT or leader-based）
  - 水平扩展 Agent Worker 池
  - 分布式调度器分布式锁（Redis / etcd）

#### P3-D2 · Skill 市场与注册中心设计
- **依赖**：P2-I4（版本控制完成后）
- **产出文档**：[`docs/design/skill-marketplace.md`](design/skill-marketplace.md)
- **包含内容**：
  - 在线 Skill Registry（OCI artifact 或 Go module proxy）
  - `moss skill install/list/remove` 命令
  - 安全沙箱隔离安装包（签名校验）
  - 社区贡献流程

### 实现阶段

#### P3-I1 · 分布式 Session Runtime
- **依赖**：P3-D1

#### P3-I2 · Skill 市场 CLI + Registry
- **依赖**：P3-D2

---

## 依赖关系图

```
P0-D1 ──► P0-I1 ──► P0-I2 ──► P0-I3
   │                    │
   └────────────────────┼──► P1-D2 ──► P1-I2 ──► P3-D1 ──► P3-I1
                        │
P0-D2 ──► P0-I4         └──► P2-D3 ──► P2-I3

P0-D3 ──► P0-I5

P0-D1 ──► P1-D4 ──► P1-I4

P1-D1 ──► P1-I1
P1-D3 ──► P1-I3
P1-D5 ──► P1-I5
P1-D6 ──► P1-I6
P1-D6 ──► P2-D4 ──► P2-I5

P2-D1 ──► P2-I4 ──► P3-D2 ──► P3-I2
P2-D2 ──► P2-I1
P2-D2 ──► P2-I2
```

---

## 当前可立即开始的任务（无前置依赖）

以下任务无前置依赖，可并行启动：

| ID | 任务 | 负责方向 |
|----|------|---------|
| P0-D1 | Memory 三层架构设计 | Knowledge |
| P0-D2 | Evaluation Harness 设计 | Testing |
| P0-D3 | Context 压缩策略设计 | Kernel |
| P1-D1 | Prompt 管理系统设计 | UserIO |
| P1-D3 | Container Sandbox 设计 | Sandbox |
| P1-D5 | 异步 HITL + 审批超时设计 | Security |
| P1-D6 | Workspace/Sandbox 边界厘清 | Infra |
| P2-D1 | Skill 版本控制与依赖拓扑设计 | Skills |
| P2-D2 | Observability 全链路 Span 设计 | Telemetry |

---

## 进度追踪

| 阶段 | 总计 | 待开始 | 进行中 | 完成 |
|------|------|--------|--------|------|
| P0 设计 | 3 | 0 | 0 | 3 |
| P0 实现 | 5 | 5 | 0 | 0 |
| P1 设计 | 6 | 4 | 0 | 2 |
| P1 实现 | 6 | 6 | 0 | 0 |
| P2 设计 | 4 | 4 | 0 | 0 |
| P2 实现 | 5 | 5 | 0 | 0 |
| P3 设计 | 2 | 2 | 0 | 0 |
| P3 实现 | 2 | 2 | 0 | 0 |
| **合计** | **33** | **28** | **0** | **5** |

---

## 设计文档索引

| 文档 | 状态 | 关联任务 |
|------|------|---------|
| [memory-architecture.md](design/memory-architecture.md) | ✅ 草稿完成 | P0-D1 |
| [evaluation-harness.md](design/evaluation-harness.md) | ✅ 草稿完成 | P0-D2 |
| [context-compression.md](design/context-compression.md) | ✅ 草稿完成 | P0-D3 |
| [prompt-management.md](design/prompt-management.md) | ✅ 草稿完成 | P1-D1 |
| [workspace-sandbox-boundary.md](design/workspace-sandbox-boundary.md) | ✅ 草稿完成 | P1-D6 |
| [budget-governance.md](design/budget-governance.md) | 📝 待创建 | P1-D2 |
| [container-sandbox.md](design/container-sandbox.md) | 📝 待创建 | P1-D3 |
| [a2a-protocol.md](design/a2a-protocol.md) | 📝 待创建 | P1-D4 |
| [async-hitl.md](design/async-hitl.md) | 📝 待创建 | P1-D5 |
| [skill-versioning.md](design/skill-versioning.md) | 📝 待创建 | P2-D1 |
| [observability-spans.md](design/observability-spans.md) | 📝 待创建 | P2-D2 |

---

## 相关文档

- [架构设计](architecture.md)
- [Kernel 设计](kernel-design.md)
- [生产就绪路线图](production-readiness.md)
- [开发路线图](roadmap.md)

---

*最后更新：2026-04-07 · P0/P1 设计文档草稿完成*
