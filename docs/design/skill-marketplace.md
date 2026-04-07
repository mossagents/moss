# Skill 市场与注册中心设计

## 问题描述

当前 Skill 只能通过代码直接注册，缺乏在线发现、分发和版本管理能力。
用户无法像 `npm install` 或 `pip install` 一样便捷地安装社区 Skill。

## 设计目标

1. `moss skill list/search/install/remove` CLI 子命令
2. SkillRegistry HTTP API：提供 Skill 元信息检索
3. 本地缓存管理：`~/.moss/skills/{name}@{version}/`
4. 安全：仅支持 HTTPS 源；安装后 Skill 在沙箱中执行
5. 零新依赖（仅 `net/http`, `encoding/json`, `os`, `archive/zip`）

---

## Registry API 设计

```
GET  /v1/skills                          列出全部 Skill（分页）
GET  /v1/skills?q=<keyword>             搜索 Skill
GET  /v1/skills/{name}                  获取 Skill 元信息（所有版本）
GET  /v1/skills/{name}/{version}        获取特定版本元信息
GET  /v1/skills/{name}/{version}/zip    下载 Skill 压缩包
```

响应格式（JSON）：
```json
{
  "name": "web-search",
  "version": "1.2.0",
  "description": "Web search tool",
  "author": "moss-community",
  "license": "MIT",
  "size_bytes": 12345,
  "checksum_sha256": "abc...",
  "requires": [{"name": "browser", "min_version": "1.0.0"}]
}
```

---

## 本地缓存结构

```
~/.moss/skills/
  web-search@1.2.0/
    moss-skill.yaml     # 元信息
    skill.so            # 或 skill.js / skill.wasm
    README.md
  installed.json        # 已安装的 name → version 索引
```

---

## CLI 命令

```
moss skill list                          列出已安装 Skill
moss skill search <keyword>             在注册中心搜索
moss skill install <name>[@<version>]   安装 Skill
moss skill remove <name>                卸载 Skill
moss skill info <name>                  查看 Skill 元信息
```

---

## SkillRegistryClient 接口

```go
type RegistryClient interface {
    Search(ctx, query string) ([]SkillEntry, error)
    GetInfo(ctx, name, version string) (*SkillEntry, error)
    Download(ctx, name, version, destDir string) error
}

type SkillEntry struct {
    Name        string     `json:"name"`
    Version     string     `json:"version"`
    Description string     `json:"description"`
    Author      string     `json:"author"`
    Checksum    string     `json:"checksum_sha256"`
    Requires    []SkillDep `json:"requires"`
}
```

---

## LocalCache 接口

```go
type LocalCache interface {
    Installed() ([]InstalledSkill, error)
    Install(entry SkillEntry, zipData []byte) error
    Remove(name string) error
    SkillDir(name, version string) string
}
```

---

## 影响范围

- `skill/registry/client.go` — RegistryClient HTTP 实现（新包）
- `skill/registry/cache.go` — LocalCache 文件实现
- `cmd/moss/main.go` — `moss skill` 子命令路由
- `cmd/moss/skill_cmd.go` — skill list/search/install/remove 实现（新文件）
