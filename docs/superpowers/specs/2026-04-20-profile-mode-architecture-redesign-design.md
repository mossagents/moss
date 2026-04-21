# Profile/Mode 架构重构设计：正交层模型与 Preset 装配

日期：2026-04-20  
状态：Draft / Ready for review

## 1. 背景

当前 moss 里的 `profile`、`mode`、`task_mode`、`trust`、`approval`、`tool policy` 在概念上没有正交拆分。

典型表现包括：

- `profile` 同时承担协作风格、权限姿态、执行预算和工具策略预设；
- `SessionConfig.Mode` 与 `task_mode` 同时存在，但职责边界不清；
- prompt 注入、session 持久化、runtime rebuild 各自读取不同字段，缺少单一真相；
- `profile` 名称在多个路径中被直接回填成 `task_mode`，导致概念耦合变成真实行为耦合。

对照 codex，collaboration mode、config profile、permission profile 至少在模型层已经分离。moss 若希望成为跨产品、跨场景复用的统一 agent runtime，仅靠修补现有 `profile` 抽象已经不够，需要一次**不考虑向后兼容**的概念重构。

## 2. 本次目标

本设计目标是建立一套**正交、强类型、可组合**的 agent 运行模型。

本次希望达成：

1. 删除 `profile` 作为核心域概念；
2. 明确区分运行形态、协作模式、权限基线、模型选择、工作区信任和领域提示包；
3. 让 prompt、session、runtime、CLI、配置文件都消费同一套概念模型；
4. 停止使用字符串 metadata 作为核心语义载体；
5. 为产品层保留高层便捷入口，但该入口只能是组合层，而不能再成为基础概念。

## 3. 非目标

本设计明确不做：

- 保留旧配置字段与旧命令名；
- 提供自动迁移、兼容 fallback 或 alias；
- 在第一版保留 `task_mode`、`profile` 的并存表示；
- 为每个现有产品先做局部适配再统一；
- 在未完成概念收敛前继续扩充新的 profile 类型。

## 4. 设计原则

### 4.1 `mode` 只表示模式

`mode` 只能用于真正的模式概念，不得再表示权限级别、产品角色或复合预设。

本设计中只保留两类 mode：

- `run_mode`
- `collaboration_mode`

### 4.2 核心语义必须有单一真相

session 摘要、prompt 组装、runtime posture、resume/rebuild 必须读取同一份 typed spec，不能依赖不同 metadata key 的拼装结果。

### 4.3 高层便利必须建立在底层正交之上

产品层允许存在一键配置，但该能力只能通过 `preset` 引用底层组件实现，而不能把多个概念重新揉成新的基础抽象。

### 4.4 工作区信任与 session posture 分离

`workspace_trust` 决定是否加载项目级资产；`permission_profile` 决定当前 session 拥有什么权限。两者相关，但不是同一概念。

额外约束：

- `workspace_trust` 只允许来自 CLI、API 请求或用户级全局信任映射；
- `workspace_trust` 不允许由 workspace 内部配置文件声明；
- trust 判定完成前，workspace 内任何文件都不得作为主解析链路的配置事实源。

### 4.5 提示词角色与协作协议分离

`prompt_pack` 描述领域角色与产品指令；`collaboration_mode` 描述如何与用户协作。二者不再互相冒充。

### 4.6 权限基线与模式约束分离

`permission_profile` 是权限基线；`collaboration_mode` 是行为约束。

规则：

- `permission_profile` 决定理论上的最大能力边界；
- `collaboration_mode` 决定当前协作协议允许采用的行为上限；
- `collaboration_mode` 可以收紧能力，但不能授予额外权限；
- 最终有效能力始终是多层约束的交集，而不是任一层单独决定。

## 5. 核心领域模型

### 5.1 WorkspaceTrust

`workspace_trust` 仅表示 workspace 资产是否可被视为可信输入。

建议内建值：

- `trusted`
- `restricted`

职责：

- 决定是否加载项目级 config、bootstrap、skills、MCP、workspace prompt assets；
- 影响可见配置边界；
- 不直接表示 session 是否只读、是否需要审批。

