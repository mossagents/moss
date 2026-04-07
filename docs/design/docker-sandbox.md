# Docker Sandbox 设计

> 状态：**草稿** · 优先级：P1 · 关联待办：P1-D3 / P1-I3

---

## 1. 问题陈述

现有 `sandbox.Sandbox` 实现基于 git stash（L0），仅提供文件系统级别快照，无法：
- 隔离恶意代码执行
- 限制 CPU/内存/网络资源
- 保护宿主机不受代码副作用影响

---

## 2. 设计目标

| 目标 | 说明 |
|------|------|
| 接口兼容 | 实现现有 `sandbox.Sandbox` 接口，上层代码零修改 |
| Docker 后端 | 使用 `docker run` 执行命令，提供 OS 级隔离 |
| 资源限制 | 支持 CPU/内存/超时限制 |
| 目录挂载 | 将工作目录挂载到容器，文件读写对外透明 |
| 降级友好 | Docker 不可用时自动降级到本地执行并记录警告 |

---

## 3. 核心类型

```go
// sandbox/docker/sandbox.go
type DockerConfig struct {
    Image          string        // 执行镜像，默认 "debian:bookworm-slim"
    WorkDir        string        // 宿主机工作目录（挂载点）
    Memory         string        // 内存限制，如 "512m"
    CPUs           float64       // CPU 配额，如 0.5
    Network        string        // 网络模式："none"|"bridge"（默认 none）
    Timeout        time.Duration // 单次命令超时
    ExtraVolumes   []string      // 额外挂载 "hostPath:containerPath"
    DockerBin      string        // docker 可执行文件路径（默认 "docker"）
}

type DockerSandbox struct { ... }

func New(cfg DockerConfig) *DockerSandbox
```

---

## 4. 命令执行流程

```
Execute(req) 
  → docker run \
      --rm \
      -v {WorkDir}:/workspace \
      -w /workspace \
      --memory={Memory} \
      --cpus={CPUs} \
      --network={Network} \
      {Image} \
      sh -c "{req.Command}"
```

---

## 5. 文件结构

```
sandbox/docker/
├── sandbox.go       # DockerSandbox 实现
└── sandbox_test.go  # 单元测试（mock exec）
```

---

*文档状态：草稿 · 待评审*
