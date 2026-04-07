# 分布式 Session/Task Runtime 设计

## 问题描述

现有 `MemoryTaskRuntime` 和 `FileSessionStore` 仅支持单进程部署。
当 Agent Worker 横向扩展到多实例时，需要：
- 共享任务队列（多 Worker 安全认领）
- 跨实例 Session 状态同步
- 分布式锁（防止同一任务被多 Worker 同时认领）

## 设计目标

1. 以 HTTP REST 作为最小公共分母协议（无 NATS/Kafka 依赖）
2. `RemoteTaskRuntime`：通过 HTTP 调用远端 TaskRuntime Server
3. `TaskRuntimeServer`：将任意 `TaskRuntime` 暴露为 HTTP API
4. `DistributedLock` 接口：支持 In-process（本地测试）和 HTTP token 实现
5. 零新外部依赖（仅 `net/http`, `encoding/json`）

---

## 架构

```
Agent Worker A ──┐
Agent Worker B ──┼──► TaskRuntimeServer (HTTP) ──► TaskRuntime backend
Agent Worker C ──┘                                  (MemoryTaskRuntime / SQLite / Redis)
```

---

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /tasks | UpsertTask |
| GET  | /tasks/{id} | GetTask |
| GET  | /tasks?agent=&status=&limit= | ListTasks |
| POST | /tasks/claim | ClaimNextReady |
| POST | /jobs | UpsertJob |
| GET  | /jobs/{id} | GetJob |
| GET  | /jobs?agent=&status= | ListJobs |
| POST | /jobs/{jobID}/items | UpsertJobItem |
| GET  | /jobs/{jobID}/items | ListJobItems |
| POST | /jobs/{jobID}/items/{itemID}/running | MarkJobItemRunning |
| POST | /jobs/{jobID}/items/{itemID}/result | ReportJobItemResult |

所有请求/响应使用 JSON，错误响应格式：`{"error": "message"}` + 4xx/5xx 状态码。

---

## RemoteTaskRuntime

```go
type RemoteTaskRuntime struct {
    baseURL    string      // e.g. "http://task-server:8080"
    httpClient *http.Client
    token      string      // 可选鉴权 Bearer token
}

func NewRemoteTaskRuntime(baseURL string, opts ...RemoteOption) *RemoteTaskRuntime
```

实现 `TaskRuntime`, `JobRuntime`, `AtomicJobRuntime` 接口。

---

## TaskRuntimeServer

```go
type TaskRuntimeServer struct {
    runtime     port.TaskRuntime
    jobRuntime  port.JobRuntime
    atomicRuntime port.AtomicJobRuntime
}

func NewTaskRuntimeServer(rt port.TaskRuntime, ...) *TaskRuntimeServer
func (s *TaskRuntimeServer) Handler() http.Handler   // 返回 http.ServeMux
func (s *TaskRuntimeServer) Serve(addr string) error // ListenAndServe
```

---

## DistributedLock 接口

```go
type DistributedLock interface {
    // TryLock 尝试获取 key 的锁，成功返回 true + 释放函数。
    TryLock(ctx context.Context, key string, ttl time.Duration) (unlock func(), ok bool, err error)
}

// InProcessLock 基于 sync.Map，用于单进程场景。
type InProcessLock struct { ... }

// TokenLock 基于 HTTP 令牌桶 API（无状态服务器实现）。
type TokenLock struct { ... }
```

ClaimNextReady 内部使用 DistributedLock 保证原子性。

---

## 影响范围

- `distributed/taskruntime.go` — RemoteTaskRuntime（新包）
- `distributed/server.go` — TaskRuntimeServer
- `distributed/lock.go` — DistributedLock, InProcessLock
- `distributed/taskruntime_test.go` — 测试
