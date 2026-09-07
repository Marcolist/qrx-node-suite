// Command agentd is the QRX Agent: one binary that discovers/activates a
// QRX Adapter, polls it on a schedule, serves the versioned REST API and
// SSE event stream, runs Guardian and the alert engine, and owns the OTA
// update pipeline for itself, the dashboard, adapters, and compatibility
// profiles. See docs/architecture.md.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/alerts"
	"qrx-node-suite/agent/api"
	"qrx-node-suite/agent/config"
	"qrx-node-suite/agent/data"
	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/logging"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/monitoring"
	"qrx-node-suite/agent/platform"
	"qrx-node-suite/agent/qrx"
	"qrx-node-suite/agent/storage"
	"qrx-node-suite/agent/updates"
	"qrx-node-suite/agent/updates/components"
	"qrx-node-suite/agent/updates/store"
	"qrx-node-suite/agent/version"
)

// These identify THIS build. suiteVersion/agentVersion are independent
// version domains (docs/updates.md#component-version-model) -- bump them
// as part of a release, never derive one from the other. They are `var`,
// not `const`, so the release pipeline can inject the real tagged version
// with `-ldflags "-X main.suiteVersion=... -X main.agentVersion=..."`
// (.github/workflows/release.yml) -- a local `go build` with no ldflags
// keeps these development-default values.
var (
	suiteVersion = "0.1.0-dev"
	agentVersion = "0.1.0-dev"
)

