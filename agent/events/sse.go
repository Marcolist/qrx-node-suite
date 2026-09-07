package events

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ServeSSE streams the bus as Server-Sent Events on GET /api/v1/events
// (docs/architecture.md, section 14). One goroutine per connected client;
// exits cleanly when the client disconnects (r.Context().Done()).
//
// Deliberately does NOT set a per-event "event: <type>" SSE field: a
// browser EventSource's onmessage only fires for the default (unnamed)
// event type, so a named "event:" line would silently make every message
// invisible to addEventListener('message', ...) / .onmessage unless the
// client separately registered a listener per exact type name -- brittle
// against this project's still-growing event vocabulary
// (agent/models/event.go). The type is already in the JSON body's "type"
// field, which every consumer parses anyway.
func (b *Bus) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Checked (and, on success, registered) before writing any response
	// headers -- so a caller at the cap gets a normal error response
	// (Retry-After included) instead of a 200 that immediately hangs with
	// no events. GET /api/v1/events is intentionally unauthenticated
	// (docs/security.md's OTA/installer threat models cover the
	// authenticated surface; this is read-only status data), so nothing
	// upstream of this already limits how many connections one caller can
	// open -- MaxSubscribers is what stops an unbounded number of open
	// connections from holding this process's goroutines and memory open
	// indefinitely (F10 fix, external security audit).
	id, ch, ok := b.TrySubscribe()
	if !ok {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many event stream subscribers", http.StatusServiceUnavailable)
		return
	}
	defer b.Unsubscribe(id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
