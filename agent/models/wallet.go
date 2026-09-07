package models

// WalletSummary normalizes getwalletinfo. It is a view of what QRX Core
// reports about the wallet it manages -- QRX Node Suite never opens, edits,
// or independently computes wallet state, and never carries a secret.
type WalletSummary struct {
	Loaded  Value[bool]   `json:"loaded"`
	Address Value[string] `json:"address"`
	Balance Value[string] `json:"balance"`
}
