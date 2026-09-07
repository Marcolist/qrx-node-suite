package updates

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates/manifest"
	"qrx-node-suite/agent/updates/sources"
	"qrx-node-suite/agent/updates/store"
	"qrx-node-suite/agent/version"
)

// QRXCoreComponent is the manifest/store/settings component name for QRX
// Core. It is deliberately never routed through Manager -- QRX Core gets
// its own, more conservative workflow. See docs/updates.md#qrx-core-updates.
const QRXCoreComponent = "qrx_core"

var (
	// ErrValidatorAutomaticDisabled is returned unconditionally for any
	// automatic (non-manual) QRX Core update attempt on a validator node.
	// There is no policy setting that overrides this -- see
	// QRXCoreUpdateManager.IsValidatorNode's doc comment.
	ErrValidatorAutomaticDisabled = errors.New("qrx_core: automatic updates are disabled on validator nodes; use an explicit manual update")
	// ErrSwitchSafetyUnknown means the target compatibility profile leaves
	// at least one of blockchain data/config/wallet/adapter/network
	// compatibility unrecorded, and no expert override was given.
	ErrSwitchSafetyUnknown = errors.New("qrx_core: switch safety has unknown dimensions; blocked without an explicit expert override")
	// ErrSwitchSafetyIncompatible means the profile explicitly records an
	// incompatibility. This is NEVER overridable -- an "expert override" is
	// for proceeding past uncertainty, not past a known-bad combination.
	ErrSwitchSafetyIncompatible = errors.New("qrx_core: switch safety reports an explicit incompatibility; this is never overridable")
	// ErrCoreNotInstalled means the requested version isn't present under
	// this manager's store.
	ErrCoreNotInstalled = errors.New("qrx_core: requested version is not installed locally")
	// ErrCoreValidationFailed wraps a post-start QRXCoreHealth validation
	// failure that triggered an automatic binary rollback.
	ErrCoreValidationFailed = errors.New("qrx_core: post-update validation failed, binary rolled back")
)

// QRXCoreHealth is what QRXCoreUpdateManager checks after starting a QRX
// Core binary, per docs/updates.md#qrx-core-updates "validate": process
// health, network connection, block height progress, peer count, and
// validator state where available. Fields left at their zero value because
// a probe implementation can't determine them must be reported as such
// (Available=false), never guessed -- see QRXCoreHealthProbe.
type QRXCoreHealth struct {
	ProcessRunning         bool
	NetworkConnected       bool
	BlockHeightProgressing bool
	PeerCount              int
	ValidatorState         string // "" if not a validator / not available
	Available              bool   // false if the probe itself couldn't run at all (e.g. CLI not reachable)
	Detail                 string
}

// Healthy reports whether every checked dimension passed. A probe that
// couldn't run at all (Available=false) is never healthy.
func (h QRXCoreHealth) Healthy() bool {
	return h.Available && h.ProcessRunning && h.NetworkConnected && h.BlockHeightProgressing
}

// QRXCoreHealthProbe validates a running QRX Core after a binary
// switch/update. Production wiring wraps agent/qrx.Runner (sampling height
// twice with a short delay to confirm progress); tests use a fake.
type QRXCoreHealthProbe interface {
	Validate(ctx context.Context) (QRXCoreHealth, error)
}

// QRXCoreUpdateManager implements docs/updates.md#qrx-core-updates and
// #qrx-core-version-selection and #core-version-switching-safety. Unlike
// Manager, it:
//   - defaults to entirely manual (no scheduled window, no auto-install path
//     is exposed at all -- Update must always be explicitly invoked),
//   - hard-disables automatic invocation on validator nodes,
//   - never touches the QRX data directory,
//   - requires a version.CompatibilityProfile's explicit switch-safety
//     judgement (not just a compatibility matrix SUPPORTED status) before
//     switching versions, and
//   - validates with QRXCoreHealthProbe (process/network/height/peers/
//     validator state) instead of a generic Controller.HealthCheck.
type QRXCoreUpdateManager struct {
	Source    sources.Source
	PublicKey ed25519.PublicKey

	History *storage.UpdateHistoryStore
	Audit   *storage.AuditLogStore
	Policy  *Policy
	BaseDir string // root for store.New(BaseDir, QRXCoreComponent)
	Probe   QRXCoreHealthProbe

	// StopCore/StartCore drive the qrxd service (agent/platform, wired in
	// cmd/agentd). StartCore receives the release directory to run from.
	StopCore  func(ctx context.Context) error
	StartCore func(ctx context.Context, releaseDir string) error

	// IsValidatorNode hard-disables automatic updates when true, with no
	// policy-level override -- "Automatic upgrades must never force a
	// validator onto an untested QRX Core version" (docs/updates.md).
	IsValidatorNode bool

	RetainVersions int
}

