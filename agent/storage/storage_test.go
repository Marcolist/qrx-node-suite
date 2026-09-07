package storage_test

import (
	"context"
	"database/sql"
	"testing"

	"qrx-node-suite/agent/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	// Open already ran Migrate once; running it again must be a no-op, not
	// an error (re-applying CREATE TABLE would fail loudly if it weren't
	// correctly skipping already-applied migrations).
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("second Migrate call: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("schema_migrations has %d rows, want 1 (0001_init.sql only)", count)
	}
}

func TestUpdateHistoryStore(t *testing.T) {
	db := openTestDB(t)
	store := storage.NewUpdateHistoryStore(db)
	ctx := context.Background()

	id, err := store.Insert(ctx, storage.UpdateHistoryRecord{
		Component:       "agent",
		FromVersion:     "0.1.0",
		ToVersion:       "0.2.0",
		Channel:         "stable",
		Status:          storage.UpdateStatusSucceeded,
		Checksum:        "deadbeef",
		ManifestVersion: 1,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Error("expected a nonzero row id")
	}

	latest, err := store.Latest(ctx, "agent")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest == nil {
		t.Fatal("Latest returned nil, want the record just inserted")
	}
	if latest.ToVersion != "0.2.0" || latest.Status != storage.UpdateStatusSucceeded {
		t.Errorf("latest = %+v, want ToVersion 0.2.0 / status succeeded", latest)
	}

	none, err := store.Latest(ctx, "dashboard")
	if err != nil {
		t.Fatalf("Latest(dashboard): %v", err)
	}
	if none != nil {
		t.Errorf("expected no history for dashboard, got %+v", none)
	}
}

func TestAuditLogStore(t *testing.T) {
	db := openTestDB(t)
	store := storage.NewAuditLogStore(db)
	ctx := context.Background()

	if err := store.Record(ctx, storage.AuditEvent{
		Actor:     "admin",
		Action:    storage.ActionSwitchAdapter,
		Component: "adapter",
		Details:   `{"from":"mock","to":"qrx007"}`,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	events, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Action != storage.ActionSwitchAdapter {
		t.Errorf("action = %q, want %q", events[0].Action, storage.ActionSwitchAdapter)
	}
}

func TestSettingsStore(t *testing.T) {
	db := openTestDB(t)
	store := storage.NewSettingsStore(db)
	ctx := context.Background()

	if _, err := store.Get(ctx, "missing"); err != storage.ErrSettingNotFound {
		t.Errorf("Get(missing) err = %v, want ErrSettingNotFound", err)
	}

	if err := store.Set(ctx, "update_channel.agent", "stable"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get(ctx, "update_channel.agent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "stable" {
		t.Errorf("Get = %q, want stable", got)
	}

	// Overwrite (exercises the ON CONFLICT upsert path).
	if err := store.Set(ctx, "update_channel.agent", "beta"); err != nil {
		t.Fatalf("Set (overwrite): %v", err)
	}
	got, _ = store.Get(ctx, "update_channel.agent")
	if got != "beta" {
		t.Errorf("Get after overwrite = %q, want beta", got)
	}

	type pin struct {
		Version string `json:"version"`
	}
	if err := store.SetJSON(ctx, "pin.qrx_core", pin{Version: "0.0.7"}); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}
	var p pin
	if err := store.GetJSON(ctx, "pin.qrx_core", &p); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if p.Version != "0.0.7" {
		t.Errorf("GetJSON version = %q, want 0.0.7", p.Version)
	}

	all, err := store.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("All() returned %d keys, want 2", len(all))
	}

	if err := store.Delete(ctx, "update_channel.agent"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, "update_channel.agent"); err != storage.ErrSettingNotFound {
		t.Errorf("Get after Delete err = %v, want ErrSettingNotFound", err)
	}
}
