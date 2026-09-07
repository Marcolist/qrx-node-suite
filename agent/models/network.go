package models

// NetworkStatus normalizes getnetworkinfo. Do not add fields QRX doesn't
// document (no TPS, hash rate, validator count, block time, epoch) unless QRX
// Core actually exposes them -- see docs/qrx-0.0.7-interface.md.
type NetworkStatus struct {
	Network         Value[string]   `json:"network"`
	PeerCount       Value[int]      `json:"peer_count"`
	ProtocolVersion Value[string]   `json:"protocol_version"`
	ListenAddresses Value[[]string] `json:"listen_addresses"`
}

// BlockchainStatus normalizes getblockchaininfo.
type BlockchainStatus struct {
	Network         Value[string] `json:"network"`
	Height          Value[int64]  `json:"height"`
	FinalizedHeight Value[int64]  `json:"finalized_height"`
	BestBlockHash   Value[string] `json:"best_block_hash"`
}

// MempoolStatus normalizes getmempoolinfo.
type MempoolStatus struct {
	TxCount   Value[int64] `json:"tx_count"`
	SizeBytes Value[int64] `json:"size_bytes"`
}

// FeeInfo normalizes getfeeinfo.
type FeeInfo struct {
	MinFee         Value[string] `json:"min_fee"`
	RecommendedFee Value[string] `json:"recommended_fee"`
}

// RecentBlock is one entry from getrecentblocks.
type RecentBlock struct {
	Height    int64  `json:"height"`
	Hash      string `json:"hash"`
	Timestamp int64  `json:"timestamp"`
	TxCount   *int   `json:"tx_count,omitempty"`
	Producer  string `json:"producer,omitempty"`
}

// RecentTransaction is one entry from getrecenttransactions.
type RecentTransaction struct {
	Hash      string `json:"hash"`
	Height    *int64 `json:"height,omitempty"`
	Timestamp int64  `json:"timestamp"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Value     string `json:"value,omitempty"`
}
