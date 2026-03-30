# mossclaw 对标 claw0 分阶段设计

## 背景与目标

`examples/mossclaw` 目前已具备基础助理能力（文件工具、知识库、调度、bootstrap 上下文），但相较 `claw0` 的生产化能力仍有明显差距：并发车道、可靠投递、5 级路由、多通道、韧性重试与认证轮换。

本设计目标是在不破坏现有使用方式的前提下，按 `P0 -> P1 -> P2` 逐步补齐关键能力，并且每个 Phase 都满足：

- 代码实现完成
- 测试通过（含全仓 `go test ./...` 与 `go build ./...`）
- 独立提交并推送

## 非目标

- 不追求 1:1 复刻 `claw0` 教学仓结构（如 `sessions/*` 课程文件体系）
- 不在本轮引入与目标无关的 UI/产品功能
- 不进行大规模无关重构

## 总体架构

采用“通用能力先抽象、示例应用做组装”的路线。

- 通用层（优先放在可复用包）：
  - `lanequeue`：命名车道 + FIFO + 可配置并发
  - `delivery`：可靠投递、重试退避、落盘恢复、死信
  - `routing`：5 级绑定解析与 session key 规范
  - `resilience`：分层重试策略与认证轮换
- 应用层（`examples/mossclaw`）：
  - 仅负责装配通用组件到现有 `tui/gateway/scheduler` 链路
  - 保持当前 CLI 参数与基础交互兼容

## 分阶段设计

### P0：执行与投递最小闭环（优先级最高）

#### 范围

- 引入命名车道执行队列，替换/包裹当前单路执行流程
- 引入可靠投递队列：失败重试 + 指数退避 + 落盘恢复 + 死信
- 在 `mossclaw` 中将用户输入路径与调度任务路径接入 lane + delivery

#### 关键接口（草案）

- `type LaneQueue interface { Enqueue(lane string, fn Task) Future; Stats() LaneStats }`
- `type DeliveryQueue interface { Publish(msg OutboundMessage) error; Start(ctx); Stop(ctx) error; Recover(ctx) error }`
- `type RetryPolicy interface { ShouldRetry(error) bool; NextDelay(attempt int) time.Duration }`

#### 数据流

1. 输入事件（用户/调度）进入指定 lane。
2. lane 内按 FIFO 执行任务。
3. 任务产出的可投递消息写入 delivery queue。
4. 发送失败按策略重试，超限进入 dead-letter 并记录。
5. 进程重启执行恢复，继续未完成投递。

#### 验收标准

- lane 并发/FIFO 行为可测且稳定。
- delivery 在故障场景可重试、可恢复、可追踪失败。
- `mossclaw` 现有交互行为保持可用。

### P1：路由与多通道骨架

#### 范围

- 引入 channel 抽象接口（先接入可用最小实现，后续扩展 Telegram/Feishu）
- 实现 5 级路由绑定：`peer > guild > account > channel > default`
- 统一 session key 生成规则，支持不同隔离粒度

#### 关键接口（草案）

- `type Router interface { Resolve(meta InboundMeta) (agentID string, sessionKey string, matched Rule, err error) }`
- `type ChannelAdapter interface { Name() string; Start(ctx, sink InboundSink) error; Stop(ctx) error }`

#### 验收标准

- 路由优先级测试覆盖完整（同层优先级、跨层覆盖、默认回退）。
- session key 规则稳定、可预测。
- `mossclaw` 可在不破坏现有模式下接入新路由入口。

### P2：生产韧性

#### 范围

- 三层重试策略（建议：模型调用层 / 工具调用层 / 投递层）
- 认证 profile 轮换与降级策略（主失败后备用）
- 补齐运行资产约定（如 heartbeat/cron/skills 相关配置载入与校验）

#### 验收标准

- 重试行为可配置、可观测，避免无限重试。
- 认证轮换路径可测，失败有明确错误与日志。
- 关键运行资产缺失时，给出显式诊断信息。

## 错误处理与可观测性

- 禁止静默失败：关键路径错误必须返回/记录。
- delivery 超重试进入 dead-letter，并记录原始错误与重试次数。
- 恢复时遇到损坏记录应“跳过并告警”，不阻塞队列整体恢复。
- 统一输出结构化日志字段（phase、component、lane、message_id、attempt）。

## 测试策略

### 单元测试

- P0：
  - `lanequeue` FIFO、并发上限、异常任务不阻塞后续任务
  - `delivery` 重试、退避、恢复、死信路径
- P1：
  - 路由 5 级匹配优先级
  - session key 规范（不同 scope）
- P2：
  - 三层重试策略触发与停止条件
  - 认证轮换与降级路径

### 集成测试

- `examples/mossclaw` 的 `tui` 和 `gateway` 启动链路回归
- 调度触发到投递链路端到端验证（最小场景）

### 回归验证

- 每个 Phase 完成后执行：
  - `go test ./...`
  - `go build ./...`

## 里程碑交付与提交策略

- P0 完成 -> 测试通过 -> 单独 commit + push
- P1 完成 -> 测试通过 -> 单独 commit + push
- P2 完成 -> 测试通过 -> 单独 commit + push

提交必须只包含该 Phase 相关改动，避免混入无关文件。

## 风险与缓解

- 风险：通用层抽象过度导致落地缓慢  
  缓解：优先最小接口，先满足 `mossclaw` 当前路径，再逐步泛化。

- 风险：并发引入后出现竞态问题  
  缓解：先在 P0 建立严格单测和 race-sensitive 用例。

- 风险：现有行为回归  
  缓解：每个 Phase 都做集成回归并独立提交，降低回滚成本。
