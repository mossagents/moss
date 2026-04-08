# Moss v1 AI Execution Docs

本目录提供可由 AI 直接读取和执行的优化计划文档。

## 核心入口（推荐顺序）

0. `ai-agent-prompt.md`
   - 执行型 AI 的系统提示词模板（角色、状态机、执行循环、失败回滚、输出格式）。
1. `ai-direct-execution-roadmap.md`
   - 30/60/90 天目标、Epic、KPI、风险总表。
2. `plan-schema.md`
   - Task 数据结构、状态机、批次规则。
3. `ai-direct-execution-task-catalog.md`
   - 可直接执行的任务清单（含命令、验收、回滚）。
4. `ai-direct-execution-batch-runbook.md`
   - 批次编排、并发约束、失败处理、批次报告模板。
5. `status-template.md`
   - 任务/批次/KPI/阻塞跟踪模板。

## 兼容文档

- `ai-optimization-planning-design.md`
- `ai-optimization-execution-playbook.md`

以上两份可作为背景说明继续保留；新执行流程以“核心入口”文档链为准。
