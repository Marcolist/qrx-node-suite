package updates_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates"
	"qrx-node-suite/agent/updates/components"
	"qrx-node-suite/agent/updates/manifest"
	"qrx-node-suite/agent/updates/sources"
	"qrx-node-suite/agent/updates/store"
)

// fakeController is a synchronous (non-self-binary) Controller test double.
type fakeController struct {
	extractedContent []byte
	startErr         error
	healthErr        error
	stopCalls        int
	startCalls       int
	healthCalls      int
	requiresRestart  bool
}

func (f *fakeController) RequiresRestart() bool { return f.requiresRestart }
func (f *fakeController) Stop(ctx context.Context) error {
	f.stopCalls++
	return nil
}
func (f *fakeController) Start(ctx context.Context) error {
	f.startCalls++
	return f.startErr
}
func (f *fakeController) HealthCheck(ctx context.Context) error {
	f.healthCalls++
	return f.healthErr
}
func (f *fakeController) Extract(ctx context.Context, artifact io.Reader, dir string) error {
	b, err := io.ReadAll(artifact)
	if err != nil {
		return err
	}
	f.extractedContent = b
	return os.WriteFile(filepath.Join(dir, "marker"), b, 0o644)
}

// fakeSelfBinary is a components.SelfBinary test double.
type fakeSelfBinary struct {
	fakeController
}

func (f *fakeSelfBinary) IsSelfBinary() bool { return true }

var _ components.Controller = (*fakeController)(nil)
var _ components.SelfBinary = (*fakeSelfBinary)(nil)

type testEnv struct {
	t       *testing.T
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
	db      *sql.DB
	mgr     *updates.Manager
	src     *sources.DevelopmentSource
	baseDir string
}

func newTestEnv(t *testing.T, controllers map[string]components.Controller) *testEnv {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	settings := storage.NewSettingsStore(db)
	policy := updates.NewPolicy(settings)
	src := sources.NewDevelopmentSource()
	baseDir := t.TempDir()

	mgr := &updates.Manager{
		Source:      src,
		PublicKey:   pub,
		History:     storage.NewUpdateHistoryStore(db),
		Audit:       storage.NewAuditLogStore(db),
		Settings:    settings,
		Policy:      policy,
		BaseDir:     baseDir,
		Controllers: controllers,
		CurrentVersions: func() updates.CurrentVersions {
			return updates.CurrentVersions{SuiteVersion: "0.1.0", QRXCoreVersion: "0.0.7", AdapterVersion: "1.0.0"}
		},
	}
	return &testEnv{t: t, pub: pub, priv: priv, db: db, mgr: mgr, src: src, baseDir: baseDir}
}

func (e *testEnv) registerManifest(t *testing.T, channel string, components map[string]string, releasedAt time.Time) *manifest.Manifest {
	t.Helper()
	comps := map[string]manifest.ComponentUpdate{}
	for name, content := range components {
		url := "dev://" + name + "/" + content
		sum := sha256.Sum256([]byte(content))
		sumHex := hex.EncodeToString(sum[:])
		sig, err := manifest.SignComponentChecksum(sumHex, e.priv)
		if err != nil {
			t.Fatalf("SignComponentChecksum: %v", err)
		}
		e.src.Artifacts[url] = []byte(content)
		version := "1.0.0"
		if len(content) > 0 {
			version = content // tests pass content == desired version string directly
		}
		comps[name] = manifest.ComponentUpdate{Version: version, URL: url, SHA256: sumHex, Signature: sig}
	}
	m := &manifest.Manifest{
		ManifestVersion: 1,
		Channel:         channel,
		SuiteVersion:    "0.2.0",
		ReleasedAt:      releasedAt.UTC().Format(time.RFC3339),
		Components:      comps,
	}
	manifest.SignManifest(m, e.priv)
	e.src.Manifests[channel] = m
	return m
}

