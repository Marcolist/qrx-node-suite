package main

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush lets SSE handlers (GET /api/v1/events) keep working through the
// middleware -- without forwarding http.Flusher, the wrapped
// ResponseWriter would silently stop flushing.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// NewLoggingMiddleware logs every API request: method, path, status,
// duration -- structured logging per docs/architecture.md principle 10.
func NewLoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}
