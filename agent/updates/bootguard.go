package updates

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates/store"
)

const pendingSelfUpdateKeyPrefix = "pending_self_update."

func bootAttemptsKey(component string) string { return "boot_attempts." + component }

// defaultMaxBootAttempts is used whenever BootGuard.MaxAttempts is left at
// its zero value.
const defaultMaxBootAttempts = 3

// BootGuard closes the crash-loop gap noted in ResumeSelfUpdate's doc
// comment: a self-binary update (agent, adapter_*) that stages and promotes
// cleanly can still crash before the restarted process ever gets far enough
// to call ResumeSelfUpdate, in which case a process supervisor configured
// with Restart=always would otherwise keep restarting the broken binary
// forever. BootGuard must be called by cmd/agentd as the very first thing
// after Settings/History/Audit are constructed, before adapter selection or
// anything else that could itself crash -- every restart attempt, including
// ones that never reach ResumeSelfUpdate, needs to be counted.
type BootGuard struct {
	Settings *storage.SettingsStore
	History  *storage.UpdateHistoryStore
	Audit    *storage.AuditLogStore
	BaseDir  string

	// MaxAttempts is how many consecutive failed boots are tolerated
	// before a forced rollback. <=0 means defaultMaxBootAttempts.
	MaxAttempts int
}

// EffectiveMaxAttempts returns MaxAttempts, or defaultMaxBootAttempts if
// MaxAttempts is unset.
func (g *BootGuard) EffectiveMaxAttempts() int {
	if g.MaxAttempts <= 0 {
		return defaultMaxBootAttempts
	}
	return g.MaxAttempts
}

// PendingComponents returns every component with an in-flight self-update
// marker (set by Manager.Install's self-binary path, cleared by
// ResumeSelfUpdate), without the caller needing to already know which
// adapter is active -- BootGuard runs before adapter selection.
func (g *BootGuard) PendingComponents(ctx context.Context) ([]string, error) {
	all, err := g.Settings.All(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for key := range all {
		if strings.HasPrefix(key, pendingSelfUpdateKeyPrefix) {
			out = append(out, strings.TrimPrefix(key, pendingSelfUpdateKeyPrefix))
		}
	}
	return out, nil
}

// CheckAndRecordAttempt increments component's persisted boot-attempt
// counter. Once it exceeds EffectiveMaxAttempts, it forces a rollback: the
// component store's current/previous pointers are reverted, an
// UpdateHistoryRecord and audit event are written, and both the pending
// self-update marker and the boot-attempt counter are cleared.
//
// rolledBack=true means the caller MUST stop startup and exit the process
// immediately after logging -- this running process is still executing the
// crash-looping binary even though the store's "current" pointer now points
// elsewhere. The next restart (by the supervisor) runs the reverted version,
// and PendingComponents will no longer report this component.
func (g *BootGuard) CheckAndRecordAttempt(ctx context.Context, component string) (rolledBack bool, attempts int, err error) {
	attempts, err = g.recordAttempt(ctx, component)
	if err != nil {
		return false, attempts, err
	}
	if attempts <= g.EffectiveMaxAttempts() {
		return false, attempts, nil
	}

	var pending PendingSelfUpdate
	if gerr := g.Settings.GetJSON(ctx, pendingSelfUpdateKey(component), &pending); gerr != nil && !errors.Is(gerr, storage.ErrSettingNotFound) {
		return false, attempts, gerr
	}

	st := store.New(g.BaseDir, component)
	rbErr := st.RollbackToPrevious()

	rec := storage.UpdateHistoryRecord{
		Component: component, FromVersion: pending.ToVersion, ToVersion: pending.FromVersion,
		Channel: pending.Channel, RollbackUsed: true,
	}
	if rbErr != nil {
		rec.Status = storage.UpdateStatusFailed
		rec.ErrorMessage = fmt.Sprintf("crash-loop detected after %d consecutive failed boots, and the forced rollback itself failed: %v", attempts, rbErr)
	} else {
		rec.Status = storage.UpdateStatusRolledBack
		rec.ErrorMessage = fmt.Sprintf("crash-loop detected: %d consecutive failed boots before ResumeSelfUpdate could run; forced rollback to the previous version", attempts)
	}
	if g.History != nil {
		if _, herr := g.History.Insert(ctx, rec); herr != nil {
			err = herr
		}
	}
	if g.Audit != nil {
		g.Audit.Record(ctx, storage.AuditEvent{
			Actor: "bootguard", Action: auditActionForRollback(component), Component: component,
			Details: rec.ErrorMessage,
		})
	}

	if rbErr != nil {
		// Nothing to roll back onto (or the rollback failed outright) --
		// leave the pending marker and counter in place so the next boot
		// attempt tries again, and let the caller decide whether to keep
		// starting up in a degraded state rather than exiting into a
		// guaranteed-empty restart loop.
		return false, attempts, err
	}

	if derr := g.Settings.Delete(ctx, pendingSelfUpdateKey(component)); derr != nil && err == nil {
		err = derr
	}
	g.clearAttempts(ctx, component)
	return true, attempts, err
}

func (g *BootGuard) recordAttempt(ctx context.Context, component string) (int, error) {
	key := bootAttemptsKey(component)
	raw, err := g.Settings.Get(ctx, key)
	n := 0
	if err == nil {
		n, _ = strconv.Atoi(raw)
	} else if !errors.Is(err, storage.ErrSettingNotFound) {
		return 0, err
	}
	n++
	if err := g.Settings.Set(ctx, key, strconv.Itoa(n)); err != nil {
		return n, err
	}
	return n, nil
}

func (g *BootGuard) clearAttempts(ctx context.Context, component string) {
	_ = g.Settings.Delete(ctx, bootAttemptsKey(component))
}

// clearBootAttempts is called by Manager.ResumeSelfUpdate on every path that
// actually runs -- success, a single health-check failure, or a rollback
// triggered by that health-check failure -- since reaching ResumeSelfUpdate
// at all proves the new binary started far enough to run it, so whatever
// boot-attempt count BootGuard accumulated on earlier restarts no longer
// applies.
func clearBootAttempts(ctx context.Context, settings *storage.SettingsStore, component string) {
	_ = settings.Delete(ctx, bootAttemptsKey(component))
}