func (m *QRXCoreUpdateManager) retainVersions() int {
	if m.RetainVersions <= 0 {
		return 2
	}
	return m.RetainVersions
}

func (m *QRXCoreUpdateManager) store() *store.Store {
	return store.New(m.BaseDir, QRXCoreComponent)
}

// AutomaticEnabled reports whether the configured channel is anything other
// than "manual" -- i.e. whether an automatic caller (were one ever wired
// up) would be allowed to proceed. Unlike every other component (whose
// unconfigured default is "stable", Policy.Channel), QRX Core's
// unconfigured default is "manual" (docs/updates.md#qrx-core-updates:
// "Default update policy: Manual"), hence ChannelOrDefault with an explicit
// ChannelManual fallback rather than Policy.Channel. Also validator-node-aware.
func (m *QRXCoreUpdateManager) AutomaticEnabled(ctx context.Context) (bool, error) {
	if m.IsValidatorNode {
		return false, nil
	}
	channel, err := m.Policy.ChannelOrDefault(ctx, QRXCoreComponent, ChannelManual)
	if err != nil {
		return false, err
	}
	return channel != ChannelManual, nil
}

// CheckSwitchSafety enforces docs/updates.md#core-version-switching-safety:
// every dimension (blockchain data, config, wallet, adapter, network) must
// be an explicit "compatible" judgement, or the switch is blocked. An
// unknown dimension can be bypassed with expertOverride; an explicitly
// incompatible one never can.
func CheckSwitchSafety(profile *version.CompatibilityProfile, expertOverride bool) error {
	if profile.CoreVersionSwitchSafety.AnyIncompatible() {
		return ErrSwitchSafetyIncompatible
	}
	if !profile.CoreVersionSwitchSafety.AllKnown() && !expertOverride {
		return ErrSwitchSafetyUnknown
	}
	return nil
}

// SwitchOptions controls both SwitchVersion (among already-installed
// versions) and Update (fetch+install a new one).
type SwitchOptions struct {
	// Profile is the target QRX Core version's compatibility profile;
	// CheckSwitchSafety is evaluated against it before anything else
	// happens.
	Profile *version.CompatibilityProfile
	// ExpertOverride bypasses an UNKNOWN switch-safety dimension. Never
	// bypasses an explicitly INCOMPATIBLE one.
	ExpertOverride bool
	Actor          string
}

// SwitchVersion activates an already-installed QRX Core version (no
// download): docs/updates.md#qrx-core-version-selection's "Switch Version"
// action. Runs the same safety-check -> stop -> activate -> start ->
// validate -> rollback-on-failure sequence as Update, just without
// download/verify since the binary is already on disk.
func (m *QRXCoreUpdateManager) SwitchVersion(ctx context.Context, targetVersion string, opts SwitchOptions) (*storage.UpdateHistoryRecord, error) {
	if opts.Profile == nil || opts.Profile.QRXCoreVersion != targetVersion {
		return nil, fmt.Errorf("qrx_core: a compatibility profile for %s is required to switch versions", targetVersion)
	}
	if err := CheckSwitchSafety(opts.Profile, opts.ExpertOverride); err != nil {
		return nil, err
	}
	st := m.store()
	installed, err := st.InstalledVersions()
	if err != nil {
		return nil, err
	}
	if !contains(installed, targetVersion) {
		return nil, fmt.Errorf("%w: %s", ErrCoreNotInstalled, targetVersion)
	}
	fromVersion, _, err := st.Current()
	if err != nil {
		return nil, err
	}
	return m.activateAndValidate(ctx, st, targetVersion, fromVersion, opts.Actor, "manual version switch")
}

// UpdateOptions controls Update.
type UpdateOptions struct {
	Channel        string
	AllowDowngrade bool
	SwitchOptions
}

