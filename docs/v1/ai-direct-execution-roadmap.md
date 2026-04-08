# Moss v1 AI 直接执行路线图（30/60/90）

更新时间：2026-04-08

## 1. 目标

将优化方案转化为可由 AI 独立推进的执行路线，要求每个阶段均具备：

- 可落地任务清单
- 可执行命令模板
- 可量化 KPI
- 可回滚策略

## 2. 阶段概览

| 阶段 | 时间 | 主题 | 核心产物 | 发布门槛 |
|---|---|---|---|---|
| P0 | 0-30 天 | 质量闭环 | eval 回归基线 + summarize/rag 开关 + 回滚手册 | eval 阻断可用 |
| P1 | 31-60 天 | 治理收敛 | prompt 统一路径 + 全局预算治理 | 成本/行为可追踪 |
| P2 | 61-90 天 | 生产化 | telemetry + 发布门禁 + 架构守卫 | 可观测发布 |

## 3. P0（0-30 天）详细规划

## Epic P0-EVAL: 构建回归与对比能力

- 作用域：`testing/eval/`
- 目标：把评测从“可跑”升级为“可阻断、可比较、可复现”。
- KPI：
  - eval flaky < 5%
  - 核心场景覆盖率 > 70%
  - 单次回归时长 < 10 分钟

## Epic P0-RUNTIME: 风险能力开关化

- 作用域：`kernel/middleware/builtins/`, `appkit/`
- 目标：summarize/rag 默认可控，出现回退可快速关闭。
- KPI：
  - 功能开关可通过配置启停
  - 开关关闭后行为回退稳定

## Epic P0-DOC: 失败快速恢复

- 作用域：`docs/`
- 目标：形成可执行回滚手册，降低故障恢复时间。
- KPI：
  - 回滚步骤可被新成员复现
  - 故障演练至少 1 次

## 4. P1（31-60 天）详细规划

## Epic P1-PROMPT: Prompt 管理统一化

- 作用域：`kernel/prompt/`, `appkit/flags.go`
- KPI：
  - Prompt 版本追溯率 100%
  - 不同 profile 行为可解释

## Epic P1-BUDGET: 全局预算治理

- 作用域：`kernel/session/`, `appkit/flags.go`
- KPI：
  - 超预算拦截准确率 > 95%
  - 预算报告可按 session/agent 聚合

## Epic P1-EVAL-REPORT: 多维评测报告

- 作用域：`testing/eval/`
- KPI：
  - 报告支持 prompt/budget 维度对比
  - 回归退化可追踪到配置版本

## 5. P2（61-90 天）详细规划

## Epic P2-OBS: 观测统一

- 作用域：`contrib/telemetry/`, `kernel/observe/`
- KPI：
  - 关键指标可视化（成功率、延迟、成本、工具错误率）

## Epic P2-RELEASE: 发布门禁

- 作用域：`testing/arch_guard.ps1`, `docs/production-readiness.md`
- KPI：
  - 未达标版本不可发布
  - 回滚恢复时间 < 30 分钟

## Epic P2-OPERABILITY: 运行健康面

- 作用域：`appkit/serve.go`
- KPI：
  - 提供基础健康状态输出
  - 故障定位平均时间 < 1 小时

## 6. AI 执行策略

- 每个 Epic 至少拆成 3 个 Task。
- 每个 Task 必须使用 `docs/v1/plan-schema.md` 结构。
- 每周按批次执行（Batch），每批次 2-4 个任务。
- 每批次结束必须更新状态文档并附测试证据。

## 7. 风险治理总表

| 风险 | 触发信号 | 降级策略 | 回滚策略 |
|---|---|---|---|
| eval 误报 | false positive 上升 | 降级为 warning | 仅保留 RuleJudge |
| summarize/rag 回退 | 成功率下降 >2% | 关闭相关开关 | 回退到 truncate |
| prompt 漂移 | 同 case 输出偏差过大 | 锁定 prompt 版本 | profile 回切旧链路 |
| telemetry 性能损耗 | p95 延迟显著上升 | 降低采样率 | 切回 NoOp observer |

## 8. 最小可交付（MVP）

30 天内必须交付：

1. `testing/eval/` baseline 对比与阈值阻断。
2. 至少 20 个核心回归 case。
3. summarize/rag 可配置开关。
4. 回归失败回滚手册。