### 5.2 RunMode

`run_mode` 仅表示运行形态。

建议内建值：

- `interactive`
- `oneshot`
- `batch`
- `background`

职责：

- 驱动输入输出行为；
- 决定 session 生命周期与 UI/transport 交互方式；
- 不参与 prompt 协作语义和权限判定。

额外约束：

- `run_mode` 不允许由 `preset` 决定；
- 每个入口命令或 API 必须在进入解析器前显式提供 `run_mode`；
- CLI 子命令只是 `run_mode` 的语法糖映射，例如 `chat -> interactive`、`exec -> oneshot`、`batch -> batch`。

### 5.3 CollaborationMode

`collaboration_mode` 仅表示 agent 与用户的协作协议。

建议内建值：

- `execute`
- `plan`
- `investigate`

语义：

- `execute`：默认直接实现与交付；
- `plan`：禁止变更，以决策完备规划为目标；
- `investigate`：强调阅读、证据、溯源与问题分析。

职责：

- 影响 system/developer prompt contract；
- 影响提问策略、可用工具约束、输出风格；
- 不直接携带审批、沙箱或文件系统权限。

### 5.4 PermissionProfile

`permission_profile` 是唯一的权限基线对象。

建议内建值：

- `read-only`
- `workspace-write`
- `full-access`

职责：

- 表达 approval policy；
- 表达 command/http/filesystem/network policy；
- 表达是否允许 mutation、外网、异步任务创建、shell escalation 等；
- 作为 runtime rebuild 的核心比较对象。

v1 schema 最小要求：

- `approval_policy`
- `command`
- `http`
- `workspace_write`
- `memory_write`
- `graph_mutation`
- `protected_path_prefixes`
- `approval_required_classes`
- `denied_classes`
- `allow_async_tasks`

桥接规则：

- `permission_profile` 必须编译为 `CompiledPolicy`，它是 v1 runtime policy 的标准形态；
- `CompiledPolicy` 在职责上是当前 `ToolPolicy` 的继任者，至少必须覆盖 command/http/workspace_write/memory_write/graph_mutation/approval-class 这些维度；
- capability gating 只负责 coarse-grained 暴露与调度，最终执行授权仍由 `CompiledPolicy + ApprovalClass + session grants` 共同决定；
- 未显式声明 required capabilities 的工具，必须通过 `ToolSpec.Effects / SideEffectClass / Capabilities` 做确定性推导；
- 若某工具既没有显式 required capabilities，又无法从现有元数据可靠推导，则默认 fail-closed，不得放行。

### 5.5 SessionPolicy

`session_policy` 表示运行预算与执行限制。

建议字段：

- `max_steps`
- `max_tokens`
- `timeout`
- `auto_compact_threshold`
- `async_task_limit`

职责：

- 约束 session 资源使用；
- 不表达权限；
- 不表达协作模式。

### 5.6 ModelProfile

`model_profile` 表示模型与推理相关选择。

建议字段：

- `provider`
- `model`
- `reasoning_effort`
- `verbosity`
- `router_lane`
- `context_window`

职责：

- 表达模型选择与参数；
- 允许被产品或预设复用；
- 不表达协作协议与权限。

### 5.7 PromptPack

`prompt_pack` 表示领域角色与产品级指令包。

建议内建值：

- `coding`
- `desktop`
- `research`
- `writing`

职责：

- 提供基础 system instructions；
- 提供产品静态工作流提示、风格与领域角色；
- 不直接表达 mode、权限、预算。

边界约束：

- `prompt_pack` 不持有 workspace bootstrap、project skills、workspace prompt fragments；
- workspace 资产始终由独立的 trusted augmentation loader 注入；
- 同一项 augmentation 不得同时从 `prompt_pack` 和 workspace loader 双重注入。

### 5.8 Preset

`preset` 是唯一允许组合多个底层组件的高层便捷入口。

关键约束：

- `preset` 不是核心语义，不可替代底层 typed fields；
- `preset` 只做引用组合，不定义新语义；
- `preset` 必须显式排除 `run_mode` 与 `workspace_trust`；
- session 持久化时必须展开为具体组件，而不是只保存 preset 名。

