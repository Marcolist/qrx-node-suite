package version

// ComponentVersionModel is the structured, independently-tracked version
// record for every domain QRX Node Suite ships. No field is derived from
// another -- each is set by the component that owns it.
type ComponentVersionModel struct {
	SuiteVersion                string          `json:"suite_version"`
	AgentVersion                string          `json:"agent_version"`
	DashboardVersion            string          `json:"dashboard_version"`
	Adapter                     *AdapterVersion `json:"adapter,omitempty"`
	QRXCoreVersion              string          `json:"qrx_core_version"`
	APIVersion                  string          `json:"api_version"`
	TelemetryProtocolVersion    int             `json:"telemetry_protocol_version"`
	ConfigSchemaVersion         int             `json:"config_schema_version"`
	InstallerVersion            string          `json:"installer_version,omitempty"`
	CompatibilityProfileVersion int             `json:"compatibility_profile_version,omitempty"`
}

// AdapterVersion identifies the active adapter and which QRX Core versions it
// declares support for. QRXCompatibility is informational only -- the
// authoritative answer to "is this combination supported" is always the
// Matrix (see compatibility.go), never this list.
type AdapterVersion struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	QRXCompatibility []string `json:"qrx_compatibility"`
}
