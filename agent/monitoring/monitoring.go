// Package monitoring collects local host/system telemetry
// (docs/architecture.md: "Collect locally: CPU usage, RAM, disk, network,
// uptime, temperature/throttling where supported"). It never talks to QRX
// Core -- see agent/models.SystemStatus's doc comment.
package monitoring

import (
	"context"

	"qrx-node-suite/agent/models"
)

// Collector produces a SystemStatus snapshot. Implementations that can't
// determine a field on the current platform must return
// models.Unsupported for it, never a zero value -- see
// docs/architecture.md, principle 11.
type Collector interface {
	Collect(ctx context.Context) models.SystemStatus
}