func TestInstallHappyPathSyncComponent(t *testing.T) {
	ctrl := &fakeController{requiresRestart: true}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
	env.registerManifest(t, "stable", map[string]string{"dashboard": "0.2.0"}, time.Now())

	res, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{Actor: "test"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.PendingRestart {
		t.Error("sync component should not report PendingRestart")
	}
	if res.History.Status != storage.UpdateStatusSucceeded {
		t.Errorf("status = %v, want succeeded", res.History.Status)
	}
	if ctrl.stopCalls != 1 || ctrl.startCalls != 1 || ctrl.healthCalls != 1 {
		t.Errorf("lifecycle calls = stop:%d start:%d health:%d, want 1/1/1", ctrl.stopCalls, ctrl.startCalls, ctrl.healthCalls)
	}
	if string(ctrl.extractedContent) != "0.2.0" {
		t.Errorf("extracted content = %q, want 0.2.0", ctrl.extractedContent)
	}
}

func TestInstallHealthCheckFailureRollsBack(t *testing.T) {
	ctrl := &fakeController{requiresRestart: false}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})

	// First install a good version 1.0.0 so there's something to roll back to.
	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())
	if _, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{}); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// Now a bad 2.0.0 whose health check fails.
	ctrl.healthErr = errors.New("simulated health check failure")
	env.registerManifest(t, "stable", map[string]string{"dashboard": "2.0.0"}, time.Now().Add(time.Minute))
	res, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{})
	if err == nil {
		t.Fatal("expected Install to fail when health check fails")
	}
	if res == nil || res.History.Status != storage.UpdateStatusRolledBack {
		t.Fatalf("expected a rolled_back history record, got %+v", res)
	}

	st := env.currentVersion(t, "dashboard")
	if st != "1.0.0" {
		t.Errorf("after failed health check, current = %q, want rollback to 1.0.0", st)
	}
}

func (e *testEnv) currentVersion(t *testing.T, component string) string {
	t.Helper()
	// Use a fresh Check call's Current field rather than reaching into
	// store internals, exercising the same path the API would.
	res, err := e.mgr.Check(context.Background(), component)
	if err != nil {
		t.Fatalf("Check(%s): %v", component, err)
	}
	return res.Current
}

func TestInstallBlocksDowngradeByDefault(t *testing.T) {
	ctrl := &fakeController{}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})

	env.registerManifest(t, "stable", map[string]string{"dashboard": "2.0.0"}, time.Now())
	if _, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 2.0.0: %v", err)
	}

	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now().Add(time.Minute))
	_, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{})
	if err == nil {
		t.Fatal("expected downgrade to be blocked")
	}

	_, err = env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{AllowDowngrade: true})
	if err != nil {
		t.Fatalf("expected explicit AllowDowngrade to permit it, got %v", err)
	}
}

func TestInstallBlockedByPin(t *testing.T) {
	ctrl := &fakeController{}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
	ctx := context.Background()

	if err := env.mgr.Policy.SetPin(ctx, "dashboard", "1.0.0"); err != nil {
		t.Fatalf("SetPin: %v", err)
	}
	env.registerManifest(t, "stable", map[string]string{"dashboard": "2.0.0"}, time.Now())

	_, err := env.mgr.Install(ctx, "dashboard", updates.InstallOptions{})
	if !errors.Is(err, updates.ErrPinned) {
		t.Fatalf("expected ErrPinned, got %v", err)
	}
}

func TestInstallBlockedByUpdateLock(t *testing.T) {
	ctrl := &fakeController{}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
	ctx := context.Background()

	if err := env.mgr.Policy.SetLocked(ctx, true); err != nil {
		t.Fatalf("SetLocked: %v", err)
	}
	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())

	_, err := env.mgr.Install(ctx, "dashboard", updates.InstallOptions{})
	if !errors.Is(err, updates.ErrUpdatesLocked) {
		t.Fatalf("expected ErrUpdatesLocked, got %v", err)
	}

	if _, err := env.mgr.Install(ctx, "dashboard", updates.InstallOptions{Force: true}); err != nil {
		t.Fatalf("expected Force to bypass the lock, got %v", err)
	}
}

