// Package updates implements docs/updates.md: the generic OTA workflow
// (CHECK/DOWNLOAD/VERIFY/STAGE/BACKUP/STOP/INSTALL/START/HEALTH
// CHECK/COMMIT/ROLLBACK) for the Agent, Dashboard, adapters, and
// compatibility profiles. QRX Core is deliberately NOT managed by Manager --
// see qrx_core.go for its separate, more conservative manager.
package updates

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"time"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates/components"
	"qrx-node-suite/agent/updates/manifest"
	"qrx-node-suite/agent/updates/sources"
	"qrx-node-suite/agent/updates/store"
	"qrx-node-suite/agent/version"
)

// CurrentVersions is what Manager needs to know about the running system to
// evaluate an update's compatibility requirements
// (docs/updates.md#breaking-update-protection).
type CurrentVersions struct {
	SuiteVersion   string
	QRXCoreVersion string
	AdapterVersion string
}

// Manager orchestrates OTA updates for every component EXCEPT QRX Core. One
// Manager instance is shared across components; component-specific behavior
// lives entirely in the Controllers map (agent/updates/components).
type Manager struct {
	Source    sources.Source
	PublicKey ed25519.PublicKey

	History  *storage.UpdateHistoryStore
	Audit    *storage.AuditLogStore
	Settings *storage.SettingsStore
	Policy   *Policy

	// Matrix, if set, is consulted for adapter components in addition to
	// each Controller's own checks -- see CheckAdapterCompatibility.
	Matrix *version.Matrix

	// BaseDir roots every component's store.Store (store.New(BaseDir, component)).
	BaseDir string
	// Controllers maps a manifest component name ("agent", "dashboard",
	// "adapter_qrx007", "compatibility_profile_0.0.7", ...) to how to
	// extract/activate/health-check it.
	Controllers map[string]components.Controller
	// RetainVersions is how many extra old versions Prune keeps beyond
	// current+previous (docs/updates.md#rollback-system). Defaults to 2.
	RetainVersions int

	// CurrentVersions reports the system's current versions for
	// compatibility checks. May be nil (checks needing it are then skipped).
	CurrentVersions func() CurrentVersions
}

func (m *Manager) retainVersions() int {
	if m.RetainVersions <= 0 {
		return 2
	}
	return m.RetainVersions
}

func (m *Manager) storeFor(component string) *store.Store {
	return store.New(m.BaseDir, component)
}

func (m *Manager) currentVersions() CurrentVersions {
	if m.CurrentVersions == nil {
		return CurrentVersions{}
	}
	return m.CurrentVersions()
}

// fetchVerifiedManifest fetches a channel's manifest and rejects it unless
// its signature verifies and it isn't a replay of a stale manifest. Nothing
// in a manifest that fails this is ever inspected further, per
// docs/security.md.
func (m *Manager) fetchVerifiedManifest(ctx context.Context, channel string) (*manifest.Manifest, error) {
	mf, err := m.Source.FetchManifest(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("updates: fetch manifest: %w", err)
	}
	if err := manifest.VerifyManifestSignature(mf, m.PublicKey); err != nil {
		return nil, err
	}
	lastSeen, err := m.Policy.LastManifestReleasedAt(ctx, channel)
	if err != nil {
		return nil, err
	}
	if err := manifest.CheckNotReplayed(mf, lastSeen); err != nil {
		return nil, err
	}
	return mf, nil
}

