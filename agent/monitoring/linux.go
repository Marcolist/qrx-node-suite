//go:build linux

package monitoring

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"qrx-node-suite/agent/models"
)

// Collector reads /proc and /sys directly -- no root privileges required
// for any of this (docs/architecture.md: "Avoid requiring root privileges
// only for metrics collection"). CPU percentage requires two samples, so
// Collector keeps the previous /proc/stat sample between calls.
type LinuxCollector struct {
	mu        sync.Mutex
	prevTotal uint64
	prevIdle  uint64
	started   time.Time

	// QRXPid, if set, returns the QRX Core process PID to report
	// QRXProcessCPUPercent/QRXProcessMemoryBytes for. Left nil (or
	// returning ok=false) reports those as Unavailable, not Unsupported --
	// this platform CAN report them, the caller just hasn't wired up which
	// process to look at (see agent/platform, cmd/agentd).
	QRXPid func() (pid int, ok bool)
}

func New() *LinuxCollector {
	return &LinuxCollector{started: time.Now()}
}

func (c *LinuxCollector) Collect(ctx context.Context) models.SystemStatus {
	status := models.SystemStatus{
		Platform: c.platformInfo(),
	}
	status.CPUCoreCount = models.Avail(runtime.NumCPU())
	status.CPUUsagePercent = c.cpuUsagePercent()
	status.LoadAverage1, status.LoadAverage5, status.LoadAverage15 = c.loadAverage()
	status.MemoryTotalBytes, status.MemoryUsedBytes = c.memory()
	status.DiskTotalBytes, status.DiskUsedBytes, status.DiskFreeBytes = c.disk("/")
	status.NetworkRxBytes, status.NetworkTxBytes = c.network()
	status.SystemUptimeSeconds = c.systemUptime()
	status.ProcessUptimeSeconds = models.Avail(int64(time.Since(c.started).Seconds()))
	status.TemperatureCelsius = c.temperature()
	status.Throttled = c.throttled()
	status.QRXProcessCPUPercent, status.QRXProcessMemoryBytes = c.qrxProcess()
	return status
}

func (c *LinuxCollector) platformInfo() models.PlatformInfo {
	model, isPi := raspberryPiModel()
	return models.PlatformInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Model: model, IsRaspberryPi: isPi}
}

// cpuUsagePercent computes utilization as (delta busy) / (delta total)
// between this call and the previous one, per the standard /proc/stat
// technique. The first call after process start has no prior sample and
// reports Unavailable.
func (c *LinuxCollector) cpuUsagePercent() models.Value[float64] {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return models.Unavail[float64]("open /proc/stat: " + err.Error())
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return models.Unavail[float64]("read /proc/stat: empty")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 8 || fields[0] != "cpu" {
		return models.Unavail[float64]("unexpected /proc/stat format")
	}
	var nums [7]uint64
	for i := 0; i < 7; i++ {
		v, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return models.Unavail[float64]("parse /proc/stat: " + err.Error())
		}
		nums[i] = v
	}
	idle := nums[3] + nums[4] // idle + iowait
	total := uint64(0)
	for _, n := range nums {
		total += n
	}

	c.mu.Lock()
	prevTotal, prevIdle := c.prevTotal, c.prevIdle
	c.prevTotal, c.prevIdle = total, idle
	c.mu.Unlock()

	if prevTotal == 0 || total <= prevTotal {
		return models.Unavail[float64]("no prior sample yet")
	}
	deltaTotal := total - prevTotal
	deltaIdle := idle - prevIdle
	if deltaIdle > deltaTotal {
		deltaIdle = deltaTotal
	}
	pct := (float64(deltaTotal-deltaIdle) / float64(deltaTotal)) * 100
	return models.Avail(pct)
}

func (c *LinuxCollector) loadAverage() (a1, a5, a15 models.Value[float64]) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		u := models.Unavail[float64]("read /proc/loadavg: " + err.Error())
		return u, u, u
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		u := models.Unavail[float64]("unexpected /proc/loadavg format")
		return u, u, u
	}
	parse := func(s string) models.Value[float64] {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return models.Unavail[float64]("parse /proc/loadavg: " + err.Error())
		}
		return models.Avail(v)
	}
	return parse(fields[0]), parse(fields[1]), parse(fields[2])
}

