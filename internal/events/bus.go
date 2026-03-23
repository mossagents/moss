package events

import "sync"

type Handler func(Event)

type Bus struct {
	mu          sync.RWMutex
	subscribers []Handler
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, h)
}

// Publish delivers the event synchronously to all subscribers in subscription order.
// Handlers should be fast; a slow handler will delay subsequent handlers.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := make([]Handler, len(b.subscribers))
	copy(handlers, b.subscribers)
	b.mu.RUnlock()
	for _, h := range handlers {
		h(e)
	}
}
