# Moss v1 AI 可执行任务目录（Task Catalog）

更新时间：2026-04-08

说明：本目录可直接被 AI 按任务 ID 顺序执行。每个任务使用统一结构，包含命令、验收、回滚和阻塞处理。

---

id: P0-EVAL-001
title: eval case schema validation
phase: P0
priority: P0
owner: testing
status: todo
scope:
  - testing/eval/loader.go
  - testing/eval/types.go
depends_on: []
inputs:
  - docs/design/evaluation-harness.md
outputs:
  - testing/eval/loader.go
  - testing/eval/eval_test.go
acceptance:
  - go test ./testing/eval/... passes
  - invalid case returns path-aware validation error
rollback_trigger:
  - strict validation rejects >10% current cases
rollback_steps:
  - downgrade strict mode to warning mode
  - rerun go test ./testing/eval/...
risk: medium

## Objective
为 eval case 建立显式校验，减少脏数据导致的回归误判。

## Preconditions
- [ ] `testing/eval/` 当前测试可通过。

## Implementation Steps
1. 在 loader 增加 `validateCase`。
2. 检查 `id/messages/expect/weights`。
3. 为 bad-case 增加测试用例。

## Commands
```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./testing/eval/...
Pop-Location
```

## Blockers
- 若历史 case 不规范过多，先生成修复清单，再批量清理。

---

id: P0-EVAL-002
title: baseline compare and regression gate
phase: P0
priority: P0
owner: testing
status: todo
scope:
  - testing/eval/runner.go
  - testing/eval/types.go
depends_on:
  - P0-EVAL-001
inputs:
  - testing/eval/cases/
outputs:
  - testing/eval/runner.go
  - testing/eval/eval_test.go
acceptance:
  - baseline JSON can be loaded
  - score drop threshold gate works
rollback_trigger:
  - gate false-positive blocks >20% normal PRs
rollback_steps:
  - change gate mode to report-only
risk: medium

## Objective
支持 baseline 对比和回归阈值阻断。

## Preconditions
- [ ] P0-EVAL-001 已完成。

## Implementation Steps
1. runner 支持 baseline 读取。
2. 增加 threshold 比较逻辑。
3. 输出 JSON + Markdown 摘要。

## Commands
```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./testing/eval/...
Pop-Location
```

## Blockers
- 若基线文件缺失，提供自动初始化命令并提示使用者确认。

---

id: P0-EVAL-003
title: build 20 core regression cases
phase: P0
priority: P0
owner: testing+kernel
status: todo
scope:
  - testing/eval/cases/
depends_on:
  - P0-EVAL-001
inputs:
  - existing cases
