# AI 执行计划 Schema（v1）

更新时间：2026-04-08

本文件定义 AI 直接执行任务时必须遵守的数据结构和状态流转。

## 1. Task 结构

每个任务必须使用以下 YAML front matter 头：

```yaml
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
  - "go test ./testing/eval/... passes"
  - "invalid case returns path-aware error"
rollback:
  trigger:
    - "loader rejects >10% existing cases unexpectedly"
  steps:
    - "revert loader validation strict mode to warning mode"
    - "re-run go test ./testing/eval/..."
risk: medium
```

## 2. Task 正文模板

```md
## Objective
一句话说明任务完成后可观测到的变化。

## Preconditions
- [ ] 前置任务已完成
- [ ] 相关设计文档已存在

## Implementation Steps
1. 修改文件 A。
2. 增加测试 B。
3. 更新文档 C。

## Commands
```powershell
Push-Location "D:\Codes\qiulin\moss"
# commands...
Pop-Location
```

## Evidence
- 测试输出摘要
- 关键 diff 说明

## Blockers
- 若阻塞，记录原因、影响、下一步。
```

## 3. 状态机

- `todo`: 已定义，未开始。
- `ready`: 前置条件满足，可执行。
- `running`: AI 正在执行。
- `blocked`: 被外部条���阻塞。
- `review`: 代码完成，等待验收。
- `done`: 验收通过。
- `rolled_back`: 已回滚。

状态转换规则：

- `todo -> ready`: 所有 `depends_on` 已 `done`。
- `ready -> running`: AI 获取执行锁。
- `running -> review`: 代码与文档改动完成，测试通过。
- `review -> done`: 验收标准全部满足。
- `running/review -> rolled_back`: 触发回滚条件。
- 任意状态 -> `blocked`: 遇到不可自动恢复问题。

## 4. 批次执行规则

- 同一批次最多 3 个并行任务。
- 不能并行执行同一路径的写任务（如同时改 `testing/eval/runner.go`）。
- 每批次必须有 1 个可验证任务（有明确测试命令）。
- 每批次结束必须产出一个批次报告。

## 5. 批次报告模板

```md
# Batch Report: BATCH-P0-01

- StartedAt:
- EndedAt:
- Tasks:
  - P0-EVAL-001 (done)
  - P0-EVAL-002 (review)
- Test Summary:
  - go test ./testing/eval/... : pass
- Risks:
  - ...
- Next Batch Suggestion:
  - ...
```

