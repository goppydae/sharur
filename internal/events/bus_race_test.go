package events

import (
	"sync"
	"testing"
	"time"
)

// TestEventBus_ConcurrentPublishClose verifies that concurrent Publish and Close calls
// do not panic (previously a race could send on a closed channel).
func TestEventBus_ConcurrentPublishClose(t *testing.T) {
	for range 50 {
		bus := NewEventBus()
		bus.Subscribe(func(any) {})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for range 100 {
				bus.Publish("event")
			}
		}()

		go func() {
			defer wg.Done()
			bus.Close()
		}()

		wg.Wait()
	}
}

// TestEventBus_PublishAfterClose verifies that publishing to a closed bus is a no-op.
func TestEventBus_PublishAfterClose(t *testing.T) {
	bus := NewEventBus()
	bus.Close()
	// Must not panic.
	bus.Publish("should be dropped")
}

// TestEventBus_SlowSubscriberDoesNotBlock verifies that a subscriber whose channel is
// full does not stall the publisher (events are dropped rather than blocking).
func TestEventBus_SlowSubscriberDoesNotBlock(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	// Subscribe but never drain — channel will fill up.
	blockCh := make(chan any, 1)
	bus.Subscribe(func(e any) {
		blockCh <- e // blocks after the first event
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range subscriberChanSize + 100 {
			bus.Publish("flood")
		}
	}()

	select {
	case <-done:
		// good — publisher finished without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber channel")
	}
}

// TestEventBus_ConcurrentPublishers verifies multiple goroutines can publish concurrently.
func TestEventBus_ConcurrentPublishers(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	const goroutines = 20
	const eventsEach = 50

	var received int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(goroutines * eventsEach)

	bus.Subscribe(func(any) {
		mu.Lock()
		received++
		mu.Unlock()
		wg.Done()
	})

	for range goroutines {
		go func() {
			for range eventsEach {
				bus.Publish("x")
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out; received %d of %d events", received, goroutines*eventsEach)
	}
}

// TestEventBus_UnsubscribeDuringPublish verifies that unsubscribing while events are
// being published does not deadlock or panic.
func TestEventBus_UnsubscribeDuringPublish(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	unsub := bus.Subscribe(func(any) {})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 200 {
			bus.Publish("e")
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond)
		unsub()
	}()

	wg.Wait()
}