// CheckResult is one component's CHECK outcome -- what
// GET /api/v1/updates and POST /api/v1/updates/plan are built from.
type CheckResult struct {
	Component         string `json:"component"`
	Channel           string `json:"channel"`
	Current           string `json:"current,omitempty"`
	Latest            string `json:"latest,omitempty"`
	UpdateAvailable   bool   `json:"update_available"`
	Pinned            bool   `json:"pinned"`
	Compatible        bool   `json:"compatible"`
	Blocked           bool   `json:"blocked"`
	BlockReason       string `json:"block_reason,omitempty"`
	RestartRequired   bool   `json:"restart_required"`
	RollbackAvailable bool   `json:"rollback_available"`
	// CheckError is set when fetching/verifying the manifest itself failed
	// (network down, source misconfigured, bad signature, replay) --
	// distinct from Blocked, which means the check succeeded but policy
	// says no. Current/RollbackAvailable/RestartRequired/Pinned are still
	// populated even when this is set, since those don't require reaching
	// the update source at all: a dashboard should keep showing what's
	// installed even when it can't currently tell what's latest.
	CheckError string `json:"check_error,omitempty"`
}

// Check performs CHECK for one component: fetch+verify the manifest for its
// configured channel, compare against the installed version, and evaluate
// (without downloading or installing anything) whether an update is
// available, compatible, and/or blocked. This is exactly what
// POST /api/v1/updates/plan is a batch of -- see Plan.
func (m *Manager) Check(ctx context.Context, component string) (CheckResult, error) {
	// component reaches here straight from an UNAUTHENTICATED HTTP body
	// (POST /api/v1/updates/check and /updates/plan, which are
	// intentionally public read-only endpoints -- see agent/api/server.go).
	// It must be validated against the known-component allowlist before
	// anything derives a filesystem path from it: m.storeFor(component)
	// below joins it onto BaseDir, so an unvalidated "../../../etc"-style
	// value would let an anonymous caller make Store read a pointer file
	// named "current"/"previous" from an attacker-chosen directory outside
	// BaseDir and get its contents back in the JSON response. Rollback
	// already guards this way (see rollback.go); Install is safe because it
	// only reaches storeFor after confirming component is a key in the
	// manifest, which is itself signature-verified. Check had no such gate.
	if _, ok := m.Controllers[component]; !ok {
		return CheckResult{}, fmt.Errorf("%w: %s", ErrUnknownComponent, component)
	}
	channel, err := m.Policy.Channel(ctx, component)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{Component: component, Channel: channel}

	st := m.storeFor(component)
	cur, _, err := st.Current()
	if err != nil {
		return CheckResult{}, err
	}
	result.Current = cur
	if _, prevOK, err := st.Previous(); err != nil {
		return CheckResult{}, err
	} else {
		result.RollbackAvailable = prevOK
	}
	if ctrl, ok := m.Controllers[component]; ok {
		result.RestartRequired = ctrl.RequiresRestart()
	}
	pin, pinned, err := m.Policy.Pin(ctx, component)
	if err != nil {
		return CheckResult{}, err
	}
	result.Pinned = pinned

	mf, err := m.fetchVerifiedManifest(ctx, channel)
	if err != nil {
		result.CheckError = err.Error()
		return result, nil
	}
	comp, ok := mf.Components[component]
	if !ok {
		return result, nil // no entry for this component this release -- not an error
	}
	result.Latest = comp.Version

	if pinned && comp.Version != pin {
		result.Blocked = true
		result.BlockReason = fmt.Sprintf("pinned to %s", pin)
	}

	if cur == "" || version.GT(comp.Version, cur) {
		result.UpdateAvailable = true
	}

	cv := m.currentVersions()
	if err := manifest.CheckCompatibility(comp, cv.SuiteVersion, cv.QRXCoreVersion, cv.AdapterVersion); err != nil {
		if !result.Blocked {
			result.Blocked = true
			result.BlockReason = err.Error()
		}
	} else {
		result.Compatible = true
	}
	if ac, ok := m.Controllers[component].(*components.Adapter); ok && m.Matrix != nil {
		if err := CheckAdapterCompatibility(m.Matrix, cv.QRXCoreVersion, ac.AdapterName, comp.Version); err != nil {
			result.Compatible = false
			if !result.Blocked {
				result.Blocked = true
				result.BlockReason = err.Error()
			}
		}
	}
	return result, nil
}

