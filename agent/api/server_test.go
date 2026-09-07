package api_test

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qrx-node-suite/agent/adapters"
	_ "qrx-node-suite/agent/adapters/mock"
	"qrx-node-suite/agent/api"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates"
	"qrx-node-suite/agent/updates/components"
	"qrx-node-suite/agent/updates/sources"
	"qrx-node-suite/agent/version"
)

func testMatrix() *version.Matrix {
	return &version.Matrix{
		Entries: []version.MatrixEntry{
			{QRXCoreVersion: "0.0.7", AdapterName: "mock", AdapterVersionRange: "*", Status: version.Supported},
		},
	}
}

func newTestDeps(t *testing.T, adminToken string) *api.Deps {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	registry := adapters.NewRegistry(testMatrix())
	if _, _, err := registry.SelectAutomatic(context.Background(), "0.0.7"); err != nil {
		t.Fatalf("SelectAutomatic: %v", err)
	}

	cache := api.NewCache()
	cache.Set(&api.Snapshot{
		Node:   models.NodeStatus{Online: true, AdapterName: "mock"},
		Health: models.HealthHealthy,
	})

	return &api.Deps{
		Cache:       cache,
		Registry:    registry,
		Audit:       storage.NewAuditLogStore(db),
		Alerts:      storage.NewAlertStore(db),
		Settings:    storage.NewSettingsStore(db),
		AdminToken:  adminToken,
		VersionInfo: func() models.VersionInfo { return models.VersionInfo{AgentVersion: "0.1.0", APIVersion: "v1"} },
	}
}

func TestHealthEndpoint(t *testing.T) {
	deps := newTestDeps(t, "")
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestStatusEndpointReadsCache(t *testing.T) {
	deps := newTestDeps(t, "")
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("GET /api/v1/status: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["health"] != "HEALTHY" {
		t.Errorf("health = %v, want HEALTHY", body["health"])
	}
}

func TestVersionEndpoint(t *testing.T) {
	deps := newTestDeps(t, "")
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/version")
	if err != nil {
		t.Fatalf("GET /api/v1/version: %v", err)
	}
	defer resp.Body.Close()
	var info models.VersionInfo
	json.NewDecoder(resp.Body).Decode(&info)
	if info.AgentVersion != "0.1.0" {
		t.Errorf("AgentVersion = %q, want 0.1.0", info.AgentVersion)
	}
}

func TestAdminEndpointDisabledWithoutToken(t *testing.T) {
	deps := newTestDeps(t, "") // no admin token configured
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/updates/install", "application/json", strings.NewReader(`{"component":"agent"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (administrative endpoints disabled without a token)", resp.StatusCode)
	}
}

func TestAdminEndpointRejectsMissingOrWrongToken(t *testing.T) {
	deps := newTestDeps(t, "s3cr3t")
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	// No Authorization header at all.
	resp, err := http.Post(srv.URL+"/api/v1/updates/install", "application/json", strings.NewReader(`{"component":"agent"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status (no header) = %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/updates/install", strings.NewReader(`{"component":"agent"}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("status (wrong token) = %d, want 401", resp2.StatusCode)
	}
}

func TestActivateVersionRequiresAdminAndActivatesAdapter(t *testing.T) {
	deps := newTestDeps(t, "s3cr3t")
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/components/mock/activate-version",
		strings.NewReader(`{"version":"1.0.0"}`))
	req.Header.Set("Authorization", "Bearer s3cr3t")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if deps.Registry.ActiveName() != "mock" {
		t.Errorf("ActiveName() = %q, want mock", deps.Registry.ActiveName())
	}

	events, err := deps.Audit.List(req.Context(), 10)
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	if len(events) != 1 || events[0].Action != "ADMIN_SWITCH_ADAPTER" {
		t.Errorf("audit events = %+v, want one ADMIN_SWITCH_ADAPTER", events)
	}
}

// noopController is a minimal components.Controller test double used only
// to populate Manager.Controllers -- these tests never reach Extract/Stop/
// Start/HealthCheck.
type noopController struct{}

