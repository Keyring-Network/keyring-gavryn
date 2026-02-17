package events

import (
	"context"
	"strings"
	"sync"
)

type RunEvent struct {
	RunID   string         `json:"run_id"`
	Seq     int64          `json:"seq"`
	Type    string         `json:"type"`
	Ts      string         `json:"ts"`
	Source  string         `json:"source"`
	TraceID string         `json:"trace_id,omitempty"`
	Payload map[string]any `json:"payload"`
}

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan RunEvent]struct{}
}

func NormalizeType(eventType string) string {
	return strings.TrimSpace(strings.ToLower(eventType))
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: map[string]map[chan RunEvent]struct{}{},
	}
}

func (b *Broker) Subscribe(ctx context.Context, runID string) <-chan RunEvent {
	ch := make(chan RunEvent, 16)

	b.mu.Lock()
	if b.subscribers[runID] == nil {
		b.subscribers[runID] = map[chan RunEvent]struct{}{}
	}
	b.subscribers[runID][ch] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if b.subscribers[runID] != nil {
			delete(b.subscribers[runID], ch)
			if len(b.subscribers[runID]) == 0 {
				delete(b.subscribers, runID)
			}
		}
		b.mu.Unlock()
		close(ch)
	}()

	return ch
}

func (b *Broker) Publish(event RunEvent) {
	b.mu.RLock()
	subscribers := b.subscribers[event.RunID]
	chans := make([]chan RunEvent, 0, len(subscribers))
	for ch := range subscribers {
		chans = append(chans, ch)
	}
	b.mu.RUnlock()

	for _, ch := range chans {
		select {
		case ch <- event:
		default:
		}
	}
}
