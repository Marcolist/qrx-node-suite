package updates

import (
	"context"
	"fmt"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates/components"
)

// Rollback reverts a component to its previous version right now, on
// operator request (POST /api/v1/updates/rollback,
// docs/updates.md#rollback-system), independent of any in-progress install.
// For a self-binary component this only flips the store's pointers --
// PendingRestart is true and the caller must arrange a process restart for
// it to take effect (the same as Install's self-binary path, and for the
// same reason: this process can't heal itself into running old code).
func (m *Manager) Rollback(ctx context.Context, component, actor string) (*InstallResult, error) {
	ctrl, ok := m.Controllers[component]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownComponent, component)
	}
	st := m.storeFor(component)
	cur, _, err := st.Current()
	if err != nil {
		return nil, err
	}
	if _, prevOK, err := st.Previous(); err != nil {
		return nil, err
	} else if !prevOK {
		return nil, ErrNoRollbackTarget
	}

	if self, ok := ctrl.(components.SelfBinary); ok && self.IsSelfBinary() {
		if err := st.RollbackToPrevious(); err != nil {
			return nil, err
		}
		newCur, _, _ := st.Current()
		rec := storage.UpdateHistoryRecord{
			Component: component, FromVersion: cur, ToVersion: newCur,
			Status: storage.UpdateStatusRolledBack, RollbackUsed: true,
		}
		id, err := m.History.Insert(ctx, rec)
		if err != nil {
			return nil, err
		}
		rec.ID = id
		m.auditRollback(ctx, actor, component)
		return &InstallResult{History: &rec, PendingRestart: true}, nil
	}

	if ctrl.RequiresRestart() {
		if err := ctrl.Stop(ctx); err != nil {
			return nil, err
		}
	}
	if err := st.RollbackToPrevious(); err != nil {
		return nil, err
	}
	newCur, _, _ := st.Current()
	if err := ctrl.Start(ctx); err != nil {
		return nil, fmt.Errorf("updates: rollback target %s failed to start: %w", newCur, err)
	}
	if err := ctrl.HealthCheck(ctx); err != nil {
		// The version we just rolled back TO is itself unhealthy. Swap
		// back rather than leaving the component on a known-bad version,
		// and surface this loudly -- it means both versions are suspect.
		st.RollbackToPrevious()
		ctrl.Start(ctx)
		return nil, fmt.Errorf("updates: rollback target %s failed health check, reverted: %w", newCur, err)
	}

	rec := storage.UpdateHistoryRecord{
		Component: component, FromVersion: cur, ToVersion: newCur,
		Status: storage.UpdateStatusSucceeded, RollbackUsed: true,
	}
	id, err := m.History.Insert(ctx, rec)
	if err != nil {
		return nil, err
	}
	rec.ID = id
	m.auditRollback(ctx, actor, component)
	return &InstallResult{History: &rec}, nil
}

func (m *Manager) auditRollback(ctx context.Context, actor, component string) {
	if m.Audit == nil {
		return
	}
	action := storage.ActionRollbackAdapter
	switch component {
	case "agent":
		action = storage.ActionRollbackAgent
	case "dashboard":
		action = storage.ActionRollbackDashboard
	}
	m.Audit.Record(ctx, storage.AuditEvent{Actor: actor, Action: action, Component: component})
}