## 6. 目标会话模型

### 6.1 SessionSpec

新的 session 配置不再依赖 `Profile + MetadataTaskMode + EffectiveTrust` 这样的隐式拼装。

建议统一为：

```go
type SessionSpec struct {
	Goal string `json:"goal"`

	Workspace struct {
		Trust string `json:"trust"`
	} `json:"workspace"`

	Intent struct {
		CollaborationMode string `json:"collaboration_mode"`
		PromptPack        string `json:"prompt_pack"`
	} `json:"intent"`

	Runtime struct {
		RunMode           string `json:"run_mode"`
		PermissionProfile string `json:"permission_profile"`
		SessionPolicy     string `json:"session_policy"`
		ModelProfile      string `json:"model_profile"`
	} `json:"runtime"`

	Origin struct {
		Preset string `json:"preset,omitempty"`
	} `json:"origin,omitempty"`
}
```

`SessionSpec` 表示请求级意图，允许保留名称引用，用于对齐 CLI、API 和配置输入。

provenance 规则：

- `SessionSpec` 只保存原始请求里显式提供的字段与引用；
- 入口默认值与 Phase A 推导结果不得回写进 `SessionSpec`；
- `workspace.trust` 与 `runtime.run_mode` 在 `SessionSpec` 中都表示“显式请求覆盖”，允许为空；
- refresh / re-resolve 必须从原始 `SessionSpec` 重新求值，而不是从旧的 resolved 默认值反推。

### 6.2 ResolvedSessionSpec

所有运行时子系统只消费 `ResolvedSessionSpec`，不直接消费 `SessionSpec`。

建议结构：

```go
type ResolvedSessionSpec struct {
  Goal string `json:"goal"`

  Workspace struct {
    Trust string `json:"trust"`
  } `json:"workspace"`

  Intent struct {
    CollaborationMode string             `json:"collaboration_mode"`
    PromptPack        string             `json:"prompt_pack"`
    CapabilityCeiling CapabilityEnvelope `json:"capability_ceiling"`
  } `json:"intent"`

  Runtime struct {
    RunMode           string            `json:"run_mode"`
    PermissionProfile string            `json:"permission_profile"`
    PermissionPolicy  CompiledPolicy    `json:"permission_policy"`
    SessionPolicy     ResolvedBudget    `json:"session_policy"`
    ModelProfile      string            `json:"model_profile"`
    ModelConfig       model.ModelConfig `json:"model_config"`
  } `json:"runtime"`

  Prompt struct {
    BasePackID             string   `json:"base_pack_id"`
    TrustedAugmentationIDs []string `json:"trusted_augmentation_ids,omitempty"`
    TrustedAugmentationDigests []string `json:"trusted_augmentation_digests,omitempty"`
    RenderedPromptVersion string   `json:"rendered_prompt_version"`
    SnapshotRef           string   `json:"snapshot_ref"`
  } `json:"prompt"`

  Origin struct {
    Preset string `json:"preset,omitempty"`
  } `json:"origin,omitempty"`
}
```

为闭合 trusted resume 语义，系统必须把 prompt snapshot 作为一等持久化对象：

```go
type PromptSnapshot struct {
  Ref            string                `json:"ref"`
  Layers         []ResolvedPromptLayer `json:"layers"`
  RenderedPrompt string                `json:"rendered_prompt"`
  Version        string                `json:"version"`
}
```

规则：

- `ResolvedSessionSpec.Prompt.SnapshotRef` 必须指向一个不可变 `PromptSnapshot`；
- `PromptSnapshot` 必须包含恢复 session 所需的完整渲染结果或等价的不可变层快照；
- 在 trusted resume 路径中，系统通过 `SnapshotRef` 恢复 prompt 语义，不重新读取 workspace 内容；
- 只有显式 `refresh` 才会重新读取 workspace augmentations，并生成新的 `PromptSnapshot`。

规则：

