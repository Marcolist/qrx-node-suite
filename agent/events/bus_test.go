package events_test

import (
	"testing"
	"time"

	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/models"
)

func TestPublishDeliversToSubscriber(t *testing.T) {
	bus := events.NewBus(4)
	_, ch := bus.Subscribe()

	bus.Publish(models.Event{Type: models.EventNodeOnline})

	select {
	case e := <-ch:
		if e.Type != models.EventNodeOnline {
			t.Errorf("event type = %v, want node.online", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	bus := events.NewBus(4)
	id, ch := bus.Subscribe()
	bus.Unsubscribe(id)

	bus.Publish(models.Event{Type: models.EventNodeOffline})

	select {
	case _, open := <-ch:
		if open {
			t.Error("expected channel to be closed after Unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected channel to be closed promptly after Unsubscribe")
	}
}

func TestPublishNeverBlocksOnFullSubscriberBuffer(t *testing.T) {
	bus := events.NewBus(1)
	_, ch := bus.Subscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			bus.Publish(models.Event{Type: models.EventSystemCPU})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full, unread subscriber buffer")
	}
	<-ch // drain one, just to touch the channel
}

func TestSubscriberCount(t *testing.T) {
	bus := events.NewBus(4)
	if bus.SubscriberCount() != 0 {
		t.Fatalf("SubscriberCount() = %d, want 0", bus.SubscriberCount())
	}
	id1, _ := bus.Subscribe()
	_, _ = bus.Subscribe()
	if bus.SubscriberCount() != 2 {
		t.Fatalf("SubscriberCount() = %d, want 2", bus.SubscriberCount())
	}
	bus.Unsubscribe(id1)
	if bus.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1", bus.SubscriberCount())
	}
}

// TestTrySubscribeRespectsMaxSubscribers is the F10 fix (external security
// audit: "no cap on concurrent SSE connections") -- TrySubscribe must
// refuse once MaxSubscribers are already registered, and start accepting
// again once one unsubscribes.
func TestTrySubscribeRespectsMaxSubscribers(t *testing.T) {
	bus := events.NewBus(4)
	bus.MaxSubscribers = 2

	id1, _, ok := bus.TrySubscribe()
	if !ok {
		t.Fatal("1st TrySubscribe should succeed (0 of 2)")
	}
	if _, _, ok := bus.TrySubscribe(); !ok {
		t.Fatal("2nd TrySubscribe should succeed (1 of 2)")
	}
	if _, _, ok := bus.TrySubscribe(); ok {
		t.Fatal("3rd TrySubscribe should be refused -- MaxSubscribers (2) already registered")
	}
	if bus.SubscriberCount() != 2 {
		t.Fatalf("SubscriberCount() = %d, want 2 (the refused attempt must not register)", bus.SubscriberCount())
	}

	bus.Unsubscribe(id1)
	if _, _, ok := bus.TrySubscribe(); !ok {
		t.Fatal("TrySubscribe should succeed again after a slot freed up")
	}
}

// TestSubscribeIgnoresMaxSubscribers confirms MaxSubscribers only bounds
// TrySubscribe (the externally-reachable SSE HTTP path), never Subscribe
// itself (in-process consumers: guardian, alerts) -- see MaxSubscribers'
// doc comment for why.
func TestSubscribeIgnoresMaxSubscribers(t *testing.T) {
	bus := events.NewBus(4)
	bus.MaxSubscribers = 1

	bus.Subscribe()
	bus.Subscribe()
	bus.Subscribe()
	if bus.SubscriberCount() != 3 {
		t.Fatalf("SubscriberCount() = %d, want 3 -- Subscribe must not be capped by MaxSubscribers", bus.SubscriberCount())
	}
}

func TestMaxSubscribersZeroMeansUnlimited(t *testing.T) {
	bus := events.NewBus(4)
	for i := 0; i < 50; i++ {
		if _, _, ok := bus.TrySubscribe(); !ok {
			t.Fatalf("TrySubscribe refused at subscriber %d with MaxSubscribers unset (0) -- should be unlimited", i)
		}
	}
}