// Update fetches, verifies, and installs a new QRX Core version from the
// configured source: CHECK -> compatibility+adapter support check ->
// DOWNLOAD -> VERIFY checksum/signature -> STOP -> replace binaries ->
// START -> VALIDATE (process/network/height/peers/validator state) ->
// COMMIT, or automatic binary ROLLBACK on any validation failure. This is
// always an explicit, manual call in this codebase -- nothing schedules it
// automatically; see AutomaticEnabled's doc comment for why that matters.
// The Node Suite data directory is never referenced or modified by this
// method at all, only the versioned binary tree under BaseDir -- QRX Core's
// actual blockchain data directory is entirely out of scope for this
// manager, by design (docs/updates.md: "preserve QRX data directory").
func (m *QRXCoreUpdateManager) Update(ctx context.Context, opts UpdateOptions) (*storage.UpdateHistoryRecord, error) {
	if opts.Profile == nil {
		return nil, errors.New("qrx_core: a target compatibility profile is required")
	}
	if err := CheckSwitchSafety(opts.Profile, opts.ExpertOverride); err != nil {
		return nil, err
	}

	// Note: this is the release channel to fetch FROM for this explicit,
	// operator-triggered call -- distinct from the "manual" policy value
	// AutomaticEnabled checks, which governs whether anything may call
	// Update on its own (nothing in this codebase does). Defaults to
	// stable, same as Policy.Channel's normal default, since an admin
	// manually checking "what's out there" with no channel override wants
	// the ordinary stable release, not an interpretation of "manual" as a
	// fetchable channel name.
	channel := opts.Channel
	if channel == "" {
		var err error
		channel, err = m.Policy.Channel(ctx, QRXCoreComponent)
		if err != nil {
			return nil, err
		}
	}
	mf, err := m.fetchVerifiedManifest(ctx, channel)
	if err != nil {
		return nil, err
	}
	comp, ok := mf.Components[QRXCoreComponent]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoUpdateForComponent, QRXCoreComponent)
	}
	// Same cross-check SwitchVersion already makes (opts.Profile.
	// QRXCoreVersion != targetVersion, above): without it, CheckSwitchSafety
	// validates whatever profile the caller happened to pass, not
	// necessarily one that actually describes comp.Version -- the version
	// this call is about to fetch and switch to. A caller (or a stale/buggy
	// automation) could pass a profile for a version it already vetted
	// while the manifest offers a different one, and get that unrelated
	// profile's judgement instead of the real target's. F12 fix, external
	// security audit.
	if opts.Profile.QRXCoreVersion != comp.Version {
		return nil, fmt.Errorf("qrx_core: compatibility profile is for %s, but the manifest offers %s -- refusing to switch-safety-check the wrong version's profile", opts.Profile.QRXCoreVersion, comp.Version)
	}

	st := m.store()
	fromVersion, _, err := st.Current()
	if err != nil {
		return nil, err
	}
	if err := manifest.CheckNotDowngrade(fromVersion, comp.Version, opts.AllowDowngrade); err != nil {
		return nil, err
	}

	// DOWNLOAD
	artifact, err := m.Source.OpenArtifact(ctx, comp.URL)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "qrx-core-*")
	if err != nil {
		artifact.Close()
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	_, copyErr := io.Copy(tmp, artifact)
	artifact.Close()
	tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}

	// VERIFY
	if err := manifest.VerifyArtifact(tmpPath, comp, m.PublicKey); err != nil {
		return nil, err
	}

	// Stage the binary onto disk under its version.
	dir, err := st.Stage(comp.Version)
	if err != nil {
		return nil, err
	}
	src, err := os.Open(tmpPath)
	if err != nil {
		st.DiscardStaged(true)
		return nil, err
	}
	extractErr := writeCoreBinary(src, dir)
	src.Close()
	if extractErr != nil {
		st.DiscardStaged(true)
		return nil, extractErr
	}
	if err := st.Promote(); err != nil {
		st.DiscardStaged(true)
		return nil, err
	}

	if err := m.Policy.RecordManifestSeen(ctx, channel, mf.ReleasedAt); err != nil {
		return nil, err
	}
	return m.activateAndValidate(ctx, st, comp.Version, fromVersion, opts.Actor, "update")
}

