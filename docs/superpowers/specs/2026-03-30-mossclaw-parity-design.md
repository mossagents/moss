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

#### 接口契约（P0 必须明确）

- `LaneQueue.Enqueue`：
  - 同一 `lane` 内严格 FIFO。
  - 不同 `lane` 可并行执行。
  - 返回 `Future` 必须承载任务结果或错误，不允许丢失错误。
- `DeliveryQueue.Start/Stop/Recover`：
  - `Start` 幂等；重复调用不应重复启动 worker。
  - `Stop` 幂等；应等待 in-flight 任务在可配置超时内收敛。
  - `Recover` 仅允许在 `Start` 前执行；若重复执行应返回明确错误。
- `Publish`：
  - 成功返回表示“已持久化入队”，不代表“已投递完成”。
  - 入队失败必须返回错误，禁止 silent drop。

#### 幂等与去重策略（P0）

- 每条 `OutboundMessage` 必须包含稳定 `message_id`（UUID 或上游事件派生 ID）。
- delivery 持久化记录 `message_id + attempt + state`，重试时沿用同一 `message_id`。
- 发送器实现必须支持“至少一次投递”语义；消费者侧以 `message_id` 做去重。
- dead-letter 必须保留 `message_id`，便于人工重放与审计。

#### 持久化与恢复格式（P0）

- 存储路径：`~/.mossclaw/delivery/`（可配置覆盖）。
- 文件：
  - `queue.jsonl`：待投递/重试中的消息记录。
  - `deadletter.jsonl`：超过最大重试的失败记录。
- `queue.jsonl` 记录字段最小集合：
  - `message_id`, `lane`, `payload`, `attempt`, `next_retry_at`, `created_at`, `last_error`
- 恢复策略：
  - 启动时逐行读取 `queue.jsonl` 重建内存队列。
  - 单条损坏记录：写入 `recovery_errors.log` 并跳过。
  - 连续损坏超过阈值（默认 100 条）则中止恢复并上报错误。

#### 数据流

1. 输入事件（用户/调度）进入指定 lane。
2. lane 内按 FIFO 执行任务。
3. 任务产出的可投递消息写入 delivery queue。
4. 发送失败按策略重试，超限进入 dead-letter 并记录。
5. 进程重启执行恢复，继续未完成投递。

#### 验收标准

- lane 测试通过：同 lane 顺序一致率 100%，跨 lane 并发执行可观测。
- delivery 测试通过：网络失败重试、进程重启恢复、超限入 dead-letter 均可复现。
- `mossclaw` 现有 `tui/gateway` 启动与基本对话回归通过。

### P1：路由与多通道骨架

#### 范围

- 引入 channel 抽象接口（先接入可用最小实现，后续扩展 Telegram/Feishu）
- 实现 5 级路由绑定：`peer > guild > account > channel > default`
- 统一 session key 生成规则，支持不同隔离粒度

#### 关键接口（草案）

- `type Router interface { Resolve(meta InboundMeta) (agentID string, sessionKey string, matched Rule, err error) }`
- `type ChannelAdapter interface { Name() string; Start(ctx, sink InboundSink) error; Stop(ctx) error }`

#### 路由规则与 session key 规范（P1）

- 5 级匹配顺序固定：
  1. `peer`（最具体）
  2. `guild`
  3. `account`
  4. `channel`
  5. `default`（兜底）
- 同层冲突：按 `priority`（高优先）再按创建时间（新覆盖旧）决议。
- session key 规则：
  - `per-peer`: `agent:{aid}:direct:{peer}`
  - `per-channel-peer`: `agent:{aid}:{channel}:direct:{peer}`
  - `per-account-channel-peer`: `agent:{aid}:{channel}:{account}:direct:{peer}`
  - 无 `peer` 时回退：`agent:{aid}:main`

#### ChannelAdapter 契约（P1）

- `Start` 必须非阻塞返回；内部事件循环独立 goroutine。
- 读取失败时应上报 `OnError`，并按 backoff 自动重连。
- 若 sink 背压（队列满）：
  - 先重试入队（短退避），
  - 超过阈值后返回明确丢弃错误并计数，不得静默丢弃。
- `Stop` 必须在超时内终止接收循环并释放资源。

#### 验收标准

- 路由优先级测试覆盖：同层冲突、跨层覆盖、默认回退全部通过。
- session key 生成规则单测覆盖 100%（包含空值/异常输入）。
- 接入后 `mossclaw` 兼容现有启动参数与运行方式。

### P2：生产韧性

#### 范围

- 三层重试策略（建议：模型调用层 / 工具调用层 / 投递层）
- 认证 profile 轮换与降级策略（主失败后备用）
- 补齐运行资产约定（如 heartbeat/cron/skills 相关配置载入与校验）

#### 三层重试与认证轮换契约（P2）

- 模型调用层：仅对可重试错误（超时、5xx、限流）重试；最大重试默认 3。
- 工具调用层：仅对声明可重试工具生效；副作用工具默认不重试。
- 投递层：沿用 P0 delivery 策略并可覆盖最大重试次数。
- 认证轮换：
  - profile 顺序：`primary -> secondary -> tertiary`。
  - 当前 profile 失败达到阈值后切换下一个。
  - 所有 profile 失败时返回聚合错误并进入降级模式（仅本地能力）。

#### 运行资产清单（P2）

- `HEARTBEAT.md`：主动任务策略模板（可选，缺失降级默认策略）。
- `CRON.json`：计划任务定义（可选，格式错误为 hard error）。
- `skills/`：技能描述目录（可选，解析错误记录告警并跳过）。
- 启动时输出资产检查报告：`found/missing/invalid` 三类统计。

#### 验收标准

- 三层重试触发条件、停止条件、上限行为均有单测。
- 认证轮换覆盖成功切换与全部失败两条路径，并输出结构化错误。
- 资产检查报告在启动日志中可见，错误资产可定位到文件级。

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

## 各 Phase Definition of Done（可量化）

### P0 DoD

- 新增 lanequeue 与 delivery 代码及单测。
- `go test ./...` 与 `go build ./...` 通过。
- 手工验证：模拟投递失败后重启，消息可恢复并继续重试。
- 提交信息包含 `phase: P0`，并推送到 `main`。

### P1 DoD

- 新增 routing 与 channel adapter 骨架及单测。
- `go test ./...` 与 `go build ./...` 通过。
- 手工验证：构造 5 级规则，路由结果与预期一致。
- 提交信息包含 `phase: P1`，并推送到 `main`。

### P2 DoD

- 新增 resilience/profile rotation 与资产校验代码及单测。
- `go test ./...` 与 `go build ./...` 通过。
- 手工验证：触发 profile 切换与降级路径，日志可追踪。
- 提交信息包含 `phase: P2`，并推送到 `main`。

## 风险与缓解

- 风险：通用层抽象过度导致落地缓慢  
  缓解：优先最小接口，先满足 `mossclaw` 当前路径，再逐步泛化。

- 风险：并发引入后出现竞态问题  
  缓解：先在 P0 建立严格单测和 race-sensitive 用例。

- 风险：现有行为回归  
  缓解：每个 Phase 都做集成回归并独立提交，降低回滚成本。