func (c *LinuxCollector) memory() (total, used models.Value[uint64]) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		u := models.Unavail[uint64]("open /proc/meminfo: " + err.Error())
		return u, u
	}
	defer f.Close()
	vals := map[string]uint64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			vals[key] = n * 1024 // /proc/meminfo is in kB
		}
	}
	memTotal, ok1 := vals["MemTotal"]
	memAvailable, ok2 := vals["MemAvailable"]
	if !ok1 {
		u := models.Unavail[uint64]("MemTotal not found in /proc/meminfo")
		return u, u
	}
	if !ok2 {
		return models.Avail(memTotal), models.Unavail[uint64]("MemAvailable not found in /proc/meminfo")
	}
	return models.Avail(memTotal), models.Avail(memTotal - memAvailable)
}

func (c *LinuxCollector) disk(path string) (total, used, free models.Value[uint64]) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		u := models.Unavail[uint64]("statfs " + path + ": " + err.Error())
		return u, u, u
	}
	blockSize := uint64(st.Bsize)
	totalBytes := st.Blocks * blockSize
	freeBytes := st.Bavail * blockSize
	usedBytes := totalBytes - (st.Bfree * blockSize)
	return models.Avail(totalBytes), models.Avail(usedBytes), models.Avail(freeBytes)
}

func (c *LinuxCollector) network() (rx, tx models.Value[uint64]) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		u := models.Unavail[uint64]("open /proc/net/dev: " + err.Error())
		return u, u
	}
	defer f.Close()
	var totalRx, totalTx uint64
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // header lines
		}
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		if v, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			totalRx += v
		}
		if v, err := strconv.ParseUint(fields[8], 10, 64); err == nil {
			totalTx += v
		}
	}
	return models.Avail(totalRx), models.Avail(totalTx)
}

func (c *LinuxCollector) systemUptime() models.Value[int64] {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return models.Unavail[int64]("read /proc/uptime: " + err.Error())
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return models.Unavail[int64]("unexpected /proc/uptime format")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return models.Unavail[int64]("parse /proc/uptime: " + err.Error())
	}
	return models.Avail(int64(secs))
}

// temperature reads the first available thermal zone. Present on
// Raspberry Pi and most modern Linux systems with a thermal driver; absent
// on some VPS/container environments, hence Unavailable (not Unsupported --
// the mechanism works on Linux in general, this host just has no exposed
// sensor).
func (c *LinuxCollector) temperature() models.Value[float64] {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return models.Unavail[float64]("no thermal zone exposed: " + err.Error())
	}
	milliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return models.Unavail[float64]("parse thermal_zone0/temp: " + err.Error())
	}
	return models.Avail(float64(milliC) / 1000.0)
}

// throttled reads the Raspberry Pi firmware's under-voltage/throttling
// status where exposed. Not present on non-Pi hardware.
func (c *LinuxCollector) throttled() models.Value[bool] {
	data, err := os.ReadFile("/sys/devices/platform/soc/soc:firmware/get_throttled")
	if err != nil {
		return models.Unsupp[bool]("throttling status not exposed on this platform (Raspberry Pi firmware interface only)")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "0x"))
	v, err := strconv.ParseUint(raw, 16, 64)
	if err != nil {
		return models.Unavail[bool]("parse get_throttled: " + err.Error())
	}
	return models.Avail(v != 0)
}

func (c *LinuxCollector) qrxProcess() (cpu models.Value[float64], mem models.Value[uint64]) {
	if c.QRXPid == nil {
		return models.Unavail[float64]("QRX process PID not configured"), models.Unavail[uint64]("QRX process PID not configured")
	}
	pid, ok := c.QRXPid()
	if !ok {
		return models.Unavail[float64]("QRX process not running"), models.Unavail[uint64]("QRX process not running")
	}
	statusPath := "/proc/" + strconv.Itoa(pid) + "/status"
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return models.Unavail[float64]("read " + statusPath + ": " + err.Error()), models.Unavail[uint64]("read " + statusPath + ": " + err.Error())
	}
	var vmRSS uint64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					vmRSS = v * 1024
				}
			}
		}
	}
	// CPU percent for a single process requires the same delta-sampling
	// technique as system-wide usage but keyed per PID; not yet
	// implemented (memory is the more commonly needed of the two for
	// "is the QRX process healthy" dashboards), so report Unavailable
	// rather than a wrong number.
	return models.Unavail[float64]("per-process CPU sampling not implemented yet"), models.Avail(vmRSS)
}

func raspberryPiModel() (model string, isPi bool) {
	data, err := os.ReadFile("/proc/device-tree/model")
	if err != nil {
		return "", false
	}
	m := strings.TrimRight(string(data), "\x00\n")
	return m, strings.Contains(m, "Raspberry Pi")
}
