# AI 执行状态看板模板（v1）

更新时间：2026-04-08

## 1. 任务状态表

| TaskID | Phase | Owner | Status | Branch | PR | Last Update | Next Action |
|---|---|---|---|---|---|---|---|
| P0-EVAL-001 | P0 | testing | todo | - | - | - | start validation design |
| P0-EVAL-002 | P0 | testing | todo | - | - | - | wait P0-EVAL-001 |

状态枚举：`todo` / `ready` / `running` / `blocked` / `review` / `done` / `rolled_back`

## 2. 批次状态表

| BatchID | Tasks | Status | Start | End | Validation | Notes |
|---|---|---|---|---|---|---|
| BATCH-P0-01 | P0-EVAL-001,P0-EVAL-003 | todo | - | - | - | - |

## 3. KPI 快照

| KPI | Target | Current | Trend | Source |
|---|---:|---:|---|---|
| eval flaky rate | <5% | - | - | testing/eval reports |
| core case coverage | >70% | - | - | testing/eval/cases |
| regression runtime | <10 min | - | - | CI/local run |
| rollback recovery time | <30 min | - | - | drill report |

## 4. 阻塞记录

| ID | TaskID | Blocker | Impact | Owner | OpenedAt | Resolution |
|---|---|---|---|---|---|---|
| BLK-001 | P0-EVAL-002 | baseline file missing | gate unavailable | testing | - | create baseline init command |

## 5. 每周回顾模板

```md
# Weekly Review YYYY-MM-DD

## Completed
- ...

## In Progress
- ...

## Blocked
- ...

## KPI Delta
- ...

## Next Week Plan
- ...
```

