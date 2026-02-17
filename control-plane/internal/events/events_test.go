package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

func receiveEvent(t *testing.T, ch <-chan RunEvent) RunEvent {
	t.Helper()

	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before receive")
		}
		return ev
	case <-timer.C:
		t.Fatal("timed out waiting for event")
	}

	return RunEvent{}
}

func waitForClosed(t *testing.T, ch <-chan RunEvent) {
	t.Helper()

	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for channel close")
		}
	}
}

func TestNewBroker(t *testing.T) {
	b := NewBroker()
	if b == nil {
		t.Fatal("expected broker")
	}
	if b.subscribers == nil {
		t.Fatal("expected subscribers map")
	}
}

func TestSubscribe_Single(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, "run-1")
	if ch == nil {
		t.Fatal("expected channel")
	}

	b.mu.RLock()
	count := len(b.subscribers["run-1"])
	b.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}

	cancel()
	waitForClosed(t, ch)

	b.mu.RLock()
	_, exists := b.subscribers["run-1"]
	b.mu.RUnlock()
	if exists {
		t.Fatal("subscriber not removed")
	}
}

func TestSubscribe_MultipleSameRun(t *testing.T) {
	b := NewBroker()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	ch1 := b.Subscribe(ctx1, "run-1")
	ch2 := b.Subscribe(ctx2, "run-1")
	if ch1 == ch2 {
		t.Fatal("expected distinct channels")
	}

	b.mu.RLock()
	subs := b.subscribers["run-1"]
	count := len(subs)
	b.mu.RUnlock()
	if count != 2 {
		t.Fatalf("expected 2 subscribers, got %d", count)
	}

	cancel1()
	cancel2()
	waitForClosed(t, ch1)
	waitForClosed(t, ch2)
}

func TestSubscribe_DifferentRuns(t *testing.T) {
	b := NewBroker()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	ch1 := b.Subscribe(ctx1, "run-1")
	ch2 := b.Subscribe(ctx2, "run-2")

	b.mu.RLock()
	_, ok1 := b.subscribers["run-1"]
	_, ok2 := b.subscribers["run-2"]
	count := len(b.subscribers)
	b.mu.RUnlock()

	if !ok1 || !ok2 {
		t.Fatal("subscribers not registered correctly")
	}
	if count != 2 {
		t.Fatalf("expected 2 run entries, got %d", count)
	}

	cancel1()
	cancel2()
	waitForClosed(t, ch1)
	waitForClosed(t, ch2)
}

func TestPublish_NoSubscribers(t *testing.T) {
	b := NewBroker()
	b.Publish(RunEvent{RunID: "run-1"})
}

func TestPublish_SingleSubscriber(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, "run-1")
	event := RunEvent{RunID: "run-1", Seq: 1, Type: "ready", Ts: "now", Source: "test"}

	b.Publish(event)
	received := receiveEvent(t, ch)
	if received.Type != event.Type || received.Seq != event.Seq {
		t.Fatalf("unexpected event: %+v", received)
	}

	for i := 0; i < 16; i++ {
		b.Publish(RunEvent{RunID: "run-1", Seq: int64(i + 2)})
	}
	if len(ch) != 16 {
		t.Fatalf("expected full buffer, got %d", len(ch))
	}
	b.Publish(RunEvent{RunID: "run-1", Seq: 18})
	if len(ch) != 16 {
		t.Fatalf("expected dropped event, got %d", len(ch))
	}

	cancel()
	waitForClosed(t, ch)
}

func TestPublish_MultipleSubscribers(t *testing.T) {
	b := NewBroker()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	ch1 := b.Subscribe(ctx1, "run-1")
	ch2 := b.Subscribe(ctx2, "run-1")

	event := RunEvent{RunID: "run-1", Seq: 1, Type: "fanout"}
	b.Publish(event)

	_ = receiveEvent(t, ch1)
	_ = receiveEvent(t, ch2)

	cancel1()
	cancel2()
	waitForClosed(t, ch1)
	waitForClosed(t, ch2)
}

func TestPublish_DifferentRuns(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, "run-2")
	b.Publish(RunEvent{RunID: "run-1", Seq: 1})

	select {
	case <-ch:
		t.Fatal("unexpected event for different run")
	default:
	}

	cancel()
	waitForClosed(t, ch)
}

func TestUnsubscribe_ContextCancellation(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())

	ch := b.Subscribe(ctx, "run-1")
	cancel()
	waitForClosed(t, ch)

	b.mu.RLock()
	_, exists := b.subscribers["run-1"]
	b.mu.RUnlock()
	if exists {
		t.Fatal("expected subscribers cleaned up")
	}
}

func TestConcurrent_SubscribePublish(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	chans := make([]<-chan RunEvent, 0, 32)

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			ch := b.Subscribe(ctx, "run-1")
			mu.Lock()
			chans = append(chans, ch)
			mu.Unlock()
			b.Publish(RunEvent{RunID: "run-1", Seq: int64(seq)})
		}(i)
	}

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			b.Publish(RunEvent{RunID: "run-1", Seq: int64(100 + seq)})
		}(i)
	}

	wg.Wait()
	cancel()

	for _, ch := range chans {
		waitForClosed(t, ch)
	}

	b.mu.RLock()
	count := len(b.subscribers)
	b.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected no subscribers, got %d", count)
	}
}
