// Package components implements the per-component lifecycle hooks the OTA
// Manager needs to turn a staged, verified artifact into a running update:
// how to extract it, whether stopping/starting means anything for this
// component, and how to tell whether the newly activated version is
// actually healthy. See docs/updates.md#component-health-after-update and
// docs/updates.md#adapter-hot-reload.
package components

import (
	"context"
	"io"
)

// Controller is what agent/updates.Manager needs from a component to carry
// it through STOP/INSTALL/START/HEALTH CHECK
// (docs/updates.md#safe-update-process).
type Controller interface {
	// Extract writes a downloaded, already-verified artifact (opened for
	// reading) into dir (a store.Store release directory). It does not
	// activate anything.
	Extract(ctx context.Context, artifact io.Reader, dir string) error
	// Stop is called before activating a new version, only if
	// RequiresRestart is true.
	Stop(ctx context.Context) error
	// Start is called after activation.
	Start(ctx context.Context) error
	// HealthCheck is called after Start and decides commit vs. rollback. A
	// non-nil error means the update failed its health check.
	HealthCheck(ctx context.Context) error
	// RequiresRestart reports whether this component needs Stop/Start
	// around activation at all (docs/updates.md#adapter-hot-reload: the
	// answer for every component in this codebase is "restart only the
	// Agent process, never QRX Core" -- see agent.go's doc comment for why
	// even a self-update can't restart in-process).
	RequiresRestart() bool
}
