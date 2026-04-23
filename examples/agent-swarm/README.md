# Agent Swarm Example

一个可落地的 **research-first swarm product shell**，直接演示新的 swarm substrate 如何被产品层消费，而不是再做旧版那种 prompt-heavy demo。

## 命令面

```bash
cd examples/agent-swarm

# 1) 运行一个 deterministic demo run
go run . run --demo

# 2) 查看最近一次 run
go run . inspect --latest
go run . inspect --latest --view threads
go run . inspect --latest --view events

# 3) 导出结果
go run . export --latest --format bundle
go run . export --latest --format json
go run . export --latest --format jsonl

# 4) 恢复可恢复的 run
go run . resume --latest
```

## 真实模式

```bash
go run . run \
  --topic "Summarize the trade-offs of local-first agent swarms." \
  --provider openai \
  --model gpt-4o
```

- `run` 在真实模式下需要可用的 provider/model 配置。
- `resume` 会保持已持久化的 `execution_mode`；demo run 只能用 demo 语义恢复，real run 不能切成 demo。
- `inspect/export --latest` 解析 **最新 run**；`resume --latest` 解析 **最新可恢复 run**。

## 主要 flag

| 命令 | 关键 flag | 说明 |
|------|-----------|------|
| `run` | `--topic` | 真实模式必填；`--demo` 时默认使用内置 topic |
| `run` | `--demo` | 启用 deterministic demo 数据流 |
| `run/resume` | `--run-id` | 显式指定 swarm run ID |
| `run/resume` | `--output` | 额外写出 `run-summary.json` / `resume-summary.json` |
| `resume` | `--session` / `--run-id` / `--latest` | 指定恢复目标 |
| `resume` | `--force-degraded-resume` | 允许从 degraded snapshot 继续 |
| `inspect` | `--view run|threads|thread|events` | 切换查看面 |
| `inspect` | `--thread-id` | `--view thread` 时必填 |
| `inspect` | `--json` | 输出 JSON |
| `export` | `--format bundle|json|jsonl` | 选择导出格式 |
| `export` | `--include-payloads` | bundle 模式下额外导出 artifact payload 文件 |

## 持久化布局

默认 app dir 为 `~/.agent-swarm/`，其中关键内容如下：

```text
~/.agent-swarm/
├── artifacts/     # swarm artifact payloads
├── checkpoints/   # session checkpoints
├── events.db      # runtime event store
├── exports/       # export 默认输出目录
├── locks/         # run-level lease files
├── sessions/      # root/child thread sessions
└── tasks/         # task graph + task messages
```

## bundle 导出内容

`go run . export --latest --format bundle` 默认生成：

```text
exports/<run-id>/
├── artifacts.json
├── events.jsonl
├── final-report.md
├── summary.json
└── payloads/      # 仅当 --include-payloads 打开
```

## 运行时行为

这个 example 固定走一条 research-first 流程：

1. `planner` 生成研究问题并拆分 worker 任务。
2. `workers` 发布 findings / source-set / confidence-note。
3. `synthesizer` 聚合 findings，产出 `final-report.md`。
4. `reviewer` 审核最终报告并写入治理消息。
5. `inspect` / `export` 直接消费持久化的 session/task/message/artifact/event 事实面。

## 回归检查

```bash
go test .
go run . run --demo
go run . inspect --latest --view threads
go run . export --latest --format bundle
```
