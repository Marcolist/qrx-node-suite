package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/models"
)

func TestGuardianEventsDistinguishHealthFromRestart(t *testing.T) {
	for _, restartFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful restart", true: "failed restart"}[restartFails], func(t *testing.T) {
			bus := events.NewBus(16)
			id, ch := bus.Subscribe()
			defer bus.Unsubscribe(id)
			nextEvent := func() models.Event {
				t.Helper()
				select {
				case e := <-ch:
					return e
				default:
					t.Fatal("expected a synchronously published event")
					return models.Event{}
				}
			}
			var logs bytes.Buffer
			calls := 0
			g := newGuardian(guardian.DefaultConfig(), func(context.Context) error {
				calls++
				if restartFails {
					return errors.New("test supervisor failure")
				}
				return nil
			}, bus, slog.New(slog.NewTextHandler(&logs, nil)))
			s := guardian.Signals{QRXProcessRunning: true, NodeOnline: true, AdapterHealthy: true, LastSuccessfulPoll: time.Now()}
			g.Evaluate(context.Background(), s)
			s.NodeOnline = false
			g.Evaluate(context.Background(), s)
			s.NodeOnline, s.LastSuccessfulPoll = true, time.Now()
			g.Evaluate(context.Background(), s)
			if calls != 0 {
				t.Fatal("transient failure must not call the supervisor")
			}
			for i := 0; i < 3; i++ {
				if e := nextEvent(); e.Type != models.EventNodeHealthChanged {
					t.Fatalf("health transition published %s", e.Type)
				}
			}
			s.NodeOnline, s.LastSuccessfulPoll = false, time.Now().Add(-3*time.Minute)
			g.Evaluate(context.Background(), s)
			if calls != 1 {
				t.Fatalf("sustained failure: supervisor calls %d, want 1", calls)
			}
			if e := nextEvent(); e.Type != models.EventNodeHealthChanged {
				t.Fatalf("unhealthy transition published %s", e.Type)
			}
			select {
			case e := <-ch:
				if restartFails || e.Type != models.EventServiceRestarted {
					t.Fatalf("unexpected restart event: %+v", e)
				}
			default:
				if !restartFails {
					t.Fatal("successful supervisor restart must publish service.restarted")
				}
			}
			if restartFails && !strings.Contains(logs.String(), "guardian recovery failed") {
				t.Fatal("failed recovery must be logged")
			}
		})
	}
}
