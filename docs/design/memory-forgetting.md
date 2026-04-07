# Memory 遗忘与合并策略设计

## 问题描述

现有 `MemoryEpisodicStore` 无限增长，所有历史事件永久保留。
随着 Agent 运行时间增长：
- `Recent()` 返回大量低重要性事件，占用 context window
- 内存/文件占用持续增加
- 过期信息干扰 LLM 判断

## 设计目标

1. 支持 TTL 过期：`Episode.ExpiresAt` 字段标记自动过期时间
2. 支持重要性衰减：随时间推移，`Importance` 自然降低
3. 提供 `Prune()` 接口：可按规则清理低价值 episode
4. 不破坏现有接口（`EpisodicStore` 接口不变）
5. 文件存储支持紧凑化（compaction）

---

## Episode 扩展

```go
type Episode struct {
    // ... 现有字段 ...
    ExpiresAt time.Time `json:"expires_at,omitempty"` // 零值=永不过期
}
```

`ExpiresAt` 零值表示永不过期，向后兼容现有存储数据。

---

## 遗忘策略

### 1. TTL 过期

Episode 的 `ExpiresAt` 时间到达后，从 `Recent()` 和 `Search()` 中自动过滤。

```
EpisodeFilter.ExcludeExpired = true  // 在查询时跳过已过期 episode
```

默认行为：`Recent()` 不自动过滤（需显式设置），确保向后兼容。

### 2. 重要性衰减（Exponential Decay）

```
I(t) = I₀ × 0.5^(elapsed / half_life)
```

- `I₀`：原始重要性（0.0-1.0）
- `elapsed`：自 `Timestamp` 到 `now` 的时间差
- `half_life`：半衰期（默认 24h），每过一个半衰期重要性减半

衰减仅在 `DecayImportance()` 显式调用时执行（非实时），避免性能影响。

### 3. 主动裁剪（Prune）

```go
type PruneConfig struct {
    Now          time.Time     // 参考时间，零值=time.Now()
    MaxAge       time.Duration // 超过此时限的 episode 删除（0=不限）
    MinImportance float64      // 低于此重要性的 episode 删除（0=不限）
    MaxCount     int           // 保留最近 N 条，超出的删除（0=不限）
}

// Prunable 扩展 EpisodicStore，提供裁剪能力
type Prunable interface {
    EpisodicStore
    Prune(ctx context.Context, cfg PruneConfig) (int, error) // 返回删除数量
    DecayImportance(ctx context.Context, halfLife time.Duration) error
}
```

---

## 内存实现

`MemoryEpisodicStore` 实现 `Prunable`：
- `Prune`: 过滤内存切片，O(n)
- `DecayImportance`: 遍历更新 Importance 值

## 文件实现

`FileEpisodicStore` 实现 `Prunable`：
- `Prune`: 加载全量 → 过滤 → 写新文件 → 原子替换
- `DecayImportance`: 加载全量 → 更新 → 写回

---

## 与 MemoryManager 集成

```go
// MemoryManager 新增自动管理 API
type MemoryManagerConfig struct {
    // ...
    PruneConfig *PruneConfig  // 定期裁剪配置
    DecayHalfLife time.Duration // 重要性半衰期
}

// RunMaintenance 执行一轮维护：decay + prune
func (m *MemoryManager) RunMaintenance(ctx context.Context) error
```

---

## 影响范围

- `knowledge/episodic.go` — 新增 `ExpiresAt`、`PruneConfig`、`Prunable` 接口
- `knowledge/decay.go` — 衰减计算函数（新文件）
- `knowledge/manager.go` — `RunMaintenance` + `PruneConfig`
- `EpisodeFilter` — 新增 `ExcludeExpired bool` 字段
