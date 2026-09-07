package updates_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates"
	"qrx-node-suite/agent/updates/manifest"
	"qrx-node-suite/agent/updates/sources"
	"qrx-node-suite/agent/version"
)

type fakeProbe struct {
	health updates.QRXCoreHealth
	err    error
}

func (p *fakeProbe) Validate(ctx context.Context) (updates.QRXCoreHealth, error) {
	return p.health, p.err
}

func healthyProbe() *fakeProbe {
	return &fakeProbe{health: updates.QRXCoreHealth{
		Available: true, ProcessRunning: true, NetworkConnected: true, BlockHeightProgressing: true, PeerCount: 8,
	}}
}

func fullyKnownProfile(qrxVersion string) *version.CompatibilityProfile {
	return &version.CompatibilityProfile{
		QRXCoreVersion: qrxVersion,
		CoreVersionSwitchSafety: version.SwitchSafety{
			BlockchainData: version.SwitchCompatible,
			Configuration:  version.SwitchCompatible,
			Wallet:         version.SwitchCompatible,
			Adapter:        version.SwitchCompatible,
			Network:        version.SwitchCompatible,
		},
	}
}

type coreTestEnv struct {
	mgr  *updates.QRXCoreUpdateManager
	src  *sources.DevelopmentSource
	priv ed25519.PrivateKey
}

func newCoreTestEnv(t *testing.T, probe updates.QRXCoreHealthProbe) *coreTestEnv {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	settings := storage.NewSettingsStore(db)
	src := sources.NewDevelopmentSource()

	mgr := &updates.QRXCoreUpdateManager{
		Source:    src,
		PublicKey: pub,
		History:   storage.NewUpdateHistoryStore(db),
		Audit:     storage.NewAuditLogStore(db),
		Policy:    updates.NewPolicy(settings),
		BaseDir:   t.TempDir(),
		Probe:     probe,
		StopCore:  func(ctx context.Context) error { return nil },
		StartCore: func(ctx context.Context, dir string) error { return nil },
	}
	return &coreTestEnv{mgr: mgr, src: src, priv: priv}
}

func (e *coreTestEnv) registerCoreManifest(t *testing.T, channel, coreVersion string, releasedAt time.Time) {
	t.Helper()
	content := "qrxd-binary-" + coreVersion
	url := "dev://qrx_core/" + coreVersion
	sum := sha256.Sum256([]byte(content))
	sumHex := hex.EncodeToString(sum[:])
	sig, err := manifest.SignComponentChecksum(sumHex, e.priv)
	if err != nil {
		t.Fatalf("SignComponentChecksum: %v", err)
	}
	e.src.Artifacts[url] = []byte(content)
	m := &manifest.Manifest{
		ManifestVersion: 1,
		Channel:         channel,
		SuiteVersion:    "0.2.0",
		ReleasedAt:      releasedAt.UTC().Format(time.RFC3339),
		Components: map[string]manifest.ComponentUpdate{
			updates.QRXCoreComponent: {Version: coreVersion, URL: url, SHA256: sumHex, Signature: sig},
		},
	}
	manifest.SignManifest(m, e.priv)
	e.src.Manifests[channel] = m
}