- session 持久化必须同时保存 `SessionSpec` 与 `ResolvedSessionSpec`；
- resume 默认恢复持久化的 `ResolvedSessionSpec`，避免因配置变化产生语义漂移；
- 只有显式 `refresh` / `re-resolve` 动作才允许基于当前配置重新解析；
- runtime rebuild、session summary、prompt composer、tool policy evaluation 全部以 `ResolvedSessionSpec` 为单一事实源。

resume 额外规则：

- resume 前必须重新校验当前 `workspace_trust`；
- 若当前 trust 已从 `trusted` 降为 `restricted`，则不得直接恢复旧的 trusted prompt snapshot，默认拒绝 resume；
- 若当前 trust 仍为 `trusted`，resume 默认复用持久化的 augmentation snapshot，不重新读取 workspace 内容；
- 只有显式 `refresh` 才允许重新加载当前 workspace augmentations，并生成新的 `ResolvedSessionSpec.Prompt` 快照。

### 6.3 SessionSummary

线程摘要只暴露真正重要且不重复的字段：

- `preset`
- `run_mode`
- `collaboration_mode`
- `permission_profile`
- `workspace_trust`
- `model_profile`

不再同时持久化与展示：

- `profile`
- `task_mode`
- `effective_trust`
- `effective_approval`

这些若仍需要可观察性，应由 resolved runtime snapshot 提供，而不是与 session identity 混在一起。

## 7. 配置模型

### 7.1 顶层结构

建议新配置结构如下：

```yaml
workspace_trust: trusted

default_preset: code

prompt_packs:
  coding:
    source: builtin:coding

collaboration_modes:
  execute:
    builtin: execute
  plan:
    builtin: plan
  investigate:
    builtin: investigate

permission_profiles:
  read-only:
    approval_policy: confirm
    filesystem: read-only
    network: limited
  workspace-write:
    approval_policy: confirm
    filesystem: workspace-write
    network: limited

session_policies:
  deep-work:
    max_steps: 200
    max_tokens: 120000

model_profiles:
  code-default:
    provider: openai
    model: gpt-5.4
    reasoning_effort: medium

presets:
  code:
    prompt_pack: coding
    collaboration_mode: execute
    permission_profile: workspace-write
    session_policy: deep-work
    model_profile: code-default
```

说明：

- `workspace_trust` 只允许存在于全局配置或显式请求；
- workspace 内配置文件只能在 `workspace_trust=trusted` 判定成立后参与第二阶段装配；
- workspace 配置只能注册 augmentations，并提供 workspace-local 默认选择；
- workspace 配置在 v1 不允许重定义 `prompt_pack`、`collaboration_mode`、`permission_profile`、`session_policy`、`model_profile`、`preset` 的同名 ID；
- workspace 配置不能回写 trust 决策本身。

### 7.2 配置约束

强制约束：

- `preset` 只能引用已定义组件；
- `collaboration_mode` 与 `prompt_pack` 都必须显式存在，不允许靠默认 profile 名推导；
- `permission_profile` 不可从 `workspace_trust` 或 mode 隐式派生；
- `workspace_trust` 仅可由显式配置或 CLI 指定；
- 每个 session 在启动前必须解析为完整 `ResolvedSessionSpec`。

### 7.3 两阶段加载模型

为避免 trust 与 workspace 配置形成循环依赖，解析器必须采用两阶段加载：

1. **Phase A: pre-trust resolution**
  - 输入源仅允许：CLI、API 请求、用户级全局配置、系统默认值；
  - 产物：`workspace_trust`、`run_mode` 与入口级默认值；
  - 此阶段禁止读取 workspace 内任何配置文件或 prompt 资产。

2. **Phase B: trusted workspace expansion**
  - 仅当 Phase A 得出 `workspace_trust=trusted` 时，才允许读取 workspace config、bootstrap、skills 与 workspace prompt fragments；
  - 这些输入只参与 augmentation 与组件覆盖，不得重新参与 trust 决策。

解析器在 v1 必须采用以下固定顺序：

