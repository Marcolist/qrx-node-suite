package models

// ServiceState is the run state of an OS-managed service (qrxd, the Agent
// itself, etc.) as reported by agent/platform.
type ServiceState string

const (
	ServiceRunning ServiceState = "running"
	ServiceStopped ServiceState = "stopped"
	ServiceFailed  ServiceState = "failed"
	ServiceUnknown ServiceState = "unknown"
)

// ServiceStatus is one row of GET /api/v1/services.
type ServiceStatus struct {
	Name          string       `json:"name"`
	State         ServiceState `json:"state"`
	PID           *int         `json:"pid,omitempty"`
	RestartCount  int          `json:"restart_count"`
	LastRestartAt *string      `json:"last_restart_at,omitempty"`
}

// VersionInfo is the API-facing summary returned by GET /api/v1/version. The
// deeper version-management data model lives in agent/version.
type VersionInfo struct {
	SuiteVersion             string `json:"suite_version"`
	AgentVersion             string `json:"agent_version"`
	DashboardVersion         string `json:"dashboard_version"`
	AdapterName              string `json:"adapter_name"`
	AdapterVersion           string `json:"adapter_version"`
	QRXCoreVersion           string `json:"qrx_core_version"`
	APIVersion               string `json:"api_version"`
	TelemetryProtocolVersion int    `json:"telemetry_protocol_version"`
	ConfigSchemaVersion      int    `json:"config_schema_version"`
}
