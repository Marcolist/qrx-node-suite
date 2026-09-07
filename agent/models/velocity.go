package models

// VelocityStatus normalizes getvelocityinfo. QRX 0.0.7 introduces VELOCITY,
// QRX Core's deterministic parallel execution engine; QRX Node Suite only
// surfaces metrics QRX Core actually reports (see docs/qrx-0.0.7-interface.md).
type VelocityStatus struct {
	EngineStatus       Value[string] `json:"engine_status"`
	TransactionVersion Value[string] `json:"transaction_version"`
	SchedulerVersion   Value[string] `json:"scheduler_version"`
	ParallelWidth      Value[int]    `json:"parallel_width"`
	ExecutionWaves     Value[int64]  `json:"execution_waves"`
	Conflicts          Value[int64]  `json:"conflicts"`
	SelectiveRetries   Value[int64]  `json:"selective_retries"`
}

// NonceLanes normalizes getnoncelanes.
type NonceLanes struct {
	LaneCount Value[int]    `json:"lane_count"`
	Lanes     Value[[]Lane] `json:"lanes"`
}

// Lane is one nonce lane entry, when QRX Core reports per-lane detail.
type Lane struct {
	Index     int    `json:"index"`
	NextNonce string `json:"next_nonce"`
	InFlight  int    `json:"in_flight"`
}
