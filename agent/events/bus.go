// Package events implements the Agent's in-process event bus and its SSE
// fan-out (docs/architecture.md step 4: state changes are published here,
// then both the dashboard's event stream and agent/guardian/agent/alerts
// consume them). One direction only -- see docs, "SSE is preferred unless
// bidirectional communication is needed."
package events

import (
	"sync"

	"qrx-node-suite/agent/models"
)

// Bus fans out published events to every current subscriber. A slow or
// gone subscriber never blocks Publish or other subscribers: each
// subscriber channel is buffered, and a full channel simply drops the
// oldest-pending event for that subscriber rather than blocking the
// publisher (this is monitoring data, not a delivery-guaranteed queue).
type Bus struct {
	mu      sync.RWMutex
	subs    map[int64]chan models.Event
	nextID  int64
	bufSize int
}

// NewBus builds an event bus. bufSize is the per-subscriber channel buffer
// (defaults to 32 if <= 0).
func NewBus(bufSize int) *Bus {
	if bufSize <= 0 {
		bufSize = 32
	}
	return &Bus{subs: make(map[int64]chan models.Event), bufSize: bufSize}
}

// Publish delivers an event to every current subscriber, non-blocking.
func (b *Bus) Publish(e models.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			// Subscriber's buffer is full -- drop for that subscriber
			// rather than block the publisher or other subscribers.
		}
	}
}

// Subscribe registers a new subscriber and returns its id (for
// Unsubscribe) and receive channel.
func (b *Bus) Subscribe() (int64, <-chan models.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan models.Event, b.bufSize)
	b.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Bus) Unsubscribe(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		close(ch)
		delete(b.subs, id)
	}
}

// SubscriberCount reports how many subscribers are currently registered
// (used by /health and /api/v1/status for observability).
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