func TestSelfBinaryInstallDefersToResume(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})
	env.registerManifest(t, "stable", map[string]string{"agent": "0.2.0"}, time.Now())

	res, err := env.mgr.Install(context.Background(), "agent", updates.InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.PendingRestart {
		t.Fatal("expected PendingRestart for a self-binary component")
	}
	if ctrl.startCalls != 0 || ctrl.healthCalls != 0 {
		t.Errorf("self-binary Install must not call Start/HealthCheck synchronously: start=%d health=%d", ctrl.startCalls, ctrl.healthCalls)
	}
	if got := env.currentVersion(t, "agent"); got != "0.2.0" {
		t.Errorf("Promote should have happened even though restart is pending: current = %q, want 0.2.0", got)
	}

	pending, err := env.mgr.PendingSelfUpdates(context.Background(), []string{"agent"})
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingSelfUpdates = %v, %v; want exactly one entry", pending, err)
	}
}

func TestResumeSelfUpdateCommitsWhenHealthy(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})
	env.registerManifest(t, "stable", map[string]string{"agent": "0.2.0"}, time.Now())
	if _, err := env.mgr.Install(context.Background(), "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	ctrl.healthErr = nil // this process is now "running" the new version and is healthy
	rec, err := env.mgr.ResumeSelfUpdate(context.Background(), "agent")
	if err != nil {
		t.Fatalf("ResumeSelfUpdate: %v", err)
	}
	if rec.Status != storage.UpdateStatusSucceeded {
		t.Errorf("status = %v, want succeeded", rec.Status)
	}
	pending, _ := env.mgr.PendingSelfUpdates(context.Background(), []string{"agent"})
	if len(pending) != 0 {
		t.Error("expected pending marker to be cleared after a successful resume")
	}
}

func TestResumeSelfUpdateRollsBackWhenUnhealthy(t *testing.T) {
	ctrl := &fakeSelfBinary{}
	env := newTestEnv(t, map[string]components.Controller{"agent": ctrl})

	env.registerManifest(t, "stable", map[string]string{"agent": "1.0.0"}, time.Now())
	if _, err := env.mgr.Install(context.Background(), "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 1.0.0: %v", err)
	}
	if _, err := env.mgr.ResumeSelfUpdate(context.Background(), "agent"); err != nil {
		t.Fatalf("resume 1.0.0: %v", err)
	}

	env.registerManifest(t, "stable", map[string]string{"agent": "2.0.0"}, time.Now().Add(time.Minute))
	if _, err := env.mgr.Install(context.Background(), "agent", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 2.0.0: %v", err)
	}

	ctrl.healthErr = errors.New("new binary is unhealthy")
	rec, err := env.mgr.ResumeSelfUpdate(context.Background(), "agent")
	if err != nil {
		t.Fatalf("ResumeSelfUpdate: %v", err)
	}
	if rec.Status != storage.UpdateStatusRolledBack {
		t.Errorf("status = %v, want rolled_back", rec.Status)
	}
	if got := env.currentVersion(t, "agent"); got != "1.0.0" {
		t.Errorf("current after rollback = %q, want 1.0.0", got)
	}
}

func TestCheckReportsIncompatibleUpdateAsBlocked(t *testing.T) {
	ctrl := &fakeController{}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})

	comps := map[string]manifest.ComponentUpdate{
		"dashboard": {
			Version:   "3.0.0",
			URL:       "dev://dashboard/3.0.0",
			SHA256:    sha256Hex(t, "3.0.0"),
			Signature: mustSign(t, env.priv, "3.0.0"),
			ReleaseNotes: &manifest.ReleaseNotes{
				RequiredQRXVersion: ">=0.0.9", // env's CurrentVersions reports 0.0.7
			},
		},
	}
	env.src.Artifacts["dev://dashboard/3.0.0"] = []byte("3.0.0")
	m := &manifest.Manifest{ManifestVersion: 1, Channel: "stable", SuiteVersion: "0.3.0", ReleasedAt: time.Now().UTC().Format(time.RFC3339), Components: comps}
	manifest.SignManifest(m, env.priv)
	env.src.Manifests["stable"] = m

	result, err := env.mgr.Check(context.Background(), "dashboard")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Blocked {
		t.Error("expected an update requiring an unmet QRX Core version to be reported as Blocked")
	}
	if result.Compatible {
		t.Error("expected Compatible=false")
	}
}