func (noopController) Extract(ctx context.Context, artifact io.Reader, dir string) error { return nil }
func (noopController) Stop(ctx context.Context) error                                    { return nil }
func (noopController) Start(ctx context.Context) error                                   { return nil }
func (noopController) HealthCheck(ctx context.Context) error                             { return nil }
func (noopController) RequiresRestart() bool                                             { return false }

var _ components.Controller = noopController{}

// depsWithUpdates builds Deps carrying a real (unauthenticated-reachable)
// *updates.Manager, so /updates/check and /updates/plan exercise the real
// Manager.Check code path instead of short-circuiting on Updates == nil.
func depsWithUpdates(t *testing.T, controllers map[string]components.Controller) *api.Deps {
	t.Helper()
	deps := newTestDeps(t, "")
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	deps.Updates = &updates.Manager{
		Source:      sources.NewDevelopmentSource(),
		PublicKey:   pub,
		History:     storage.NewUpdateHistoryStore(mustOpenDB(t)),
		Audit:       deps.Audit,
		Settings:    deps.Settings,
		Policy:      updates.NewPolicy(deps.Settings),
		BaseDir:     t.TempDir(),
		Controllers: controllers,
	}
	names := make([]string, 0, len(controllers))
	for name := range controllers {
		names = append(names, name)
	}
	deps.UpdateComponents = names
	return deps
}

func mustOpenDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestUnauthenticatedComponentTraversalRead is a regression test for the F03
// finding (external security audit): POST /api/v1/updates/check and
// /updates/plan are intentionally public, unauthenticated endpoints
// (docs/updates.md#update-status). Before the fix, a "component" value that
// wasn't a registered Controller still reached store.New(BaseDir,
// component) before any allowlist check, so a "../"-style value could
// resolve outside BaseDir and have a pointer file's contents ("current"/
// "previous") come back in the JSON response. This proves an anonymous
// caller supplying a traversal-shaped or simply-unknown component name gets
// rejected (never a 200 with store data) via both endpoints.
func TestUnauthenticatedComponentTraversalRead(t *testing.T) {
	deps := depsWithUpdates(t, map[string]components.Controller{"dashboard": noopController{}})
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	traversalNames := []string{"../../etc", "..", "unknown-component"}

	for _, name := range traversalNames {
		resp, err := http.Post(srv.URL+"/api/v1/updates/check", "application/json",
			strings.NewReader(`{"component":"`+name+`"}`))
		if err != nil {
			t.Fatalf("POST /updates/check (component=%q): %v", name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("POST /updates/check (component=%q) = 200 OK, want a non-200 rejection; body: %s", name, body)
		}
	}

	for _, name := range traversalNames {
		resp, err := http.Post(srv.URL+"/api/v1/updates/plan", "application/json",
			strings.NewReader(`{"components":["dashboard","`+name+`"]}`))
		if err != nil {
			t.Fatalf("POST /updates/plan (component=%q): %v", name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("POST /updates/plan (component=%q) = 200 OK, want a non-200 rejection; body: %s", name, body)
		}
	}

	// A legitimate, registered component must still work -- this isn't
	// blocking updates/check outright, only unknown/traversal names.
	resp, err := http.Post(srv.URL+"/api/v1/updates/check", "application/json", strings.NewReader(`{"component":"dashboard"}`))
	if err != nil {
		t.Fatalf("POST /updates/check (component=dashboard): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("POST /updates/check (component=dashboard) = %d, want 200; body: %s", resp.StatusCode, body)
	}
}

func TestAlertsEndpoint(t *testing.T) {
	deps := newTestDeps(t, "")
	deps.Alerts.Create(context.Background(), models.Alert{RuleID: "test", Severity: models.SeverityWarning, Title: "t", Message: "m"})
	srv := httptest.NewServer(api.NewMux(deps))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/alerts")
	if err != nil {
		t.Fatalf("GET /api/v1/alerts: %v", err)
	}
	defer resp.Body.Close()
	var list []models.Alert
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("got %d alerts, want 1", len(list))
	}
}
