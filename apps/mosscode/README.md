# mosscode

`mosscode` 是 Moss Agents 框架的核心代码助手应用：默认 TUI，多轮会话，支持 one-shot 执行。打包后的 `moss` CLI 入口指向这个应用面。

## 功能

- 基于 `presets/deepagent.BuildKernel` 的增强装配（轻量入口 + 生产向默认能力）
- 内置 8 个核心工具 + 持久 memories + context offload + 异步任务生命周期工具
- TUI 支持增强斜杠命令（`/session`、`/offload`、`/tasks`、`/task`）
- 支持 one-shot 模式：`--prompt "..."`
- 支持 provider/model/base_url/api_key 配置
- 支持 system prompt 模板覆盖

## 运行

```bash
cd apps/mosscode
go run .
```

常用参数：

```bash
go run . --provider openai --model gpt-4o
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
go run . --trust restricted
go run . --prompt "Analyze flaky tests and propose a fix plan"
go run . -p "Analyze flaky tests and propose a fix plan"
```

## 配置

- 全局配置目录：`~/.mosscode`
- 全局配置文件：`~/.mosscode/config.yaml`

示例：

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
```

## System Prompt 模板覆盖

- 项目级（优先）：`./.mosscode/system_prompt.tmpl`
- 全局级：`~/.mosscode/system_prompt.tmpl`

默认模板文件：`templates/system_prompt.tmpl`

## 运行模式

- **TUI（默认）**：`go run .`
- **one-shot**：`go run . --prompt "<your task>"`

当启用 `restricted` 信任级别时，危险工具会触发审批策略，适合更稳妥的生产使用。
