//go:build !linux

package platform

import (
	"context"
	"errors"
	"runtime"

	"qrx-node-suite/agent/models"
)

// Unsupported is used on any OS without a ServiceManager implementation
// yet (macOS/launchd, Windows Service Control Manager -- see
// docs/deployment.md's per-platform notes). It fails loudly rather than
// pretending to manage a service it can't, per docs/architecture.md
// principle 26.
type Unsupported struct{}

// New returns this platform's ServiceManager (Unsupported, until macOS/
// Windows implementations exist).
func New() ServiceManager { return &Unsupported{} }

func (u *Unsupported) err() error {
	return errors.New("platform: service management not implemented for " + runtime.GOOS + " yet")
}

func (u *Unsupported) Start(ctx context.Context, unit string) error   { return u.err() }
func (u *Unsupported) Stop(ctx context.Context, unit string) error    { return u.err() }
func (u *Unsupported) Restart(ctx context.Context, unit string) error { return u.err() }
func (u *Unsupported) Status(ctx context.Context, unit string) (models.ServiceState, error) {
	return models.ServiceUnknown, u.err()
}