// Plan runs Check for several components and returns their results in
// order -- the dry-run comparison table behind POST /api/v1/updates/plan
// (docs/updates.md: show every component's current->available, aggregate
// compatibility, which components need a restart, and whether rollback is
// available, all without installing anything).
func (m *Manager) Plan(ctx context.Context, componentNames []string) ([]CheckResult, error) {
	out := make([]CheckResult, 0, len(componentNames))
	for _, c := range componentNames {
		r, err := m.Check(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("plan: check %s: %w", c, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// InstallOptions controls one Install call.
type InstallOptions struct {
	// Channel overrides the component's configured channel for this
	// install only (used by "install from a specific channel/version" admin
	// actions). Empty uses the configured channel.
	Channel string
	// AllowDowngrade permits installing a version older than the current
	// one. Must only be set for an explicit, operator-confirmed manual
	// downgrade (docs/updates.md#downgrade-protection) -- never for an
	// automatic/background install.
	AllowDowngrade bool
	// Manual marks this as an operator-triggered install, which is exempt
	// from the scheduled update window (the window only restricts
	// automatic/background installs) but NOT exempt from the update lock.
	Manual bool
	// Force bypasses the update lock. Use sparingly and always audit it.
	Force bool
	// Actor identifies who/what triggered this install, for the audit log.
	Actor string
}

// InstallResult is what Install returns on success (which, for a
// self-binary component, means "staged and promoted, awaiting restart" --
// see PendingRestart).
type InstallResult struct {
	History        *storage.UpdateHistoryRecord
	PendingRestart bool
}

// Install runs the full safe update workflow from
// docs/updates.md#safe-update-process for one component:
// CHECK (via fetchVerifiedManifest) -> DOWNLOAD -> VERIFY -> STAGE ->
// BACKUP (implicit: Promote preserves the old current as previous) ->
// STOP -> INSTALL (Promote) -> START -> HEALTH CHECK -> COMMIT, with
// ROLLBACK on any failure from STOP onward. For a self-binary component
// (agent/adapter -- see components.SelfBinary) STOP/START/HEALTH
// CHECK/COMMIT are deferred to the next process restart; see
// ResumeSelfUpdate.
func (m *Manager) Install(ctx context.Context, component string, opts InstallOptions) (*InstallResult, error) {
	if locked, err := m.Policy.Locked(ctx); err != nil {
		return nil, err
	} else if locked && !opts.Force {
		return nil, ErrUpdatesLocked
	}
	if !opts.Manual {
		if w, configured, err := m.Policy.Window(ctx); err != nil {
			return nil, err
		} else if configured && !w.InWindow(time.Now()) {
			return nil, ErrOutsideWindow
		}
	}

	channel := opts.Channel
	if channel == "" {
		var err error
		channel, err = m.Policy.Channel(ctx, component)
		if err != nil {
			return nil, err
		}
	}

	mf, err := m.fetchVerifiedManifest(ctx, channel)
	if err != nil {
		return nil, err
	}
	comp, ok := mf.Components[component]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoUpdateForComponent, component)
	}

	if pin, pinned, err := m.Policy.Pin(ctx, component); err != nil {
		return nil, err
	} else if pinned && comp.Version != pin {
		return nil, fmt.Errorf("%w: pinned to %s, manifest offers %s", ErrPinned, pin, comp.Version)
	}

	st := m.storeFor(component)
	curVersion, _, err := st.Current()
	if err != nil {
		return nil, err
	}
	if curVersion != "" && curVersion == comp.Version {
		// Short-circuit before ever reaching st.Stage: staging the
		// already-active version would reuse the live release directory in
		// place (store.ErrAlreadyActive), and a failed extraction there
		// could destroy it -- see the F08 fix. "Already installed" is a
		// clean, expected outcome, not a failure, so this returns early
		// rather than letting it surface as a generic store error.
		return nil, fmt.Errorf("%w: %s", ErrAlreadyInstalled, comp.Version)
	}
	if err := manifest.CheckNotDowngrade(curVersion, comp.Version, opts.AllowDowngrade); err != nil {
		return nil, err
	}
	if prevVersion, ok, err := st.Previous(); err != nil {
		return nil, err
	} else if ok && prevVersion == comp.Version {
		// Same short-circuit as ErrAlreadyInstalled above, for the other
		// version Stage now also refuses (store.ErrAlreadyPrevious, R03):
		// reinstalling the exact version already recorded as the rollback
		// target should never go through Install at all -- Rollback is the
		// existing, atomic, no-download way to reactivate it, and reaching
		// Stage here would risk the previous release's own directory on a
		// failed extraction.
		return nil, fmt.Errorf("%w: %s -- use Rollback instead", ErrAlreadyPrevious, comp.Version)
	}

	cv := m.currentVersions()
	if err := manifest.CheckCompatibility(comp, cv.SuiteVersion, cv.QRXCoreVersion, cv.AdapterVersion); err != nil {
		return nil, err
	}

	ctrl, ok := m.Controllers[component]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownComponent, component)
	}
	if ac, ok := ctrl.(*components.Adapter); ok && m.Matrix != nil {
		if err := CheckAdapterCompatibility(m.Matrix, cv.QRXCoreVersion, ac.AdapterName, comp.Version); err != nil {
			return nil, err
		}
	}

	rec := storage.UpdateHistoryRecord{
		Component: component, FromVersion: curVersion, ToVersion: comp.Version, Channel: channel,
		Checksum: comp.SHA256, ManifestVersion: mf.ManifestVersion,
	}
	record := func(status storage.UpdateStatus, errMsg string, rollbackUsed bool) (*storage.UpdateHistoryRecord, error) {
		r := rec
		r.Status = status
		r.ErrorMessage = errMsg
		r.RollbackUsed = rollbackUsed
		id, insertErr := m.History.Insert(ctx, r)
		if insertErr != nil {
			return nil, insertErr
		}
		r.ID = id
		return &r, nil
	}
	if _, err := record(storage.UpdateStatusStarted, "", false); err != nil {
		return nil, err
	}

	// DOWNLOAD
	artifactReader, err := m.Source.OpenArtifact(ctx, comp.URL)
	if err != nil {
		record(storage.UpdateStatusFailed, err.Error(), false)
		return nil, err
	}
	tmp, err := os.CreateTemp("", "qrx-update-*")
	if err != nil {
		artifactReader.Close()
		record(storage.UpdateStatusFailed, err.Error(), false)
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	_, copyErr := io.Copy(tmp, artifactReader)
	artifactReader.Close()
	tmp.Close()
	if copyErr != nil {
		record(storage.UpdateStatusFailed, copyErr.Error(), false)
		return nil, copyErr
	}

	// VERIFY
	if err := manifest.VerifyArtifact(tmpPath, comp, m.PublicKey); err != nil {
		record(storage.UpdateStatusFailed, err.Error(), false)
		return nil, err
	}

	// STAGE
	dir, err := st.Stage(comp.Version)
	if err != nil {
		record(storage.UpdateStatusFailed, err.Error(), false)
		return nil, err
	}
	artifactFile, err := os.Open(tmpPath)
	if err != nil {
		st.DiscardStaged(true)
		record(storage.UpdateStatusFailed, err.Error(), false)
		return nil, err
	}
	extractErr := ctrl.Extract(ctx, artifactFile, dir)
	artifactFile.Close()
	if extractErr != nil {
		st.DiscardStaged(true)
		record(storage.UpdateStatusFailed, extractErr.Error(), false)
		return nil, extractErr
	}

	// Self-binary components (agent, adapter_*): promote now, defer
	// STOP/START/HEALTH CHECK/COMMIT to the next restart. See
	// components.SelfBinary's doc comment for why.
	if self, ok := ctrl.(components.SelfBinary); ok && self.IsSelfBinary() {
		if err := st.Promote(); err != nil {
			st.DiscardStaged(true)
			record(storage.UpdateStatusFailed, err.Error(), false)
			return nil, err
		}
		final, err := record(storage.UpdateStatusStarted, "staged and promoted; awaiting process restart to health-check and commit", false)
		if err != nil {
			return nil, err
		}
		pending := PendingSelfUpdate{Component: component, FromVersion: curVersion, ToVersion: comp.Version, Channel: channel, HistoryID: final.ID}
		if err := m.Settings.SetJSON(ctx, pendingSelfUpdateKey(component), pending); err != nil {
			return nil, fmt.Errorf("updates: promoted %s to %s but failed to record the pending-restart marker: %w", component, comp.Version, err)
		}
		if err := m.Policy.RecordManifestSeen(ctx, channel, mf.ReleasedAt); err != nil {
			return nil, err
		}
		return &InstallResult{History: final, PendingRestart: true}, nil
	}

	// Every other component: synchronous STOP/INSTALL/START/HEALTH CHECK.
	if ctrl.RequiresRestart() {
		if err := ctrl.Stop(ctx); err != nil {
			st.DiscardStaged(true)
			record(storage.UpdateStatusFailed, err.Error(), false)
			return nil, err
		}
	}
	if err := st.Promote(); err != nil {
		record(storage.UpdateStatusFailed, err.Error(), false)
		return nil, err
	}
	if err := ctrl.Start(ctx); err != nil {
		rollbackErr := m.rollbackAndRestart(ctx, st, ctrl)
		final, _ := record(storage.UpdateStatusRolledBack, combineErrors("start failed", err, rollbackErr), true)
		return &InstallResult{History: final}, fmt.Errorf("updates: start failed, rolled back: %w", err)
	}
	if err := ctrl.HealthCheck(ctx); err != nil {
		rollbackErr := m.rollbackAndRestart(ctx, st, ctrl)
		final, _ := record(storage.UpdateStatusRolledBack, combineErrors("health check failed", err, rollbackErr), true)
		return &InstallResult{History: final}, fmt.Errorf("updates: health check failed, rolled back: %w", err)
	}

	// COMMIT
	_ = st.Prune(m.retainVersions())
	if err := m.Policy.RecordManifestSeen(ctx, channel, mf.ReleasedAt); err != nil {
		return nil, err
	}
	final, err := record(storage.UpdateStatusSucceeded, "", false)
	if err != nil {
		return nil, err
	}
	if m.Audit != nil {
		m.Audit.Record(ctx, storage.AuditEvent{
			Actor: opts.Actor, Action: auditActionForInstall(component), Component: component,
			Details: fmt.Sprintf("%s -> %s", curVersion, comp.Version),
		})
	}
	return &InstallResult{History: final}, nil
}

// rollbackAndRestart reverts the store to the previous version and
// restarts the controller on it, best-effort (the failure that triggered
// this is what gets returned to the caller; a secondary failure here is
// appended to that error rather than replacing it).
func (m *Manager) rollbackAndRestart(ctx context.Context, st *store.Store, ctrl components.Controller) error {
	if err := st.RollbackToPrevious(); err != nil {
		return err
	}
	return ctrl.Start(ctx)
}

func combineErrors(prefix string, primary, secondary error) string {
	if secondary == nil {
		return fmt.Sprintf("%s: %v", prefix, primary)
	}
	return fmt.Sprintf("%s: %v (rollback also encountered an error: %v)", prefix, primary, secondary)
}

func auditActionForInstall(component string) string {
	switch {
	case component == "agent":
		return storage.ActionUpdateAgent
	case component == "dashboard":
		return storage.ActionUpdateDashboard
	default:
		return storage.ActionUpdateAdapter
	}
}
