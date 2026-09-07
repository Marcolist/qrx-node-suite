//go:build !linux

package monitoring

import (
	"context"
	"runtime"

	"qrx-node-suite/agent/models"
)

// unsupportedReason explains why every metric field is Unsupported on this
// build: no platform-specific collector has been written yet for
// runtime.GOOS. Raspberry Pi (linux/arm64) is covered by linux.go; macOS
// and Windows collectors are not yet implemented -- see
// docs/development.md's contribution notes. Per docs/architecture.md
// principle 11 and 26, this is reported honestly as Unsupported rather than
// silently returning zeros or panicking.
const unsupportedReason = "no monitoring collector implemented for this platform yet"

// Collector is the fallback used on any OS without a dedicated
// implementation.
type FallbackCollector struct{}

func New() *FallbackCollector { return &FallbackCollector{} }

func (c *FallbackCollector) Collect(ctx context.Context) models.SystemStatus {
	u := func() models.Value[float64] { return models.Unsupp[float64](unsupportedReason) }
	ui := func() models.Value[int] { return models.Unsupp[int](unsupportedReason) }
	u64 := func() models.Value[uint64] { return models.Unsupp[uint64](unsupportedReason) }
	i64 := func() models.Value[int64] { return models.Unsupp[int64](unsupportedReason) }
	b := func() models.Value[bool] { return models.Unsupp[bool](unsupportedReason) }

	return models.SystemStatus{
		CPUUsagePercent:       u(),
		CPUCoreCount:          ui(),
		LoadAverage1:          u(),
		LoadAverage5:          u(),
		LoadAverage15:         u(),
		MemoryTotalBytes:      u64(),
		MemoryUsedBytes:       u64(),
		DiskTotalBytes:        u64(),
		DiskUsedBytes:         u64(),
		DiskFreeBytes:         u64(),
		NetworkRxBytes:        u64(),
		NetworkTxBytes:        u64(),
		SystemUptimeSeconds:   i64(),
		ProcessUptimeSeconds:  i64(),
		QRXProcessCPUPercent:  u(),
		QRXProcessMemoryBytes: u64(),
		TemperatureCelsius:    u(),
		Throttled:             b(),
		Platform: models.PlatformInfo{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
		},
	}
}
