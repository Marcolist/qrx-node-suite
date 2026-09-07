// Package logging provides the Agent's one structured logger
// (docs/architecture.md, principle 10: "structured logging"), a thin
// wrapper over the standard library's log/slog so every subsystem logs in
// the same shape without pulling in a logging dependency (ADR-001).
package logging

import (
	"context"
	"log/slog"
	"os"
)

// New builds the Agent's root logger. Format is "json" (production,
// machine-parseable) or anything else for human-readable text (development).
// level is one of "debug"/"info"/"warn"/"error" (default "info").
func New(format, level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// component returns a logger tagged with a "component" field, so every log
// line can be filtered/grouped by subsystem (guardian, adapters, updates, ...).
func Component(base *slog.Logger, name string) *slog.Logger {
	return base.With(slog.String("component", name))
}

// FromContext returns the logger attached to ctx by WithContext, or a
// no-output discard logger if none was attached -- callers deep in a call
// stack can always safely log without threading a *slog.Logger parameter
// through every function signature.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// WithContext attaches a logger to ctx for FromContext to retrieve.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

type ctxKey struct{}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
