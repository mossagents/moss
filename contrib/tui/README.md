# contrib/tui — Mosscode 终端 UI 包

本包实现了 Mosscode 的终端交互界面（TUI），基于 [Bubbletea](https://github.com/charmbracelet/bubbletea) v1.3。

---

## 架构：内联底部抽屉

### 核心原则

Mosscode TUI 以**内联模式**（Inline Mode）运行，不占用整个终端屏幕（无 AltScreen），只在终端底部渲染一个紧凑的**底部抽屉**。所有已完成的输出通过 `tea.Println` 写入终端滚动缓冲区，使其可以被终端本身向上滚动查看。

```
┌─────────────────────────────────────────┐
│  流式输出预览（最后几行）  [可滚动]    │  ← 由终端管理
│  ... 已完成的消息 ...                  │
│─────────────────────────────────────────│  ← 顶部分隔线
│ ❯ 输入内容...                          │  ← 底部抽屉（View 渲染区域）
│─────────────────────────────────────────│  ← 底部分隔线
│  斜杠命令 / @提及 / 技能弹窗            │
│─────────────────────────────────────────│
│  状态栏                                 │
└─────────────────────────────────────────┘
```

程序入口使用 `tea.NewProgram(m)`（无 `tea.WithAltScreen()`），保持内联渲染。

---

## 组件说明

### 消息打印队列

完成的消息不保存在 Viewport 中，而是通过以下机制打印到终端：

- 消息加入 `pendingPrints []string` 队列
- `drainPrints()` 在每次 `Update` 后由 `appModel.updateChat()` 调用，返回 `tea.Println` 命令
- 终端负责显示和滚动历史消息

### 输入框（Composer）

- **无边框**：使用 `inputBorderStyle = lipgloss.NewStyle().PaddingLeft(2)`，`GetHorizontalFrameSize() == 2`
- **❯ 指示符**：第一行左侧显示 `❯`（带颜色），后续行用等宽空格对齐
- **水平分隔线**：上下各一条 `────` 线，通过 `renderComposerInput()` 渲染
- **宽度**：`mainWidth - 2`（减去 `❯ ` 前缀宽度）

```go
func (m chatModel) renderComposerInput(mainWidth int) string {
    ruleStr := strings.Repeat("─", mainWidth)
    rule := indStyle.Render(ruleStr)
    // 上分隔线
    // ❯ 内容行（后续行用空格对齐）
    // 下分隔线
}
```

### 弹窗（Popups）

所有弹窗在输入框**下方**内联渲染，而非浮动覆盖层。渲染顺序见 `renderEditorPane`：

| 弹窗类型 | 触发条件 | 字段 |
|---|---|---|
| 斜杠命令 | 输入 `/` 时 | `slashPopup` |
| @ 文件提及 | 输入 `@` 时 | `mentionPopup` |
| 技能管理 | 输入 `/skills` 时 | `skillsPopup` |

- 最多显示 10 条斜杠命令候选
- 选中项使用 `Reverse(true)` 样式（亮/暗终端均有良好对比）
- 技能弹窗支持空格键切换启用/禁用

---

## 用户输入请求（Ask Form）

当 Agent 调用 `ask_user` 或 `io.Ask()` 时，TUI 展示交互式输入表单。根据请求类型采用不同的渲染策略：

### InputSelect（选项选择）— 内联设计

完全内联渲染，不使用覆盖层，直接替换编辑器区域：

```
□  请选择一个选项              ← 标题徽章（无边框）

  1. 选项 A                   ← 普通选项
> 2. 选项 B                   ← 当前选中（> 前缀 + 高亮）
     说明文字（如有）           ← 灰色说明（从选项文本的 \n 分隔）

────────────────────────────

  3. Chat about this          ← 逃脱选项（取消并回到聊天）

Enter to select  ·  ↑/↓ to navigate  ·  Esc to cancel
```

- 按 `↑/↓` 导航选项（包括「Chat about this」）
- 按 `Enter` 确认选项
- 选择「Chat about this」：取消当前 Agent 运行，恢复输入框
- 按 `Esc`：取消并中断 Agent 运行

选项文本支持通过 `\n` 分隔主标题和说明文字：
```
"选项标题\n详细说明..."
```

### InputConfirm（确认 / 审批）— 底部面板

使用覆盖层渲染，宽度接近终端宽度，位于底部：

- 简单确认（`isSimpleConfirmAskActive`）：Yes/No 选项
- 审批请求（带 `Approval`）：工具信息 + 授权范围选项（Allow once / Session / Project / Deny）

### InputForm（多字段表单）— 居中覆盖层

使用传统覆盖层（`overlayAsk`），居中渲染，`Tab`/`Shift+Tab` 在字段间切换。

---

## 覆盖层系统

覆盖层在底部抽屉区域内渲染（取代编辑器区域），通过 `overlayStack` 管理。

| 覆盖层 ID | 内容 |
|---|---|
| `overlayTranscript` | 全屏转录（独占整个抽屉高度）|
| `overlayAsk` | 确认 / 审批表单（全宽，底部）|
| `overlayModel` | 模型选择器 |
| `overlayTheme` | 主题选择器 |
| `overlayHelp` | 命令帮助列表 |
| `overlayResume` | 会话恢复选择器 |
| `overlayFork` | 会话 Fork 选择器 |
| `overlayAgent` | 子 Agent 管理 |
| `overlayMCP` | MCP 服务器管理 |
| `overlaySchedule` | 定时任务浏览器 |

> **InputSelect 不使用覆盖层**，直接内联渲染到编辑器区域。

---

## 布局系统

```go
type chatUILayout struct {
    Width        int  // 终端宽度
    Height       int  // 终端高度
    MainWidth    int  // 内容区宽度（可配置侧边栏后的剩余宽度）
    BodyHeight   int  // Height - 1（状态栏）
    EditorHeight int  // 编辑器区域高度（有覆盖层时为 0）
}
```

`View()` 渲染顺序：
1. 流式预览（`renderStreamingPreview`，streaming 时显示）
2. 编辑器区域（`renderEditorPane`）
3. 状态栏（`renderStatusPane`）

---

## 主题系统

三个内置主题，通过环境变量或 `/theme` 命令切换：

| 主题名 | 说明 |
|---|---|
| `default` | 彩色输出，适配大多数终端 |
| `dark` | 深色终端优化配色 |
| `plain` | 无色纯文本，适合受限终端 |

样式变量在 `styles.go` 中声明为包级变量，由 `theme.go` 中的 `applyTheme()` 初始化。

---

## 扩展系统

见 [harness/extensions](../../harness/extensions/) 了解扩展 API，可添加：
- 状态栏 widget（`HeaderMetaWidgets`）
- 自定义键绑定（`KeyBindings`）
- 工具回调（`skillItemsFn`、`skillToggleFn`）
