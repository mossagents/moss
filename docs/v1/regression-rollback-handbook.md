# Regression Rollback Handbook (P0-DOC-001)

更新时间：2026-04-08

## Scope

本手册用于 `testing/eval` 回归门禁异常时的快速回退。

关联实现：
- `testing/eval/runner.go`
- `testing/eval/types.go`
- `docs/v1/ai-direct-execution-task-catalog.md` (`P0-EVAL-002`, `P0-DOC-001`)

## Trigger

### L1 (warning, continue in report-only)
- baseline 文件缺失。
- baseline 读取失败，但可切换 report-only。

### L2 (partial rollback)
- score drop 超过阈值（默认 `0.03`）且命中多个核心 case。
- 单日误拦截正常 PR 比例接近 20%。

### L3 (full rollback)
- gate 阻断持续扩大，核心分支无法合并。
- pass->fail 在核心 case 上连续出现，且无法在 30 分钟内定位原因。

## Owner

- L1 Owner: `testing`
- L2 Owner: `testing` + `kernel`
- L3 Owner: `release` + `testing` + `kernel/appkit`

## Steps

### 0-15 分钟
1. 记录触发 case、阈值、最近提交。
2. 切换 `GateReportOnly=true`，避免继续误拦截。
3. 重跑 `go test ./testing/eval/...` 并保存输出。

### 15-30 分钟
1. 若 baseline 缺失，初始化 baseline 文件。
2. 复核 `GateScoreDrop`，必要时从 `0.03` 临时放宽到 `0.05`。
3. 根据影响范围决定保持 report-only 或恢复阻断。

### 30 分钟后
1. 仍无法恢复时升级到 L3。
2. 冻结高风险变更合并，优先恢复主干稳定性。

## Escalation

- L1 -> L2: 15 分钟内无法确认是数据问题还是行为回归。
- L2 -> L3: 30 分钟内无法恢复稳定 gate，或出现主干阻塞。
- L3 处理要求：必须附带 incident 记录和恢复时间线。

## PowerShell Rollback Template

```powershell
Push-Location "D:\Codes\qiulin\moss"

# 1) 先跑 eval，收集当前结果
go test ./testing/eval/...

# 2) 若 baseline 不存在，先初始化（示例路径）
# go test ./testing/eval/... -run TestBaselineRoundTrip
# 或在实际执行脚本中调用 WriteBaseline("testing/eval/baseline.json", results)

# 3) 紧急回退策略：强制 report-only（通过配置注入 RunnerConfig）
# GateReportOnly=true
# GateScoreDrop=0.03 (必要时临时放宽到 0.05)

# 4) 回退后复验
go test ./testing/eval/...

Pop-Location
```

## Checklist

- [ ] 已记录触发条件（threshold/pass->fail/baseline 问题）
- [ ] 已执行 report-only 回退
- [ ] 已完成一次复验并保存证据
- [ ] 已确认是否需要升级

