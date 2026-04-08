// Package gateway 实现嵌入式消息网关。
//
// Gateway 将多个 Channel 的入站消息汇聚（fan-in），通过 Router 路由到
// 正确的 Session，驱动 Kernel.Run()，最后将回复发回原始 Channel。
//
// 用法:
//
//	gw := gateway.New(k, router, gateway.WithSystemPrompt(sysPrompt))
//	gw.AddChannel(cliChannel)
//	gw.Serve(ctx)  // 阻塞运行直到 ctx 取消
package gateway

import (
	"context"
	"fmt"
	kchannel "github.com/mossagents/moss/kernel/channel"
	"github.com/mossagents/moss/kernel/loop"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"sync"
	"time"
)

// Kernel 是 Gateway 所需的 Kernel 子集接口。
// 使用接口而非直接依赖 *kernel.Kernel，便于测试。
type Kernel interface {
	NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error)
	Run(ctx context.Context, sess *session.Session) (*loop.SessionResult, error)
}

// Config 配置 Gateway 行为。
type Config struct {
	// SystemPrompt 为自动创建的 Session 注入的系统提示词。
	SystemPrompt string

	// OnError 可选的错误回调，默认打印到 stderr。
	OnError func(err error)

	// LaneQueue 用于按 lane 串行化任务执行；为空则使用默认实现。
	LaneQueue *LaneQueue

	// DeliveryQueue 用于可靠投递；为空且 DeliveryDir 非空时自动创建。
	DeliveryQueue *DeliveryQueue

	// DeliveryDir 配置可靠投递持久化目录。
	DeliveryDir string

	// TraceExtractor extracts distributed trace context from InboundMessage.Metadata
	// and returns an enriched context that is passed to kernel.Run.
	// When nil, the original context is used unchanged.
	// Use mossotel.MetadataExtractor() from contrib/telemetry/otel to enable
	// W3C TraceContext (traceparent / tracestate) propagation.
	TraceExtractor func(ctx context.Context, metadata map[string]any) context.Context
}

// Option 是 Gateway 的函数式配置选项。
type Option func(*Config)

// WithSystemPrompt 设置 Gateway 创建 Session 时使用的系统提示词。
func WithSystemPrompt(prompt string) Option {
	return func(c *Config) { c.SystemPrompt = prompt }
}

// WithOnError 设置错误回调。
func WithOnError(fn func(error)) Option {
	return func(c *Config) { c.OnError = fn }
}

// WithLaneQueue 注入自定义 LaneQueue。
func WithLaneQueue(q *LaneQueue) Option {
	return func(c *Config) { c.LaneQueue = q }
}

// WithDeliveryQueue 注入自定义 DeliveryQueue。
func WithDeliveryQueue(q *DeliveryQueue) Option {
	return func(c *Config) { c.DeliveryQueue = q }
}

// WithDeliveryDir 配置自动创建 DeliveryQueue 的持久化目录。
func WithDeliveryDir(dir string) Option {
	return func(c *Config) { c.DeliveryDir = dir }
}

// WithTraceExtractor sets a function that extracts distributed trace context from
// InboundMessage.Metadata into the context before kernel.Run is called.
// Use mossotel.MetadataExtractor() from contrib/telemetry/otel to enable W3C
// TraceContext propagation without adding OTEL as a direct gateway dependency.
func WithTraceExtractor(fn func(ctx context.Context, metadata map[string]any) context.Context) Option {
	return func(c *Config) { c.TraceExtractor = fn }
}

// Gateway 是嵌入式消息网关，组合 Channel → Router → Kernel。
type Gateway struct {
	kernel     Kernel
	router     *session.Router
	channels   []kchannel.Channel
	config     Config
	chanByName map[string]kchannel.Channel
}

// New 创建 Gateway。
func New(k Kernel, router *session.Router, opts ...Option) *Gateway {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gateway{
		kernel:     k,
		router:     router,
		config:     cfg,
		chanByName: make(map[string]kchannel.Channel),
	}
}

// AddChannel 注册一个消息通道。必须在 Serve 之前调用。
func (gw *Gateway) AddChannel(ch kchannel.Channel) {
	gw.channels = append(gw.channels, ch)
	gw.chanByName[ch.Name()] = ch
}

