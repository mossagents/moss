# Moss v1 优化规划设计（AI 可执行）

更新时间：2026-04-08

## 1. 文档目标

把现有优化方向拆解为 AI 可执行的规划设计，确保每个阶段都具备：

- 明确目标
- 受控范围
- 可量化验收指标
- 可执行任务拆分
- 风险与回滚策略

## 2. 适用范围

本规划覆盖以下代码区域：

- `kernel/`
- `appkit/`
- `testing/eval/`
- `contrib/telemetry/`
- `docs/`

不在本期范围：

- 大规模架构重写
- 跨仓库联动改造
- 未定义 owner 的实验功能默认上线

## 3. 设计原则（AI 执行约束）

- 小步快跑：每个任务应可在 1~2 个 PR 内完成。
- 先验证再扩展：先保证回归稳定，再新增能力。
- 默认可回滚：每个高风险改动必须有 feature flag 或兼容分支。
- 证据驱动：所有结论必须附测试结果或报告。
- 文档同步：行为变化必须同步更新 `docs/`。

## 4. 阶段路线图

## Phase P0（0-30 天）

目标：建立质量闭环，避免无度量迭代。

关键结果：

- `testing/eval/` 具备稳定回归能力（可跑、可比、可阻断）。
- 上下文压缩与 RAG 路径支持安全开关。
- 形成最小回滚手册。

重点模块：

- `testing/eval/loader.go`
- `testing/eval/runner.go`
- `testing/eval/cases/`
- `kernel/middleware/builtins/summarize.go`
- `kernel/middleware/builtins/rag.go`
- `appkit/runtime_builder.go`

阶段 KPI：

- PR eval 准入覆盖率达到 100%。
- eval flaky 率小于 5%。
- 核心场景 case 覆盖率大于 70%。
- 回归评测总耗时小于 10 分钟。

风险与回滚：

- 风险：LLM Judge 波动导致误报。
- 回滚：默认只启用 `RuleJudge`，`LLMJudge` 作为可选项。

- 风险：summarize/rag 导致输出质量回退。
- 回滚：关闭中间件开关，退回 `truncate` 路径。

## Phase P1（31-60 天）

目标：统一 Prompt 和预算治理路径。

关键结果：

- Prompt 组装路径统一且可追踪。
- 跨 session/agent 预算聚合可观测。
- eval 报告支持按 prompt 版本和预算策略对比。

重点模块：

- `kernel/prompt/`
- `kernel/session/`
- `appkit/flags.go`
- `docs/design/prompt-management.md`
- `docs/design/budget-governance.md`

阶段 KPI：

- Prompt 版本追溯率 100%。
- 超预算拦截准确率大于 95%。
- profile 间评测报告可重复对比。

风险与回滚：

- 风险：新 PromptBuilder 引发行为漂移。
- 回滚：保留旧拼装路径，通过 profile 开关回切。

- 风险：预算策略误伤正常任务。
- 回滚：切换到 observe-only 模式（只记录不拦截）。

## Phase P2（61-90 天）

目标：建立生产级观测与发布门禁。

关键结果：

- eval、成本、稳定性指标统一可视。
- 发布门禁固化进流水线。
- 架构守卫规则作为常规检查项。

重点模块：

- `contrib/telemetry/`
- `appkit/serve.go`
- `testing/arch_guard.ps1`
- `docs/production-readiness.md`

阶段 KPI：

- 异常定位平均时间小于 1 小时。
- 回滚恢复时间小于 30 分钟。
- 发布准点率大于 90%。

风险与回滚：

- 风险：观测开销影响性能。
- 回滚：启用采样上报或 `NoOp observer`。

## 5. 优先级矩阵

- `P0`: 必做，不做会直接影响质量可控性。
- `P1`: 高优先，影响成本与行为稳定性。
- `P2`: 中优先，影响规模化与运维效率。

## 6. MVP 定义（30 天内必须交付）

MVP-01:

- `RuleJudge` + 20 个核心 case + baseline 对比报告。

MVP-02:

- summarize/rag 可配置开关（默认保守策略）。

MVP-03:

- 回归失败回滚手册（位于 `docs/`）。

## 7. AI 执行输入/输出契约

每个任务必须按以下字段组织：

- `TaskID`: 唯一标识（如 `P0-EVAL-001`）
- `Goal`: 任务目标
- `Scope`: 涉及路径
- `Preconditions`: 前置条件
- `Actions`: 执行动作
- `Validation`: 验收方法
- `Artifacts`: 产物（PR、报告、截图、日志）
- `Rollback`: 回滚条件和步骤

## 8. 治理节奏

- 每周一次计划评审：看任务完成率、阻塞项、风险。
- 每两周一次里程碑评审：看 KPI 是否达到阶段阈值。
- 每个阶段结束输出总结：保留成功策略与失败案例。

