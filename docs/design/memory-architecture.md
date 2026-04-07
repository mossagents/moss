# Memory 三层架构设计

> 状态：**草稿** · 优先级：P0 · 关联待办：P0-D1 / P0-I1 / P0-I2 / P0-I3

---

## 1. 问题陈述

当前 `knowledge/memory.go` 仅提供：
- 单一 in-memory 余弦相似度检索
- 无持久化（进程重启全部丢失）
- 无记忆层次（工作记忆 vs 知识库混为一谈）
- 无 RAG 自动注入（应用层需手动调用）
- 无遗忘/合并策略（只增不减）

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 三层分离 | Working / Episodic / Semantic 各自独立存储和检索策略 |
| 持久化 | `port.VectorStore` 接口，支持 pgvector / Qdrant / in-memory |
| RAG 内建 | AgentLoop 每轮自动检索相关记忆注入 context |
| 可插拔 | 各层可独立替换后端，零侵入 kernel 核心 |
| 遗忘预留 | 接口预留 TTL / 重要性衰减入口（P2 实现）|

---

## 3. 三层记忆模型

```
┌─────────────────────────────────────────────────────────────┐
│                     MemoryManager                            │
│  统一入口：Inject(ctx, session) → []ContextChunk            │
├───────────────┬─────────────────┬───────────────────────────┤
│ Working Memory│ Episodic Memory │    Semantic Memory         │
│ (当前激活状态) │ (历史事件序列)   │  (持久知识库)              │
│               │                 │                            │
│ - 对话摘要     │ - 带时间戳事件   │ - 文档 / 代码片段          │
│ - 临时变量     │ - 任务执行记录   │ - 用户偏好                 │
│ - 当前 goal   │ - 错误与修正     │ - 领域知识                 │
│               │                 │                            │
│ 存储：Session  │ 存储：FileStore  │ 存储：VectorStore          │
│ State KV      │ or DB           │ (pgvector/Qdrant)          │
└───────────────┴─────────────────┴───────────────────────────┘
```

### 3.1 Working Memory

**职责**：管理当前 session 的激活状态，生命周期与 session 绑定。

**存储**：扩展 `session.Session.State map[string]any`，无需独立存储。

**接口**：
```go
type WorkingMemory interface {
    Set(ctx context.Context, key string, value any) error
    Get(ctx context.Context, key string) (any, bool)
    Summary(ctx context.Context) string          // 生成当前状态摘要
    Clear(ctx context.Context) error
}
```

**注入策略**：将 Summary() 结果附加到每轮 system message 末尾（低 token 开销）。

---

### 3.2 Episodic Memory

**职责**：按时间顺序记录 agent 经历的事件（tool calls、decisions、errors）。

**存储**：`port.EpisodicStore`，实现：FileStore（JSONL 追加写）+ DB（可选）。

**数据模型**：
```go
type Episode struct {
    ID          string         `json:"id"`
    SessionID   string         `json:"session_id"`
    Timestamp   time.Time      `json:"timestamp"`
    Kind        EpisodeKind    `json:"kind"`   // tool_call / decision / error / checkpoint
    Summary     string         `json:"summary"`
    Importance  float64        `json:"importance"` // 0.0-1.0，用于遗忘衰减
    Metadata    map[string]any `json:"metadata,omitempty"`
}

type EpisodeKind string
const (
    EpisodeToolCall  EpisodeKind = "tool_call"
    EpisodeDecision  EpisodeKind = "decision"
    EpisodeError     EpisodeKind = "error"
    EpisodeUserMsg   EpisodeKind = "user_message"
)
```

**接口**：
```go
type EpisodicStore interface {
    Append(ctx context.Context, ep Episode) error
    Recent(ctx context.Context, sessionID string, limit int) ([]Episode, error)
    Search(ctx context.Context, query string, filter EpisodeFilter) ([]Episode, error)
    SetImportance(ctx context.Context, id string, score float64) error
}
```

**注入策略**：检索最近 N 条 + 语义相关 K 条，格式化为 `<episodic_context>` XML block 注入。

---

### 3.3 Semantic Memory

**职责**：持久知识库，存储文档、代码、领域知识，通过向量检索。

**存储**：`port.VectorStore`，实现：in-memory（测试/开发）、pgvector、Qdrant。

**接口**：
```go
// port/vector_store.go
type VectorStore interface {
    Upsert(ctx context.Context, docs []VectorDoc) error
    Search(ctx context.Context, query VectorQuery) ([]VectorResult, error)
    Delete(ctx context.Context, ids []string) error
    Count(ctx context.Context, namespace string) (int, error)
}

type VectorDoc struct {
    ID        string         `json:"id"`
    Namespace string         `json:"namespace"`
    Text      string         `json:"text"`
    Embedding []float64      `json:"embedding,omitempty"` // nil = 自动生成
    Metadata  map[string]any `json:"metadata,omitempty"`
    TTL       time.Duration  `json:"ttl,omitempty"`      // 0 = 永久
    Score     float64        `json:"score,omitempty"`    // 重要性评分
}

type VectorQuery struct {
    Text      string         `json:"text"`
    Namespace string         `json:"namespace,omitempty"`
    Limit     int            `json:"limit"`
    Threshold float64        `json:"threshold,omitempty"` // 最低相似度
    Filter    map[string]any `json:"filter,omitempty"`
}

type VectorResult struct {
    Doc       VectorDoc `json:"doc"`
    Score     float64   `json:"score"`   // 余弦相似度
}
```

