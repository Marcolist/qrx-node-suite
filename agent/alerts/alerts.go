// Package alerts implements a small rule engine over Guardian/monitoring
// state. Persistence goes through the Store interface below (implemented by
// agent/storage.AlertStore against the alerts table) rather than a direct
// SQLite import, and firing publishes alert.created/alert.resolved events
// (docs/architecture.md, section 14) through the Publisher interface
// (agent/events.Bus). Neither dependency is imported directly here, which
// keeps Engine trivially testable with fakes and avoids a
// storage<->alerts<->events import cycle.
package alerts

import (
	"context"
	"time"

	"qrx-node-suite/agent/models"
)

// Rule evaluates a snapshot of Agent state and decides whether an alert
// condition currently holds. Implementations should be cheap and
// side-effect-free -- Engine handles debouncing, persistence, and
// create/resolve transitions.
type Rule struct {
	ID       string
	Severity models.AlertSeverity
	Title    string
	// Evaluate returns (firing, message). message is used as the Alert's
	// Message when firing; ignored otherwise.
	Evaluate func(s Snapshot) (firing bool, message string)
}

// Snapshot is everything a Rule can look at. Kept as plain fields (not an
// interface) so rules stay simple pure functions.
type Snapshot struct {
	Health          models.HealthState
	System          models.SystemStatus
	Node            models.NodeStatus
	UpdateAvailable map[string]bool // component -> update_available, from agent/updates.CheckResult
}

// DefaultRules returns a starter set covering the most common operator
// concerns. Operators can add more via Engine.AddRule.
func DefaultRules() []Rule {
	return []Rule{
		{
			ID: "node_offline", Severity: models.SeverityCritical, Title: "QRX node offline",
			Evaluate: func(s Snapshot) (bool, string) {
				if s.Health == models.HealthOffline {
					return true, "QRX Core process is not running or has not responded in the configured offline threshold."
				}
				return false, ""
			},
		},
		{
			ID: "node_unhealthy", Severity: models.SeverityWarning, Title: "QRX node unhealthy",
			Evaluate: func(s Snapshot) (bool, string) {
				if s.Health == models.HealthUnhealthy {
					return true, "QRX node is running but reporting unhealthy (adapter cannot reach it, or stale for longer than the configured threshold)."
				}
				return false, ""
			},
		},
		{
			ID: "disk_low", Severity: models.SeverityWarning, Title: "Disk space low",
			Evaluate: func(s Snapshot) (bool, string) {
				if !s.System.DiskTotalBytes.Ok() || !s.System.DiskFreeBytes.Ok() || s.System.DiskTotalBytes.Value == 0 {
					return false, ""
				}
				pctFree := float64(s.System.DiskFreeBytes.Value) / float64(s.System.DiskTotalBytes.Value) * 100
				if pctFree < 10 {
					return true, "Free disk space is below 10%."
				}
				return false, ""
			},
		},
		{
			ID: "cpu_sustained_high", Severity: models.SeverityWarning, Title: "Sustained high CPU usage",
			Evaluate: func(s Snapshot) (bool, string) {
				if s.System.CPUUsagePercent.Ok() && s.System.CPUUsagePercent.Value > 90 {
					return true, "CPU usage above 90%."
				}
				return false, ""
			},
		},
		{
			ID: "throttled", Severity: models.SeverityWarning, Title: "System throttling",
			Evaluate: func(s Snapshot) (bool, string) {
				if s.System.Throttled.Ok() && s.System.Throttled.Value {
					return true, "Host reports active throttling (undervoltage or thermal) -- see docs/raspberry-pi.md."
				}
				return false, ""
			},
		},
	}
}

// Store is the persistence boundary alerts needs; agent/storage provides an
// implementation once wired up in cmd/agentd (kept as an interface here so
// Engine has no direct SQLite/agent-storage import, avoiding a dependency
// cycle risk and keeping this package trivially testable with a fake).
type Store interface {
	Create(ctx context.Context, a models.Alert) (models.Alert, error)
	ResolveByRule(ctx context.Context, ruleID string, resolvedAt time.Time) error
	OpenByRule(ctx context.Context, ruleID string) (models.Alert, bool, error)
}

// Publisher is the event-bus boundary (agent/events.Bus satisfies this).
type Publisher interface {
	Publish(e models.Event)
}

// Engine evaluates rules against a Snapshot and manages
// create-on-first-fire / resolve-on-stop-firing transitions, so a rule
// firing on every tick doesn't create a new alert every tick.
type Engine struct {
	rules []Rule
	store Store
	bus   Publisher
}

func NewEngine(store Store, bus Publisher, rules []Rule) *Engine {
	return &Engine{rules: rules, store: store, bus: bus}
}

// AddRule registers an additional rule.
func (e *Engine) AddRule(r Rule) { e.rules = append(e.rules, r) }

// Evaluate runs every rule against s. For each: if it newly fires (wasn't
// already open), create+persist+publish alert.created; if it was firing and
// no longer is, resolve+publish alert.resolved. Rules that keep firing
// across ticks do not create duplicate alerts.
func (e *Engine) Evaluate(ctx context.Context, s Snapshot) error {
	now := time.Now().UTC()
	for _, rule := range e.rules {
		firing, message := rule.Evaluate(s)
		existing, open, err := e.store.OpenByRule(ctx, rule.ID)
		if err != nil {
			return err
		}
		switch {
		case firing && !open:
			a := models.Alert{RuleID: rule.ID, Severity: rule.Severity, Title: rule.Title, Message: message, CreatedAt: now}
			created, err := e.store.Create(ctx, a)
			if err != nil {
				return err
			}
			if e.bus != nil {
				e.bus.Publish(models.Event{Type: models.EventAlertCreated, Timestamp: now, Data: created})
			}
		case !firing && open:
			if err := e.store.ResolveByRule(ctx, rule.ID, now); err != nil {
				return err
			}
			if e.bus != nil {
				existing.Resolved = true
				existing.ResolvedAt = &now
				e.bus.Publish(models.Event{Type: models.EventAlertResolved, Timestamp: now, Data: existing})
			}
		}
	}
	return nil
}
