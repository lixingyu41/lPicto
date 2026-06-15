package events

import (
	"context"
	"sync"
)

type Event struct {
	Type    string
	Payload any
}

type Bus struct {
	mu          sync.Mutex
	subscribers map[chan Event]struct{}
}

func NewBus() *Bus {
	return &Bus{subscribers: map[chan Event]struct{}{}}
}

func (b *Bus) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 32)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subscribers, ch)
		b.mu.Unlock()
		close(ch)
	}()
	return ch
}

func (b *Bus) Publish(event Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
