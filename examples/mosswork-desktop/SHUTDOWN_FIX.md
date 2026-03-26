# 应用关闭进程问题修复

## 问题描述
关闭 mosswork-desktop 桌面应用后，终端中的进程不会立即关闭，导致僵尸进程存在。

## 根本原因
1. **FileService 缺少生命周期管理** - `ServiceShutdown` 方法未实现
2. **ChatService 的 Shutdown 没有超时** - 使用 `context.Background()` 无法强制关闭无响应的操作
3. **后台 goroutine 未被等待** - `SendMessage` 启动的 goroutine 在应用关闭时可能继续运行
4. **缺少系统信号处理** - 应用未监听 SIGINT/SIGTERM，无法响应终端关闭

## 实施的改进

### 1. FileService - 添加 ServiceShutdown 方法
**文件**: `fileservice.go`
```go
func (s *FileService) ServiceShutdown() error {
    // 清理文件服务资源
    return nil
}
```
**作用**: 确保 Wails 框架能够正确管理服务生命周期。

### 2. ChatService - 改进 ServiceShutdown 实现
**文件**: `chatservice.go`

**主要改进**:
- 添加 `time` 包以支持超时
- 实现**优雅关闭流程**:
  1. 取消任何正在运行的 agent
  2. 等待后台 goroutine 结束（最多 5 秒）
  3. 使用带超时的 context 关闭 kernel
  4. 完整的日志记录

**代码**:
```go
func (s *ChatService) ServiceShutdown() error {
    slog.Info("ChatService shutting down...")
    s.mu.Lock()
    
    // 停止任何正在运行的 agent
    if s.cancel != nil {
        slog.Info("Cancelling running agent...")
        s.cancel()
    }
    s.mu.Unlock()
    
    // 给后台任务一些时间来响应取消（最多 5 秒）
    shutdown := make(chan struct{})
    go func() {
        ticker := time.NewTicker(100 * time.Millisecond)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                s.mu.Lock()
                if !s.running {
                    s.mu.Unlock()
                    shutdown <- struct{}{}
                    return
                }
                s.mu.Unlock()
            }
        }
    }()
    
    select {
    case <-shutdown:
        slog.Info("Agent stopped gracefully")
    case <-time.After(5 * time.Second):
        slog.Warn("Agent shutdown timeout, forcing shutdown")
    }
    
    // 关闭 kernel
    if s.k != nil {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        
        slog.Info("Shutting down kernel...")
        if err := s.k.Shutdown(shutdownCtx); err != nil && err != context.DeadlineExceeded {
            slog.Error("Error shutting down kernel", slog.Any("error", err))
        }
    }
    
    slog.Info("ChatService shutdown complete")
    return nil
}
```

### 3. main.go - 添加系统信号处理
**文件**: `main.go`

**改进**:
- 导入 `os/signal` 和 `syscall` 包
- 在后台 goroutine 中监听 SIGINT (Ctrl+C) 和 SIGTERM
- 接收信号时调用 `app.Quit()` 触发优雅关闭

**代码**:
```go
// 监听系统信号，实现优雅关闭
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

go func() {
    sig := <-sigChan
    slog.Info("Received signal, triggering graceful shutdown", slog.String("signal", sig.String()))
    if app != nil {
        app.Quit()
    }
}()

if err := app.Run(); err != nil {
    log.Fatal(err)
}

slog.Info("Application shutdown complete")
```

## 工作流程

### 关闭应用时的顺序:
1. **用户关闭窗口或按 Ctrl+C**
2. **信号处理器** → 调用 `app.Quit()`
3. **Wails 框架** → 调用所有服务的 `ServiceShutdown()`
   - FileService 完成清理（如果有）
   - ChatService 执行:
     - 取消运行中的 agent
     - 等待后台任务（超时 5 秒）
     - 使用超时 context 关闭 kernel
4. **应用** → 正常退出

## 验证

编译命令:
```bash
cd examples/mosswork-desktop
go build -o ./bin/mosswork-desktop.exe .
```

## 预期效果

- ✅ 关闭应用后进程立即结束
- ✅ 后台任务正确清理
- ✅ Kernel 资源被释放
- ✅ 完整的日志记录关闭过程
- ✅ 不再出现僵尸进程

## 备注

- 所有关闭操作都有超时保护，防止无限挂起
- 日志级别为 `slog.Info` 和 `slog.Warn`，便于调试
- 改动兼容 Windows、macOS 和 Linux
