// Package legacy006 wraps the QRX 0.0.6 command surface -- the pre-VELOCITY
// branch. Its compatibility matrix status is PARTIAL (0.0.6) or EXPERIMENTAL
// (against 0.0.7's RPC surface); see agent/data/compatibility-matrix.json.
// It intentionally does not implement validator status, block producer
// info, or VELOCITY -- those commands did not exist on this branch
// (docs/qrx-0.0.7-interface.md), so this adapter reports them Unsupported
// rather than attempting and failing.
package legacy006

import (
	"context"
	"encoding/json"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/qrx"
)

func init() {
	adapters.Register("legacy006", func() adapters.Adapter { return New(qrx.Config{}) })
}

const adapterVersion = "0.9.5"

var supportedQRXVersions = []string{"0.0.6"}

const unsupportedReason = "not implemented on the QRX 0.0.6 command surface (pre-VELOCITY)"

// Adapter wraps qrx-cli for a QRX 0.0.6 node.
type Adapter struct {
	cfg    qrx.Config
	runner *qrx.Runner
}

// New builds a legacy006 adapter with the given qrx-cli connection config.
func New(cfg qrx.Config) *Adapter {
	return &Adapter{cfg: cfg, runner: qrx.NewRunner(cfg)}
}

// SetConfig replaces the runner's connection config. Must be called before
// Activate.
func (a *Adapter) SetConfig(cfg qrx.Config) {
	a.cfg = cfg
	a.runner = qrx.NewRunner(cfg)
}

func (a *Adapter) Name() string                   { return "legacy006" }
func (a *Adapter) Version() string                { return adapterVersion }
func (a *Adapter) SupportedQRXVersions() []string { return supportedQRXVersions }

func (a *Adapter) Activate(ctx context.Context) error   { return a.runner.Ping(ctx) }
func (a *Adapter) Deactivate(ctx context.Context) error { return nil }
func (a *Adapter) Health(ctx context.Context) error     { return a.runner.Ping(ctx) }

func (a *Adapter) Capabilities(ctx context.Context) map[string]bool {
	return map[string]bool{
		adapters.CapValidatorStatus:   false,
		adapters.CapBlockProducerInfo: false,
		adapters.CapRecentBlocks:      true,
		adapters.CapVelocity:          false,
		adapters.CapNonceLanes:        false,
	}
}

func (a *Adapter) GetNodeStatus(ctx context.Context) (models.NodeStatus, error) {
	raw, err := a.runner.Call(ctx, "getnodestatus")
	if err != nil {
		return models.NodeStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.NodeStatus{Online: true, AdapterName: a.Name(), LastError: "unexpected response shape"}, nil
	}
	return models.NodeStatus{
		Online:         true,
		Network:        qrx.Str(fields, "network", "chain"),
		Height:         qrx.Int64(fields, "height", "blocks"),
		SimulationMode: false,
		AdapterName:    a.Name(),
	}, nil
}

func (a *Adapter) GetNetworkInfo(ctx context.Context) (models.NetworkStatus, error) {
	raw, err := a.runner.Call(ctx, "getnetworkinfo")
	if err != nil {
		return models.NetworkStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.NetworkStatus{}, err
	}
	return models.NetworkStatus{
		Network:   qrx.Str(fields, "network"),
		PeerCount: qrx.Int(fields, "peer_count", "connections"),
	}, nil
}

func (a *Adapter) GetBlockchainInfo(ctx context.Context) (models.BlockchainStatus, error) {
	raw, err := a.runner.Call(ctx, "getblockchaininfo")
	if err != nil {
		return models.BlockchainStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.BlockchainStatus{}, err
	}
	return models.BlockchainStatus{
		Network: qrx.Str(fields, "network", "chain"),
		Height:  qrx.Int64(fields, "height", "blocks"),
	}, nil
}

func (a *Adapter) GetMempoolInfo(ctx context.Context) (models.MempoolStatus, error) {
	raw, err := a.runner.Call(ctx, "getmempoolinfo")
	if err != nil {
		return models.MempoolStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.MempoolStatus{}, err
	}
	return models.MempoolStatus{TxCount: qrx.Int64(fields, "size", "tx_count")}, nil
}

