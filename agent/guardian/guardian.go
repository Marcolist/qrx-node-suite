// Package guardian implements the QRX Guardian: a local health and
// recovery engine (docs/architecture.md's component list). Guardian only
// ever reacts to already-normalized state (agent/models, via signals fed to
// it) and only ever requests recovery through agent/services -- never by
// talking to QRX Core directly. See docs/guardian.md.
package guardian

import (
	"context"
	"sync"
	"time"

	"qrx-node-suite/agent/models"
)

// Signals is the input Guardian evaluates each tick. All of it comes from
// already-normalized sources (the active adapter's NodeStatus, the
// platform's ServiceStatus for qrxd) -- Guardian itself never polls QRX
// Core or a process table directly.
type Signals struct {
	QRXProcessRunning  bool
	NodeOnline         bool
	AdapterHealthy     bool
	LastSuccessfulPoll time.Time
}

// RestartFunc requests a qrxd restart via agent/services. Guardian never
// calls exec/systemctl itself.
type RestartFunc func(ctx context.Context, reason string) error

// Config tunes how Guardian reacts.
type Config struct {
	// DegradedAfter: no successful poll for this long -> DEGRADED.
	DegradedAfter time.Duration
	// UnhealthyAfter: no successful poll for this long -> UNHEALTHY.
	UnhealthyAfter time.Duration
	// OfflineAfter: no successful poll for this long, or process not
	// running -> OFFLINE.
	OfflineAfter time.Duration
	// MaxAutoRestarts bounds how many automatic recovery attempts Guardian
	// makes within RestartWindow before giving up and staying UNHEALTHY
	// (never silently retrying forever -- docs/architecture.md principle 26).
	MaxAutoRestarts int
	RestartWindow   time.Duration
	// MinRestartInterval is a cooldown between automatic restart attempts.
	MinRestartInterval time.Duration
}

// DefaultConfig is a reasonable starting point; every field is
// operator-configurable (docs/configuration.md).
func DefaultConfig() Config {
	return Config{
		DegradedAfter:      30 * time.Second,
		UnhealthyAfter:     2 * time.Minute,
		OfflineAfter:       5 * time.Minute,
		MaxAutoRestarts:    3,
		RestartWindow:      15 * time.Minute,
		MinRestartInterval: 1 * time.Minute,
	}
}

// Guardian is the health/recovery state machine.
type Guardian struct {
	cfg     Config
	restart RestartFunc
	onEvent func(models.HealthState, models.HealthState) // old, new

	mu           sync.Mutex
	state        models.HealthState
	restartTimes []time.Time
	lastRestart  time.Time
}

// New builds a Guardian. restart is called for automatic recovery attempts;
// onEvent (optional) is notified on every state transition (wired to
// agent/events by cmd/agentd).
func New(cfg Config, restart RestartFunc, onEvent func(old, new models.HealthState)) *Guardian {
	return &Guardian{cfg: cfg, restart: restart, onEvent: onEvent, state: models.HealthOffline}
}

// State returns the current health state.
func (g *Guardian) State() models.HealthState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

// Evaluate runs one Guardian tick: classify health from signals, transition
// state if it changed, and trigger an automatic restart if the new state
// warrants one and the restart budget allows it.
func (g *Guardian) Evaluate(ctx context.Context, s Signals) models.HealthState {
	newState := classify(s, g.cfg, time.Now())

	g.mu.Lock()
	old := g.state
	changed := old != newState
	g.state = newState
	// Restart on entering UNHEALTHY or OFFLINE regardless of whether the
	// process is technically still running -- a hung/unresponsive-but-alive
	// qrxd is exactly the case an automatic restart is for, not just a
	// crashed one.
	shouldRestart := changed && (newState == models.HealthUnhealthy || newState == models.HealthOffline)
	g.mu.Unlock()

	if changed && g.onEvent != nil {
		g.onEvent(old, newState)
	}
	if shouldRestart {
		g.attemptRestart(ctx)
	}
	return newState
}

func classify(s Signals, cfg Config, now time.Time) models.HealthState {
	if !s.QRXProcessRunning {
		return models.HealthOffline
	}
	since := now.Sub(s.LastSuccessfulPoll)
	if s.LastSuccessfulPoll.IsZero() {
		since = cfg.OfflineAfter // never polled successfully yet -- treat as fully stale
	}
	switch {
	case since >= cfg.OfflineAfter || !s.NodeOnline:
		return models.HealthOffline
	case since >= cfg.UnhealthyAfter || !s.AdapterHealthy:
		return models.HealthUnhealthy
	case since >= cfg.DegradedAfter:
		return models.HealthDegraded
	default:
		return models.HealthHealthy
	}
}

// attemptRestart enforces the restart budget (MaxAutoRestarts within
// RestartWindow, plus MinRestartInterval cooldown) before actually calling
// restart -- an unhealthy QRX process that keeps crashing must not be
// restarted in an unbounded loop.
func (g *Guardian) attemptRestart(ctx context.Context) {
	g.mu.Lock()
	now := time.Now()
	if now.Sub(g.lastRestart) < g.cfg.MinRestartInterval {
		g.mu.Unlock()
		return
	}
	cutoff := now.Add(-g.cfg.RestartWindow)
	kept := g.restartTimes[:0]
	for _, t := range g.restartTimes {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	g.restartTimes = kept
	if len(g.restartTimes) >= g.cfg.MaxAutoRestarts {
		g.mu.Unlock()
		return // budget exhausted -- stay UNHEALTHY/OFFLINE rather than loop
	}
	g.restartTimes = append(g.restartTimes, now)
	g.lastRestart = now
	g.mu.Unlock()

	if g.restart != nil {
		_ = g.restart(ctx, "guardian: automatic recovery")
	}
}

// RestartAttemptsInWindow reports how many automatic restarts have happened
// within the configured window, for observability (GET /api/v1/status).
func (g *Guardian) RestartAttemptsInWindow() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.restartTimes)
}
