# 异步 HITL + 审批超时设计

> 状态：**草稿** · 优先级：P1 · 关联待办：P1-D5 / P1-I5

---

## 1. 问题陈述

现有审批流程为同步阻塞式：
- Agent 在等待用户审批期间完全阻塞
- 无超时机制：用户长时间不响应会永久挂起 Agent
- 审批记录无持久化：重启后历史审批决策丢失
- 无异步渠道：无法通过 webhook/消息队列接收审批决策

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 超时降级 | 等待超时后自动采用配置的默认决策（拒绝/批准）|
| 审批记录 | 持久化所有审批请求和决策，支持审计 |
| 异步渠道 | 通过 channel 接收外部审批决策 |
| 向后兼容 | 现有同步 `port.UserIO` 审批流程不受影响 |

---

## 3. 核心类型

```go
// kernel/port/approval_store.go
type ApprovalRecord struct {
    Request   ApprovalRequest   `json:"request"`
    Decision  *ApprovalDecision `json:"decision,omitempty"`
    Status    ApprovalStatus    `json:"status"`
    CreatedAt time.Time         `json:"created_at"`
    ResolvedAt *time.Time       `json:"resolved_at,omitempty"`
}

type ApprovalStatus string
const (
    ApprovalStatusPending  ApprovalStatus = "pending"
    ApprovalStatusApproved ApprovalStatus = "approved"
    ApprovalStatusDenied   ApprovalStatus = "denied"
    ApprovalStatusTimedOut ApprovalStatus = "timed_out"
    ApprovalStatusCancelled ApprovalStatus = "cancelled"
)

type ApprovalStore interface {
    Save(ctx context.Context, record ApprovalRecord) error
    Get(ctx context.Context, requestID string) (*ApprovalRecord, error)
    ListPending(ctx context.Context, sessionID string) ([]ApprovalRecord, error)
    Resolve(ctx context.Context, requestID string, decision ApprovalDecision) error
}
```

---

## 4. 超时 UserIO 包装器

```go
// kernel/port/timed_approval.go
type TimedApprovalConfig struct {
    Timeout        time.Duration     // 等待用户审批超时，默认 30s
    DefaultDecision bool             // 超时后默认决策（false = 拒绝）
    Store          ApprovalStore     // 记录存储（可选）
    Inner          UserIO            // 被包装的 UserIO
}

// NewTimedApproval 包装现有 UserIO，添加超时和记录能力
func NewTimedApproval(cfg TimedApprovalConfig) UserIO
```

---

## 5. 文件结构

```
kernel/port/
├── approval_store.go    # ApprovalStore 接口 + 内存实现 + 文件实现
└── timed_approval.go    # TimedApproval UserIO 包装器
```

---

*文档状态：草稿 · 待评审*
