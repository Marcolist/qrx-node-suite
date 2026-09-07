// Package platform is the Agent's one OS-service abstraction: starting,
// stopping, and checking the status of the qrxd service the Agent
// supervises. See docs/deployment.md for what each platform actually needs
// installed (systemd unit, launchd plist, Windows service).
package platform

import (
	"context"

	"qrx-node-suite/agent/models"
)

// ServiceManager controls one OS-managed service (systemd unit, launchd
// job, Windows service). "unit" is whatever name that platform's manager
// uses to identify it (e.g. "qrxd.service" on systemd).
type ServiceManager interface {
	Start(ctx context.Context, unit string) error
	Stop(ctx context.Context, unit string) error
	Restart(ctx context.Context, unit string) error
	Status(ctx context.Context, unit string) (models.ServiceState, error)
}
