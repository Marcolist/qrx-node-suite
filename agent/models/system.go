package models

// SystemStatus is locally collected host/system telemetry -- never sourced
// from QRX Core. See agent/monitoring.
type SystemStatus struct {
	CPUUsagePercent       Value[float64] `json:"cpu_usage_percent"`
	CPUCoreCount          Value[int]     `json:"cpu_core_count"`
	LoadAverage1          Value[float64] `json:"load_average_1"`
	LoadAverage5          Value[float64] `json:"load_average_5"`
	LoadAverage15         Value[float64] `json:"load_average_15"`
	MemoryTotalBytes      Value[uint64]  `json:"memory_total_bytes"`
	MemoryUsedBytes       Value[uint64]  `json:"memory_used_bytes"`
	DiskTotalBytes        Value[uint64]  `json:"disk_total_bytes"`
	DiskUsedBytes         Value[uint64]  `json:"disk_used_bytes"`
	DiskFreeBytes         Value[uint64]  `json:"disk_free_bytes"`
	NetworkRxBytes        Value[uint64]  `json:"network_rx_bytes"`
	NetworkTxBytes        Value[uint64]  `json:"network_tx_bytes"`
	SystemUptimeSeconds   Value[int64]   `json:"system_uptime_seconds"`
	ProcessUptimeSeconds  Value[int64]   `json:"process_uptime_seconds"`
	QRXProcessCPUPercent  Value[float64] `json:"qrx_process_cpu_percent"`
	QRXProcessMemoryBytes Value[uint64]  `json:"qrx_process_memory_bytes"`

	// Raspberry Pi / SBC specific, unavailable elsewhere.
	TemperatureCelsius Value[float64] `json:"temperature_celsius"`
	Throttled          Value[bool]    `json:"throttled"`

	Platform PlatformInfo `json:"platform"`
}

// PlatformInfo describes the host OS/hardware the Agent is running on.
type PlatformInfo struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Model         string `json:"model,omitempty"` // e.g. "Raspberry Pi 5"
	IsRaspberryPi bool   `json:"is_raspberry_pi"`
}
