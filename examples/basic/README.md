# basic — 最简集成示例

用最少代码启动一个可对话的 AI Agent。

## 特点

- **零配置文件**：无需 `moss.yaml` 或模板文件
- **一行装配**：`appkit.BuildKernel` 自动完成 LLM 适配器、工具注册、技能加载
- **REPL 交互**：终端命令行对话，支持 `/help`、`/clear`、`/exit`
- **8 个内置工具**：read_file、write_file、edit_file、glob、list_files、grep、run_command、ask_user

## 用法

```bash
# 使用 OpenAI 兼容 API
go run . --provider openai --model gpt-4o

# 使用本地模型
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1

# 使用 Claude
go run . --provider claude
```

## 代码解读

```go
// 1. 解析 CLI 参数 + 合并配置文件 + 环境变量
flags := appkit.ParseAppFlags()

// 2. 创建 Kernel（自动装配工具、技能、MCP servers）
// intr 来自: github.com/mossagents/moss/kernel/io
k, err := appkit.BuildKernel(ctx, flags, intr.NewConsoleIO())

// 3. 启动 REPL 交互
appkit.REPL(ctx, appkit.REPLConfig{...}, k, sess)
```

总共约 60 行代码，展示了 moss kernel 作为库的最小集成方式。