1. 载入显式请求字段；
2. 若存在显式 `preset`，先展开其组件引用；
3. 对仍为空的字段应用 trusted workspace default selectors；
4. 对仍为空的字段应用 global defaults；
5. 若仍不完整则报错。

## 8. CLI 与 API 设计

### 8.1 CLI

推荐 CLI 改为显式正交参数：

```text
moss chat --preset code
moss chat --mode plan
moss chat --mode investigate --permissions read-only
moss exec --run oneshot --preset code --goal "fix tests"
moss chat --prompt-pack coding --mode execute --permissions workspace-write
```

规则：

- `--mode` 指 `collaboration_mode`；
- `--run` 指 `run_mode`；
- `--permissions` 指 `permission_profile`；
- `--preset` 只是组合快捷方式；
- 不再提供 `--profile`。

优先级规则：

- 显式 CLI / API 字段优先级最高；
- 其次是显式 `preset` 展开的组件值；
- 再其次是 trusted workspace default selectors；
- 再其次是全局默认配置；
- 仍未补齐的必需字段直接报错；
- `run_mode` 不参与 preset 展开，必须由入口或显式参数提供。

补充约束：

- trusted workspace default selectors 只在对应字段仍为空时参与补值；
- 一旦显式 `preset` 或显式字段已给出，同一字段的 workspace 默认值不得覆盖；
- `default_preset` 只是默认选择器，不是组件定义覆盖机制。

### 8.2 API

对外 API 使用同一套结构化字段：

```json
{
  "goal": "fix failing test",
  "workspace": {
    "trust": "trusted"
  },
  "intent": {
    "collaboration_mode": "execute",
    "prompt_pack": "coding"
  },
  "runtime": {
    "run_mode": "interactive",
    "permission_profile": "workspace-write",
    "session_policy": "deep-work",
    "model_profile": "code-default"
  },
  "origin": {
    "preset": "code"
  }
}
```

## 9. 解析与运行流

### 9.1 解析阶段

启动 session 前统一执行：

1. 读取 global config，并解析 CLI / API 输入；
2. 执行 pre-trust resolution，产出显式 `run_mode` 与 `workspace_trust`；
3. 若 trust 允许，再读取 workspace config 与 augmentation assets；
4. 若指定 `preset`，展开为组件引用；
5. 合成 `ResolvedSessionSpec`；
6. 校验 mode、permission、model、trust 的一致性；
7. 将 `SessionSpec + ResolvedSessionSpec` 写入 session 与 runtime。

### 9.2 运行阶段

各子系统消费规则如下：

- prompt composer：读取 `ResolvedSessionSpec.Prompt + ResolvedSessionSpec.Intent`；
- kernel/runtime：读取 `ResolvedSessionSpec.Runtime.PermissionPolicy + ResolvedSessionSpec.Runtime.ModelConfig + ResolvedSessionSpec.Runtime.SessionPolicy`；
- workspace asset loader：读取 `workspace_trust`；
- UI/session summary：读取 `ResolvedSessionSpec`；
- rebuild planner：主要比较 `permission_profile`、`model_profile`、`workspace_trust`。

能力组合规则：

- `permission_profile` 生成权限基线；
- `collaboration_mode` 生成 capability ceiling；
- 最终 effective capability = baseline policy ∩ capability ceiling ∩ run-mode restrictions；
- `plan` 模式必须强制得到非变更有效能力，即便 `permission_profile=workspace-write` 也只产生只读 effective capability；
- `execute` 与 `investigate` 不得隐式提升 baseline policy。

### 9.4 Capability 契约

为避免各子系统自行解释 `plan` / `investigate` / `read-only`，系统必须引入统一 typed capability vocabulary。

第一版至少包含：

- `read_workspace`
- `mutate_workspace`
- `execute_commands`
- `access_network`
- `create_async_tasks`
- `load_trusted_workspace_assets`

规则：

- `permission_profile` 编译为 baseline capability set；
- `collaboration_mode` 编译为 capability ceiling；
- runtime features、tools、hosted abilities 必须显式声明 required capabilities；
- orchestration layer 只基于 required capabilities 与 effective capability 做 gating，不允许工具层各自硬编码 `plan` 特判；
- `plan` 至少必须移除 `mutate_workspace`；
- `investigate` 默认保留 `execute_commands` 与 `access_network`，但不得超出 baseline capability set。

