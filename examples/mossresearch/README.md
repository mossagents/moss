# mossresearch

`mossresearch` 是一个基于 `presets/deepagent` 的 deep research 示例。

它演示如何在 Moss 里组合：

- deepagent preset（planning、context offload、task lifecycle、delegation）
- research orchestrator prompt
- 专用 `researcher` 子 agent
- `jina_search` / `jina_reader` / `think_tool` 研究工具
- TUI 与 one-shot 研究模式

## 功能

- 主 agent 负责拆解研究问题、委派子任务、汇总结果
- `researcher` 子 agent 负责聚焦式网页搜索与网页阅读
- 将研究请求写入 `./.mossresearch/research_request.md`
- 将最终报告写入 `./.mossresearch/final_report.md`
- 支持多轮 TUI，也支持 `--goal` one-shot

## 运行

```bash
cd examples/mossresearch
go run .
```

常用参数：

```bash
go run . --provider openai --model gpt-4o
go run . --goal "Compare local-first note-taking apps for engineering teams"
go run . --trust restricted
```

## 配置

- 全局配置目录：`~/.mossresearch`
- 全局配置文件：`~/.mossresearch/config.yaml`

示例：

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
```

## Research Tools

示例默认注册以下 research tools：

- `jina_search`：搜索网页结果
- `jina_reader`：读取网页正文
- `think_tool`：记录研究过程中的阶段性反思

`jina_search` / `jina_reader` 由示例内置实现，并会读取 `JINA_API_KEY`（如果提供）。未设置时也可按 Jina 服务可用性直接使用。

## 输出文件

- 研究请求：`./.mossresearch/research_request.md`
- 最终报告：`./.mossresearch/final_report.md`

## System Prompt 模板覆盖

- 项目级（优先）：`./.mossresearch/system_prompt.tmpl`
- 全局级：`~/.mossresearch/system_prompt.tmpl`

默认模板文件：`templates/system_prompt.tmpl`
