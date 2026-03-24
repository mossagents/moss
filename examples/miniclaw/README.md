# miniclaw

miniclaw 是一个 Web 抓取 Agent 示例，演示如何在 Moss 中加入网络抓取类工具。

## 功能

- 自定义抓取工具：`fetch_url`
- 自定义链接提取工具：`extract_links`
- 可配合内置文件工具将抓取结果落盘
- 支持交互式多轮抓取

## 运行

```bash
cd examples/miniclaw
go run .
```

常用参数：

```bash
go run . --provider openai --model gpt-4o
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
```

## 配置

- 全局配置目录：`~/.miniclaw`
- 全局配置文件：`~/.miniclaw/config.yaml`

## System Prompt 模板覆盖

- 项目级（优先）：`./.miniclaw/system_prompt.tmpl`
- 全局级：`~/.miniclaw/system_prompt.tmpl`

默认模板文件：`templates/system_prompt.tmpl`