---

## 4. RAG Pipeline 设计

### 4.1 触发时机

在 `AgentLoop.Run()` 每轮 LLM 调用前，通过 `RAGMiddleware` 注入。

```
每轮循环：
  1. 读取当前 messages 最后一条 user message
  2. 并行检索：WorkingMemory.Summary() + EpisodicStore.Search() + VectorStore.Search()
  3. 合并结果，按相关性排序，截断到 MaxRAGTokens
  4. 格式化注入 system message（追加 <memory_context> block）
  5. 调用 LLM
```

### 4.2 RAGMiddleware 接口

```go
// kernel/middleware/builtins/rag.go
type RAGConfig struct {
    Manager      *knowledge.MemoryManager
    MaxTokens    int     // 注入的最大 token 数，默认 2000
    EpisodicN    int     // 最近事件数，默认 10
    SemanticK    int     // 语义检索结果数，默认 5
    Threshold    float64 // 相似度阈值，默认 0.7
}

func RAG(cfg RAGConfig) middleware.Middleware
```

### 4.3 注入格式

```xml
<memory_context>
<working_memory>
当前目标: 修复认证模块的 JWT 过期 bug
当前状态: 已定位到 auth/jwt.go:142
</working_memory>
<recent_events>
- [10:23] 调用 read_file(auth/jwt.go) → 读取成功
- [10:24] 发现 token 过期时间硬编码为 1h
- [10:25] 调用 write_file 失败 → 权限不足
</recent_events>
<relevant_knowledge>
[相关度: 0.92] JWT 最佳实践：过期时间应从配置读取...
[相关度: 0.85] auth 模块架构说明：token 生成在 GenerateToken()...
</relevant_knowledge>
</memory_context>
```

---

## 5. MemoryManager 聚合接口

```go
// knowledge/manager.go
type MemoryManager struct {
    Working  WorkingMemory
    Episodic EpisodicStore
    Semantic VectorStore
    Embedder port.Embedder
}

// Inject 为当前 session 生成注入 block
func (m *MemoryManager) Inject(ctx context.Context, sess *session.Session, query string) (string, error)

// Record 记录一条事件到 Episodic Memory
func (m *MemoryManager) Record(ctx context.Context, ep Episode) error

// Learn 将文档加入 Semantic Memory
func (m *MemoryManager) Learn(ctx context.Context, docs []VectorDoc) error
```

---

## 6. 文件结构规划

```
knowledge/
├── manager.go          # MemoryManager 聚合
├── working.go          # WorkingMemory 实现（基于 session.State）
├── episodic.go         # EpisodicStore 接口
├── episodic_file.go    # FileStore 实现（JSONL）
├── episodic_test.go
├── store.go            # VectorStore in-memory 实现（保留）
├── chunker.go          # 文本分块（保留）
├── memory.go           # 旧接口兼容层（deprecated，逐步迁移）
└── adapters/
    ├── pgvector.go     # PostgreSQL pgvector 适配器
    ├── qdrant.go       # Qdrant 适配器
    └── memory.go       # in-memory VectorStore（重命名自 store.go）

kernel/port/
└── vector_store.go     # VectorStore + EpisodicStore 接口

kernel/middleware/builtins/
└── rag.go              # RAGMiddleware
```

---

## 7. 实现顺序

1. **P0-I1**：定义 `port.VectorStore`，实现 in-memory 适配器（重构现有 MemoryStore）
2. **P0-I1**：实现 pgvector 适配器（带集成测试，需 Docker）
3. **P0-I2**：实现 WorkingMemory + EpisodicStore（FileStore 版本）
4. **P0-I2**：实现 `MemoryManager` 聚合层
5. **P0-I3**：实现 `RAGMiddleware`，集成到 `kernel/middleware/builtins`
6. **P2-I3**（后续）：TTL 索引 + 重要性衰减 + Consolidator

---

## 8. 测试策略

- Unit：各层接口的 mock 测试
- Integration：pgvector 需要 `testcontainers-go` 启动 Postgres
- E2E：`testing/eval/` 中添加记忆相关 eval case（依赖 P0-D2）

---

## 9. 兼容性说明

- `knowledge.MemoryStore` 保留为 deprecated，重定向到新 `adapters/memory.go`
- `port.Embedder` 接口不变
- 现有 `mossclaw` 示例使用 Knowledge 的代码需迁移（提供迁移示例）

---

*文档状态：草稿 · 待评审*
