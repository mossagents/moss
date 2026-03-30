# mosswriter

`mosswriter` 是一个基于 `presets/deepagent` 的 content builder / writing agent 示例。

它参考了 deepagents 的 `content-builder-agent`，并在 Moss 中用三类文件系统原语实现：

- `AGENTS.md`：长期写作记忆、品牌语气、风格规范
- `.agents/skills/*/SKILL.md`：按需加载的内容工作流技能
- `subagents.yaml`：通过代码加载并注册的专用子 agent

## 功能

- 支持写博客、LinkedIn 帖子、X/Twitter 线程等内容
- 通过 `researcher` 子 agent 做前置研究
- 通过 `editor` 子 agent 做结构与语气润色
- 支持 `jina_search` / `jina_reader` / `think_tool`
- 提供 `make_slug` 与 `generate_image_brief` 工具，便于落盘内容与封面创意
- 支持 TUI 与 `--prompt` one-shot

## 文件系统原语

示例目录内已经包含默认素材：

- `AGENTS.md`
- `subagents.yaml`
- `.agents/skills/blog-post/SKILL.md`
- `.agents/skills/social-media/SKILL.md`

当你在 `examples/mosswriter` 目录运行时，这些内容会自动成为 agent 的工作上下文。

## 运行

```bash
cd examples/mosswriter
go run .
```

常用参数：

```bash
go run . --provider openai --model gpt-4o
go run . --prompt "Write a blog post about prompt engineering for backend teams"
go run . -p "Create a LinkedIn post about why context engineering matters"
go run . --trust restricted
```

## 配置

- 全局配置目录：`~/.mosswriter`
- 全局配置文件：`~/.mosswriter/config.yaml`

示例：

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
```

## 输出目录约定

建议输出到工作区下的 `.mosswriter/`：

- `./.mosswriter/research/`：研究笔记
- `./.mosswriter/blogs/<slug>/post.md`：博客正文
- `./.mosswriter/blogs/<slug>/cover_prompt.md`：封面图提示词 / brief
- `./.mosswriter/linkedin/<slug>/post.md`：LinkedIn 文案
- `./.mosswriter/twitter/<slug>/thread.md`：线程草稿

## Research Tools

示例内置注册：

- `jina_search`
- `jina_reader`
- `think_tool`
- `make_slug`
- `generate_image_brief`

`jina_search` / `jina_reader` 会读取 `JINA_API_KEY`（如果提供）。

## System Prompt 模板覆盖

- 项目级（优先）：`./.mosswriter/system_prompt.tmpl`
- 全局级：`~/.mosswriter/system_prompt.tmpl`

默认模板文件：`templates/system_prompt.tmpl`