func TestQRXCoreUpdateHappyPath(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())
	env.registerCoreManifest(t, "stable", "0.0.8", time.Now())

	rec, err := env.mgr.Update(context.Background(), updates.UpdateOptions{
		SwitchOptions: updates.SwitchOptions{Profile: fullyKnownProfile("0.0.8"), Actor: "admin"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec.Status != storage.UpdateStatusSucceeded {
		t.Errorf("status = %v, want succeeded", rec.Status)
	}
	if rec.ToVersion != "0.0.8" {
		t.Errorf("ToVersion = %q, want 0.0.8", rec.ToVersion)
	}
}

func TestQRXCoreUpdateValidationFailureRollsBackBinary(t *testing.T) {
	probe := healthyProbe()
	env := newCoreTestEnv(t, probe)

	env.registerCoreManifest(t, "stable", "0.0.7", time.Now())
	if _, err := env.mgr.Update(context.Background(), updates.UpdateOptions{
		SwitchOptions: updates.SwitchOptions{Profile: fullyKnownProfile("0.0.7")},
	}); err != nil {
		t.Fatalf("install base version 0.0.7: %v", err)
	}

	// Now the new version fails validation.
	probe.health = updates.QRXCoreHealth{Available: true, ProcessRunning: true, NetworkConnected: false}
	env.registerCoreManifest(t, "stable", "0.0.8", time.Now().Add(time.Minute))
	rec, err := env.mgr.Update(context.Background(), updates.UpdateOptions{
		SwitchOptions: updates.SwitchOptions{Profile: fullyKnownProfile("0.0.8")},
	})
	if err == nil {
		t.Fatal("expected Update to fail when post-update validation fails")
	}
	if !errors.Is(err, updates.ErrCoreValidationFailed) {
		t.Errorf("error = %v, want ErrCoreValidationFailed", err)
	}
	if rec == nil || rec.Status != storage.UpdateStatusRolledBack {
		t.Fatalf("expected a rolled_back history record, got %+v", rec)
	}
	if rec.FromVersion != "0.0.7" || rec.ToVersion != "0.0.8" {
		t.Errorf("record = %+v, want from 0.0.7 to 0.0.8", rec)
	}
}

func TestSwitchVersionBlockedOnUnknownSafetyWithoutOverride(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())
	unknownProfile := &version.CompatibilityProfile{QRXCoreVersion: "0.0.6"} // all switch-safety fields zero-value == unknown

	_, err := env.mgr.SwitchVersion(context.Background(), "0.0.6", updates.SwitchOptions{Profile: unknownProfile})
	if !errors.Is(err, updates.ErrSwitchSafetyUnknown) {
		t.Fatalf("expected ErrSwitchSafetyUnknown, got %v", err)
	}
}

func TestSwitchVersionIncompatibleNeverOverridable(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())
	incompatible := fullyKnownProfile("0.0.6")
	incompatible.CoreVersionSwitchSafety.Wallet = version.SwitchIncompatible

	_, err := env.mgr.SwitchVersion(context.Background(), "0.0.6", updates.SwitchOptions{Profile: incompatible, ExpertOverride: true})
	if !errors.Is(err, updates.ErrSwitchSafetyIncompatible) {
		t.Fatalf("expected ErrSwitchSafetyIncompatible even with ExpertOverride=true, got %v", err)
	}
}

func TestSwitchVersionRequiresLocallyInstalled(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())
	profile := fullyKnownProfile("0.0.9")
	_, err := env.mgr.SwitchVersion(context.Background(), "0.0.9", updates.SwitchOptions{Profile: profile})
	if !errors.Is(err, updates.ErrCoreNotInstalled) {
		t.Fatalf("expected ErrCoreNotInstalled for a version never downloaded, got %v", err)
	}
}

func TestSwitchVersionBetweenInstalledVersions(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())

	env.registerCoreManifest(t, "stable", "0.0.7", time.Now())
	if _, err := env.mgr.Update(context.Background(), updates.UpdateOptions{
		SwitchOptions: updates.SwitchOptions{Profile: fullyKnownProfile("0.0.7")},
	}); err != nil {
		t.Fatalf("install 0.0.7: %v", err)
	}
	env.registerCoreManifest(t, "stable", "0.0.8", time.Now().Add(time.Minute))
	if _, err := env.mgr.Update(context.Background(), updates.UpdateOptions{
		SwitchOptions: updates.SwitchOptions{Profile: fullyKnownProfile("0.0.8")},
	}); err != nil {
		t.Fatalf("install 0.0.8: %v", err)
	}

	// Both 0.0.7 and 0.0.8 are now installed; switch back to 0.0.7 without downloading anything.
	rec, err := env.mgr.SwitchVersion(context.Background(), "0.0.7", updates.SwitchOptions{Profile: fullyKnownProfile("0.0.7"), Actor: "admin"})
	if err != nil {
		t.Fatalf("SwitchVersion: %v", err)
	}
	if rec.ToVersion != "0.0.7" || rec.FromVersion != "0.0.8" {
		t.Errorf("record = %+v, want switch from 0.0.8 to 0.0.7", rec)
	}
}

func TestValidatorNodeAutomaticAlwaysDisabled(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())
	env.mgr.IsValidatorNode = true
	ctx := context.Background()

	// Even if an admin were to set a non-manual channel, validator nodes
	// must never report automatic updates as enabled.
	if err := env.mgr.Policy.SetChannel(ctx, updates.QRXCoreComponent, updates.ChannelStable); err != nil {
		t.Fatalf("SetChannel: %v", err)
	}
	enabled, err := env.mgr.AutomaticEnabled(ctx)
	if err != nil {
		t.Fatalf("AutomaticEnabled: %v", err)
	}
	if enabled {
		t.Error("expected AutomaticEnabled to be false unconditionally on a validator node")
	}
}

func TestNonValidatorDefaultsToManual(t *testing.T) {
	env := newCoreTestEnv(t, healthyProbe())
	enabled, err := env.mgr.AutomaticEnabled(context.Background())
	if err != nil {
		t.Fatalf("AutomaticEnabled: %v", err)
	}
	if enabled {
		t.Error("expected AutomaticEnabled to default to false (manual) even for a non-validator node")
	}
}