const (
	dashboardVersionFallback = "unbuilt" // overridden once GET /api/v1/version can read the served dashboard's own version marker
	apiVersion               = "v1"
	telemetryProtocolVersion = 1
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "qrx-agentd:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", os.Getenv("QRX_AGENT_CONFIG"), "path to a JSON config file (optional -- Mock mode works with none)")
	flag.Parse()

	cfg := config.Default()
	if *configPath != "" {
		if err := config.LoadInto(*configPath, &cfg); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	logger := logging.New(cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("starting qrx-agentd", "suite_version", suiteVersion, "agent_version", agentVersion, "adapter", cfg.Adapter.Name)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	componentsBaseDir := filepath.Join(cfg.DataDir, "components")

	db, err := storage.Open(filepath.Join(cfg.DataDir, "agent.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	historyStore := storage.NewUpdateHistoryStore(db)
	auditStore := storage.NewAuditLogStore(db)
	settingsStore := storage.NewSettingsStore(db)
	alertStore := storage.NewAlertStore(db)
	policy := updates.NewPolicy(settingsStore)

	// BootGuard closes the crash-loop gap in the self-binary update flow
	// (docs/updates.md#self-binary-components): if a self-update leaves the
	// process unable to even start (rather than starting and failing its
	// health check), a supervisor with Restart=always would otherwise retry
	// forever. Must run before anything below that could itself crash --
	// adapter selection in particular.
	bootGuard := &updates.BootGuard{
		Settings: settingsStore, History: historyStore, Audit: auditStore,
		BaseDir: componentsBaseDir, MaxAttempts: cfg.Updates.MaxBootAttempts,
	}
	pendingBoot, err := bootGuard.PendingComponents(ctx)
	if err != nil {
		return fmt.Errorf("check pending self-updates for crash-loop guard: %w", err)
	}
	for _, component := range pendingBoot {
		rolledBack, attempts, err := bootGuard.CheckAndRecordAttempt(ctx, component)
		if err != nil {
			logger.Error("boot guard: crash-loop check failed", "component", component, "error", err)
			continue
		}
		if rolledBack {
			return fmt.Errorf("boot guard: %s crash-looped for %d consecutive boots and was rolled back to its previous version -- exiting so the process supervisor restarts onto it", component, attempts)
		}
		logger.Warn("boot guard: recorded a boot attempt for a pending self-update", "component", component, "attempt", attempts, "max_attempts", bootGuard.EffectiveMaxAttempts())
	}

	matrix, err := data.LoadMatrix()
	if err != nil {
		return fmt.Errorf("load compatibility matrix: %w", err)
	}

	registry := adapters.NewRegistry(matrix)
	adapterConfig := qrx.Config{
		CLIPath: cfg.Adapter.CLIPath, DataDir: cfg.Adapter.DataDir,
		Network: cfg.Adapter.Network, WalletName: cfg.Adapter.WalletName,
	}
	// Pre-configure every INSTALLED adapter (adapters.Installed(), not just
	// cfg.Adapter.Name): when Adapter.Name is empty -- the default,
	// automatic-selection config install.sh generates -- Configure("", ...)
	// looks up an adapter literally named "" (which never exists) and
	// silently does nothing, so the adapter registry.SelectAutomatic later
	// picks and activates below would activate with its factory's
	// zero-value defaults instead of the operator's configured
	// cli_path/network/wallet_name/data_dir -- QRX Core connection settings
	// the installer went out of its way to detect and write. Configure is
	// safe to call on every registered adapter here regardless of which
	// one ends up active: it only touches an unactivated instance
	// (Configure's own contract is "reconfigure before Activate"). Fix for
	// an external security audit's F11 finding.
	for _, name := range adapters.Installed() {
		if err := registry.Configure(name, func(a adapters.Adapter) {
			if c, ok := a.(interface{ SetConfig(qrx.Config) }); ok {
				c.SetConfig(adapterConfig)
			}
		}); err != nil {
			logger.Warn("could not pre-configure adapter connection settings", "adapter", name, "error", err)
		}
	}

	qrxCoreVersion := detectQRXCoreVersion(ctx, logger, cfg)

	var activeAdapterName string
	if cfg.Adapter.Name != "" {
		if err := registry.Activate(ctx, cfg.Adapter.Name, adapters.ActivateOptions{AllowUnsupported: cfg.Adapter.ManualOverride}); err != nil {
			return fmt.Errorf("activate configured adapter %q: %w", cfg.Adapter.Name, err)
		}
		activeAdapterName = cfg.Adapter.Name
		logger.Info("adapter activated (manual selection)", "adapter", activeAdapterName)
	} else if info, status, err := registry.SelectAutomatic(ctx, qrxCoreVersion); err != nil {
		logger.Warn("no compatible adapter selected automatically -- QRX integration is UNSUPPORTED", "qrx_core_version", qrxCoreVersion, "error", err)
	} else {
		activeAdapterName = info.Name
		logger.Info("adapter selected automatically", "adapter", info.Name, "compatibility", status)
	}

	bus := events.NewBus(64)

	svcManager := platform.New()
	restartQRX := func(ctx context.Context) error { return svcManager.Restart(ctx, "qrxd.service") }
	guardianRestart := func(ctx context.Context, reason string) error { return restartQRX(ctx) }

	g := guardian.New(guardian.DefaultConfig(), guardianRestart, func(old, new models.HealthState) {
		logger.Info("guardian state transition", "from", old, "to", new)
		bus.Publish(models.Event{Type: models.EventServiceRestarted, Timestamp: time.Now(), Data: map[string]string{"from": string(old), "to": string(new)}})
	})

	alertEngine := alerts.NewEngine(alertStore, bus, alerts.DefaultRules())

	updateSource, err := buildUpdateSource(cfg.Updates)
	if err != nil {
		return fmt.Errorf("build update source: %w", err)
	}
	publicKey, err := parsePublicKey(cfg.Updates.PublicKeyBase64)
	if err != nil {
		logger.Warn("no valid update manifest public key configured -- update installs will fail signature verification until one is set", "error", err)
	}

	versionInfo := func() models.VersionInfo {
		active := registry.Active()
		adapterName, adapterVersion := "", ""
		if active != nil {
			adapterName, adapterVersion = active.Name(), active.Version()
		}
		return models.VersionInfo{
			SuiteVersion: suiteVersion, AgentVersion: agentVersion, DashboardVersion: dashboardVersionFallback,
			AdapterName: adapterName, AdapterVersion: adapterVersion, QRXCoreVersion: qrxCoreVersion,
			APIVersion: apiVersion, TelemetryProtocolVersion: telemetryProtocolVersion, ConfigSchemaVersion: cfg.SchemaVersion,
		}
	}

	updateComponents := []string{"agent", "dashboard"}
	if activeAdapterName != "" {
		updateComponents = append(updateComponents, "adapter_"+activeAdapterName)
	}

	controllers := buildControllers(componentsBaseDir, registry, activeAdapterName, matrix, &qrxCoreVersion, logger)

	mgr := &updates.Manager{
		Source: updateSource, PublicKey: publicKey,
		History: historyStore, Audit: auditStore, Settings: settingsStore, Policy: policy,
		Matrix: matrix, BaseDir: componentsBaseDir, Controllers: controllers, RetainVersions: cfg.Updates.RetainVersions,
		CurrentVersions: func() updates.CurrentVersions {
			active := registry.Active()
			av := ""
			if active != nil {
				av = active.Version()
			}
			return updates.CurrentVersions{SuiteVersion: suiteVersion, QRXCoreVersion: qrxCoreVersion, AdapterVersion: av}
		},
	}

	qrxCoreMgr := &updates.QRXCoreUpdateManager{
		Source: updateSource, PublicKey: publicKey, History: historyStore, Audit: auditStore, Policy: policy,
		BaseDir: filepath.Join(cfg.DataDir, "qrx-core"), IsValidatorNode: cfg.IsValidatorNode,
		StopCore:  func(ctx context.Context) error { return svcManager.Stop(ctx, "qrxd.service") },
		StartCore: func(ctx context.Context, dir string) error { return svcManager.Start(ctx, "qrxd.service") },
	}

	// Resume any self-binary update (agent, adapter_*) staged before a
	// prior restart -- see docs/updates.md's "self-binary components" note.
	for _, component := range []string{"agent", "adapter_" + activeAdapterName} {
		if activeAdapterName == "" && component == "adapter_" {
			continue
		}
		rec, err := mgr.ResumeSelfUpdate(ctx, component)
		if err != nil {
			logger.Error("resume self-update failed", "component", component, "error", err)
			continue
		}
		if rec != nil {
			logger.Info("resumed pending self-update", "component", component, "status", rec.Status, "to_version", rec.ToVersion)
		}
		if selfUpdateRollbackRequiresRestart(component, rec) {
			return fmt.Errorf("agent self-update health check failed and was rolled back to %s -- exiting so the process supervisor restarts onto the reverted binary (this process is still running the unhealthy one)", rec.FromVersion)
		}
	}

	sysCollector := monitoring.New()

	// requestSelfRestart triggers the same graceful shutdown a SIGTERM
	// would (cancel ctx -> srv.Shutdown -> ListenAndServe returns ->
	// run() returns nil -> process exits 0 -> systemd's Restart=always
	// starts a fresh process, landing on whatever ${QRX_PREFIX}/bin/agentd
	// now resolves to via its symlink chain into the OTA store's
	// "current" release). Delayed slightly and run in its own goroutine
	// so the HTTP handler that triggered this (POST /api/v1/updates/
	// install or /updates/rollback, on PendingRestart) can finish writing
	// its response to the client first -- an operator/dashboard polling
	// that request should see "pending_restart: true" before the
	// connection drops, not a connection reset instead of a response.
	// sync.Once: Install and Rollback can each trigger this, and
	// cancel() itself is idempotent, but there's no reason to spawn more
	// than one pending shutdown.
	var restartOnce sync.Once
	requestSelfRestart := func() {
		restartOnce.Do(func() {
			logger.Info("self-restart requested after a self-binary update -- exiting shortly so the process supervisor restarts onto it")
			go func() {
				time.Sleep(500 * time.Millisecond)
				cancel()
			}()
		})
	}

	deps := &api.Deps{
		Cache: api.NewCache(), Bus: bus, Guardian: g, Registry: registry,
		Updates: mgr, QRXCore: qrxCoreMgr, Policy: policy,
		History: historyStore, Audit: auditStore, Alerts: alertStore, Settings: settingsStore,
		VersionInfo: versionInfo, UpdateComponents: updateComponents, AdminToken: cfg.AdminToken,
		RestartQRXService: restartQRX, RequestSelfRestart: requestSelfRestart, Log: logger,
	}

	poller := &Poller{
		Registry: registry, Monitoring: sysCollector, Cache: deps.Cache, Bus: bus,
		Guardian: g, Alerts: alertEngine, Interval: time.Duration(cfg.Poll.NodeStatusSeconds) * time.Second, Logger: logger,
	}
	go poller.Run(ctx)

	apiMux := api.NewMux(deps)
	mux := http.NewServeMux()
	mux.Handle("/api/", NewLoggingMiddleware(logger, apiMux))
	mux.Handle("/health", NewLoggingMiddleware(logger, apiMux))
	mux.Handle("/", dashboardHandler(cfg.DashboardDir, storeFor(componentsBaseDir, "dashboard")))

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("listening", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}

func parsePublicKey(b64 string) (ed25519.PublicKey, error) {
	if b64 == "" {
		return nil, fmt.Errorf("no public key configured")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// buildControllers wires up one components.Controller per OTA-managed
// component. qrxCoreVersion is a pointer so the compatibility_profile
// controller's Reload callback can update it in place if an operator ships
// a corrected profile for a version this Agent has already detected.
//
// Also bootstrap-registers "agent" and any active adapter's store to their
// RunningVersion (store.Store.BootstrapCurrent) -- a no-op once a real OTA
// update has ever promoted something, but on a freshly bootstrapped
// install (never gone through Stage/Promote at all) this is what makes
// Current() non-empty from the very first update check, restoring
// manifest.CheckNotDowngrade/CheckNotReplayed's protection on that first
// check instead of leaving both silently disabled (see BootstrapCurrent's
// doc comment; R05 fix, external security re-review). Deliberately NOT
// done for "dashboard": its install-time content lives outside the OTA
// store entirely (cfg.DashboardDir, copied there by install.sh), and
// cmd/agentd's dashboard handler (the F07 fix) already falls back to that
// directory specifically when the store has nothing real to serve --
// bootstrap-registering a version with no corresponding on-disk release
// would defeat that fallback and break dashboard serving on every fresh
// install.
// selfUpdateRollbackRequiresRestart reports whether resuming a self-update
// for component with the given history record requires exiting this
// process so the process supervisor (systemd's Restart=always) starts a
// fresh one that lands on the reverted binary.
//
// Scoped to component == "agent" only: adapter code (adapter_*) is
// compiled into this same agentd binary (docs/architecture.md), so an
// adapter-only rollback just changes a version-tracking pointer in the OTA
// store, not what code this process is actually running -- restarting
// would accomplish nothing for that case. "agent" is different: a rolled
// back agent self-update means this process is still running the binary
// whose health check just failed, and only an exit + restart gets it back
// onto the reverted one (ResumeSelfUpdate's own doc comment says "the
// caller is responsible for exiting" after a rollback -- this is that,
// actually implemented; R05 fix, external security re-review).
func selfUpdateRollbackRequiresRestart(component string, rec *storage.UpdateHistoryRecord) bool {
	return component == "agent" && rec != nil && rec.Status == storage.UpdateStatusRolledBack
}

func buildControllers(baseDir string, registry *adapters.Registry, activeAdapterName string, matrix *version.Matrix, qrxCoreVersion *string, logger *slog.Logger) map[string]components.Controller {
	agentStore := storeFor(baseDir, "agent")
	if err := agentStore.BootstrapCurrent(agentVersion); err != nil {
		logger.Warn("could not bootstrap-register the running agent version into the OTA store", "error", err)
	}
	c := map[string]components.Controller{
		"agent":     &components.Agent{Store: agentStore, RunningVersion: agentVersion},
		"dashboard": &components.Dashboard{Store: storeFor(baseDir, "dashboard")},
	}
	if activeAdapterName != "" {
		active := registry.Active()
		adapterVersion := ""
		if active != nil {
			adapterVersion = active.Version()
		}
		adapterStore := storeFor(baseDir, "adapter_"+activeAdapterName)
		if adapterVersion != "" {
			if err := adapterStore.BootstrapCurrent(adapterVersion); err != nil {
				logger.Warn("could not bootstrap-register the running adapter version into the OTA store", "adapter", activeAdapterName, "error", err)
			}
		}
		c["adapter_"+activeAdapterName] = &components.Adapter{
			Store: adapterStore, RunningVersion: adapterVersion,
			Registry: registry, AdapterName: activeAdapterName, Matrix: matrix,
			QRXCoreVersion: func() string { return *qrxCoreVersion },
		}
	}
	return c
}

func storeFor(baseDir, component string) *store.Store {
	return store.New(baseDir, component)
}
