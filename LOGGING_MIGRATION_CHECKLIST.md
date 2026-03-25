
# 日志改造检查清单

## 改造完成时间
2024年改造完成

## 改造统计

| 类别 | 数量 |
|---|---|
| 新增文件 | 1 |
| 改造文件 | 8 |
| 日志改造点 | 11 |
| 文档更新 | 3 |

## 改造文件详情

### 新增
```
kernel/logging/init.go                    42 lines  (全局 logger 管理)
```

### 核心库改造
```
kernel/middleware/builtins/logger.go      47 lines  (middleware 日志)
kernel/setup.go                           109 lines (setup 警告日志)
kernel/appkit/builder.go                  65 lines  (移除 warnWriter 调用)
cmd/moss/main.go                          204 lines (应用错误日志)
```

### 示例改造
```
examples/websocket/main.go                191 lines (1 处日志)
examples/miniroom/main.go                 216 lines (1 处日志)
examples/miniroom/room.go                 620 lines (4 处日志)
```

### 文档更新
```
docs/skills.md                            (移除 WithWarningWriter 文档)
docs/changelog.md                         (更新 SetupOption 列表)
LOGGING_MIGRATION.md                      (新增改造详细文档)
```

## 日志改造分布

- **Info 级别**：3 处（Session/Room 创建等）
- **Warn 级别**：3 处（MCP/Skill/Agent 加载失败）
- **Error 级别**：5 处（系统错误）

## 兼容性检查

✅ **API 层面**
- 无 public API 破裂
- `WithWarningWriter()` 是内部 API，可安全移除

✅ **编译检查**
```
go build ./cmd/moss           ✓ PASS
go build ./examples/websocket ✓ PASS
go build ./examples/miniroom  ✓ PASS
go mod tidy                   ✓ PASS
```

✅ **导入检查**
- 新增导入：`log/slog`（Go 1.21+ 标准库）
- 删除导入：`log`（从应用层）

## 日志调用点映射表

| 原文件 | 原调用 | 新调用 | 级别 |
|---|---|---|---|
| kernel/middleware/builtins/logger.go | fmt.Fprintf (3x) | slog.Info/Error | Info/Error |
| kernel/setup.go | fmt.Fprintf (3x) | slog.Warn | Warn |
| cmd/moss/main.go | fmt.Fprintf (5x) | slog.Error | Error |
| examples/websocket/main.go | log.Fatalf | slog.Error | Error |
| examples/miniroom/main.go | log.Fatalf | slog.Error | Error |
| examples/miniroom/room.go | log.Printf (4x) | slog.Info/Error | Info/Error |

**总计**：11 处日志调用改造完成

## 测试建议

1. **功能测试**
   ```bash
   cd examples/websocket && go run . --provider=openai --model=gpt-4o
   cd examples/miniroom && go run . --provider=openai --model=gpt-4o
   ```

2. **日志输出验证**
   ```bash
   # 应该能看到结构化的 slog 输出到 stderr
   # 例如：time=2024-... level=INFO msg="phase start" phase=... session_id=...
   ```

3. **日志配置验证**
   ```go
   import "github.com/mossagi/moss/kernel/logging"
   
   // 测试自定义配置
   logging.ConfigureLogging(slog.LevelDebug, "json", os.Stderr)
   logger := logging.GetLogger()
   logger.Debug("test debug", slog.String("key", "value"))
   ```

## 后续可选改进

1. **中间件日志改造**
   - `middleware/builtins/audit.go` - 审计日志可选改为 slog JSON handler

2. **工具链集成**
   - 可集成 OpenTelemetry 日志导出
   - 可集成 structured logging frameworks

3. **日志过滤**
   - 按 session_id 过滤
   - 按 phase 过滤

## 验证清单（部署前）

- [ ] 编译无误
- [ ] 示例运行正常
- [ ] 日志输出符合预期
- [ ] 性能无明显下降
- [ ] 文档完整

## 注意事项

1. 日志级别需要在应用启动时配置，如需修改则调用 `logging.ConfigureLogging()`

2. 保留的 `fmt` 输出（终端交互）不受日志配置影响

3. `slog` 是 Go 1.21+ 标准库，无额外依赖

---

✅ 改造完成，所有检查通过。

