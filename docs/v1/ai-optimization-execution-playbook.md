# Moss v1 优化执行方案（AI Runbook）

更新时间：2026-04-08

## 1. 执行总则

- 一个任务一个分支，禁止在同一 PR 混入无关改动。
- 每个任务必须包含自动化验证证据。
- 失败优先回滚，不在主干做长时间不稳定实验。
- 每次改动同步更新相关文档。

## 2. 标准任务模板（复制即用）

```md
## TASK <TaskID>
- Priority: P0|P1|P2
- Owner: <role>
- Goal: <one sentence>
- Scope: <paths>
- Preconditions:
  - [ ] ...
- Actions:
  - [ ] ...
  - [ ] ...
- Validation:
  - [ ] unit/integration tests
  - [ ] eval regression
- Deliverables:
  - [ ] PR link
  - [ ] report file path
- Rollback:
  - Trigger: <condition>
  - Steps: <exact revert steps>
```

## 3. 阶段执行清单

## P0（0-30 天）

### TASK P0-EVAL-001: Eval Case 校验器

- Priority: P0
- Owner: Testing
- Goal: 防止无效 case 进入回归流程。
- Scope: `testing/eval/loader.go`, `testing/eval/types.go`
- Actions:
  - 增加 case 字段校验（ID、messages、expect、weights）。
  - 输出清晰错误信息（含 case 文件路径和字段）。
- Validation:
  - `go test ./testing/eval/...`
  - 增加 bad-case 测试样例。
- Rollback:
  - 若校验误伤，降级为 warning 模式并保留日志。

### TASK P0-EVAL-002: Baseline 对比与回归阈值

- Priority: P0
- Owner: Testing
- Goal: 让评测可比较、可阻断。
- Scope: `testing/eval/runner.go`
- Actions:
  - 支持读取 baseline 结果（JSON）。
  - 支持阈值策略（如 score 降低 > 0.03 则 fail）。
  - 产出 Markdown 与 JSON 两种摘要报告。
- Validation:
  - `go test ./testing/eval/...`
  - 人工构造退化样例验证阻断逻辑。
- Rollback:
  - 阈值误判时仅标记 warning，不阻断合并。

### TASK P0-EVAL-003: 核心回归集补齐

- Priority: P0
- Owner: Testing + Kernel
- Goal: 覆盖关键行为。
- Scope: `testing/eval/cases/`
- Actions:
  - 新增至少 20 个 case，覆盖：工具调用、安全审批、长对话、错误恢复。
  - 对每类 case 添加 tags，便于分组运行。
- Validation:
  - 批量执行并记录耗时和通过率。
- Rollback:
  - 如运行过慢，分层执行 smoke/full 两套集合。

### TASK P0-RUNTIME-001: summarize/rag 开关化

- Priority: P0
- Owner: Kernel + Appkit
- Goal: 新能力默认可控，防止行为突变。
- Scope: `kernel/middleware/builtins/summarize.go`, `kernel/middleware/builtins/rag.go`, `appkit/runtime_builder.go`
- Actions:
  - 暴露启停开关和关键参数。
  - 默认采用保守值，并支持 profile 覆盖。
- Validation:
  - `go test ./kernel/... ./appkit/...`
  - 跑一轮 eval，确认成功率无明显回退。
- Rollback:
  - 一键关闭 summarize/rag，回退 `truncate`。

### TASK P0-DOC-001: 回滚手册

- Priority: P0
- Owner: Docs
- Goal: 发生回归时可快速恢复。
- Scope: `docs/`
- Actions:
  - 新增故障分级、回滚触发条件、指令模板。
  - 记录常见失败场景（评测退化、成本激增、超时上升）。
- Validation:
  - 组织一次演练并补充缺口。
- Rollback:
  - 文档本身无需回滚，仅版本迭代。

## P1（31-60 天）

### TASK P1-PROMPT-001: Prompt 统一装配路径

- Scope: `kernel/prompt/`, `appkit/flags.go`
- 目标: Prompt 版本化和可追踪。

### TASK P1-BUDGET-001: 全局预算聚合与策略

- Scope: `kernel/session/`, `appkit/flags.go`
- 目标: 跨 session/agent 成本治理。

### TASK P1-EVAL-001: 按 prompt/budget 维度输出评测报告

- Scope: `testing/eval/runner.go`
- 目标: 支持多配置对比。

## P2（61-90 天）

### TASK P2-OBS-001: 指标统一上报

- Scope: `contrib/telemetry/`, `kernel/observe/`
- 目标: 将稳定性、成本、质量指标统一观察。

### TASK P2-REL-001: 发布门禁流程固化

- Scope: `testing/arch_guard.ps1`, `docs/production-readiness.md`
- 目标: 未达标版本不可发布。

### TASK P2-SERVE-001: 运行健康状态暴露

- Scope: `appkit/serve.go`
- 目标: 提供基础可运维健康面板数据源。

## 4. 验收标准（DoD）

每个任务完成必须满足：

- [ ] 代码改动已提交并通过相关测试。
- [ ] 影响范围文档已更新。
- [ ] 评测结果已归档（JSON/Markdown）。
- [ ] 回滚路径可执行且已验证。
- [ ] PR 描述包含动机、方案、风险、验证证据。

## 5. PR 模板（建议）

```md
## Why
- 背景:
- 问题:

## What
- 改动点:
- 影响路径:

## Risk
- 风险:
- 回滚方式:

## Validation
- [ ] go test ...
- [ ] eval ...
- [ ] 手工验证 ...

## Artifacts
- 报告路径:
- 日志/截图:
```

## 6. AI 执行命令模板

以下命令为 PowerShell 示例，可按任务裁剪。

```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./testing/eval/...
go test ./kernel/... ./appkit/... ./presets/...
Pop-Location
```

```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./... 
Pop-Location
```

## 7. 状态追踪建议

建议维护 `docs/v1/status.md`（后续新增），按以下字段记录：

- TaskID
- Status（todo/doing/done/blocked）
- Owner
- PR
- KPI snapshot
- Next action

