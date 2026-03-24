# miniwork

miniwork 是一个多 Agent 编排示例，采用 Manager -> Worker 委派模型执行复杂任务。默认启动 TUI，传入 `--goal` 时保留一次性执行模式。

## 功能

- Manager 将目标拆解为可并行子任务
- Worker 在独立 Session 中执行子任务
- 自定义工具 `delegate_tasks` 负责并发调度
- 支持最大并发 worker 数控制
- TUI 右侧显示 worker 执行摘要面板
- 自动读取 `~/.miniwork/config.yaml` 作为 provider/model/base_url/api_key 默认值

## 运行

```bash
cd examples/miniwork
go run .
```

常用参数：

```bash
go run . --goal "分析项目并输出改造建议"
go run . --goal "为 main.go 补测试" --workers 4
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1 --goal "重构日志模块"
```

## 配置

- 全局配置目录：`~/.miniwork`
- 全局配置文件：`~/.miniwork/config.yaml`

示例：

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
```

优先级：CLI 参数 > `~/.miniwork/config.yaml` > 环境变量。

## System Prompt 模板覆盖

- 项目级（优先）：`./.miniwork/system_prompt.tmpl`
- 全局级：`~/.miniwork/system_prompt.tmpl`

默认模板：

- Manager：`templates/manager_system_prompt.tmpl`
- Worker：`templates/worker_system_prompt.tmpl`
