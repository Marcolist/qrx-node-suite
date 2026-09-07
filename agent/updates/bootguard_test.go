package updates_test

import (
	"context"
	"testing"
	"time"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates"
	"qrx-node-suite/agent/updates/components"
)

func newBootGuard(env *testEnv, maxAttempts int) *updates.BootGuard {
	return &updates.BootGuard{
		Settings: env.mgr.Settings, History: env.mgr.History, Audit: env.mgr.Audit,
		BaseDir: env.baseDir, MaxAttempts: maxAttempts,
	}
}

func TestBootGuardNoOpWithoutPendingMarker(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})
	guard := newBootGuard(env, 3)

	pending, err := guard.PendingComponents(context.Background())
	if err != nil {
		t.Fatalf("PendingComponents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected no pending components, got %v", pending)
	}
}

func TestBootGuardAllowsAttemptsWithinBudget(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})
	ctx := context.Background()
	guard := newBootGuard(env, 3)

	env.registerManifest(t, "stable", map[string]string{"agent": "2.0.0"}, time.Now())
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 2.0.0: %v", err)
	}

	for i := 1; i <= 3; i++ {
		rolledBack, attempts, err := guard.CheckAndRecordAttempt(ctx, "agent")
		if err != nil {
			t.Fatalf("CheckAndRecordAttempt #%d: %v", i, err)
		}
		if rolledBack {
			t.Fatalf("attempt #%d: expected no rollback within budget", i)
		}
		if attempts != i {
			t.Errorf("attempt #%d: attempts = %d, want %d", i, attempts, i)
		}
	}

	if got := env.currentVersion(t, "agent"); got != "2.0.0" {
		t.Errorf("current = %q, want 2.0.0 (no rollback should have happened yet)", got)
	}
	pending, err := guard.PendingComponents(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingComponents = %v, %v; want exactly one entry still pending", pending, err)
	}
}

func TestBootGuardForcesRollbackAfterMaxAttempts(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})
	ctx := context.Background()
	guard := newBootGuard(env, 3)

	// Establish a healthy baseline so there is a "previous" to roll back to.
	env.registerManifest(t, "stable", map[string]string{"agent": "1.0.0"}, time.Now())
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 1.0.0: %v", err)
	}
	if _, err := env.mgr.ResumeSelfUpdate(ctx, "agent"); err != nil {
		t.Fatalf("resume 1.0.0: %v", err)
	}

	// Stage a new version whose binary will "crash-loop": never reaches
	// ResumeSelfUpdate.
	env.registerManifest(t, "stable", map[string]string{"agent": "2.0.0"}, time.Now().Add(time.Minute))
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 2.0.0: %v", err)
	}

	var lastRolledBack bool
	var lastAttempts int
	for i := 1; i <= 4; i++ {
		rolledBack, attempts, err := guard.CheckAndRecordAttempt(ctx, "agent")
		if err != nil {
			t.Fatalf("CheckAndRecordAttempt #%d: %v", i, err)
		}
		lastRolledBack, lastAttempts = rolledBack, attempts
	}
	if !lastRolledBack {
		t.Fatalf("expected the 4th attempt (over MaxAttempts=3) to force a rollback, attempts=%d", lastAttempts)
	}
	if lastAttempts != 4 {
		t.Errorf("attempts = %d, want 4", lastAttempts)
	}

	if got := env.currentVersion(t, "agent"); got != "1.0.0" {
		t.Errorf("current after forced rollback = %q, want 1.0.0", got)
	}
	pending, err := guard.PendingComponents(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("PendingComponents = %v, %v; pending marker should be cleared after forced rollback", pending, err)
	}

	// The next boot after the forced rollback must start counting from
	// scratch, not continue from where the crash loop left off.
	env.registerManifest(t, "stable", map[string]string{"agent": "3.0.0"}, time.Now().Add(2*time.Minute))
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 3.0.0: %v", err)
	}
	rolledBack, attempts, err := guard.CheckAndRecordAttempt(ctx, "agent")
	if err != nil {
		t.Fatalf("CheckAndRecordAttempt after rollback: %v", err)
	}
	if rolledBack || attempts != 1 {
		t.Errorf("post-rollback attempt = (rolledBack=%v, attempts=%d), want (false, 1)", rolledBack, attempts)
	}

	records, err := env.mgr.History.ListForComponent(ctx, "agent", 10)
	if err != nil {
		t.Fatalf("ListForComponent: %v", err)
	}
	var found bool
	for _, r := range records {
		if r.Status == storage.UpdateStatusRolledBack && r.RollbackUsed && r.ToVersion == "1.0.0" && r.FromVersion == "2.0.0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an update_history record for the forced rollback 2.0.0 -> 1.0.0, got %+v", records)
	}

	auditEvents, err := env.mgr.Audit.List(ctx, 10)
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	var auditFound bool
	for _, e := range auditEvents {
		if e.Actor == "bootguard" && e.Component == "agent" {
			auditFound = true
		}
	}
	if !auditFound {
		t.Errorf("expected an audit event recorded for the forced rollback, got %+v", auditEvents)
	}
}

func TestResumeSelfUpdateClearsBootAttempts(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})
	ctx := context.Background()
	guard := newBootGuard(env, 3)

	env.registerManifest(t, "stable", map[string]string{"agent": "1.0.0"}, time.Now())
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 1.0.0: %v", err)
	}
	if _, err := env.mgr.ResumeSelfUpdate(ctx, "agent"); err != nil {
		t.Fatalf("resume 1.0.0: %v", err)
	}

	env.registerManifest(t, "stable", map[string]string{"agent": "2.0.0"}, time.Now().Add(time.Minute))
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 2.0.0: %v", err)
	}

	// Two failed-to-reach-Resume boots, still within budget.
	if _, _, err := guard.CheckAndRecordAttempt(ctx, "agent"); err != nil {
		t.Fatalf("CheckAndRecordAttempt #1: %v", err)
	}
	if _, _, err := guard.CheckAndRecordAttempt(ctx, "agent"); err != nil {
		t.Fatalf("CheckAndRecordAttempt #2: %v", err)
	}

	// The third boot reaches ResumeSelfUpdate and is healthy -- this must
	// reset the crash-loop count, not just clear the pending marker.
	ctrl.healthErr = nil
	if _, err := env.mgr.ResumeSelfUpdate(ctx, "agent"); err != nil {
		t.Fatalf("ResumeSelfUpdate: %v", err)
	}

	env.registerManifest(t, "stable", map[string]string{"agent": "3.0.0"}, time.Now().Add(2*time.Minute))
	if _, err := env.mgr.Install(ctx, "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 3.0.0: %v", err)
	}
	rolledBack, attempts, err := guard.CheckAndRecordAttempt(ctx, "agent")
	if err != nil {
		t.Fatalf("CheckAndRecordAttempt after resume: %v", err)
	}
	if rolledBack || attempts != 1 {
		t.Errorf("attempt after a successful ResumeSelfUpdate = (rolledBack=%v, attempts=%d), want (false, 1) -- boot_attempts should have been cleared", rolledBack, attempts)
	}
}
