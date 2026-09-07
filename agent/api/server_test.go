package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qrx-node-suite/agent/adapters"
	_ "qrx-node-suite/agent/adapters/mock"
	"qrx-node-suite/agent/api"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/storage"
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
