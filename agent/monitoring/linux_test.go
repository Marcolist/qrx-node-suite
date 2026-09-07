//go:build linux

package monitoring_test

import (
	"context"
	"testing"
	"time"

	"qrx-node-suite/agent/monitoring"
)

func TestLinuxCollectorReturnsSaneValues(t *testing.T) {
	c := monitoring.New()
	ctx := context.Background()

	status := c.Collect(ctx)
	if status.Platform.OS != "linux" {
		t.Errorf("Platform.OS = %q, want linux", status.Platform.OS)
	}
	if !status.CPUCoreCount.Ok() || status.CPUCoreCount.Value < 1 {
		t.Errorf("CPUCoreCount = %+v, want at least 1 available core", status.CPUCoreCount)
	}
	if !status.MemoryTotalBytes.Ok() || status.MemoryTotalBytes.Value == 0 {
		t.Errorf("MemoryTotalBytes = %+v, want a nonzero available value", status.MemoryTotalBytes)
	}
	if !status.SystemUptimeSeconds.Ok() {
		t.Errorf("SystemUptimeSeconds = %+v, want available", status.SystemUptimeSeconds)
	}

	// CPU usage needs two samples -- first call is expected to be
	// Unavailable, second (after any delay at all) should be Available.
	if status.CPUUsagePercent.Ok() {
		t.Log("first CPU sample was already available (unexpected but not wrong)")
	}
	time.Sleep(50 * time.Millisecond)
	status2 := c.Collect(ctx)
	if !status2.CPUUsagePercent.Ok() {
		t.Errorf("second CPUUsagePercent sample = %+v, want available", status2.CPUUsagePercent)
	}
	if status2.CPUUsagePercent.Value < 0 || status2.CPUUsagePercent.Value > 100 {
		t.Errorf("CPUUsagePercent = %v, want in [0,100]", status2.CPUUsagePercent.Value)
	}
}

func TestLinuxCollectorQRXProcessUnavailableWithoutPID(t *testing.T) {
	c := monitoring.New()
	status := c.Collect(context.Background())
	if status.QRXProcessMemoryBytes.Ok() {
		t.Error("expected QRXProcessMemoryBytes to be unavailable with no QRXPid configured")
	}
}