func TestCheckReportsCheckErrorInsteadOfFailingOutright(t *testing.T) {
	ctrl := &fakeController{requiresRestart: true}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
	// Install a version so there's real Current/RollbackAvailable data to
	// preserve even when the manifest fetch below fails.
	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())
	if _, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 1.0.0: %v", err)
	}
	// No manifest registered for "stable" now (simulates a source outage /
	// misconfiguration) -- Check must not fail outright, since a dashboard
	// showing "what's installed" should survive "can't reach the update
	// server right now."
	delete(env.src.Manifests, "stable")

	result, err := env.mgr.Check(context.Background(), "dashboard")
	if err != nil {
		t.Fatalf("Check returned an error instead of a partial result: %v", err)
	}
	if result.CheckError == "" {
		t.Error("expected CheckError to be set")
	}
	if result.Current != "1.0.0" {
		t.Errorf("Current = %q, want 1.0.0 (must survive a failed manifest fetch)", result.Current)
	}
}

// TestInstallSameVersionAgainIsCleanNoopNotDataLoss is a regression test
// for the F08 finding (external security audit): installing a version
// that's already active must be a clean, expected "nothing to do" outcome
// (ErrAlreadyInstalled), never reach store.Store.Stage at all -- otherwise
// a manifest offering the same version again (e.g. a re-check on the same
// channel) followed by any Extract failure could destroy the still-active
// release (see store.ErrAlreadyActive and the store-level regression test
// in agent/updates/store/store_test.go).
func TestInstallSameVersionAgainIsCleanNoopNotDataLoss(t *testing.T) {
	ctrl := &fakeController{}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())

	if _, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{}); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// Same manifest, same version, offered again -- Install must refuse
	// cleanly rather than re-extracting into the live release directory.
	_, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{})
	if !errors.Is(err, updates.ErrAlreadyInstalled) {
		t.Fatalf("second install of the same version = %v, want ErrAlreadyInstalled", err)
	}

	// The originally-installed content must be completely intact.
	dir, derr := store.New(env.baseDir, "dashboard").CurrentDir()
	if derr != nil {
		t.Fatalf("CurrentDir: %v", derr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "marker")); statErr != nil {
		t.Fatalf("live release content is gone after a same-version reinstall attempt: %v", statErr)
	}
}

// TestInstallRefusesReinstallingThePreviousVersion is a regression test
// for the R03 finding (an external re-review of the F08 fix): F08 only
// short-circuited Install for the CURRENTLY ACTIVE version, not the
// PREVIOUS (rollback-target) one -- and CheckNotDowngrade explicitly
// permits re-offering an older version when AllowDowngrade is set, so
// Install would reach store.Store.Stage for the previous version, which
// (before the store-level R03 fix) reused that version's own live
// directory as a staging target and could destroy it on a failed
// extraction. This proves Install now refuses early instead, and that
// the previous release survives and Rollback still works.
func TestInstallRefusesReinstallingThePreviousVersion(t *testing.T) {
	ctrl := &fakeController{}
	env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})

	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())
	if _, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 1.0.0: %v", err)
	}
	env.registerManifest(t, "stable", map[string]string{"dashboard": "2.0.0"}, time.Now().Add(time.Minute))
	if _, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{}); err != nil {
		t.Fatalf("install 2.0.0: %v", err)
	}
	// current=2.0.0, previous=1.0.0.

	env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now().Add(2*time.Minute))
	_, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{AllowDowngrade: true})
	if !errors.Is(err, updates.ErrAlreadyPrevious) {
		t.Fatalf("reinstalling the previous version with AllowDowngrade = %v, want ErrAlreadyPrevious", err)
	}

	// The previous release's content must be intact, and Rollback (the
	// correct way to reactivate it) must still work.
	dir := store.New(env.baseDir, "dashboard").ReleaseDir("1.0.0")
	if _, statErr := os.Stat(filepath.Join(dir, "marker")); statErr != nil {
		t.Fatalf("previous release content is gone: %v", statErr)
	}
	if _, err := env.mgr.Rollback(context.Background(), "dashboard", "test"); err != nil {
		t.Fatalf("Rollback after refused reinstall: %v", err)
	}
	if got := env.currentVersion(t, "dashboard"); got != "1.0.0" {
		t.Errorf("current after Rollback = %q, want 1.0.0", got)
	}
}

