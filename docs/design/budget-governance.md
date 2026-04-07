# 跨 Agent 全局 Budget 治理设计

> 状态：**草稿** · 优先级：P1 · 关联待办：P1-D2 / P1-I2

---

## 1. 问题陈述

当前 `session.Budget` 仅跟踪单个 Session 的 token/step 消耗。在多 Agent 场景中：
- 主 Agent 派生多个 SubAgent，各自独立计算预算
- 全局 token 消耗无法收口，容易超出账户限额
- 无法按任务/项目分配预算上限并统一监控

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 全局汇总 | 聚合所有子 Session 的 token/step 消耗 |
| 层级治理 | 支持 Task → Agent → SubAgent 多层预算嵌套 |
| 硬限制 | 超出全局预算时拒绝新的 LLM 调用 |
| 软警告 | 接近阈值时触发 Observer 事件 |
| 零侵入 | 现有 Session/Kernel 代码无需修改（通过 middleware 集成）|

---

## 3. 核心类型

```go
// kernel/budget/governor.go
type GlobalBudget struct {
    MaxTokens  int64   // 0 = 无限制
    MaxSteps   int64   // 0 = 无限制
    WarnAt     float64 // 触发警告的比例，如 0.8 = 80%
}

type BudgetSnapshot struct {
    UsedTokens  int64
    UsedSteps   int64
    MaxTokens   int64
    MaxSteps    int64
    SessionCount int
}

type Governor interface {
    // Record 记录一次消耗（线程安全）
    Record(sessionID string, tokens, steps int)
    // Check 检查是否还有预算（true = 可以继续）
    Check() bool
    // Snapshot 返回当前使用状态快照
    Snapshot() BudgetSnapshot
    // Reset 重置计数器（用于测试或新任务）
    Reset()
}
```

---

## 4. Middleware 集成

```go
// 在 BeforeLLM 阶段检查全局预算
func BudgetGuard(gov budget.Governor) middleware.Middleware

// 在 AfterLLM 阶段记录实际消耗
func BudgetRecorder(gov budget.Governor) middleware.Middleware
```

---

## 5. 文件结构

```
kernel/budget/
├── governor.go       # Governor 接口 + 内存实现
├── middleware.go     # BudgetGuard + BudgetRecorder middleware
└── governor_test.go
```

---

*文档状态：草稿 · 待评审*
