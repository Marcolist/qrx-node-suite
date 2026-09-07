package alerts_test

import (
	"context"
	"testing"

	"qrx-node-suite/agent/alerts"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/storage"
)

type fakePublisher struct {
	events []models.Event
}

func (p *fakePublisher) Publish(e models.Event) { p.events = append(p.events, e) }

func openTestStore(t *testing.T) *storage.AlertStore {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return storage.NewAlertStore(db)
}

func TestEngineCreatesAlertOnFirstFire(t *testing.T) {
	store := openTestStore(t)
	pub := &fakePublisher{}
	engine := alerts.NewEngine(store, pub, []alerts.Rule{
		{ID: "always_fires", Severity: models.SeverityWarning, Title: "test", Evaluate: func(s alerts.Snapshot) (bool, string) { return true, "firing" }},
	})

	if err := engine.Evaluate(context.Background(), alerts.Snapshot{}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	list, err := store.List(context.Background(), false, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d alerts, want 1", len(list))
	}
	if len(pub.events) != 1 || pub.events[0].Type != models.EventAlertCreated {
		t.Errorf("events = %+v, want one alert.created", pub.events)
	}
}

func TestEngineDoesNotDuplicateWhileStillFiring(t *testing.T) {
	store := openTestStore(t)
	pub := &fakePublisher{}
	engine := alerts.NewEngine(store, pub, []alerts.Rule{
		{ID: "always_fires", Severity: models.SeverityWarning, Title: "test", Evaluate: func(s alerts.Snapshot) (bool, string) { return true, "firing" }},
	})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := engine.Evaluate(ctx, alerts.Snapshot{}); err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
	}
	list, err := store.List(ctx, false, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d alerts after 3 ticks of the same firing rule, want exactly 1", len(list))
	}
}

func TestEngineResolvesWhenNoLongerFiring(t *testing.T) {
	store := openTestStore(t)
	pub := &fakePublisher{}
	firing := true
	engine := alerts.NewEngine(store, pub, []alerts.Rule{
		{ID: "toggle", Severity: models.SeverityWarning, Title: "test", Evaluate: func(s alerts.Snapshot) (bool, string) { return firing, "firing" }},
	})
	ctx := context.Background()
	if err := engine.Evaluate(ctx, alerts.Snapshot{}); err != nil {
		t.Fatalf("Evaluate (firing): %v", err)
	}
	firing = false
	if err := engine.Evaluate(ctx, alerts.Snapshot{}); err != nil {
		t.Fatalf("Evaluate (resolved): %v", err)
	}

	open, err := store.List(ctx, true, 10)
	if err != nil {
		t.Fatalf("List(unresolved): %v", err)
	}
	if len(open) != 0 {
		t.Errorf("expected no open alerts after resolving, got %d", len(open))
	}
	all, _ := store.List(ctx, false, 10)
	if len(all) != 1 || !all[0].Resolved {
		t.Errorf("expected exactly one resolved alert, got %+v", all)
	}

	var sawResolved bool
	for _, e := range pub.events {
		if e.Type == models.EventAlertResolved {
			sawResolved = true
		}
	}
	if !sawResolved {
		t.Error("expected an alert.resolved event to be published")
	}
}

func TestDefaultRulesDiskLow(t *testing.T) {
	rules := alerts.DefaultRules()
	var diskRule *alerts.Rule
	for i := range rules {
		if rules[i].ID == "disk_low" {
			diskRule = &rules[i]
		}
	}
	if diskRule == nil {
		t.Fatal("expected a disk_low default rule")
	}
	firing, _ := diskRule.Evaluate(alerts.Snapshot{
		System: models.SystemStatus{
			DiskTotalBytes: models.Avail(uint64(100)),
			DiskFreeBytes:  models.Avail(uint64(5)),
		},
	})
	if !firing {
		t.Error("expected disk_low to fire at 5% free")
	}
	firing, _ = diskRule.Evaluate(alerts.Snapshot{
		System: models.SystemStatus{
			DiskTotalBytes: models.Avail(uint64(100)),
			DiskFreeBytes:  models.Avail(uint64(50)),
		},
	})
	if firing {
		t.Error("expected disk_low not to fire at 50% free")
	}
}