### 9.3 切换语义

不同配置切换具有不同成本：

- 切 `collaboration_mode`：刷新 prompt contract，不重建 kernel；
- 切 `prompt_pack`：刷新 prompt，必要时重建相关 extension bindings；
- 切 `permission_profile`：重建 runtime；
- 切 `model_profile`：重建 runtime；
- 切 `workspace_trust`：重新加载资产，必要时全量重建；
- 切 `session_policy`：更新 budget/runtime limits，无需改变 prompt 语义。

额外说明：

- `run_mode=background` 只表示当前 session 以 detached 方式运行；
- agent 是否允许创建异步子任务由 `permission_profile` 决定；
- 并发异步子任务数量由 `session_policy.async_task_limit` 限制；
- “session 在后台运行”和“session 内创建后台任务”是两个独立概念，不得混用。

## 10. Prompt 设计

统一 prompt 组装顺序应为：

1. `prompt_pack` 基础指令；
2. `collaboration_mode` 协作协议层；
3. trusted workspace augmentations 注入；
4. runtime notices；
5. session-local instructions。

关键约束：

- prompt 中不再出现 “Active profile”；
- prompt 中不再注入 `task_mode`；
- prompt 只显示 `collaboration_mode`；
- 若需要展示权限信息，应以 runtime notices 或 permissions summary 表达，而不是复用 mode 概念。
- trusted workspace augmentations 的来源与顺序必须由单一 augmentation graph 决定，不允许 `prompt_pack` 与 workspace loader 双重决定优先级。

## 11. 实现边界重排

建议按以下包职责重排：

- `harness/runtime/sessionspec`
  - 定义 `SessionSpec` / `ResolvedSessionSpec`
  - 负责解析与校验

- `harness/runtime/collaboration`
  - 定义 collaboration modes 与其 prompt contract

- `harness/runtime/permissions`
  - 定义 permission profiles 与 runtime policy compilation

- `harness/runtime/promptpacks`
  - 定义 prompt pack registry 与加载策略

- `harness/runtime/presets`
  - 仅负责组合引用

- `kernel/session`
  - 仅持久化 typed session spec 与 session runtime state

旧的 `runtime/profile` 应整体删除，而不是继续保留为兼容层。

## 12. 错误处理与校验

必须在 session 创建前做 fail-fast 校验：

- 未知 `run_mode`：启动失败；
- 未知 `workspace_trust`：启动失败；
- 未知 `prompt_pack`：启动失败；
- 未知 `collaboration_mode`：启动失败；
- 未知 `permission_profile`：启动失败；
- 未知 `session_policy`：启动失败；
- 未知 `model_profile`：启动失败；
- `preset` 引用缺失组件：启动失败；
- `workspace_trust=restricted` 但请求加载项目资产：显式拒绝；
- `permission_profile=read-only` 与显式 mutation-only runtime feature 冲突：启动失败。
- `run_mode=background` 但入口不支持 detached session：启动失败；
- `collaboration_mode=plan` 与显式要求 mutation-only tool contract 冲突：启动失败；
- `ResolvedSessionSpec` 缺少任一必需 resolved field：启动失败。

不得再使用“字符串为空时自动回退到 profile 名”这类弱约束兜底。

## 13. 测试与验收

### 13.1 核心测试

- `preset` 能稳定展开为 `ResolvedSessionSpec`；
- `collaboration_mode` 切换只影响 prompt，不影响 permission baseline；
- `permission_profile` 切换会触发 runtime rebuild；
- `workspace_trust` 影响项目资产加载，但不隐式修改 permission profile；
- session summary 与 persisted session spec 保持一致；
- prompt composer 仅消费 `collaboration_mode`，不再消费 `task_mode/profile`。

### 13.2 负向测试

- 缺少组件引用时报错；
- 不合法 mode 名称时报错；
- 只指定 `preset` 且 preset 不完整时报错；
- restricted workspace 下尝试加载项目级 prompt pack/skills 被拒绝。

