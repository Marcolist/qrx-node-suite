package updates

import (
	"context"
	"errors"
	"fmt"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates/store"
)

// PendingSelfUpdate records a self-binary component (agent, adapter_*) that
// has been staged and promoted but not yet health-checked, because that can
// only happen after the process restarts onto the new binary -- see
// components.SelfBinary.
type PendingSelfUpdate struct {
	Component   string `json:"component"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Channel     string `json:"channel"`
	HistoryID   int64  `json:"history_id"`
}

func pendingSelfUpdateKey(component string) string { return "pending_self_update." + component }

// PendingSelfUpdates returns the pending self-update marker for each of the
// given components that actually has one. Called by cmd/agentd very early
// at startup to decide which components need ResumeSelfUpdate.
func (m *Manager) PendingSelfUpdates(ctx context.Context, componentNames []string) ([]PendingSelfUpdate, error) {
	var out []PendingSelfUpdate
	for _, c := range componentNames {
		var p PendingSelfUpdate
		err := m.Settings.GetJSON(ctx, pendingSelfUpdateKey(c), &p)
		if errors.Is(err, storage.ErrSettingNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ResumeSelfUpdate must be called by cmd/agentd for every component
// PendingSelfUpdates reports, after the rest of the Agent (in particular,
// for an adapter component, the normal adapter auto-selection flow) has
// already started up -- Controller.HealthCheck for an Adapter needs the
// registry's active adapter to already be set. It runs the deferred HEALTH
// CHECK now that the new code is actually executing, then COMMITs (clears
// the pending marker, prunes old versions) or ROLLBACKs (reverts the
// store's current/previous pointers).
//
// IMPORTANT, known limitation: if the new binary crashes before this runs
// at all (rather than starting and failing HealthCheck), there is currently
// no crash-loop watchdog here -- a process supervisor configured with
// Restart=always will keep restarting the crashing binary indefinitely.
// ResumeSelfUpdate only protects against "starts, but is unhealthy," not
// "doesn't start." A future improvement is a boot-attempt counter that
// forces an automatic rollback after N consecutive failed starts within a
// window; see docs/updates.md's note on this.
//
// A rollback here does NOT itself restart the process: it only flips the
// store's pointers back. The caller (cmd/agentd) is responsible for exiting
// afterward so the supervisor restarts the process onto the now-reverted
// "current" binary.
func (m *Manager) ResumeSelfUpdate(ctx context.Context, component string) (*storage.UpdateHistoryRecord, error) {
	var pending PendingSelfUpdate
	err := m.Settings.GetJSON(ctx, pendingSelfUpdateKey(component), &pending)
	if errors.Is(err, storage.ErrSettingNotFound) {
		return nil, nil // nothing pending -- normal case, not an error
	}
	if err != nil {
		return nil, err
	}

	ctrl, ok := m.Controllers[component]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownComponent, component)
	}
	st := store.New(m.BaseDir, component)

	rec := storage.UpdateHistoryRecord{
		Component: component, FromVersion: pending.FromVersion, ToVersion: pending.ToVersion, Channel: pending.Channel,
	}
	if healthErr := ctrl.HealthCheck(ctx); healthErr != nil {
		if rbErr := st.RollbackToPrevious(); rbErr != nil {
			rec.Status = storage.UpdateStatusFailed
			rec.ErrorMessage = combineErrors("health check failed", healthErr, rbErr)
		} else {
			rec.Status = storage.UpdateStatusRolledBack
			rec.RollbackUsed = true
			rec.ErrorMessage = healthErr.Error()
		}
	} else {
		_ = st.Prune(m.retainVersions())
		rec.Status = storage.UpdateStatusSucceeded
	}

	id, err := m.History.Insert(ctx, rec)
	if err != nil {
		return nil, err
	}
	rec.ID = id
	if err := m.Settings.Delete(ctx, pendingSelfUpdateKey(component)); err != nil {
		return &rec, err
	}
	return &rec, nil
}
