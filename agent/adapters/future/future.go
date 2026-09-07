// Package future is not a real adapter -- it exists only as a minimal,
// working example of the extension pattern described in
// agent/adapters/README.md, matching this project's adapters/{mock,
// legacy006,qrx007,future} layout. It registers under the name
// "future-example" (never "future", to avoid any accidental collision with
// a later real adapter) and reports every field Unsupported. It has no
// compatibility-matrix row, so it is never auto-selected and DisableIncompatible
// always disables it -- activating it requires an explicit manual override.
package future

import (
	"context"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/models"
)

func init() {
	adapters.Register("future-example", func() adapters.Adapter { return &Adapter{} })
}

const unimplemented = "future-example is a template adapter and implements nothing"

type Adapter struct{}

func (a *Adapter) Name() string                   { return "future-example" }
func (a *Adapter) Version() string                { return "0.0.0" }
func (a *Adapter) SupportedQRXVersions() []string { return nil }

func (a *Adapter) Activate(ctx context.Context) error               { return nil }
func (a *Adapter) Deactivate(ctx context.Context) error             { return nil }
func (a *Adapter) Health(ctx context.Context) error                 { return nil }
func (a *Adapter) Capabilities(ctx context.Context) map[string]bool { return map[string]bool{} }

func (a *Adapter) GetNodeStatus(ctx context.Context) (models.NodeStatus, error) {
	return models.NodeStatus{Online: false, LastError: unimplemented, AdapterName: a.Name()}, nil
}
func (a *Adapter) GetNetworkInfo(ctx context.Context) (models.NetworkStatus, error) {
	return models.NetworkStatus{}, nil
}
func (a *Adapter) GetBlockchainInfo(ctx context.Context) (models.BlockchainStatus, error) {
	return models.BlockchainStatus{}, nil
}
func (a *Adapter) GetMempoolInfo(ctx context.Context) (models.MempoolStatus, error) {
	return models.MempoolStatus{}, nil
}
func (a *Adapter) GetFeeInfo(ctx context.Context) (models.FeeInfo, error) {
	return models.FeeInfo{}, nil
}
func (a *Adapter) GetRecentBlocks(ctx context.Context, limit int) ([]models.RecentBlock, error) {
	return nil, nil
}
func (a *Adapter) GetRecentTransactions(ctx context.Context, limit int) ([]models.RecentTransaction, error) {
	return nil, nil
}
func (a *Adapter) GetValidatorStatus(ctx context.Context) (models.ValidatorStatus, error) {
	return models.ValidatorStatus{}, nil
}
func (a *Adapter) GetBlockProducerInfo(ctx context.Context) (models.BlockProducerStatus, error) {
	return models.BlockProducerStatus{}, nil
}
func (a *Adapter) GetWalletInfo(ctx context.Context) (models.WalletSummary, error) {
	return models.WalletSummary{}, nil
}
func (a *Adapter) GetVelocityInfo(ctx context.Context) (models.VelocityStatus, error) {
	return models.VelocityStatus{}, nil
}
func (a *Adapter) GetNonceLanes(ctx context.Context) (models.NonceLanes, error) {
	return models.NonceLanes{}, nil
}
