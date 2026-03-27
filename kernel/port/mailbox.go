package port

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MailMessage 是代理间异步通信的最小消息模型。
type MailMessage struct {
	ID         string         `json:"id"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	Subject    string         `json:"subject,omitempty"`
	Content    string         `json:"content"`
	RequestID  string         `json:"request_id,omitempty"`
	InReplyTo  string         `json:"in_reply_to,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	ReceivedAt time.Time      `json:"received_at"`
}

// Mailbox 提供异步消息收发能力。
type Mailbox interface {
	Send(ctx context.Context, msg MailMessage) (string, error)
	Read(ctx context.Context, owner string, limit int) ([]MailMessage, error)
}

// ErrMailboxOwnerRequired 表示未提供 mailbox owner。
var ErrMailboxOwnerRequired = errors.New("mailbox owner is required")

// MemoryMailbox 是内存实现；Read 为出队语义。
type MemoryMailbox struct {
	mu       sync.Mutex
	seq      int64
	messages map[string][]MailMessage
}

func NewMemoryMailbox() *MemoryMailbox {
	return &MemoryMailbox{
		messages: make(map[string][]MailMessage),
	}
}

func (m *MemoryMailbox) Send(_ context.Context, msg MailMessage) (string, error) {
	if msg.To == "" {
		return "", ErrMailboxOwnerRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("mail-%d", m.seq)
	}
	msg.ReceivedAt = time.Now()
	if msg.Metadata != nil {
		cp := make(map[string]any, len(msg.Metadata))
		for k, v := range msg.Metadata {
			cp[k] = v
		}
		msg.Metadata = cp
	}
	m.messages[msg.To] = append(m.messages[msg.To], msg)
	return msg.ID, nil
}

func (m *MemoryMailbox) Read(_ context.Context, owner string, limit int) ([]MailMessage, error) {
	if owner == "" {
		return nil, ErrMailboxOwnerRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	queue := m.messages[owner]
	if len(queue) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > len(queue) {
		limit = len(queue)
	}
	out := make([]MailMessage, limit)
	for i := 0; i < limit; i++ {
		msg := queue[i]
		if msg.Metadata != nil {
			cp := make(map[string]any, len(msg.Metadata))
			for k, v := range msg.Metadata {
				cp[k] = v
			}
			msg.Metadata = cp
		}
		out[i] = msg
	}
	remain := append([]MailMessage(nil), queue[limit:]...)
	m.messages[owner] = remain
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReceivedAt.Before(out[j].ReceivedAt)
	})
	return out, nil
}

