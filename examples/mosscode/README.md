# mosscode

mosscode 是一个类 Claude Code 的极简代码助手示例，当前为默认 TUI 交互。

## 功能

- 基于 Moss Kernel 的交互式代码助手
- 内置 8 个核心工具（read_file、write_file、edit_file、glob、list_files、search_text、run_command、ask_user）
- 支持 provider/model/base_url/api_key 配置
- 支持 system prompt 模板覆盖

## 运行

```bash
cd examples/mosscode
go run .
```

常用参数：

```bash
go run . --provider openai --model gpt-4o
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
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
