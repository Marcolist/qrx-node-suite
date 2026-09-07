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
// as part of a release, never derive one from the other.
const (
	suiteVersion             = "0.1.0"
	agentVersion             = "0.1.0"
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

	matrix, err := data.LoadMatrix()
	if err != nil {
		return fmt.Errorf("load compatibility matrix: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	registry := adapters.NewRegistry(matrix)
	if err := registry.Configure(cfg.Adapter.Name, func(a adapters.Adapter) {
		if c, ok := a.(interface{ SetConfig(qrx.Config) }); ok {
			c.SetConfig(qrx.Config{
				CLIPath: cfg.Adapter.CLIPath, DataDir: cfg.Adapter.DataDir,
				Network: cfg.Adapter.Network, WalletName: cfg.Adapter.WalletName,
			})
		}
	}); err != nil && cfg.Adapter.Name != "" {
		logger.Warn("could not pre-configure adapter connection settings", "adapter", cfg.Adapter.Name, "error", err)
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

	controllers := buildControllers(componentsBaseDir, registry, activeAdapterName, matrix, &qrxCoreVersion)

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
	}

	sysCollector := monitoring.New()

	deps := &api.Deps{
		Cache: api.NewCache(), Bus: bus, Guardian: g, Registry: registry,
		Updates: mgr, QRXCore: qrxCoreMgr, Policy: policy,
		History: historyStore, Audit: auditStore, Alerts: alertStore, Settings: settingsStore,
		VersionInfo: versionInfo, UpdateComponents: updateComponents, AdminToken: cfg.AdminToken,
		RestartQRXService: restartQRX, Log: logger,
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
	mux.Handle("/", dashboardHandler(cfg.DashboardDir))

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
func buildControllers(baseDir string, registry *adapters.Registry, activeAdapterName string, matrix *version.Matrix, qrxCoreVersion *string) map[string]components.Controller {
	c := map[string]components.Controller{
		"agent":     &components.Agent{Store: storeFor(baseDir, "agent"), RunningVersion: agentVersion},
		"dashboard": &components.Dashboard{Store: storeFor(baseDir, "dashboard")},
	}
	if activeAdapterName != "" {
		active := registry.Active()
		adapterVersion := ""
		if active != nil {
			adapterVersion = active.Version()
		}
		c["adapter_"+activeAdapterName] = &components.Adapter{
			Store: storeFor(baseDir, "adapter_"+activeAdapterName), RunningVersion: adapterVersion,
			Registry: registry, AdapterName: activeAdapterName, Matrix: matrix,
			QRXCoreVersion: func() string { return *qrxCoreVersion },
		}
	}
	return c
}

func storeFor(baseDir, component string) *store.Store {
	return store.New(baseDir, component)
}
