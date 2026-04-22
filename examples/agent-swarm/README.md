# Agent Swarm 示例

演示如何使用 moss kernel 构建**智能多 Agent 协作研究系统**。

## 核心特性

| 特性 | 说明 |
|------|------|
| **动态人设生成** | 每轮研究前通过 LLM 实时生成定制专家角色；第 2 轮起基于前轮发现自动补全视角盲点 |
| 自主问题分解 | DecomposerAgent 通过 LLM 将研究主题拆解为 N 个子问题 |
| 10 种内置人设 | 技术专家、批判思维者、实用主义者、未来学家、伦理学家等（`--personas 0` 启用） |
| 多轮头脑风暴 | 第 2 轮起，每个 Agent 阅读所有人的上轮发现并进行思维碰撞 |
| 并行执行 | `ParallelAgent` 驱动批次并发，安全遵守 `maxActiveAgents=16` 约束 |
| 结构化报告 | SynthesisAgent 汇总所有发现，生成含执行摘要/共识/争议/建议的完整报告 |

## 研究流程

```
研究主题
    │
    ▼ LLM 分解
子问题 × M
    │
    ┌─────────────── 每轮循环 ───────────────┐
    │                                         │
    ▼ LLM 生成人设（--personas N）            │
    第 1 轮：基于研究主题生成初始专家团队     │
    第 2 轮：分析前轮发现 → 填补视角盲点     │
    第 N 轮：持续补全尚未充分探索的维度       │
    │                                         │
    ▼ 并行研究（batch 个 Agent 同时进行）     │
    各 Agent 独立研究 / 碰撞前轮发现 → 发现  │
    │                                         │
    └──────────────（下一轮）────────────────┘
    │
    ▼ LLM 综合
综合研究报告（Markdown）
```

## 快速开始

### 前置条件

需要一个支持的 LLM Provider（OpenAI / Anthropic / DeepSeek / Azure 等）。

### 运行命令

```bash
cd examples/agent-swarm

# OpenAI（推荐）
go run . \
  --provider openai \
  --model gpt-4o \
  --api-key $OPENAI_API_KEY \
  --topic "大规模语言模型在教育领域的应用前景" \
  --agents 8 \
  --rounds 2

# Anthropic Claude
go run . \
  --provider claude \
  --model claude-3-5-sonnet-20241022 \
  --api-key $ANTHROPIC_API_KEY \
  --topic "量子计算的产业化路径" \
  --agents 10 \
  --rounds 3 \
  --batch 5

# DeepSeek（国内可用）
go run . \
  --provider deepseek \
  --model deepseek-chat \
  --api-key $DEEPSEEK_API_KEY \
  --topic "人工智能对劳动力市场的长期影响" \
  --agents 6 \
  --rounds 2
```

## 参数说明

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--topic` | 大规模语言模型在教育领域的应用前景 | 研究主题 |
| `--personas` | 5 | LLM 动态生成的人设数量（2–20）；`0` = 使用内置 10 种预设人设 |
| `--agents` | 8 | Agent 数量（2–30），循环使用生成的人设 |
| `--rounds` | 2 | 研究轮次（1–5）；≥2 轮时 Agent 间相互评论 |
| `--batch` | 5 | 每批并行 Agent 数（≤10） |
| `--provider` | openai | LLM Provider |
| `--model` | — | LLM 模型名称 |
| `--api-key` | — | API 密钥（也可通过环境变量设置） |

## 并发约束

moss kernel 默认 `maxActiveAgents = 16`（全局并发子 Agent 上限）。  
本示例 peak 并发为：`1 (ParallelAgent) + batch (workers)`，故 `--batch ≤ 10` 可安全运行。

## 代码结构

```
examples/agent-swarm/
├── main.go       # 入口：flag 解析、kernel 初始化、事件输出
├── swarm.go      # 核心逻辑：ResearchSwarm + PersonaWorkerAgent
├── personas.go   # 10 种研究人设定义（含完整 SystemPrompt）
└── go.mod        # 模块依赖（kernel + harness）
```

## 扩展思路

- **增加工具**：为 `PersonaWorkerAgent` 注入 `web_search`、`calculator` 等工具，使 Agent 能主动获取外部数据
- **更多人设**：在 `personas.go` 中追加更多领域专家（法学家、医学家、物理学家等）
- **动态人设匹配**：根据子问题类型自动分配最相关的人设，而非循环分配
- **分层讨论**：先分组讨论，再跨组汇报，模拟更复杂的组织结构