func (a *Adapter) GetFeeInfo(ctx context.Context) (models.FeeInfo, error) {
	return models.FeeInfo{
		MinFee:         models.Unsupp[string](unsupportedReason),
		RecommendedFee: models.Unsupp[string](unsupportedReason),
	}, nil
}

func (a *Adapter) GetRecentBlocks(ctx context.Context, limit int) ([]models.RecentBlock, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	raw, err := a.runner.Call(ctx, "getrecentblocks", qrx.Itoa(limit))
	if err != nil {
		return nil, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	blocks := make([]models.RecentBlock, 0, len(entries))
	for _, e := range entries {
		blocks = append(blocks, models.RecentBlock{
			Height:    qrx.Int64(e, "height").Value,
			Hash:      qrx.Str(e, "hash", "block_hash").Value,
			Timestamp: qrx.Int64(e, "timestamp", "time").Value,
		})
	}
	return blocks, nil
}

func (a *Adapter) GetRecentTransactions(ctx context.Context, limit int) ([]models.RecentTransaction, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	raw, err := a.runner.Call(ctx, "getrecenttransactions", qrx.Itoa(limit))
	if err != nil {
		return nil, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	txs := make([]models.RecentTransaction, 0, len(entries))
	for _, e := range entries {
		txs = append(txs, models.RecentTransaction{
			Hash:      qrx.Str(e, "hash", "txid").Value,
			Timestamp: qrx.Int64(e, "timestamp", "time").Value,
		})
	}
	return txs, nil
}

func (a *Adapter) GetValidatorStatus(ctx context.Context) (models.ValidatorStatus, error) {
	return models.ValidatorStatus{
		Active:         models.Unsupp[bool](unsupportedReason),
		Address:        models.Unsupp[string](unsupportedReason),
		Stake:          models.Unsupp[string](unsupportedReason),
		DelegatedStake: models.Unsupp[string](unsupportedReason),
		VotingWeight:   models.Unsupp[string](unsupportedReason),
	}, nil
}

func (a *Adapter) GetBlockProducerInfo(ctx context.Context) (models.BlockProducerStatus, error) {
	return models.BlockProducerStatus{
		ProducerStatus: models.Unsupp[string](unsupportedReason),
		BlocksProduced: models.Unsupp[int64](unsupportedReason),
		MissedBlocks:   models.Unsupp[int64](unsupportedReason),
	}, nil
}

func (a *Adapter) GetWalletInfo(ctx context.Context) (models.WalletSummary, error) {
	raw, err := a.runner.Call(ctx, "getwalletinfo")
	if err != nil {
		return models.WalletSummary{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.WalletSummary{}, err
	}
	return models.WalletSummary{
		Loaded:  qrx.Bool(fields, "loaded"),
		Address: qrx.Str(fields, "address"),
		Balance: qrx.Str(fields, "balance"),
	}, nil
}

func (a *Adapter) GetVelocityInfo(ctx context.Context) (models.VelocityStatus, error) {
	return models.VelocityStatus{
		EngineStatus:       models.Unsupp[string](unsupportedReason),
		TransactionVersion: models.Unsupp[string](unsupportedReason),
		SchedulerVersion:   models.Unsupp[string](unsupportedReason),
		ParallelWidth:      models.Unsupp[int](unsupportedReason),
		ExecutionWaves:     models.Unsupp[int64](unsupportedReason),
		Conflicts:          models.Unsupp[int64](unsupportedReason),
		SelectiveRetries:   models.Unsupp[int64](unsupportedReason),
	}, nil
}

func (a *Adapter) GetNonceLanes(ctx context.Context) (models.NonceLanes, error) {
	return models.NonceLanes{
		LaneCount: models.Unsupp[int](unsupportedReason),
		Lanes:     models.Unsupp[[]models.Lane](unsupportedReason),
	}, nil
}
