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
	"sync"

	"github.com/mossagi/moss/kernel/loop"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
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

// Gateway 是嵌入式消息网关，组合 Channel → Router → Kernel。
type Gateway struct {
	kernel   Kernel
	router   *session.Router
	channels []port.Channel
	config   Config
}

// New 创建 Gateway。
func New(k Kernel, router *session.Router, opts ...Option) *Gateway {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gateway{
		kernel: k,
		router: router,
		config: cfg,
	}
}

// AddChannel 注册一个消息通道。必须在 Serve 之前调用。
func (gw *Gateway) AddChannel(ch port.Channel) {
	gw.channels = append(gw.channels, ch)
}

// Serve 启动 Gateway 消息分发循环。
// 阻塞运行，直到 ctx 取消或所有 Channel 关闭。
func (gw *Gateway) Serve(ctx context.Context) error {
	if len(gw.channels) == 0 {
		return fmt.Errorf("gateway: no channels registered")
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
			go func(m inboundWithChannel) {
				defer wg.Done()
				gw.handleMessage(ctx, m)
			}(msg)
		}
	}
}

// inboundWithChannel 关联入站消息和来源 Channel。
type inboundWithChannel struct {
	msg port.InboundMessage
	ch  port.Channel
}

// fanIn 将多个 Channel 的 Receive 合并到一个 Go channel。
func (gw *Gateway) fanIn(ctx context.Context) <-chan inboundWithChannel {
	merged := make(chan inboundWithChannel)
	var wg sync.WaitGroup

	for _, ch := range gw.channels {
		wg.Add(1)
		go func(c port.Channel) {
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

	// 1. 路由到 Session
	sess, err := gw.router.Resolve(ctx, msg.ChannelName, msg.SenderID, msg.SessionHint)
	if err != nil {
		gw.onError(fmt.Errorf("route message from %s/%s: %w", msg.ChannelName, msg.SenderID, err))
		return
	}

	// 2. 追加用户消息
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: msg.Content})

	// 3. 运行 Agent Loop
	result, err := gw.kernel.Run(ctx, sess)
	if err != nil {
		gw.onError(fmt.Errorf("run session %s: %w", sess.ID, err))
		return
	}

	// 4. 回复到原始 Channel
	if result.Output != "" {
		outMsg := port.OutboundMessage{
			To:      msg.SenderID,
			Content: result.Output,
		}
		if err := m.ch.Send(ctx, outMsg); err != nil {
			gw.onError(fmt.Errorf("send reply to %s/%s: %w", msg.ChannelName, msg.SenderID, err))
		}
	}
}

func (gw *Gateway) onError(err error) {
	if gw.config.OnError != nil {
		gw.config.OnError(err)
	}
}
