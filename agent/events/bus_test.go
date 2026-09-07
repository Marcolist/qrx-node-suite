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
