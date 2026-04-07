# Agent-to-Agent (A2A) 通信协议设计

> 状态：**草稿** · 优先级：P1 · 关联待办：P1-D4 / P1-I4

---

## 1. 问题陈述

现有 `port.Mailbox` 使用非结构化 `Content string` 传递消息，导致：
- 消息解析依赖双方约定的字符串格式，易出错
- 无法在运行时区分消息类型（任务委派 vs 结果回传 vs 状态更新）
- 缺乏标准的请求/响应关联机制
- 无法携带结构化 payload（如工具调用结果、错误码）

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 类型安全 | 消息类型在编译期可区分 |
| 向后兼容 | 在现有 `MailMessage` 上扩展，不破坏现有代码 |
| 请求/响应关联 | 通过 `CorrelationID` 关联请求和响应 |
| 结构化 Payload | 支持 JSON 编码的类型化载荷 |
| 可扩展 | 新消息类型无需修改核心接口 |

---

## 3. 消息类型体系

```go
// kernel/port/a2a.go
type A2AMessageKind string

const (
    A2AKindTaskDelegate  A2AMessageKind = "task.delegate"   // 主→子：委派任务
    A2AKindTaskResult    A2AMessageKind = "task.result"     // 子→主：任务结果
    A2AKindStatusUpdate  A2AMessageKind = "status.update"   // 子→主：进度更新
    A2AKindQuery         A2AMessageKind = "query"           // 任意方向：查询
    A2AKindQueryResponse A2AMessageKind = "query.response"  // 查询响应
    A2AKindError         A2AMessageKind = "error"           // 错误通知
    A2AKindHeartbeat     A2AMessageKind = "heartbeat"       // 保活
)

// A2AEnvelope 是类型化 A2A 消息的标准信封。
// 通过 MailMessage.Metadata["a2a"] 携带，保持向后兼容。
type A2AEnvelope struct {
    Kind          A2AMessageKind `json:"kind"`
    CorrelationID string         `json:"correlation_id,omitempty"`
    ReplyTo       string         `json:"reply_to,omitempty"` // 响应发送地址
    Payload       json.RawMessage `json:"payload,omitempty"`
    Error         *A2AError      `json:"error,omitempty"`
}

type A2AError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

// 常见 Payload 类型
type TaskDelegatePayload struct {
    Goal        string         `json:"goal"`
    SystemPrompt string        `json:"system_prompt,omitempty"`
    MaxSteps    int            `json:"max_steps,omitempty"`
    MaxTokens   int            `json:"max_tokens,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
}

type TaskResultPayload struct {
    Success  bool           `json:"success"`
    Summary  string         `json:"summary"`
    Artifacts []string      `json:"artifacts,omitempty"`
    Steps    int            `json:"steps"`
    Tokens   int            `json:"tokens"`
    Metadata map[string]any `json:"metadata,omitempty"`
}

type StatusUpdatePayload struct {
    Status   string  `json:"status"`   // "thinking"|"executing"|"waiting"
    Progress float64 `json:"progress"` // 0.0-1.0
    Message  string  `json:"message,omitempty"`
    Step     int     `json:"step,omitempty"`
}
```

---

## 4. 辅助函数

```go
// NewA2AMessage 构造携带 A2AEnvelope 的 MailMessage
func NewA2AMessage(from, to string, env A2AEnvelope) MailMessage

// ExtractA2AEnvelope 从 MailMessage 中提取 A2AEnvelope
func ExtractA2AEnvelope(msg MailMessage) (*A2AEnvelope, bool)

// IsA2AMessage 判断是否为 A2A 协议消息
func IsA2AMessage(msg MailMessage) bool
```

---

## 5. 文件结构

```
kernel/port/
└── a2a.go          # A2A 类型 + 辅助函数 + 测试
```

---

*文档状态：草稿 · 待评审*
