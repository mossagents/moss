# Regression Rollback Drill 001

日期：2026-04-08

## Trigger

- 场景：baseline 文件缺失，门禁进入 report-only 回退路径。
- 影响：未阻断测试运行，但无法进行硬阻断决策。

## Steps

1. 执行 `go test ./testing/eval/...`，确认 eval 基本可用。
2. 使用 report-only 逻辑继续输出回归报告。
3. 记录需要补齐 baseline 初始化动作。

## Owner

- Primary: `testing`
- Backup: `kernel`

## Escalation

- 15 分钟内无法恢复 baseline 初始化流程 -> 升级 L2。
- 30 分钟内影响主干门禁策略 -> 升级 L3。

## Drill Record

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:20:00Z
- Duration: 20 min

### Timeline

- T+00m: 发现 baseline 缺失。
- T+05m: 启用 report-only 继续运行。
- T+10m: `go test ./testing/eval/...` 通过。
- T+20m: 输出恢复建议，进入后续 baseline 初始化任务。

### Outcome

- 门禁未扩大阻塞范围。
- 核心验证命令保持可执行。

### Follow-up

- 增加 baseline 初始化脚本或命令入口。
- 在 PR 模板中补充 report-only 说明。

