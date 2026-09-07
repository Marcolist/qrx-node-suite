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
