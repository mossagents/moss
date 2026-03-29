# mossquant

`mossquant` 现在是一个 **基于 AGENTS.md 的投资研究与决策参考 Agent**。

它会读取用户在 `AGENTS.md` / `USER.md` 中定义的持仓、关注标的、风险承受能力与决策因素，并结合定时研究、来源可信度评估与报告落盘，持续输出投资分析建议。

## 已实现能力

- 从 `AGENTS.md` / `USER.md` 读取并结构化解析：
  - 持仓
  - 关注标的
  - 风险承受能力
  - 复盘频率
  - 决策因素 / 约束条件
- 默认按 `10m` 周期自动执行投资复盘，可通过参数或画像配置覆盖
- 通过 `jina_search` / `jina_reader` 自主搜索市场、政策、宏观与地缘信息
- 使用 `assess_source_credibility` 对来源做可信度评分
- 内置两个子 agent：
  - `market-researcher`：负责补充市场 / 政策 / 地缘证据
  - `investment-reviewer`：负责审核结论是否符合用户风险偏好、证据是否充分、是否存在过度推断
- 保留原有模拟市场与技术分析能力：
  - `get_market_data`
  - `analyze_market`
  - `get_portfolio`
  - `get_trade_history`
  - `place_order`（仍需审批，且默认不主动使用）
- 生成带理由的投资分析报告：
  - 默认工作区为 `~/.mossquant` 时，直接保存到 `~/.mossquant\reports\`
  - 自定义工作区时，保存到 `<workspace>\.mossquant\reports\`

## 工作流

1. 默认情况下，用户把 `AGENTS.md` 放在 `~/.mossquant` 下；如果显式传入 `--workspace`，则改为对应工作区根目录。
2. `mossquant` 启动后解析画像，并创建默认定时复盘任务。
3. 每次复盘时，Agent 会：
   - 读取投资画像
   - 查询当前资产相关信息
   - 补充政策、宏观、战争/地缘等影响因素
   - 评估来源可信度
   - 调用审核 agent 复核结论
   - 输出“持有 / 观察 / 减仓 / 增持 / 回避”等参考意见
4. 最终报告会写入：
   - 默认工作区为 `~/.mossquant` 时：
     - `~/.mossquant\latest_report.md`
     - `~/.mossquant\reports\<timestamp>-investment-review-*.md`
   - 自定义工作区时：
     - `<workspace>\.mossquant\latest_report.md`
     - `<workspace>\.mossquant\reports\<timestamp>-investment-review-*.md`

## 推荐的 AGENTS.md 写法

支持两种方式混用：**YAML frontmatter** + **自然语言描述**。

### 方式 1：推荐使用 YAML frontmatter

```md
---
risk_tolerance: medium
review_interval: 10m
investment_style: medium-term allocation
holdings:
  - asset: gold
    quantity: 10
    unit: gram
    cost_basis: 1000
    currency: CNY
    price_unit: gram
    acquired_at: 2026-03-12
watchlist:
  - 比特币
  - 美元指数
decision_factors:
  - 中国与美国货币政策
  - 中东战争
  - 通胀走势
constraints:
  - 不接受高杠杆
  - 单一资产不希望仓位过高
---
```

### 方式 2：自然语言描述

```md
# 持仓
- 我在 2026年3月12日以1000元每克的价格购入黄金10克。

# 关注标的
- 比特币
- 美元指数

# 风险承受能力
- 中等风险，能接受波动，但不希望出现大幅回撤。

# 决策因素
- 中国与美国货币政策
- 中东局势
- 全球避险情绪
```

## 运行

```bash
cd examples/mossquant
go run .
```

默认情况下，`mossquant` 会把 **workspace 设为 `~/.mossquant`**，并默认从这个目录读取 `AGENTS.md` / `USER.md`。

也就是说，你可以直接把画像文件放到：

```text
~/.mossquant/AGENTS.md
```

常用参数：

```bash
go run . --capital 100000 --review-interval 10m
go run . --api-type openai --name deepseek --model deepseek-chat --review-interval 30m
go run . --auto-review=false
go run . --workspace D:\Codes\my-portfolio
```

## 参数

```text
--api-type          LLM API type: claude|openai
--name              Provider display name, e.g. openai|deepseek
--capital           Starting capital reference for simulated portfolio (default: 100000)
--review-interval   Default advisory review interval, e.g. 10m / 30m / 1h
--auto-review       Whether to auto-create the default periodic review job (default: true)
--provider          Deprecated alias for --api-type
--model             Model name
--workspace         Workspace directory (default: ~/.mossquant)
--trust             Trust level: trusted|restricted
--api-key           API key
--base-url          API base URL
```

## 说明

- `AGENTS.md` / `USER.md` 会被同时注入 system prompt，并且 `mossquant` 会额外做结构化解析。
- 若未显式传入 `--workspace`，默认工作区就是 `~/.mossquant`，因此 `AGENTS.md` 也默认从该目录读取。
- `api_type` 决定走哪类 API 适配器（如 `openai` / `claude`），`name` 用于在 TUI 入口和状态栏中显示具体提供方（如 `openai` / `deepseek`）。
- 若未提供结构化信息，Agent 仍可运行，但个性化程度会降低。
- 当前依然保留模拟交易能力，但新的默认定位是 **投资顾问 / 研究助手**，不是自动下单机器人。
- 外部研究依赖 `JINA_API_KEY` 才能稳定使用 `jina_search` / `jina_reader`。

## TUI 命令

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/schedules` | 显示后台定时任务 |
| `/clear` | 清空对话历史 |
| `/compact` | 压缩到最近 8 条消息 |
| `/exit` | 退出 |