// Serve 启动 Gateway 消息分发循环。
// 阻塞运行，直到 ctx 取消或所有 Channel 关闭。
func (gw *Gateway) Serve(ctx context.Context) error {
	if len(gw.channels) == 0 {
		return fmt.Errorf("gateway: no channels registered")
	}

	if gw.config.LaneQueue == nil {
		gw.config.LaneQueue = NewLaneQueue()
	}
	if gw.config.DeliveryQueue == nil && gw.config.DeliveryDir != "" {
		dq, err := NewDeliveryQueue(gw.config.DeliveryDir, gw.sendOutbound)
		if err != nil {
			return err
		}
		gw.config.DeliveryQueue = dq
	}
	if gw.config.DeliveryQueue != nil {
		if err := gw.config.DeliveryQueue.Recover(ctx); err != nil {
			return err
		}
		if err := gw.config.DeliveryQueue.Start(ctx); err != nil {
			return err
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = gw.config.DeliveryQueue.Stop(stopCtx)
		}()
	}

	// Fan-in: 合并所有 Channel 的入站消息到一个 channel
	merged := gw.fanIn(ctx)

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()

		case msg, ok := <-merged:
			if !ok {
				// 所有 channel 关闭
				wg.Wait()
				return nil
			}
			wg.Add(1)
			lane := fmt.Sprintf("%s:%s", msg.msg.ChannelName, msg.msg.SenderID)
			gw.config.LaneQueue.Enqueue(ctx, lane, func(taskCtx context.Context) error {
				defer wg.Done()
				gw.handleMessage(taskCtx, msg)
				return nil
			})
		}
	}
}

// inboundWithChannel 关联入站消息和来源 Channel。
type inboundWithChannel struct {
	msg kchannel.InboundMessage
	ch  kchannel.Channel
}

// fanIn 将多个 Channel 的 Receive 合并到一个 Go channel。
func (gw *Gateway) fanIn(ctx context.Context) <-chan inboundWithChannel {
	merged := make(chan inboundWithChannel)
	var wg sync.WaitGroup

	for _, ch := range gw.channels {
		wg.Add(1)
		go func(c kchannel.Channel) {
			defer wg.Done()
			incoming := c.Receive(ctx)
			for msg := range incoming {
				select {
				case merged <- inboundWithChannel{msg: msg, ch: c}:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// 所有 channel 关闭后关闭 merged
	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}

// handleMessage 处理单条入站消息：路由 → Session → Run → 回复。
func (gw *Gateway) handleMessage(ctx context.Context, m inboundWithChannel) {
	msg := m.msg

	// Extract distributed trace context from message metadata, if configured.
	if gw.config.TraceExtractor != nil {
		ctx = gw.config.TraceExtractor(ctx, msg.Metadata)
	}

	// 1. 路由到 Session
	sess, err := gw.router.Resolve(ctx, msg.ChannelName, msg.SenderID, msg.SessionHint)
	if err != nil {
		gw.onError(fmt.Errorf("route message from %s/%s: %w", msg.ChannelName, msg.SenderID, err))
		return
	}

	// 2. 追加用户消息
	sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart(msg.Content)}})

	// 3. 运行 Agent Loop
	result, err := gw.kernel.Run(ctx, sess)
	if err != nil {
		gw.onError(fmt.Errorf("run session %s: %w", sess.ID, err))
		return
	}

	// 4. 回复到原始 Channel
	if result.Output != "" {
		if gw.config.DeliveryQueue != nil {
			err := gw.config.DeliveryQueue.Publish(OutboundMessage{
				Channel: msg.ChannelName,
				To:      msg.SenderID,
				Content: result.Output,
			})
			if err != nil {
				gw.onError(fmt.Errorf("queue reply to %s/%s: %w", msg.ChannelName, msg.SenderID, err))
			}
		} else {
			outMsg := kchannel.OutboundMessage{
				To:      msg.SenderID,
				Content: result.Output,
			}
			if err := m.ch.Send(ctx, outMsg); err != nil {
				gw.onError(fmt.Errorf("send reply to %s/%s: %w", msg.ChannelName, msg.SenderID, err))
			}
		}
	}
}

func (gw *Gateway) onError(err error) {
	if gw.config.OnError != nil {
		gw.config.OnError(err)
	}
}

func (gw *Gateway) sendOutbound(ctx context.Context, msg OutboundMessage) error {
	ch, ok := gw.chanByName[msg.Channel]
	if !ok {
		return fmt.Errorf("channel %q not found", msg.Channel)
	}
	return ch.Send(ctx, kchannel.OutboundMessage{
		To:       msg.To,
		Content:  msg.Content,
		Metadata: msg.Metadata,
	})
}