// TestCheckRejectsUnknownComponentBeforeTouchingStore is a regression test
// for the F03 finding (external security audit): POST /api/v1/updates/check
// and /updates/plan are intentionally unauthenticated (docs/updates.md#update-status),
// so Check must reject any component name that isn't a registered
// Controller BEFORE ever building a store.Store for it -- otherwise an
// anonymous caller could pass a "../"-style component and have
// m.storeFor(component) resolve outside BaseDir (see
// agent/updates/store's ErrInvalidComponent test for the store-layer half
// of this defense). This proves the rejection happens at the Manager layer,
// for a name that isn't even path-traversal-shaped (a component simply not
// in Controllers), matching the same allowlist Rollback already enforces.
func TestCheckRejectsUnknownComponentBeforeTouchingStore(t *testing.T) {
	env := newTestEnv(t, map[string]components.Controller{"dashboard": &fakeController{}})

	for _, bad := range []string{"../../etc", "..", "unknown-component", ""} {
		_, err := env.mgr.Check(context.Background(), bad)
		if !errors.Is(err, updates.ErrUnknownComponent) {
			t.Errorf("Check(%q) error = %v, want ErrUnknownComponent", bad, err)
		}
	}

	// Plan must reject the same way for every component in the batch, not
	// silently skip the bad one and return partial results.
	_, err := env.mgr.Plan(context.Background(), []string{"dashboard", "../../etc"})
	if !errors.Is(err, updates.ErrUnknownComponent) {
		t.Errorf("Plan with a traversal component = %v, want ErrUnknownComponent", err)
	}
}

// TestBootstrapRegistrationRestoresDowngradeAndReplayProtection is a
// regression test for the R05 finding (external security re-review of the
// F07 fix): on a freshly bootstrapped install (nothing ever staged/
// promoted through the OTA store -- exactly what a system that has never
// completed one real OTA update looks like), Current() is empty, and
// manifest.CheckNotDowngrade / manifest.CheckNotReplayed both explicitly
// treat an empty current-version / empty last-seen-released-at as
// "nothing recorded yet" and let anything through. This proves that gap
// two ways: first, confirming the bypass is real with NO bootstrap
// registration (a manifest offering an OLDER version than what's actually
// running is accepted, without AllowDowngrade, simply because the store
// has no baseline to compare against); second, confirming
// store.Store.BootstrapCurrent -- what cmd/agentd's buildControllers now
// calls at startup -- closes it (the same install is then correctly
// rejected).
func TestBootstrapRegistrationRestoresDowngradeAndReplayProtection(t *testing.T) {
	t.Run("without bootstrap registration, a downgrade is silently accepted", func(t *testing.T) {
		ctrl := &fakeController{}
		env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
		// No BootstrapCurrent call -- store.Current() is empty, exactly
		// like a freshly installed system that has never completed an OTA
		// update. The manifest offers 1.0.0, an OLDER version than
		// whatever a real install.sh-bootstrapped binary would actually
		// be running (e.g. 2.0.0) -- without AllowDowngrade.
		env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())
		_, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{})
		if err != nil {
			t.Fatalf("CONFIRMED bypass did not reproduce: expected the downgrade to be silently accepted (no baseline to compare against), got error %v", err)
		}
	})

	t.Run("with bootstrap registration, the same downgrade is rejected", func(t *testing.T) {
		ctrl := &fakeController{}
		env := newTestEnv(t, map[string]components.Controller{"dashboard": ctrl})
		if err := store.New(env.baseDir, "dashboard").BootstrapCurrent("2.0.0"); err != nil {
			t.Fatalf("BootstrapCurrent: %v", err)
		}
		env.registerManifest(t, "stable", map[string]string{"dashboard": "1.0.0"}, time.Now())
		_, err := env.mgr.Install(context.Background(), "dashboard", updates.InstallOptions{})
		if err == nil {
			t.Fatal("expected the downgrade to be blocked once a bootstrap baseline is recorded")
		}
	})
}

func sha256Hex(t *testing.T, s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func mustSign(t *testing.T, priv ed25519.PrivateKey, content string) string {
	sig, err := manifest.SignComponentChecksum(sha256Hex(t, content), priv)
	if err != nil {
		t.Fatalf("SignComponentChecksum: %v", err)
	}
	return sig
}
