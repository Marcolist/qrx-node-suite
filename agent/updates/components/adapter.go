package components

import (
	"context"
	"fmt"
	"io"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/updates/store"
	"qrx-node-suite/agent/version"
)

// Adapter is the Controller for an "adapter_<name>" manifest component. In
// this project's single-binary architecture, the artifact IS a rebuilt
// Agent binary containing the updated adapter code (see binary.go) -- what
// makes this its own tracked component, rather than just folded into
// "agent", is independent versioning/audit history (docs/updates.md#adapter-release-strategy)
// and, critically, its own health check.
//
// Per explicit product guidance: an adapter update must be verified against
// the QRX Core it is actually running against, not merely accepted because
// its declared version "should" work. HealthCheck therefore does two
// independent checks after restart: the adapter's own Health() probe
// against the live QRX Core, AND a fresh compatibility.Matrix lookup for
// the (adapter name, adapter version, detected QRX Core version) triple.
// Either one failing blocks the update, even if the other passed --
// a mock/stub adapter that always reports healthy would otherwise sail
// through despite being UNSUPPORTED for the running Core version.
type Adapter struct {
	Store          *store.Store
	RunningVersion string // this process's own compiled-in version

	Registry       *adapters.Registry
	AdapterName    string // registry key, e.g. "qrx007"
	Matrix         *version.Matrix
	QRXCoreVersion func() string // returns the currently detected QRX Core version
}

func (a *Adapter) IsSelfBinary() bool              { return true }
func (a *Adapter) RequiresRestart() bool           { return true }
func (a *Adapter) Stop(ctx context.Context) error  { return nil }
func (a *Adapter) Start(ctx context.Context) error { return nil }

func (a *Adapter) Extract(ctx context.Context, artifact io.Reader, dir string) error {
	return extractBinary(ctx, artifact, dir)
}

func (a *Adapter) HealthCheck(ctx context.Context) error {
	cur, ok, err := a.Store.Current()
	if err != nil {
		return fmt.Errorf("adapter health check: %w", err)
	}
	if !ok || cur != a.RunningVersion {
		return fmt.Errorf("adapter health check: restarted process is running version %q but store's current is %q (ok=%v) -- restart did not land on the promoted binary",
			a.RunningVersion, cur, ok)
	}

	active := a.Registry.Active()
	if active == nil {
		return fmt.Errorf("adapter health check: no adapter is active after restart")
	}
	if active.Name() != a.AdapterName {
		return fmt.Errorf("adapter health check: active adapter is %q, expected %q", active.Name(), a.AdapterName)
	}

	// Real probe against the live QRX Core -- not just "the process started".
	if err := active.Health(ctx); err != nil {
		return fmt.Errorf("adapter health check: %s failed its health probe: %w", a.AdapterName, err)
	}

	// Independently, re-validate against the compatibility matrix: a
	// passing Health() call does not by itself mean this combination is
	// SUPPORTED (docs/qrx-compatibility.md) -- an adapter can technically
	// respond while still being the wrong adapter for this QRX Core
	// version (e.g. EXPERIMENTAL/PARTIAL, selected only via manual
	// override before this update, and this update's compatibility matrix
	// has since changed).
	qrxVersion := ""
	if a.QRXCoreVersion != nil {
		qrxVersion = a.QRXCoreVersion()
	}
	if qrxVersion != "" {
		_, status := a.Matrix.Lookup(qrxVersion, a.AdapterName, active.Version())
		if status == version.Unsupported || status == version.Unknown {
			return fmt.Errorf("adapter health check: %s v%s is %s for QRX Core %s per the compatibility matrix",
				a.AdapterName, active.Version(), status, qrxVersion)
		}
	}
	return nil
}
