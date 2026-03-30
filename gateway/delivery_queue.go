package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/port"
)

type OutboundMessage struct {
	MessageID string         `json:"message_id"`
	Channel   string         `json:"channel"`
	To        string         `json:"to"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type RetryPolicy interface {
	ShouldRetry(error) bool
	NextDelay(attempt int) time.Duration
}

type ExponentialRetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (p ExponentialRetryPolicy) ShouldRetry(err error) bool {
	return err != nil
}

func (p ExponentialRetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	base := p.BaseDelay
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	if p.MaxDelay > 0 && d > p.MaxDelay {
		return p.MaxDelay
	}
	return d
}

type persistentEvent struct {
	Type        string         `json:"type"`
	MessageID   string         `json:"message_id"`
	Channel     string         `json:"channel"`
	To          string         `json:"to"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Attempt     int            `json:"attempt,omitempty"`
	NextRetryAt string         `json:"next_retry_at,omitempty"`
	LastError   string         `json:"last_error,omitempty"`
	CreatedAt   string         `json:"created_at"`
}

type messageState struct {
	msg         OutboundMessage
	attempt     int
	nextRetryAt time.Time
	terminal    bool
}

type DeliveryQueue struct {
	mu         sync.Mutex
	queue      []messageState
	sender     func(context.Context, OutboundMessage) error
	policy     ExponentialRetryPolicy
	queuePath  string
	dlqPath    string
	started    bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
	wakeCh     chan struct{}
	recovered  bool
}

func NewDeliveryQueue(baseDir string, sender func(context.Context, OutboundMessage) error) (*DeliveryQueue, error) {
	if sender == nil {
		return nil, fmt.Errorf("delivery sender is nil")
	}
	if baseDir == "" {
		return nil, fmt.Errorf("delivery base dir is empty")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create delivery dir: %w", err)
	}
	return &DeliveryQueue{
		sender:    sender,
		policy:    ExponentialRetryPolicy{MaxAttempts: 5, BaseDelay: 200 * time.Millisecond, MaxDelay: 5 * time.Second},
		queuePath: filepath.Join(baseDir, "queue.jsonl"),
		dlqPath:   filepath.Join(baseDir, "deadletter.jsonl"),
		stopCh:    make(chan struct{}),
		wakeCh:    make(chan struct{}, 1),
	}, nil
}

func (dq *DeliveryQueue) Recover(ctx context.Context) error {
	dq.mu.Lock()
	if dq.started {
		dq.mu.Unlock()
		return fmt.Errorf("recover must run before start")
	}
	if dq.recovered {
		dq.mu.Unlock()
		return fmt.Errorf("recover already executed")
	}
	dq.mu.Unlock()

	states := make(map[string]*messageState)
	if err := readJSONL(dq.queuePath, func(ev persistentEvent) error {
		st := states[ev.MessageID]
		if st == nil {
			st = &messageState{
				msg: OutboundMessage{
					MessageID: ev.MessageID,
					Channel:   ev.Channel,
					To:        ev.To,
					Content:   ev.Content,
					Metadata:  ev.Metadata,
				},
				attempt: ev.Attempt,
			}
			states[ev.MessageID] = st
		}
		switch ev.Type {
		case "delivered", "deadlettered":
			st.terminal = true
		case "attempted":
			if ev.Attempt > st.attempt {
				st.attempt = ev.Attempt
			}
			if ev.NextRetryAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, ev.NextRetryAt); err == nil {
					st.nextRetryAt = t
				}
			}
		case "enqueued":
			// keep latest payload fields already copied above
		}
		return nil
	}); err != nil {
		return err
	}

	dq.mu.Lock()
	defer dq.mu.Unlock()
	for _, st := range states {
		if st.terminal {
			continue
		}
		if st.msg.MessageID == "" {
			st.msg.MessageID = uuid.NewString()
		}
		dq.queue = append(dq.queue, *st)
	}
	dq.recovered = true
	return ctx.Err()
}

func (dq *DeliveryQueue) Start(ctx context.Context) error {
	dq.mu.Lock()
	if dq.started {
		dq.mu.Unlock()
		return nil
	}
	dq.started = true
	dq.wg.Add(1)
	dq.mu.Unlock()

	go dq.worker(ctx)
	return nil
}

