# Skill 版本控制与依赖拓扑设计

## 问题描述

现有 `skill.Metadata.DependsOn []string` 只记录依赖名称，无法表达版本约束。
当多个 Skill 存在依赖关系时，注册顺序不当会导致 Init 失败，且错误信息不友好。

## 设计目标

1. 支持语义化版本（SemVer 兼容）版本声明与范围约束
2. 在注册时进行依赖拓扑排序，确保依赖先于依赖方完成 Init
3. 检测循环依赖并给出清晰错误
4. 向后兼容：保留 `DependsOn []string` 字段

---

## 核心类型扩展

```go
// SkillDep 描述对另一个 Skill 的版本约束依赖。
type SkillDep struct {
    Name       string // 被依赖的 Skill 名称
    MinVersion string // 最低可接受版本（含），空 = 不限
    MaxVersion string // 最高可接受版本（含），空 = 不限
}

// Metadata 新增 Requires 字段（DependsOn 保留用于向后兼容）
type Metadata struct {
    // ... 现有字段 ...
    Requires []SkillDep `json:"requires,omitempty" yaml:"requires,omitempty"`
}
```

---

## 版本比较规则

采用"简化三段式"比较：`MAJOR.MINOR.PATCH`

- 版本字符串格式：`vMAJOR.MINOR.PATCH` 或 `MAJOR.MINOR.PATCH`（前缀 v 可选）
- 比较：按段位逐级数值比较（非字符串比较）
- 空版本 `""` 匹配任意版本

```
ParseVersion("v1.2.3") → [1,2,3]
CompareVersion("1.0.0", "2.0.0") → -1
IsVersionInRange("1.5.0", min="1.0.0", max="2.0.0") → true
```

---

## 拓扑排序算法（Kahn's Algorithm）

1. 构建有向无环图（DAG）：`A DependsOn B` → edge B→A
2. 计算入度（in-degree）
3. 入度为 0 的节点入队列
4. 依次出队，入度递减，更新队列
5. 若处理节点数 < 总节点数 → 存在环，报错

```
RegisterAll(providers, deps):
    sorted = TopologicalSort(providers)     // 检测循环依赖
    for each provider in sorted:
        ValidateDeps(provider, registered)  // 检查依赖是否满足版本约束
        Register(provider, deps)
```

---

## 新增 API

```go
// skill/version.go
func ParseVersion(v string) ([3]int, error)
func CompareVersion(a, b string) int           // -1 < 0 = 1 >
func IsVersionInRange(v, min, max string) bool

// skill/manager.go
func (m *Manager) RegisterAll(ctx, providers []Provider, deps Deps) error
func TopologicalSort(providers []Provider) ([]Provider, error)
func (m *Manager) ValidateDeps(s Provider) error
```

---

## 错误场景

| 场景 | 错误 |
|------|------|
| 依赖未注册 | `skill "B" required by "A" is not registered` |
| 版本不满足 | `skill "B" v1.0.0 does not satisfy "A" requirement >=2.0.0` |
| 循环依赖 | `cycle detected: A → B → A` |

---

## 影响范围

- `skill/skill.go` — 新增 `SkillDep`, `Requires` 字段
- `skill/version.go` — 版本解析与比较（新文件）
- `skill/manager.go` — `RegisterAll`, `TopologicalSort`, `ValidateDeps`
- 现有 `DependsOn []string` 保持不变，`Requires` 优先用于版本约束
