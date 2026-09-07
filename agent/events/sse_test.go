package events_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/models"
)

func TestServeSSEOmitsNamedEventField(t *testing.T) {
	// Regression test: a named "event: <type>" SSE field makes a browser
	// EventSource's onmessage/.addEventListener('message', ...) never fire
	// for it (only the default unnamed event type does). The type must
	// travel in the JSON body's "type" field only.
	bus := events.NewBus(4)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		bus.ServeSSE(rec, req)
		close(done)
	}()

	// Give ServeSSE a moment to subscribe, then publish and let it flow.
	time.Sleep(20 * time.Millisecond)
	bus.Publish(models.Event{Type: models.EventNodeOnline, Timestamp: time.Now()})
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if strings.Contains(body, "event: ") {
		t.Errorf("SSE output must not contain a named \"event:\" field, got:\n%s", body)
	}
	if !strings.Contains(body, `"type":"node.online"`) {
		t.Errorf("expected the event type in the JSON data body, got:\n%s", body)
	}
	if !strings.HasPrefix(strings.TrimLeft(body, "\n"), "data: ") {
		t.Errorf("expected output to start with a \"data: \" line, got:\n%s", body)
	}
}

// TestServeSSERefusesOverMaxSubscribers is the F10 fix (external security
// audit): once MaxSubscribers connections are already open, a new one
// must get a clean error response, not a 200 that hangs forever with no
// events -- and must never register as a subscriber (SubscriberCount must
// not exceed the cap).
func TestServeSSERefusesOverMaxSubscribers(t *testing.T) {
	bus := events.NewBus(4)
	bus.MaxSubscribers = 1

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx1)
	rec1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		bus.ServeSSE(rec1, req1)
		close(done1)
	}()
	time.Sleep(20 * time.Millisecond) // let the first connection register

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec2 := httptest.NewRecorder()
	bus.ServeSSE(rec2, req2) // no context to cancel -- must return on its own

	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("2nd connection status = %d, want %d", rec2.Code, http.StatusServiceUnavailable)
	}
	if bus.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1 -- the refused connection must not register", bus.SubscriberCount())
	}

	cancel1()
	<-done1
}
