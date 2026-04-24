package events

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_SubscribePublish(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	received := make([]string, 0)
	var mu sync.Mutex

	handler := func(ev any) {
		mu.Lock()
		received = append(received, ev.(string))
		mu.Unlock()
		wg.Done()
	}

	bus.Subscribe(handler)
	bus.Subscribe(handler)

	bus.Publish("hello")

	// Wait for delivery with timeout
	c := make(chan struct{})
	go func() {
		wg.Wait()
		c <- struct{}{}
	}()

	select {
	case <-c:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	if len(received) != 2 {
		t.Errorf("expected 2 received events, got %d", len(received))
	}
	if received[0] != "hello" || received[1] != "hello" {
		t.Errorf("received wrong events: %v", received)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	var count int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	handler := func(ev any) {
		mu.Lock()
		count++
		mu.Unlock()
		wg.Done()
	}

	unsub := bus.Subscribe(handler)
	bus.Publish("event 1")

	// Wait for delivery with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event delivery")
	}

	unsub()
	bus.Publish("event 2")

	// No sleep needed: unsub() is synchronous with the subscriber map, 
	// and Publish() is also synchronous regarding its iteration over that map.
	// Once unsub() returns, Publish() called afterwards will not see the subscriber.

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Errorf("expected 1 event after unsubscribe, got %d", got)
	}
}

