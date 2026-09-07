package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qrx-node-suite/agent/updates/store"
)

func writeIndex(t *testing.T, dir, marker string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
}

func get(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(body)
}

// TestDashboardHandlerServesInstallDirWhenNothingPromoted is a regression
// test for the F07 finding (external security audit) that a fresh install
// (install.sh copies the initial build straight into installDir; the OTA
// store has nothing staged/promoted yet) must still serve something,
// falling back to installDir when store.CurrentDir() has no active
// version.
func TestDashboardHandlerServesInstallDirWhenNothingPromoted(t *testing.T) {
	installDir := t.TempDir()
	writeIndex(t, installDir, "install-time build")

	st := store.New(t.TempDir(), "dashboard") // nothing ever staged/promoted here

	h := dashboardHandler(installDir, st)
	if got := get(t, h, "/"); got != "install-time build" {
		t.Errorf("body = %q, want the install-time build", got)
	}
}

// TestDashboardHandlerServesOTAStoreOnceAvailable is a regression test for
// the F07 finding: dashboardHandler was wired to a static, config-set
// directory (cfg.DashboardDir) that never changed after startup, even
// though the OTA store's own doc comment already claimed "the Agent serves
// whichever directory store.Store.CurrentDir() currently resolves to on
// each request." A dashboard OTA install would verify, stage, and promote
// successfully (Manager.Install, components.Dashboard.RequiresRestart() ==
// false) yet have zero user-visible effect. This proves that once a
// version IS promoted through the OTA store, dashboardHandler serves it --
// live, on the very next request, no Agent restart -- exactly matching
// components.Dashboard's documented RequiresRestart()==false design.
func TestDashboardHandlerServesOTAStoreOnceAvailable(t *testing.T) {
	installDir := t.TempDir()
	writeIndex(t, installDir, "install-time build")

	base := t.TempDir()
	st := store.New(base, "dashboard")
	dir, err := st.Stage("1.2.3")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	writeIndex(t, dir, "OTA-updated build 1.2.3")
	if err := st.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	h := dashboardHandler(installDir, st)
	if got := get(t, h, "/"); got != "OTA-updated build 1.2.3" {
		t.Errorf("body = %q, want the OTA-promoted build (not the stale install-time one)", got)
	}

	// A second promotion (a further OTA update) must take effect
	// immediately too -- no caching of the resolved directory across
	// requests.
	dir2, err := st.Stage("1.3.0")
	if err != nil {
		t.Fatalf("Stage 1.3.0: %v", err)
	}
	writeIndex(t, dir2, "OTA-updated build 1.3.0")
	if err := st.Promote(); err != nil {
		t.Fatalf("Promote 1.3.0: %v", err)
	}
	if got := get(t, h, "/"); got != "OTA-updated build 1.3.0" {
		t.Errorf("body = %q, want the newly-promoted 1.3.0 build", got)
	}
}
