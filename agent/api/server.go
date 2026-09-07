package api

import (
	"context"
	"log/slog"
	"net/http"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates"
)

// Deps bundles everything the API needs. Handlers hang off *Deps rather
// than a grab-bag of package-level globals, which is what makes this
// package testable with httptest against fakes/in-memory stores (see
// server_test.go).
type Deps struct {
	Cache    *Cache
	Bus      *events.Bus
	Guardian *guardian.Guardian
	Registry *adapters.Registry

	Updates *updates.Manager
	QRXCore *updates.QRXCoreUpdateManager
	Policy  *updates.Policy

	History  *storage.UpdateHistoryStore
	Audit    *storage.AuditLogStore
	Alerts   *storage.AlertStore
	Settings *storage.SettingsStore

	// VersionInfo returns the current component version model
	// (GET /api/v1/version).
	VersionInfo func() models.VersionInfo
	// UpdateComponents lists every component name GET /api/v1/updates and
	// POST /api/v1/updates/plan should report on by default.
	UpdateComponents []string

	// AdminToken gates administrative endpoints; see RequireAdmin.
	AdminToken string

	// RestartQRXService is called by POST /api/v1/services/qrx/restart.
	// nil disables the endpoint (404), distinct from an admin-token check
	// failing (403/401) -- the capability simply doesn't exist yet if this
	// isn't wired up (docs/architecture.md principle 26: never pretend an
	// action happened).
	RestartQRXService func(ctx context.Context) error

	// RequestSelfRestart is called after a self-binary component (agent,
	// adapter_*) Install/Rollback returns InstallResult.PendingRestart ==
	// true: staging/promoting a self-binary update only flips a pointer in
	// the OTA store, and it's this process's own exit (letting the
	// systemd Restart=always supervisor start a fresh one, landing on the
	// newly promoted binary via ${QRX_PREFIX}/bin/agentd's symlink into
	// the OTA store's "current" release -- see cmd/agentd/main.go and
	// install.sh's install_release()) that actually makes the promotion
	// take effect. Before this existed, nothing ever read
	// PendingRestart at all: a "successful" self-update verified, staged,
	// and promoted correctly but had no way to ever actually run (the R05
	// finding, external security re-review). nil is a safe no-op (tests
	// and any deployment that doesn't want auto-restart-on-update can
	// leave it unset); the handler still returns pending_restart: true in
	// the response either way, so a caller can always act on it manually
	// (e.g. `systemctl restart qrx-agent`) if this isn't wired up.
	RequestSelfRestart func()

	Log *slog.Logger
}

func (d *Deps) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

// NewMux builds the Agent's HTTP handler: GET /health plus every
// /api/v1/... route from docs/architecture.md section 13, using Go's
// standard-library method+path routing (net/http, Go 1.22+) -- no router
// dependency (ADR-001).
func NewMux(d *Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", d.handleHealth)

	mux.HandleFunc("GET /api/v1/version", d.handleVersion)
	mux.HandleFunc("GET /api/v1/versions", d.handleVersions)

	mux.HandleFunc("GET /api/v1/status", d.handleStatus)
	mux.HandleFunc("GET /api/v1/node", d.handleNode)
	mux.HandleFunc("GET /api/v1/network", d.handleNetwork)
	mux.HandleFunc("GET /api/v1/blockchain", d.handleBlockchain)
	mux.HandleFunc("GET /api/v1/mempool", d.handleMempool)
	mux.HandleFunc("GET /api/v1/validator", d.handleValidator)
	mux.HandleFunc("GET /api/v1/velocity", d.handleVelocity)
	mux.HandleFunc("GET /api/v1/system", d.handleSystem)
	mux.HandleFunc("GET /api/v1/activity", d.handleActivity)
	mux.HandleFunc("GET /api/v1/services", d.handleServices)
	mux.HandleFunc("GET /api/v1/settings", d.handleSettingsGet)

	mux.HandleFunc("GET /api/v1/alerts", d.handleAlertsList)

	mux.HandleFunc("GET /api/v1/events", d.handleEvents)

	mux.HandleFunc("GET /api/v1/updates", d.handleUpdatesStatus)
	mux.HandleFunc("POST /api/v1/updates/check", d.handleUpdatesCheck)
	mux.HandleFunc("POST /api/v1/updates/plan", d.handleUpdatesPlan)
	mux.HandleFunc("POST /api/v1/updates/install", RequireAdmin(d.AdminToken, d.handleUpdatesInstall))
	mux.HandleFunc("POST /api/v1/updates/rollback", RequireAdmin(d.AdminToken, d.handleUpdatesRollback))
	mux.HandleFunc("POST /api/v1/components/{component}/activate-version", RequireAdmin(d.AdminToken, d.handleActivateVersion))

	mux.HandleFunc("POST /api/v1/qrx-core/switch", RequireAdmin(d.AdminToken, d.handleQRXCoreSwitch))

	mux.HandleFunc("POST /api/v1/services/qrx/restart", RequireAdmin(d.AdminToken, d.handleServiceRestart))

	return mux
}

func (d *Deps) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
