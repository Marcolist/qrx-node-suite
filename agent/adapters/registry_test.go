package adapters_test

import (
	"context"
	"testing"

	"qrx-node-suite/agent/adapters"
	_ "qrx-node-suite/agent/adapters/future"
	_ "qrx-node-suite/agent/adapters/mock"
	"qrx-node-suite/agent/adapters/qrx007"
	"qrx-node-suite/agent/qrx"
	"qrx-node-suite/agent/version"
)

func loadMatrix(t *testing.T) *version.Matrix {
	t.Helper()
	m, err := version.LoadMatrixFile("../data/compatibility-matrix.json")
	if err != nil {
		t.Fatalf("LoadMatrixFile: %v", err)
	}
	return m
}

// mockOnlyMatrix mirrors the shape of the real matrix but names "mock" as
// the SUPPORTED adapter for 0.0.7, so automatic-selection tests can run
// without a real qrx-cli binary on PATH (qrx007.Activate/Health genuinely
// shell out, which this sandbox has no binary for). Registry behavior
// under test (never infer, never fall back to an incompatible adapter) is
// identical regardless of which adapter name the matrix recommends.
func mockOnlyMatrix() *version.Matrix {
	return &version.Matrix{
		MatrixVersion: 1,
		Entries: []version.MatrixEntry{
			{QRXCoreVersion: "0.0.7", AdapterName: "mock", AdapterVersionRange: "*", Status: version.Supported},
			{QRXCoreVersion: "0.0.8", AdapterName: "UNKNOWN", AdapterVersionRange: "*", Status: version.Unsupported},
		},
	}
}

func TestDiscoverListsRegisteredAdapters(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	infos, err := r.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	names := map[string]bool{}
	for _, i := range infos {
		names[i.Name] = true
	}
	if !names["mock"] {
		t.Error("expected mock adapter to be discovered")
	}
	if !names["future-example"] {
		t.Error("expected future-example adapter to be discovered")
	}
}

func TestSelectAutomaticPicksSupportedAdapter(t *testing.T) {
	r := adapters.NewRegistry(mockOnlyMatrix())
	info, status, err := r.SelectAutomatic(context.Background(), "0.0.7")
	if err != nil {
		t.Fatalf("SelectAutomatic: %v", err)
	}
	if info.Name != "mock" {
		t.Errorf("selected adapter = %q, want mock", info.Name)
	}
	if status != version.Supported {
		t.Errorf("status = %v, want SUPPORTED", status)
	}
	if r.ActiveName() != "mock" {
		t.Errorf("ActiveName() = %q, want mock", r.ActiveName())
	}
}

func TestSelectAutomaticUnsupportedCoreNeverFallsBack(t *testing.T) {
	r := adapters.NewRegistry(mockOnlyMatrix())
	_, _, err := r.SelectAutomatic(context.Background(), "0.0.8")
	if err == nil {
		t.Fatal("expected an error for an unsupported QRX Core version, got nil")
	}
	if r.ActiveName() != "" {
		t.Errorf("expected no adapter activated for unsupported core, got %q", r.ActiveName())
	}
}

func TestSelectAutomaticUnknownCoreVersionBlocked(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	_, _, err := r.SelectAutomatic(context.Background(), "9.9.9")
	if err == nil {
		t.Fatal("expected an error for a completely unlisted QRX Core version")
	}
	if r.ActiveName() != "" {
		t.Errorf("expected no adapter activated, got %q", r.ActiveName())
	}
}

func TestManualActivateOfDisabledAdapterRequiresOverride(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	if _, err := r.DisableIncompatible("0.0.7"); err != nil {
		t.Fatalf("DisableIncompatible: %v", err)
	}
	// future-example has no matrix row at all for 0.0.7 -> disabled.
	err := r.Activate(context.Background(), "future-example", adapters.ActivateOptions{})
	if err == nil {
		t.Fatal("expected activation of a disabled adapter to fail without override")
	}

	// mock IS supported for 0.0.7, so plain activation should work even
	// after DisableIncompatible re-evaluated everything.
	if err := r.Activate(context.Background(), "mock", adapters.ActivateOptions{}); err != nil {
		t.Fatalf("Activate(mock): %v", err)
	}
}

func TestRollbackRestoresPreviousAdapter(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	ctx := context.Background()

	if err := r.Activate(ctx, "mock", adapters.ActivateOptions{}); err != nil {
		t.Fatalf("Activate(mock): %v", err)
	}
	if err := r.Activate(ctx, "future-example", adapters.ActivateOptions{AllowUnsupported: true}); err != nil {
		t.Fatalf("Activate(future-example): %v", err)
	}
	if r.ActiveName() != "future-example" {
		t.Fatalf("ActiveName() = %q, want future-example", r.ActiveName())
	}

	if err := r.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if r.ActiveName() != "mock" {
		t.Errorf("after rollback ActiveName() = %q, want mock", r.ActiveName())
	}
}

func TestRollbackWithNoHistoryFails(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	if err := r.Rollback(context.Background()); err == nil {
		t.Fatal("expected Rollback with no prior activation to fail")
	}
}

func TestValidateCompatibilityNeverInfers(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	status, _, err := r.ValidateCompatibility("mock", "0.0.99")
	if err != nil {
		t.Fatalf("ValidateCompatibility: %v", err)
	}
	if status != version.Unknown {
		t.Errorf("status for unlisted core version = %v, want UNKNOWN", status)
	}
}

// TestPreConfiguringEveryInstalledAdapterAppliesSettingsBeforeSelection is
// a regression test for the F11 finding (external security audit):
// cmd/agentd/main.go used to call registry.Configure(cfg.Adapter.Name, ...)
// -- but cfg.Adapter.Name is EMPTY in the default, automatic-selection
// config install.sh generates, so Configure("", ...) looked up an adapter
// literally named "" (which never exists) and silently did nothing. The
// adapter registry.SelectAutomatic later picked and activated would then
// activate with its factory's zero-value defaults instead of the
// operator's configured cli_path/network/wallet_name/data_dir -- the QRX
// Core connection settings the installer went out of its way to detect and
// write. The fix loops over adapters.Installed() instead of a single
// caller-supplied name; this proves that loop, run against the real
// qrx007 adapter type (not a fake), actually applies the settings to an
// unactivated instance -- without ever calling Activate, which genuinely
// shells out to qrx-cli and has no binary available in this sandbox.
func TestPreConfiguringEveryInstalledAdapterAppliesSettingsBeforeSelection(t *testing.T) {
	r := adapters.NewRegistry(loadMatrix(t))
	want := qrx.Config{CLIPath: "/custom/qrx-cli", Network: "testnet", WalletName: "w1", DataDir: "/data"}

	for _, name := range adapters.Installed() {
		if err := r.Configure(name, func(a adapters.Adapter) {
			if c, ok := a.(interface{ SetConfig(qrx.Config) }); ok {
				c.SetConfig(want)
			}
		}); err != nil {
			t.Fatalf("Configure(%s): %v", name, err)
		}
	}

	var got qrx.Config
	sawInstance := false
	if err := r.Configure("qrx007", func(a adapters.Adapter) {
		if c, ok := a.(*qrx007.Adapter); ok {
			got = c.Config()
			sawInstance = true
		}
	}); err != nil {
		t.Fatalf("Configure(qrx007) readback: %v", err)
	}
	if !sawInstance {
		t.Fatal("qrx007 adapter instance was not the expected type")
	}
	if got != want {
		t.Errorf("qrx007's applied config = %+v, want %+v -- SetConfig was not actually called on it", got, want)
	}
}