outputs:
  - testing/eval/cases/*
acceptance:
  - at least 20 runnable cases
  - tags for coding/tooling/security/long-run
rollback_trigger:
  - full suite runtime > 15 min
rollback_steps:
  - split smoke/full suites
risk: low

## Objective
建立覆盖核心行为的回归样本集。

## Implementation Steps
1. 定义 case 分类与标签规范。
2. 新增 case 并逐一跑通。
3. 生成 smoke/full 两套列表。

## Commands
```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./testing/eval/...
Pop-Location
```

---

id: P0-RUNTIME-001
title: summarize and rag feature flags
phase: P0
priority: P0
owner: kernel+appkit
status: todo
scope:
  - kernel/middleware/builtins/summarize.go
  - kernel/middleware/builtins/rag.go
  - appkit/runtime_builder.go
depends_on:
  - P0-EVAL-002
inputs:
  - current middleware defaults
outputs:
  - configurable flags and defaults
acceptance:
  - flags can disable summarize/rag
  - disabled mode behavior remains stable
rollback_trigger:
  - success rate drops >2% in core cases
rollback_steps:
  - disable flags by default
  - fallback to truncate middleware
risk: high

## Objective
让高影响能力可控可回滚。

## Implementation Steps
1. 增加运行时配置开关。
2. 修改装配逻辑读取开关。
3. 补齐单测和集成测试。

## Commands
```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./kernel/... ./appkit/...
go test ./testing/eval/...
Pop-Location
```

---

id: P0-DOC-001
title: regression rollback handbook
phase: P0
priority: P0
owner: docs
status: todo
scope:
  - docs/
depends_on:
  - P0-EVAL-002
inputs:
  - known failure patterns
outputs:
  - docs/*rollback*.md
acceptance:
  - handbook includes trigger/steps/owner/escalation
  - one drill record attached
rollback_trigger:
  - not applicable
rollback_steps:
  - not applicable
risk: low

## Objective
把回滚流程固化为可执行文档。

## Implementation Steps
1. 列出失败分级。
2. 定义触发阈值和负责人。
3. 给出 PowerShell 回滚模板。

## Commands
```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./testing/eval/...
Pop-Location
```

---

id: P1-PROMPT-001
title: unify prompt assembly path
phase: P1
priority: P1
owner: appkit+kernel
status: todo
scope:
  - kernel/prompt/
  - appkit/flags.go
depends_on:
  - P0-RUNTIME-001
inputs:
  - prompt docs and current builders
outputs:
  - unified prompt path with version tag
acceptance:
  - prompt version trace visible in run metadata
rollback_trigger:
  - behavior drift across key cases
rollback_steps:
  - switch to legacy prompt assembly by profile
risk: high

---

id: P1-BUDGET-001
title: global budget governance
phase: P1
priority: P1
owner: kernel-session
status: todo
scope:
  - kernel/session/
  - appkit/flags.go
depends_on:
  - P0-EVAL-002
inputs:
  - existing budget model
outputs:
  - aggregated budget report and policy
acceptance:
  - over-budget block accuracy >95%
rollback_trigger:
  - false block harms normal runs
rollback_steps:
  - set governance to observe-only
risk: medium

---

id: P1-EVAL-001
title: report by prompt and budget dimensions
phase: P1
priority: P1
owner: testing
status: todo
scope:
  - testing/eval/runner.go
depends_on:
  - P1-PROMPT-001
  - P1-BUDGET-001
inputs:
  - baseline report format
outputs:
  - grouped report output
acceptance:
  - report supports prompt_version and budget_policy dimensions
rollback_trigger:
  - report generation time doubles
rollback_steps:
  - disable grouping and emit flat report
risk: low

---

id: P2-OBS-001
title: unify telemetry metrics
phase: P2
priority: P2
owner: telemetry
status: todo
scope:
  - contrib/telemetry/
  - kernel/observe/
depends_on:
  - P1-EVAL-001
inputs:
  - existing observer events
outputs:
  - normalized metrics map
acceptance:
  - success/latency/cost/tool-error visible in one report
rollback_trigger:
  - p95 latency regression after telemetry on
rollback_steps:
  - lower sample rate or use NoOp observer
risk: medium

---

id: P2-REL-001
title: release gates and arch guard integration
phase: P2
priority: P2
owner: release+infra
status: todo
scope:
  - testing/arch_guard.ps1
  - docs/production-readiness.md
depends_on:
  - P2-OBS-001
inputs:
  - release criteria
outputs:
  - gate checklist and scripts
acceptance:
  - non-compliant release is blocked
rollback_trigger:
  - gate blocks hotfix path
rollback_steps:
  - allow manual override with incident record
risk: medium

---

id: P2-SERVE-001
title: operability health surface
phase: P2
priority: P2
owner: appkit
status: todo
scope:
  - appkit/serve.go
depends_on:
  - P2-OBS-001
inputs:
  - health metric definitions
outputs:
  - basic health endpoint/output
acceptance:
  - health output includes status, latency, active runs
rollback_trigger:
  - health checks overload service
rollback_steps:
  - reduce fields and sampling
risk: low

