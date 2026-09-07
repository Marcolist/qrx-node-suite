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
