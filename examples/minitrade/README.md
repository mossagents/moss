# miniloop

miniloop 是一个通用的 **有状态自主循环 Agent** 框架，通过可插拔的 Domain 适配器支持不同领域。

## 架构

```
┌─────────────────────────────────────────┐
│            miniloop (main.go)           │
│  REPL · Config · LLM · ConsoleIO       │
├─────────────────────────────────────────┤
│           Domain Interface              │
│  Setup · Prompt · Policies · Events     │
│  Start (background) · Banner            │
├─────────────────────────────────────────┤
│         Domain Adapters                 │
│  trading.go │ (your-domain.go)          │
└─────────────────────────────────────────┘
```

核心思想：**Agent 运行时与领域逻辑彻底解耦**。

- `main.go` — 通用循环 Agent：CLI 解析、Kernel 构建、REPL、LLM 适配
- `domain.go` — Domain 接口定义：工具、提示词、策略、后台进程、UI 提示
- `trading.go` — Trading 领域适配器：模拟市场、交易工具、组合管理

## 内置领域

### trading — 模拟交易

演示有状态工具、策略交互与权限确认流程。

**功能**：
- 内存模拟市场（10 种资产，每 5 秒随机游走）
- 4 个交易工具：`get_market_data`、`place_order`、`get_portfolio`、`get_trade_history`
- 下单前需要用户确认（PolicyRule）
- 交易事件自动日志

**运行**：

```bash
cd examples/miniloop
go run . --domain trading --capital 100000
```

## 添加新领域

实现 `Domain` 接口并注册：

```go
// mydom.go
package main

func init() {
    registerDomain("mydom", func(cfg *config) Domain {
        return &myDomain{...}
    })
}

type myDomain struct { ... }

func (d *myDomain) Name() string              { return "mydom" }
func (d *myDomain) Description() string       { return "My custom domain" }
func (d *myDomain) Setup(k *kernel.Kernel) error { /* register tools */ }
func (d *myDomain) SystemPrompt(ws string) string { /* render prompt */ }
func (d *myDomain) Policies() []builtins.PolicyRule { /* approval rules */ }
func (d *myDomain) EventHooks() map[string]builtins.EventHandler { /* event handlers */ }
func (d *myDomain) Start(ctx context.Context) func() { /* background tasks */ }
func (d *myDomain) Banner() []string          { /* startup info */ }
func (d *myDomain) Prompt() string            { return "> " }
```

然后运行：`go run . --domain mydom`

## 适用场景

miniloop 的 Domain 模式适合所有 **持续运行、有状态、需要周期性感知和决策** 的 Agent 场景：

| 领域 | 工具示例 | 后台进程 |
|------|---------|---------|
| **交易** | get_market_data, place_order | 价格更新 (5s tick) |
| **DevOps 运维** | get_metrics, get_alerts, run_remediation | 指标采集、告警检测 |
| **库存管理** | get_inventory, place_reorder, check_suppliers | 库存水平监控 |
| **游戏 NPC** | observe, take_action, check_status | 环境状态更新 |
| **IoT 监控** | read_sensors, set_actuator, get_history | 传感器数据采集 |

## 通用参数

```bash
go run . [flags]

Flags:
  --provider    LLM provider: claude|openai (default: openai)
  --model       Model name
  --workspace   Workspace directory (default: .)
  --api-key     API key
  --base-url    API base URL
  --domain      Domain adapter (default: trading)
  --capital     Starting capital, trading domain (default: 100000)
```

## 配置

- 全局配置目录：`~/.miniloop`
- 全局配置文件：`~/.miniloop/config.yaml`

```yaml
# ~/.miniloop/config.yaml
provider: openai
model: gpt-4o
api_key: sk-...
base_url: https://api.openai.com/v1
```

## REPL 命令

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/clear` | 清空对话历史 |
| `/compact` | 压缩到最近 8 条消息 |
| `/exit` | 退出 |
