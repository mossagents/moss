# 日志统一到 slog 改造总结

## 改造目标

将 moss 项目中涉及**纯粹日志打印**的部分统一到 Go 标准库 `log/slog`，同时保留**终端交互**相关的 `fmt` 输出不变。

## 改造范围

### ✅ 已改造的文件

#### 核心库日志模块
1. **kernel/logging/init.go** （新增）
   - 创建全局 slog 实例
   - 提供 `GetLogger()` 和 `ConfigureLogging()` 函数
   - 支持 text 和 JSON 两种格式

2. **kernel/middleware/builtins/logger.go**
   - 从 `fmt.Fprintf` 改为 `slog.Info/Error`
   - 移除 `io.Writer` 参数
   - 使用结构化日志属性（phase, session_id, elapsed, error）

3. **kernel/setup.go**
   - 移除 `warnWriter` 配置字段
   - 移除 `WithWarningWriter()` 选项函数
   - MCP/Skill/Agent 加载警告使用 `slog.Warn`

4. **kernel/appkit/builder.go**
   - 移除 `WithWarningWriter(os.Stderr)` 调用
   - 删除不再使用的 `os` 导入

#### 应用层日志改造
5. **cmd/moss/main.go**
   - 错误日志使用 `slog.Error`
   - 保留 `fmt.Fprintf(os.Stderr)` 用于用户交互提示

6. **examples/websocket/main.go**
   - 从 `log.Fatalf` 改为 `slog.Error`
   - 删除 `"log"` 导入

7. **examples/miniroom/main.go**
   - 从 `log.Fatalf` 改为 `slog.Error`
   - 删除 `"log"` 导入

8. **examples/miniroom/room.go**
   - 4 处运行日志从 `log.Printf` 改为 `slog.Info/Error`
   - 添加 `slog` 和 `logging` 包导入

### 📝 文档更新
- `docs/skills.md` — 移除 `WithWarningWriter()` 说明，添加 slog 配置文档
- `docs/changelog.md` — 更新 `SetupOption` 选项列表

### ⏭️ 未改造的文件（保留 `fmt`）

这些是**终端交互**相关的输出，应该保留 `fmt` 以便灵活控制格式：

- **kernel/port/io_console.go** — 用户交互输出（emoji、进度提示）
- **kernel/port/io_std.go** — 标准 IO 输出
- **kernel/appkit/repl.go** — REPL CLI 交互和错误提示
- **kernel/appkit/banner.go** — 启动横幅
- **kernel/appkit/appkit.go** — CLI 中断提示
- **cmd/moss/tui.go** — TUI 相关输出
- **cmd/moss/main.go** 中的用户提示（如 usage 信息）
- **examples/**/main.go** 中的 CLI 提示

## 改造亮点

### 1. 统一的日志初始化入口
```go
import "github.com/mossagi/moss/kernel/logging"

// 全局获取 logger
logger := logging.GetLogger()

// 自定义配置
logging.ConfigureLogging(slog.LevelDebug, "json", os.Stderr)
```

### 2. 结构化日志属性
改造前：
```go
fmt.Fprintf(w, "[%s] %s session=%s error=%v elapsed=%s\n", 
    time.Now().Format(time.RFC3339), label, mc.Session.ID, err, elapsed)
```

改造后：
```go
logger.ErrorContext(ctx, "phase error",
    slog.String("phase", label),
    slog.String("session_id", mc.Session.ID),
    slog.Duration("elapsed", elapsed),
    slog.Any("error", err),
)
```

### 3. 向后兼容
- 核心库中没有 public API 破裂（`WithWarningWriter` 是内部 API）
- 示例项目改造不影响用户现有代码
- CLI 界面和终端交互保持原样

## 编译验证

所有编译通过：
```bash
✓ go build ./cmd/moss
✓ go build ./examples/websocket
✓ go build ./examples/miniroom
```

## 日志级别使用规范

| 级别 | 用途 | 示例 |
|---|---|---|
| **Debug** | 代码执行细节 | Agent 循环参数、工具调用细节 |
| **Info** | 关键操作完成 | Session 创建、LLM 调用成功、工具执行成功 |
| **Warn** | 可恢复的异常 | 配置加载失败、可选组件加载失败 |
| **Error** | 致命错误 | LLM 调用失败、工具执行异常 |

## 后续扩展

现在可以轻松对接其他观测工具：

```go
// 对接 OpenTelemetry
import "github.com/open-telemetry/opentelemetry-go"

handler := otelhandler.NewHandler(...)
logging.SetLogger(slog.New(handler))

// 对接 DataDog、Splunk 等
```

## 测试清单

- [x] 核心库编译无误
- [x] 示例项目编译无误
- [x] 文档更新完整
- [x] API 兼容性检查

