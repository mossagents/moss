# Prompt 管理系统设计

> 状态：**草稿** · 优先级：P1 · 关联待办：P1-D1 / P1-I1

---

## 1. 问题陈述

当前 Moss 中 prompt 管理存在以下问题：
- system prompt 硬编码在各 preset 和示例中（字符串拼接）
- 无版本管理：prompt 变更没有 diff 可见性，无法回滚
- 无模板化：重复的 prompt 片段（如安全约束、格式要求）在多处复制粘贴
- 无 A/B 测试支持：无法对同一功能的不同 prompt 进行对比评测
- 国际化缺失：prompt 语言切换需要代码改动
- prompt 与代码耦合：调整 prompt 需要重新编译

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 模板化 | Go template 语法，支持变量插值和条件逻辑 |
| 版本化 | 每个 prompt 有显式版本，变更可追踪 |
| 可组合 | 支持 prompt 片段（partial）嵌套和继承 |
| 运行时加载 | 支持从文件系统/嵌入资源/数据库加载 |
| A/B 测试就绪 | 接口预留实验组标识（实现在 P2）|
| 向后兼容 | 现有字符串 prompt 不需要立即迁移 |

---

## 3. 核心类型

```go
// kernel/prompt/types.go
type PromptTemplate struct {
    ID          string            `yaml:"id"`
    Version     string            `yaml:"version"`      // semver 或 git hash
    Description string            `yaml:"description,omitempty"`
    Tags        []string          `yaml:"tags,omitempty"`

    // 模板内容（Go template 语法）
    Template    string            `yaml:"template"`

    // 默认变量值
    Defaults    map[string]any    `yaml:"defaults,omitempty"`

    // 引用的 partial（片段）
    Partials    []string          `yaml:"partials,omitempty"`

    // 模型适配（不同模型可能需要不同格式）
    ModelHints  map[string]string `yaml:"model_hints,omitempty"`
}

type PromptRender struct {
    Content  string            // 渲染后的内容
    Tokens   int               // 估算 token 数（0 = 未计算）
    Metadata map[string]any    // 渲染元数据（用于调试/追踪）
}
```

---

## 4. Registry 接口

```go
// kernel/prompt/registry.go
type Registry interface {
    // Get 按 ID 获取模板（返回最新版本）
    Get(id string) (*PromptTemplate, error)
    // GetVersion 获取指定版本
    GetVersion(id, version string) (*PromptTemplate, error)
    // Render 渲染模板
    Render(id string, vars map[string]any) (PromptRender, error)
    // Register 注册模板（用于编程式注册）
    Register(t PromptTemplate) error
    // List 列出所有 ID（可按 tag 过滤）
    List(tags ...string) []string
}
```

---

## 5. 加载器

### 5.1 文件系统加载器

```go
// kernel/prompt/loader_fs.go
type FSLoader struct {
    // Root 是 prompt 文件根目录
    Root fs.FS
    // WatchChanges 是否监听文件变更（开发模式热重载）
    WatchChanges bool
}
```

**目录约定**：
```
prompts/
├── system/
│   ├── base.yaml           # 基础 system prompt
│   ├── coding.yaml         # 代码场景扩展
│   └── planning.yaml       # 规划场景扩展
├── tools/
│   ├── file_ops.yaml       # 文件操作工具描述
│   └── code_exec.yaml      # 代码执行工具描述
└── partials/
    ├── safety.yaml         # 安全约束片段
    ├── format_json.yaml    # JSON 格式要求
    └── chinese.yaml        # 中文回复指令
```

### 5.2 嵌入资源加载器

```go
// 支持通过 go:embed 将 prompt 文件打包进二进制
//go:embed prompts
var defaultPrompts embed.FS

func DefaultRegistry() Registry {
    loader := &FSLoader{Root: defaultPrompts}
    return loader.Build()
}
```

---

## 6. 模板语法示例

### 6.1 基础 system prompt

```yaml
# prompts/system/base.yaml
id: system.base
version: "1.2.0"
description: 基础 system prompt，适用于所有 agent
partials: [safety, format_output]

template: |
  你是 Moss，一个强大的 AI 编程助手。

  ## 能力
  你可以：
  {{- if .HasFileAccess }}
  - 读写文件系统
  {{- end }}
  {{- if .HasCodeExec }}
  - 执行代码和命令
  {{- end }}
  {{- if .HasWebSearch }}
  - 搜索互联网
  {{- end }}

  ## 工作目录
  {{if .WorkDir}}当前工作目录：`{{.WorkDir}}`{{else}}无工作目录限制{{end}}

  {{template "partial.safety" .}}
  {{template "partial.format_output" .}}

defaults:
  HasFileAccess: true
  HasCodeExec: false
  HasWebSearch: false
```

### 6.2 Partial 片段

```yaml
# prompts/partials/safety.yaml
id: partial.safety
version: "1.0.0"
description: 安全约束（所有 agent 共用）

template: |
  ## 安全约束
  - 不执行破坏性操作前必须确认
  - 不访问 .env、密钥文件等敏感文件
  - 不发送数据到外部服务（除非明确授权）
```

---

## 7. 与 kernel 集成

### 7.1 Kernel 扩展

```go
// kernel/kernel.go
type Kernel struct {
    // ... 现有字段 ...
    Prompts prompt.Registry  // 新增
}
```

### 7.2 AgentLoop 使用

```go
// 在 AgentLoop 初始化时渲染 system prompt
rendered, err := k.Prompts.Render("system.base", map[string]any{
    "HasFileAccess": true,
    "HasCodeExec":   loopCfg.AllowCodeExec,
    "WorkDir":       workspace.Root(),
})
systemMsg := session.Message{Role: "system", Content: rendered.Content}
```

---

## 8. 文件结构规划

```
kernel/prompt/
├── types.go         # PromptTemplate, PromptRender
├── registry.go      # Registry 接口
├── registry_impl.go # 内存 Registry 实现
├── loader_fs.go     # 文件系统加载器
├── loader_embed.go  # go:embed 加载器
├── renderer.go      # Go template 渲染逻辑
└── registry_test.go

prompts/             # 内置 prompt 文件（go:embed）
├── system/
├── tools/
└── partials/
```

---

## 9. 实现顺序

1. 定义 `PromptTemplate` 类型和 `Registry` 接口
2. 实现内存 Registry（支持编程式注册）
3. 实现 `FSLoader`（文件系统加载）
4. 将现有 preset prompt 迁移为 YAML 文件
5. 实现 `go:embed` 加载器（生产部署）
6. 集成到 `Kernel` 和 `AgentLoop`

---

*文档状态：草稿 · 待评审*
