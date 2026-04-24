# Research Swarm Example

一个可落地的 **research-first swarm product shell**，直接演示新的 swarm substrate 如何被产品层消费，而不是再做旧版那种 prompt-heavy demo。

## 命令面

```bash
cd examples/research-swarm

# 1) 运行一次真实研究 run
go run . run \
  --topic "Summarize the trade-offs of local-first agent swarms." \
  --model gpt-4o \
  --workers 3 \
  --lang zh

# 2) 查看最近一次 run
go run . inspect --latest
go run . inspect --latest --view threads
go run . inspect --latest --view events

# 3) 导出结果
go run . export --latest --format bundle
go run . export --latest --format json
go run . export --latest --format jsonl

# 4) 恢复可恢复的 run
go run . resume --latest --model gpt-4o
```

## 关键 flag

| 命令 | 关键 flag | 说明 |
|------|-----------|------|
| `run` | `--topic` | 研究课题（必填） |
| `run` | `--model` | LLM 模型（必填） |
| `run` | `--workers N` | 并行 worker 数量（默认 3，最小 1） |
| `run` | `--lang <lang>` | 输出语言，如 `zh`、`en`、`ja`（默认：模型自然语言） |
| `run` | `--detail brief|standard|comprehensive` | 控制最终报告篇幅、论据深度与覆盖面 |
| `run` | `--as-of <RFC3339>` | 指定研究的基准时间，帮助报告显式声明 freshness |
| `run/resume` | `--allow-all` | 允许所有操作；通过 AI guardian 审批代替交互确认（需要 --model） |
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

默认 app dir 为 `~/.research-swarm/`，其中关键内容如下：

```text
~/.research-swarm/
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
3. `synthesizer` 聚合 findings 与 source-set，产出显式包含 `As of` 与证据论据的 `final-report.md`。
4. `reviewer` 审核最终报告并写入治理消息。
5. `inspect` / `export` 直接消费持久化的 session/task/message/artifact/event 事实面。

## 回归检查

```bash
go test .
```
