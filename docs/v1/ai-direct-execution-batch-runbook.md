# AI 批次执行 Runbook（v1）

更新时间：2026-04-08

本文件定义 AI 按批次执行任务的流程，目标是降低冲突、提高通过率、缩短回滚半径。

## 1. 批次定义

- `Batch` 是一组可并行或串行执行的任务集合。
- 每个批次推荐 2-4 个任务。
- 批次内任务必须具备明确依赖关系。

批次命名：

- `BATCH-P0-01`
- `BATCH-P0-02`
- `BATCH-P1-01`

## 2. 推荐批次编排

## BATCH-P0-01（低风险先行）

- `P0-EVAL-001`
- `P0-EVAL-003`

执行目标：先保证 case 质量与覆盖。

## BATCH-P0-02（回归能力）

- `P0-EVAL-002`
- `P0-DOC-001`

执行目标：形成基线比较和回滚流程。

## BATCH-P0-03（运行时开关）

- `P0-RUNTIME-001`

执行目标：高风险能力开关化，确保可回退。

## BATCH-P1-01（治理收敛）

- `P1-PROMPT-001`
- `P1-BUDGET-001`

## BATCH-P1-02（评测可视）

- `P1-EVAL-001`

## BATCH-P2-01（观测统一）

- `P2-OBS-001`

## BATCH-P2-02（发布与运维）

- `P2-REL-001`
- `P2-SERVE-001`

## 3. 批次执行步骤（AI 标准流程）

1. 读取任务目录 `docs/v1/ai-direct-execution-task-catalog.md`。
2. 检查批次内每个任务的 `depends_on` 是否全部 `done`。
3. 将任务状态更新为 `running`。
4. 逐任务执行代码与文档改动。
5. 运行批次级验证命令。
6. 收集证据并更新状态为 `review`/`done`。
7. 生成批次报告。

## 4. 批次级验证命令模板

```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./testing/eval/...
go test ./kernel/... ./appkit/... ./presets/...
Pop-Location
```

按需全量：

```powershell
Push-Location "D:\Codes\qiulin\moss"
go test ./...
Pop-Location
```

## 5. 批次失败处理

- 单任务失败：
  - 该任务标记 `blocked`。
  - 批次继续执行其他无依赖任务。
- 关键任务失败（P0 或批次核心任务）：
  - 停止批次。
  - 执行回滚步骤。
  - 输出失败报告并升级给 owner。

## 6. 批次报告模板

```md
# Batch Report: BATCH-P0-01

- Start: 2026-04-08T10:00:00Z
- End: 2026-04-08T12:20:00Z
- Executor: AI

## Tasks
- P0-EVAL-001: done
- P0-EVAL-003: review

## Validation
- go test ./testing/eval/... : pass

## Risks
- regression cases runtime increased by 18%

## Rollback Actions
- none

## Next
- run BATCH-P0-02
```

## 7. 并发与冲突规则

- 不允许并行编辑同一个文件。
- 不允许并行执行共享测试基线写入任务。
- 如果两个任务都改 `testing/eval/runner.go`，必须串行。

## 8. 质量红线

出现以下任一情况，批次自动失败：

- 核心测试未通过。
- 回归分数低于阈值且未声明豁免。
- 无回滚步骤的高风险变更尝试合并。


