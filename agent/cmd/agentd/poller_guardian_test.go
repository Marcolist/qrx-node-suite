package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/adapters/mock"
	"qrx-node-suite/agent/api"
	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/version"
)

// Optional metrics are supplied by the inert mock; only liveness is varied.
// No real CLI, supervisor, network connection or background simulator is used.
type guardianPollerAdapter struct {
	adapters.Adapter
	online    bool
	statusErr error
	healthErr error
}

func init() {
	adapters.Register("guardian-poller-test", func() adapters.Adapter {
		return &guardianPollerAdapter{Adapter: mock.New(), online: true}
	})
}

func (*guardianPollerAdapter) Name() string                     { return "guardian-poller-test" }
func (*guardianPollerAdapter) Activate(context.Context) error   { return nil }
func (*guardianPollerAdapter) Deactivate(context.Context) error { return nil }
func (a *guardianPollerAdapter) Health(context.Context) error   { return a.healthErr }
func (a *guardianPollerAdapter) GetNodeStatus(context.Context) (models.NodeStatus, error) {
	return models.NodeStatus{Online: a.online}, a.statusErr
}

func TestPollerGuardianRequiresBothLivenessChecks(t *testing.T) {
	for _, failure := range []string{"status timeout", "health timeout", "reported offline"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			r := adapters.NewRegistry(&version.Matrix{})
			var a *guardianPollerAdapter
			if err := r.Configure("guardian-poller-test", func(adapter adapters.Adapter) {
				a = adapter.(*guardianPollerAdapter)
			}); err != nil {
				t.Fatal(err)
			}
			if err := r.Activate(ctx, a.Name(), adapters.ActivateOptions{AllowUnsupported: true}); err != nil {
				t.Fatal(err)
			}
			restarts := 0
			p := &Poller{
				Registry: r,
				Cache:    api.NewCache(),
				Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
				Guardian: guardian.New(guardian.DefaultConfig(), func(context.Context, string) error {
					restarts++
					return nil
				}, nil),
				CoreProcessRunning: func(context.Context) bool { return true },
			}
			p.tick(ctx)
			if p.lastSuccess.IsZero() || p.Cache.Get().Health != models.HealthHealthy {
				t.Fatal("fully successful poll must establish healthy liveness")
			}
			switch failure {
			case "status timeout":
				a.statusErr = context.DeadlineExceeded
			case "health timeout":
				a.healthErr = context.DeadlineExceeded
			case "reported offline":
				a.online = false
			}
			lastGood := p.lastSuccess
			p.tick(ctx)
			if !p.lastSuccess.Equal(lastGood) || p.Cache.Get().Health != models.HealthDegraded || restarts != 0 {
				t.Fatal("a transient failure must preserve last success and degrade without restarting")
			}
			// Simulate elapsed time by aging the previous joint success. A
			// working status query must not conceal a sustained health failure.
			lastGood = time.Now().Add(-3 * time.Minute)
			p.lastSuccess = lastGood
			p.tick(ctx)
			if !p.lastSuccess.Equal(lastGood) || p.Cache.Get().Health != models.HealthUnhealthy || restarts != 1 {
				t.Fatal("sustained failure must expire the grace period and request recovery")
			}
			a.online, a.statusErr, a.healthErr = true, nil, nil
			p.tick(ctx)
			if !p.lastSuccess.After(lastGood) || p.Cache.Get().Health != models.HealthHealthy || restarts != 1 {
				t.Fatal("both successful probes must restore HEALTHY without an extra restart")
			}
		})
	}
}
