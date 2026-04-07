# 远程 Workspace / VFS 设计

## 问题描述

现有 `port.Workspace` 实现（`sandbox.LocalSandbox`）绑定本地文件系统，
无法用于云端/容器化 Agent 工作区，也不支持跨节点共享工作区。

## 设计目标

1. 实现基于 S3 兼容对象存储的 `port.Workspace`
2. 无额外 SDK 依赖（使用 `net/http` + AWS Signature V4）
3. 同时支持 AWS S3、GCS（HMAC 兼容模式）、MinIO、Cloudflare R2
4. 提供 `BlobClient` 抽象，便于 mock 测试

---

## 架构

```
port.Workspace
    └── ObjectStoreWorkspace
            └── BlobClient (interface)
                    ├── S3BlobClient       (AWS S3 / S3-compat)
                    └── MockBlobClient     (测试用)
```

---

## BlobClient 接口

```go
type BlobClient interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Put(ctx context.Context, key string, body []byte) error
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]string, error)
    Head(ctx context.Context, key string) (BlobMeta, error)
}

type BlobMeta struct {
    Size        int64
    ContentType string
    LastModified time.Time
    ETag        string
}
```

---

## Workspace 路径 → Object Key 映射

```
workspace root prefix: "workspaces/{workspace_id}/"

ReadFile("path/to/file.txt")
    → bucket.Get("workspaces/ws-001/path/to/file.txt")

ListFiles("*.go")
    → bucket.List("workspaces/ws-001/")
    → 客户端 glob 过滤
```

---

## S3 签名（AWS Signature V4）

使用 `crypto/hmac` + `crypto/sha256` 实现 V4 签名，
无需 aws-sdk 依赖。支持：

- `s3.amazonaws.com` (region-based endpoint)
- GCS XML API (`storage.googleapis.com`)
- MinIO / Cloudflare R2 (自定义 endpoint)

```go
type S3Config struct {
    Endpoint        string // 空 = AWS S3 自动推导
    Region          string
    Bucket          string
    AccessKeyID     string
    SecretAccessKey string
    RootPrefix      string // key 前缀
    PathStyle       bool   // true = path-style (MinIO)
}
```

---

## ObjectStoreWorkspace API

```go
type ObjectStoreWorkspace struct {
    client     BlobClient
    rootPrefix string
}

func NewObjectStoreWorkspace(client BlobClient, rootPrefix string) *ObjectStoreWorkspace
func NewS3Workspace(cfg S3Config) (*ObjectStoreWorkspace, error)

// 实现 port.Workspace 接口
func (w *ObjectStoreWorkspace) ReadFile(ctx, path) ([]byte, error)
func (w *ObjectStoreWorkspace) WriteFile(ctx, path, content) error
func (w *ObjectStoreWorkspace) ListFiles(ctx, pattern) ([]string, error)
func (w *ObjectStoreWorkspace) Stat(ctx, path) (port.FileInfo, error)
func (w *ObjectStoreWorkspace) DeleteFile(ctx, path) error
```

---

## 性能与限制

| 限制 | 说明 |
|------|------|
| `ListFiles` glob | S3 不支持服务端 glob，客户端过滤 |
| 文件大小 | 单文件 ≤ 5GB（S3 Put 限制），大文件需 Multipart（超出 P2 范围） |
| 强一致性 | AWS S3 强一致性（2020+），GCS 强一致性 |
| 目录概念 | 以 `/` 结尾的 key 模拟目录；`ListFiles` 自动处理 |

---

## 影响范围

- `sandbox/objectstore/workspace.go` — ObjectStoreWorkspace + BlobClient（新包）
- `sandbox/objectstore/s3client.go` — S3BlobClient，纯 net/http（新文件）
- `sandbox/objectstore/workspace_test.go` — 基于 MockBlobClient 的单元测试
