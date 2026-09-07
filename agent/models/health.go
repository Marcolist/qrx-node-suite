package models

// HealthState is Guardian's coarse health classification for a component.
type HealthState string

const (
	HealthHealthy   HealthState = "HEALTHY"
	HealthDegraded  HealthState = "DEGRADED"
	HealthUnhealthy HealthState = "UNHEALTHY"
	HealthOffline   HealthState = "OFFLINE"
)

// HealthStatus is one component's health, as exposed by GET /health and
// consumed by agent/guardian.
type HealthStatus struct {
	Component string      `json:"component"`
	State     HealthState `json:"state"`
	Detail    string      `json:"detail,omitempty"`
}