// activateAndValidate runs STOP -> START -> VALIDATE -> COMMIT-or-ROLLBACK
// for a version already staged/promoted (or, for SwitchVersion, already
// installed) as "current". It assumes st.Current() already reflects
// targetVersion by the time it's called -- SwitchVersion promotes before
// calling this via a dedicated path below.
func (m *QRXCoreUpdateManager) activateAndValidate(ctx context.Context, st *store.Store, targetVersion, fromVersion, actor, kind string) (*storage.UpdateHistoryRecord, error) {
	// For SwitchVersion, activation (the pointer swap) hasn't happened yet
	// at this point -- do it now, after the stop, matching
	// docs/updates.md's "stop QRX Core -> replace binaries -> start" order.
	if kind == "manual version switch" {
		if m.StopCore != nil {
			if err := m.StopCore(ctx); err != nil {
				return nil, fmt.Errorf("qrx_core: stop before switch: %w", err)
			}
		}
		if err := st.PromoteVersion(targetVersion); err != nil {
			return nil, err
		}
	} else if m.StopCore != nil {
		if err := m.StopCore(ctx); err != nil {
			return nil, fmt.Errorf("qrx_core: stop before start: %w", err)
		}
	}

	dir := st.ReleaseDir(targetVersion)
	if m.StartCore != nil {
		if err := m.StartCore(ctx, dir); err != nil {
			return m.rollbackBinary(ctx, st, targetVersion, fromVersion, actor, err.Error())
		}
	}

	health, err := m.probeOrUnavailable(ctx)
	if err != nil || !health.Healthy() {
		reason := health.Detail
		if err != nil {
			reason = err.Error()
		}
		return m.rollbackBinary(ctx, st, targetVersion, fromVersion, actor, reason)
	}

	_ = st.Prune(m.retainVersions())
	rec := storage.UpdateHistoryRecord{Component: QRXCoreComponent, FromVersion: fromVersion, ToVersion: targetVersion, Status: storage.UpdateStatusSucceeded}
	id, err := m.History.Insert(ctx, rec)
	if err != nil {
		return nil, err
	}
	rec.ID = id
	if m.Audit != nil {
		m.Audit.Record(ctx, storage.AuditEvent{Actor: actor, Action: storage.ActionSwitchQRXCore, Component: QRXCoreComponent, Details: fmt.Sprintf("%s: %s -> %s", kind, fromVersion, targetVersion)})
	}
	return &rec, nil
}

func (m *QRXCoreUpdateManager) probeOrUnavailable(ctx context.Context) (QRXCoreHealth, error) {
	if m.Probe == nil {
		return QRXCoreHealth{}, errors.New("qrx_core: no health probe configured")
	}
	return m.Probe.Validate(ctx)
}

func (m *QRXCoreUpdateManager) rollbackBinary(ctx context.Context, st *store.Store, targetVersion, fromVersion, actor, reason string) (*storage.UpdateHistoryRecord, error) {
	var rollbackErr error
	if fromVersion != "" {
		rollbackErr = st.PromoteVersion(fromVersion)
		if rollbackErr == nil && m.StopCore != nil {
			rollbackErr = m.StopCore(ctx)
		}
		if rollbackErr == nil && m.StartCore != nil {
			rollbackErr = m.StartCore(ctx, st.ReleaseDir(fromVersion))
		}
	}
	rec := storage.UpdateHistoryRecord{
		Component: QRXCoreComponent, FromVersion: fromVersion, ToVersion: targetVersion,
		Status: storage.UpdateStatusRolledBack, RollbackUsed: true,
		ErrorMessage: combineErrors("validation failed", errors.New(reason), rollbackErr),
	}
	id, insertErr := m.History.Insert(ctx, rec)
	if insertErr != nil {
		return nil, insertErr
	}
	rec.ID = id
	if m.Audit != nil {
		m.Audit.Record(ctx, storage.AuditEvent{Actor: actor, Action: storage.ActionRollbackQRXCore, Component: QRXCoreComponent, Details: reason})
	}
	return &rec, fmt.Errorf("%w: %s", ErrCoreValidationFailed, reason)
}

func (m *QRXCoreUpdateManager) fetchVerifiedManifest(ctx context.Context, channel string) (*manifest.Manifest, error) {
	mf, err := m.Source.FetchManifest(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("qrx_core: fetch manifest: %w", err)
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

func writeCoreBinary(src io.Reader, dir string) error {
	path := dir + string(os.PathSeparator) + "qrxd"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
