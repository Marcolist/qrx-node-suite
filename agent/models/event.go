package models

import "time"

// EventType enumerates the SSE event vocabulary from docs/architecture.md /
// section 14 of the product spec.
type EventType string

const (
	EventNodeOnline           EventType = "node.online"
	EventNodeOffline          EventType = "node.offline"
	EventNodeRecovered        EventType = "node.recovered"
	EventNodeHeightChanged    EventType = "node.height_changed"
	EventNodeSyncChanged      EventType = "node.sync_changed"
	EventNodePeerCountChanged EventType = "node.peer_count_changed"
	EventNodeMempoolChanged   EventType = "node.mempool_changed"

	EventValidatorActive        EventType = "validator.active"
	EventValidatorInactive      EventType = "validator.inactive"
	EventValidatorBlockProduced EventType = "validator.block_produced"
	EventValidatorPenalty       EventType = "validator.penalty"
	EventValidatorReward        EventType = "validator.reward"

	EventVelocityMetrics EventType = "velocity.metrics"

	EventSystemCPU         EventType = "system.cpu"
	EventSystemMemory      EventType = "system.memory"
	EventSystemDisk        EventType = "system.disk"
	EventSystemTemperature EventType = "system.temperature"

	EventServiceStarted   EventType = "service.started"
	EventServiceStopped   EventType = "service.stopped"
	EventServiceRestarted EventType = "service.restarted"

	EventUpdateAvailable EventType = "update.available"

	EventAlertCreated  EventType = "alert.created"
	EventAlertResolved EventType = "alert.resolved"
)

// Event is a single item on the SSE stream (GET /api/v1/events).
type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}