func (dq *DeliveryQueue) Stop(ctx context.Context) error {
	dq.mu.Lock()
	if !dq.started {
		dq.mu.Unlock()
		return nil
	}
	close(dq.stopCh)
	dq.mu.Unlock()

	done := make(chan struct{})
	go func() {
		dq.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (dq *DeliveryQueue) Publish(msg OutboundMessage) error {
	if msg.MessageID == "" {
		msg.MessageID = uuid.NewString()
	}
	ev := persistentEvent{
		Type:      "enqueued",
		MessageID: msg.MessageID,
		Channel:   msg.Channel,
		To:        msg.To,
		Content:   msg.Content,
		Metadata:  msg.Metadata,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := appendJSONL(dq.queuePath, ev); err != nil {
		return err
	}
	dq.mu.Lock()
	dq.queue = append(dq.queue, messageState{msg: msg})
	dq.mu.Unlock()
	dq.wake()
	return nil
}

func (dq *DeliveryQueue) worker(ctx context.Context) {
	defer dq.wg.Done()
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-dq.stopCh:
			return
		case <-dq.wakeCh:
		case <-timer.C:
		}
		wait := dq.processOne(ctx)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if wait <= 0 {
			wait = 50 * time.Millisecond
		}
		timer.Reset(wait)
	}
}

func (dq *DeliveryQueue) processOne(ctx context.Context) time.Duration {
	dq.mu.Lock()
	if len(dq.queue) == 0 {
		dq.mu.Unlock()
		return 200 * time.Millisecond
	}
	// pick first ready item
	idx := -1
	now := time.Now()
	nextDelay := 200 * time.Millisecond
	for i := range dq.queue {
		if dq.queue[i].nextRetryAt.IsZero() || !dq.queue[i].nextRetryAt.After(now) {
			idx = i
			break
		}
		delay := dq.queue[i].nextRetryAt.Sub(now)
		if delay > 0 && delay < nextDelay {
			nextDelay = delay
		}
	}
	if idx == -1 {
		dq.mu.Unlock()
		return nextDelay
	}
	item := dq.queue[idx]
	dq.queue = append(dq.queue[:idx], dq.queue[idx+1:]...)
	dq.mu.Unlock()

	err := dq.sender(ctx, item.msg)
	if err == nil {
		_ = appendJSONL(dq.queuePath, persistentEvent{
			Type:      "delivered",
			MessageID: item.msg.MessageID,
			Channel:   item.msg.Channel,
			To:        item.msg.To,
			Content:   item.msg.Content,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return 10 * time.Millisecond
	}

	item.attempt++
	if !dq.policy.ShouldRetry(err) || item.attempt >= dq.policy.MaxAttempts {
		_ = appendJSONL(dq.queuePath, persistentEvent{
			Type:      "deadlettered",
			MessageID: item.msg.MessageID,
			Channel:   item.msg.Channel,
			To:        item.msg.To,
			Content:   item.msg.Content,
			Attempt:   item.attempt,
			LastError: err.Error(),
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		_ = appendJSONL(dq.dlqPath, persistentEvent{
			Type:      "deadlettered",
			MessageID: item.msg.MessageID,
			Channel:   item.msg.Channel,
			To:        item.msg.To,
			Content:   item.msg.Content,
			Attempt:   item.attempt,
			LastError: err.Error(),
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return 10 * time.Millisecond
	}

	delay := dq.policy.NextDelay(item.attempt)
	item.nextRetryAt = time.Now().Add(delay)
	_ = appendJSONL(dq.queuePath, persistentEvent{
		Type:        "attempted",
		MessageID:   item.msg.MessageID,
		Channel:     item.msg.Channel,
		To:          item.msg.To,
		Content:     item.msg.Content,
		Attempt:     item.attempt,
		NextRetryAt: item.nextRetryAt.UTC().Format(time.RFC3339Nano),
		LastError:   err.Error(),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	})
	dq.mu.Lock()
	dq.queue = append(dq.queue, item)
	dq.mu.Unlock()
	return delay
}

func (dq *DeliveryQueue) wake() {
	select {
	case dq.wakeCh <- struct{}{}:
	default:
	}
}

func appendJSONL(path string, ev persistentEvent) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := enc.Encode(ev); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return nil
}

func readJSONL(path string, fn func(persistentEvent) error) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev persistentEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func senderForChannel(ch port.Channel) func(context.Context, OutboundMessage) error {
	return func(ctx context.Context, msg OutboundMessage) error {
		return ch.Send(ctx, port.OutboundMessage{
			To:       msg.To,
			Content:  msg.Content,
			Metadata: msg.Metadata,
		})
	}
}