### 13.3 验收标准

当以下条件全部成立时，视为设计完成：

1. 核心代码中不再存在 `profile` 作为运行主概念；
2. 核心代码中不再存在 `task_mode`；
3. `ResolvedSessionSpec` 成为 prompt、runtime、summary 的唯一事实源；
4. CLI、配置、API 全部使用正交字段表达；
5. `preset` 退化为纯组合层，不再承担底层语义。

## 14. 备选方案与结论

### 14.1 备选方案 A：保留 profile，但重命名为 preset

优点：

- 改动面小；
- 用户易迁移。

缺点：

- 仍未解决概念耦合；
- 只是把问题从 `profile` 改名为 `preset`。

### 14.2 备选方案 B：完全去掉 preset，只保留底层组件

优点：

- 模型最纯；
- 不会再出现大而全抽象。

缺点：

- 产品层与 CLI 使用成本过高；
- 用户无法方便复用常见工作姿态。

### 14.3 结论

推荐采用“**正交分层 + preset 组合**”方案。

该方案相对 codex 更进一步：

- codex 已把 collaboration mode 与 permission/config 层部分拆开；
- moss 在此基础上进一步显式拆分 `workspace_trust`、`prompt_pack`、`session_policy`；
- 最终使 prompt、runtime、session、配置、CLI 全部围绕同一套 typed model 收敛。

这是在不考虑兼容性前提下，最适合 moss 长期演进的目标架构。

## 14. 用户界面与交互（UI/UX）

### 14.1 协作模式（Collaboration Mode）在 UI 的统一展示

- 所有终端用户界面（TUI、CLI、Session Summary、Prompt）只展示“协作模式（mode）”，不再出现“profile”或“task_mode”字样。
- 推荐三种协作模式：
  - **执行（Agent/execute）**：直接实现与交付，适合代码生成、自动修复等场景。
  - **规划（Plan/plan）**：以决策和完备规划为目标，禁止直接变更，适合需求分析、方案设计等场景。
  - **调研（Ask/investigate）**：强调阅读、证据、溯源与问题分析，适合代码理解、文档检索、因果分析等场景。
- UI 示例：

  | 模式         | 中文标签 | 英文标签   | 典型用途           |
  |--------------|----------|------------|--------------------|
  | execute      | 执行     | Agent      | 代码生成、修复     |
  | plan         | 规划     | Plan       | 需求分析、设计     |
  | investigate  | 调研     | Ask        | 阅读、分析、检索   |

- 切换模式时，TUI/CLI 应有明确提示，如“当前协作模式：规划（Plan）”。
- Session summary 只展示 preset、run_mode、collaboration_mode、permission_profile、workspace_trust、model_profile 等新字段。
- Prompt 只注入 collaboration_mode，不再注入 profile/task_mode。

### 14.2 负向交互与错误提示

- 对未知 mode、preset、permission_profile 等 typed selectors，所有入口（CLI/TUI/API）应 fail-fast 并给出明确报错，如“未知协作模式：foo，请选择 execute/plan/investigate”。
- 旧参数（如 `--profile`）不再保留兼容入口，直接作为非法参数拒绝。

### 14.3 典型 UI 展示片段

- TUI 顶栏/状态栏示例：

  ```
  [工作区: trusted] [协作模式: 规划（Plan）] [权限: workspace-write] [模型: gpt-4o]
  ```

- CLI 启动参数示例：

  ```
  moss chat --mode plan --permissions read-only --prompt-pack coding
  moss exec --run oneshot --mode execute --goal "run all tests"
  ```

- Session summary 示例：

  ```
  任务目标: 修复所有测试
  协作模式: 执行（Agent）
  权限: workspace-write
  运行模式: oneshot
  预设: code
  ...
  ```

- Prompt 片段示例：

  ```
  # System
  当前协作模式：规划（Plan）
  ...
  ```

---

> 所有 UI/Prompt/配置/文档均应以“协作模式（mode）”为唯一入口，彻底移除 profile/task_mode 等遗留概念。