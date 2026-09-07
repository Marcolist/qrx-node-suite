package guardian_test

import (
	"context"
	"testing"
	"time"

	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/models"
)

func fastConfig() guardian.Config {
	return guardian.Config{
		DegradedAfter:      10 * time.Millisecond,
		UnhealthyAfter:     20 * time.Millisecond,
		OfflineAfter:       30 * time.Millisecond,
		MaxAutoRestarts:    2,
		RestartWindow:      time.Second,
		MinRestartInterval: 0,
	}
}

func TestClassifyHealthyWhenRecentAndOnline(t *testing.T) {
	g := guardian.New(fastConfig(), nil, nil)
	state := g.Evaluate(context.Background(), guardian.Signals{
		QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: time.Now(),
	})
	if state != models.HealthHealthy {
		t.Errorf("state = %v, want HEALTHY", state)
	}
}

func TestClassifyOfflineWhenProcessNotRunning(t *testing.T) {
	g := guardian.New(fastConfig(), nil, nil)
	state := g.Evaluate(context.Background(), guardian.Signals{QRXProcessRunning: false})
	if state != models.HealthOffline {
		t.Errorf("state = %v, want OFFLINE", state)
	}
}

func TestClassifyDegradesOverTime(t *testing.T) {
	g := guardian.New(fastConfig(), nil, nil)
	stale := time.Now().Add(-15 * time.Millisecond)
	state := g.Evaluate(context.Background(), guardian.Signals{
		QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: stale,
	})
	if state != models.HealthDegraded {
		t.Errorf("state = %v, want DEGRADED", state)
	}
}

func TestClassifyUnhealthyWhenAdapterUnhealthy(t *testing.T) {
	g := guardian.New(fastConfig(), nil, nil)
	state := g.Evaluate(context.Background(), guardian.Signals{
		QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: false, LastSuccessfulPoll: time.Now(),
	})
	if state != models.HealthUnhealthy {
		t.Errorf("state = %v, want UNHEALTHY", state)
	}
}

func TestRestartTriggeredOnTransitionToUnhealthy(t *testing.T) {
	var restartCalls int
	restart := func(ctx context.Context, reason string) error {
		restartCalls++
		return nil
	}
	g := guardian.New(fastConfig(), restart, nil)

	// Start healthy.
	g.Evaluate(context.Background(), guardian.Signals{
		QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: time.Now(),
	})
	// Transition to unhealthy (process still "running" but adapter can't
	// reach it -- the hung-process case).
	g.Evaluate(context.Background(), guardian.Signals{
		QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: false, LastSuccessfulPoll: time.Now(),
	})
	if restartCalls != 1 {
		t.Errorf("restartCalls = %d, want 1", restartCalls)
	}
}

func TestRestartBudgetExhausted(t *testing.T) {
	var restartCalls int
	restart := func(ctx context.Context, reason string) error {
		restartCalls++
		return nil
	}
	cfg := fastConfig()
	cfg.MaxAutoRestarts = 2
	g := guardian.New(cfg, restart, nil)
	ctx := context.Background()

	// Flap between healthy and offline repeatedly to trigger many restart
	// attempts; only MaxAutoRestarts should actually fire.
	for i := 0; i < 5; i++ {
		g.Evaluate(ctx, guardian.Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: time.Now()})
		g.Evaluate(ctx, guardian.Signals{QRXProcessRunning: false})
	}
	if restartCalls != 2 {
		t.Errorf("restartCalls = %d, want 2 (MaxAutoRestarts budget)", restartCalls)
	}
	if g.RestartAttemptsInWindow() != 2 {
		t.Errorf("RestartAttemptsInWindow() = %d, want 2", g.RestartAttemptsInWindow())
	}
}

func TestOnEventCalledOnStateChange(t *testing.T) {
	var transitions [][2]models.HealthState
	onEvent := func(old, new models.HealthState) {
		transitions = append(transitions, [2]models.HealthState{old, new})
	}
	g := guardian.New(fastConfig(), nil, onEvent)
	ctx := context.Background()

	g.Evaluate(ctx, guardian.Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: time.Now()})
	g.Evaluate(ctx, guardian.Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: time.Now()}) // no change
	g.Evaluate(ctx, guardian.Signals{QRXProcessRunning: false})

	if len(transitions) != 2 {
		t.Fatalf("got %d transitions, want 2 (repeat healthy eval must not fire onEvent again): %+v", len(transitions), transitions)
	}
	if transitions[1][1] != models.HealthOffline {
		t.Errorf("second transition target = %v, want OFFLINE", transitions[1][1])
	}
}
