package models

import "time"

// NodeStatus is the normalized view of getnodestatus / getbuildinfo / getuptime.
type NodeStatus struct {
	Online             bool               `json:"online"`
	Network            Value[string]      `json:"network"`
	Height             Value[int64]       `json:"height"`
	FinalizedHeight    Value[int64]       `json:"finalized_height"`
	Sync               Value[SyncStatus]  `json:"sync"`
	Peers              Value[PeerSummary] `json:"peers"`
	MempoolTxCount     Value[int64]       `json:"mempool_tx_count"`
	QRXVersion         Value[string]      `json:"qrx_version"`
	NodeUptimeSeconds  Value[int64]       `json:"node_uptime_seconds"`
	LastSuccessfulPoll *time.Time         `json:"last_successful_poll,omitempty"`
	LastError          string             `json:"last_error,omitempty"`
	SimulationMode     bool               `json:"simulation_mode"`
	AdapterName        string             `json:"adapter_name"`
}

// SyncStatus describes chain sync progress.
type SyncStatus struct {
	Syncing         bool     `json:"syncing"`
	CurrentHeight   int64    `json:"current_height"`
	TargetHeight    *int64   `json:"target_height,omitempty"`
	ProgressPercent *float64 `json:"progress_percent,omitempty"`
}

// PeerSummary describes network peer counts.
type PeerSummary struct {
	Connected int `json:"connected"`
	Inbound   int `json:"inbound"`
	Outbound  int `json:"outbound"`
}
