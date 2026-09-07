package main

import (
	"context"
	"log/slog"

	"qrx-node-suite/agent/config"
	"qrx-node-suite/agent/qrx"
)

// detectQRXCoreVersion implements docs/updates.md's startup step 1: probe
// the real qrxd via getbuildinfo (a command assumed stable across
// versions -- see docs/qrx-0.0.7-interface.md's honesty note that this is
// unverified). If that fails -- no real node reachable, as in Mock-mode
// development -- it falls back to AssumedQRXCoreVersion and logs loudly
// that the version is an assumption, never silently treating a guess as a
// detected fact.
func detectQRXCoreVersion(ctx context.Context, logger *slog.Logger, cfg config.Config) string {
	runner := qrx.NewRunner(qrx.Config{
		CLIPath: cfg.Adapter.CLIPath, DataDir: cfg.Adapter.DataDir,
		Network: cfg.Adapter.Network, WalletName: cfg.Adapter.WalletName,
	})
	raw, err := runner.Call(ctx, "getbuildinfo")
	if err == nil {
		if fields, ferr := qrx.Fields(raw); ferr == nil {
			if v := qrx.Str(fields, "version", "build_version"); v.Ok() {
				logger.Info("detected QRX Core version via getbuildinfo", "version", v.Value)
				return v.Value
			}
		}
	}
	fallback := cfg.Adapter.AssumedQRXCoreVersion
	logger.Warn("could not detect QRX Core version from a live qrx-cli; falling back to the configured assumption -- this is NOT a confirmed detection",
		"assumed_version", fallback, "probe_error", err)
	return fallback
}
