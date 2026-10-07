package guardian

import (
	"context"
	"testing"
	"time"

	"qrx-node-suite/agent/models"
)

// These tests advance a private clock; they neither sleep nor touch a service.
func testGuardian(t *testing.T) (*Guardian, *time.Time, *int) {
	t.Helper()
	now := time.Date(2026, 10, 6, 12, 10, 0, 0, time.UTC)
	restarts := 0
	g := New(DefaultConfig(), func(context.Context, string) error {
		restarts++
		return nil
	}, nil)
	g.now = func() time.Time { return now }
	return g, &now, &restarts
}

func TestTransientFailureDoesNotRestart(t *testing.T) {
	for _, failure := range []string{"node status", "adapter health", "both"} {
		t.Run(failure, func(t *testing.T) {
			g, now, restarts := testGuardian(t)
			s := Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: *now}
			if got := g.Evaluate(context.Background(), s); got != models.HealthHealthy {
				t.Fatalf("initial state = %s, want HEALTHY", got)
			}
			*now = now.Add(6 * time.Second)
			s.NodeOnline = failure == "adapter health"
			s.AdapterHealthy = failure == "node status"
			if got := g.Evaluate(context.Background(), s); got != models.HealthDegraded {
				t.Fatalf("transient failure = %s, want DEGRADED", got)
			}
			if *restarts != 0 || g.RestartAttemptsInWindow() != 0 {
				t.Fatal("a single failed poll consumed the restart budget")
			}
			*now = now.Add(5 * time.Second)
			s.NodeOnline, s.AdapterHealthy, s.LastSuccessfulPoll = true, true, *now
			if got := g.Evaluate(context.Background(), s); got != models.HealthHealthy || *restarts != 0 {
				t.Fatalf("recovery = %s, restarts = %d; want HEALTHY without restart", got, *restarts)
			}
		})
	}
}

func TestSustainedFailureHonorsThresholds(t *testing.T) {
	for _, failure := range []string{"node status", "adapter health", "stale sample"} {
		t.Run(failure, func(t *testing.T) {
			g, now, restarts := testGuardian(t)
			start := *now
			s := Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: start}
			g.Evaluate(context.Background(), s)
			s.NodeOnline = failure != "node status"
			s.AdapterHealthy = failure != "adapter health"
			cases := []struct {
				age      time.Duration
				state    models.HealthState
				restarts int
			}{
				{30 * time.Second, models.HealthDegraded, 0},
				{2*time.Minute - time.Nanosecond, models.HealthDegraded, 0},
				{2 * time.Minute, models.HealthUnhealthy, 1},
				{3 * time.Minute, models.HealthUnhealthy, 1},
				{5*time.Minute - time.Nanosecond, models.HealthUnhealthy, 1},
				{5 * time.Minute, models.HealthOffline, 2},
				{6 * time.Minute, models.HealthOffline, 2},
			}
			for _, tc := range cases {
				*now = start.Add(tc.age)
				if got := g.Evaluate(context.Background(), s); got != tc.state || *restarts != tc.restarts {
					t.Fatalf("age %s: state %s, restarts %d; want %s/%d", tc.age, got, *restarts, tc.state, tc.restarts)
				}
			}
		})
	}
}

func TestRunningProcessGetsInitialPollGrace(t *testing.T) {
	g, now, restarts := testGuardian(t)
	start := *now
	s := Signals{QRXProcessRunning: true}
	for _, age := range []time.Duration{0, 30 * time.Second, 2*time.Minute - time.Nanosecond} {
		*now = start.Add(age)
		if got := g.Evaluate(context.Background(), s); got != models.HealthDegraded || *restarts != 0 {
			t.Fatalf("startup age %s: %s, restarts %d; want DEGRADED without restart", age, got, *restarts)
		}
	}
	*now = start.Add(2 * time.Minute)
	if got := g.Evaluate(context.Background(), s); got != models.HealthUnhealthy || *restarts != 1 {
		t.Fatalf("startup grace must expire: state %s, restarts %d", got, *restarts)
	}
}

func TestStoppedProcessRecoversEvenOnFirstObservation(t *testing.T) {
	g, _, restarts := testGuardian(t)
	for i := 0; i < 2; i++ {
		if got := g.Evaluate(context.Background(), Signals{}); got != models.HealthOffline {
			t.Fatalf("stopped process = %s, want OFFLINE", got)
		}
	}
	if *restarts != 1 {
		t.Fatalf("restarts = %d, want exactly one initial recovery attempt", *restarts)
	}
}

func TestRecoveryStillHonorsCooldown(t *testing.T) {
	g, now, restarts := testGuardian(t)
	for _, advance := range []time.Duration{0, 30 * time.Second, 31 * time.Second} {
		*now = now.Add(advance)
		g.Evaluate(context.Background(), Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: *now})
		g.Evaluate(context.Background(), Signals{})
	}
	if *restarts != 2 {
		t.Fatalf("restarts = %d, want 2 (middle attempt blocked by cooldown)", *restarts)
	}
}
