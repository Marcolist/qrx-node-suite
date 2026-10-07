package main

import (
	"context"
	"log/slog"
	"time"

	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/models"
)

// Health changes and actual recovery actions must be distinguishable. A
// transient DEGRADED state or a return to HEALTHY is not a service restart.
func newGuardian(cfg guardian.Config, restart func(context.Context) error, bus *events.Bus, logger *slog.Logger) *guardian.Guardian {
	return guardian.New(cfg, func(ctx context.Context, reason string) error {
		logger.Warn("guardian recovery requested", "service", "qrxd.service", "reason", reason)
		if err := restart(ctx); err != nil {
			logger.Error("guardian recovery failed", "service", "qrxd.service", "reason", reason, "error", err)
			return err
		}
		logger.Info("guardian recovery completed", "service", "qrxd.service", "reason", reason)
		bus.Publish(models.Event{Type: models.EventServiceRestarted, Timestamp: time.Now(), Data: map[string]string{"service": "qrxd.service", "reason": reason}})
		return nil
	}, func(old, new models.HealthState) {
		logger.Info("guardian state transition", "from", old, "to", new)
		bus.Publish(models.Event{Type: models.EventNodeHealthChanged, Timestamp: time.Now(), Data: map[string]string{"from": string(old), "to": string(new)}})
	})
}
