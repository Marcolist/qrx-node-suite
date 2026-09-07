package models

// ValidatorStatus normalizes getvalidatorstatus. Every field is optional
// because QRX 0.0.7's exact validator surface has not been verified against
// source -- see docs/qrx-0.0.7-interface.md. Unknown fields must render as
// "Not exposed by QRX Core 0.0.7" in the dashboard, never a guess.
type ValidatorStatus struct {
	Active         Value[bool]   `json:"active"`
	Address        Value[string] `json:"address"`
	Stake          Value[string] `json:"stake"`
	DelegatedStake Value[string] `json:"delegated_stake"`
	VotingWeight   Value[string] `json:"voting_weight"`
}

// BlockProducerStatus normalizes getblockproducerinfo.
type BlockProducerStatus struct {
	ProducerStatus Value[string] `json:"producer_status"`
	BlocksProduced Value[int64]  `json:"blocks_produced"`
	MissedBlocks   Value[int64]  `json:"missed_blocks"`
}

// RewardSummary is only populated when QRX Core exposes an authoritative
// reward/penalty figure. QRX Node Suite must never compute this value itself
// (see docs/architecture.md, principle 2).
type RewardSummary struct {
	Rewards   Value[string] `json:"rewards"`
	Penalties Value[string] `json:"penalties"`
}
