# trader

trader 是一个模拟交易 Agent 示例，演示有状态工具、策略交互与权限确认流程。

## 功能

- 模拟市场数据（内存随机游走）
- 交易工具：`get_market_data`、`place_order`、`get_portfolio`、`get_trade_history`
- 交易执行前可配置确认策略
- 支持组合与交易历史查询

## 运行

```bash
cd examples/trader
go run . --capital 100000
```

常用参数：

```bash
go run . --provider openai --model gpt-4o --capital 100000
go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1 --capital 50000
```

## 配置

- 全局配置目录：`~/.trader`
- 全局配置文件：`~/.trader/config.yaml`

## System Prompt 模板覆盖

- 项目级（优先）：`./.trader/system_prompt.tmpl`
- 全局级：`~/.trader/system_prompt.tmpl`

默认模板文件：`templates/system_prompt.tmpl`
