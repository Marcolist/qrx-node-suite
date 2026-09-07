// Package qrx007 wraps the QRX 0.0.7 command surface documented in
// docs/qrx-0.0.7-interface.md.
//
// IMPORTANT: as recorded in that document, the exact JSON response shape for
// each command has NOT been verified against QRX 0.0.7 source, a running
// node, or an official RPC/CLI reference -- this sandbox had neither network
// access to fetch the QRX source nor a running qrxd to inspect. Every method
// here therefore parses defensively via agent/qrx's field helpers: known/
// likely field names are attempted, and anything that doesn't decode as
// expected degrades to models.Unavailable rather than panicking or guessing
// a default. Before relying on this adapter against a real node, verify
// field names per docs/qrx-0.0.7-interface.md and tighten the parsing here.
package qrx007

import (
	"context"
	"encoding/json"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/qrx"
)

func init() {
	adapters.Register("qrx007", func() adapters.Adapter { return New(qrx.Config{}) })
}

const adapterVersion = "1.0.0"

var supportedQRXVersions = []string{"0.0.7"}

// Adapter wraps qrx-cli for a QRX 0.0.7 node via the centralized
// qrx.Runner (agent/qrx), the only place allowed to spawn qrx-cli.
type Adapter struct {
	cfg    qrx.Config
	runner *qrx.Runner
	caps   map[string]bool
}

// New builds a qrx007 adapter with the given qrx-cli connection config.
func New(cfg qrx.Config) *Adapter {
	return &Adapter{cfg: cfg, runner: qrx.NewRunner(cfg)}
}

// SetConfig replaces the runner's connection config, e.g. once loaded from
// agent/config. Must be called before Activate.
func (a *Adapter) SetConfig(cfg qrx.Config) {
	a.cfg = cfg
	a.runner = qrx.NewRunner(cfg)
}

func (a *Adapter) Name() string                   { return "qrx007" }
func (a *Adapter) Version() string                { return adapterVersion }
func (a *Adapter) SupportedQRXVersions() []string { return supportedQRXVersions }

func (a *Adapter) Activate(ctx context.Context) error {
	a.caps = a.probeCapabilities(ctx)
	return a.runner.Ping(ctx)
}

func (a *Adapter) Deactivate(ctx context.Context) error {
	a.caps = nil
	return nil
}

func (a *Adapter) Health(ctx context.Context) error {
	return a.runner.Ping(ctx)
}

// probeCapabilities calls each optional command once and records whether it
// succeeded, per docs/architecture.md "Feature Capability Detection": this
// adapter does not assume getvelocityinfo/getvalidatorstatus/getrecentblocks
// exist just because the QRX Core version claims to be 0.0.7.
func (a *Adapter) probeCapabilities(ctx context.Context) map[string]bool {
	caps := map[string]bool{}
	probe := func(key, command string, args ...string) {
		_, err := a.runner.Call(ctx, command, args...)
		caps[key] = err == nil
	}
	probe(adapters.CapValidatorStatus, "getvalidatorstatus")
	probe(adapters.CapBlockProducerInfo, "getblockproducerinfo")
	probe(adapters.CapRecentBlocks, "getrecentblocks", "1")
	probe(adapters.CapVelocity, "getvelocityinfo")
	probe(adapters.CapNonceLanes, "getnoncelanes")
	return caps
}

func (a *Adapter) Capabilities(ctx context.Context) map[string]bool {
	if a.caps == nil {
		a.caps = a.probeCapabilities(ctx)
	}
	return a.caps
}

func (a *Adapter) GetNodeStatus(ctx context.Context) (models.NodeStatus, error) {
	raw, err := a.runner.Call(ctx, "getnodestatus")
	if err != nil {
		return models.NodeStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.NodeStatus{
			Online:      true, // command succeeded, shape just wasn't a JSON object
			LastError:   "unexpected getnodestatus response shape: " + err.Error(),
			AdapterName: a.Name(),
		}, nil
	}
	var buildFields, uptimeFields map[string]json.RawMessage
	if buildInfo, err := a.runner.Call(ctx, "getbuildinfo"); err == nil {
		buildFields, _ = qrx.Fields(buildInfo)
	}
	if uptime, err := a.runner.Call(ctx, "getuptime"); err == nil {
		uptimeFields, _ = qrx.Fields(uptime)
	}

	return models.NodeStatus{
		Online:            true,
		Network:           qrx.Str(fields, "network", "chain"),
		Height:            qrx.Int64(fields, "height", "blocks", "block_height"),
		FinalizedHeight:   qrx.Int64(fields, "finalized_height", "finalizedheight"),
		MempoolTxCount:    qrx.Int64(fields, "mempool_size", "mempool_tx_count"),
		QRXVersion:        qrx.Str(buildFields, "version", "build_version"),
		NodeUptimeSeconds: qrx.Int64(uptimeFields, "uptime", "uptime_seconds"),
		SimulationMode:    false,
		AdapterName:       a.Name(),
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
		Network:         qrx.Str(fields, "network"),
		PeerCount:       qrx.Int(fields, "peer_count", "connections"),
		ProtocolVersion: qrx.Str(fields, "protocol_version"),
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
		Network:         qrx.Str(fields, "network", "chain"),
		Height:          qrx.Int64(fields, "height", "blocks"),
		FinalizedHeight: qrx.Int64(fields, "finalized_height"),
		BestBlockHash:   qrx.Str(fields, "best_block_hash", "bestblockhash"),
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
	return models.MempoolStatus{
		TxCount:   qrx.Int64(fields, "size", "tx_count"),
		SizeBytes: qrx.Int64(fields, "bytes", "size_bytes"),
	}, nil
}

func (a *Adapter) GetFeeInfo(ctx context.Context) (models.FeeInfo, error) {
	raw, err := a.runner.Call(ctx, "getfeeinfo")
	if err != nil {
		return models.FeeInfo{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.FeeInfo{}, err
	}
	return models.FeeInfo{
		MinFee:         qrx.Str(fields, "min_fee", "minfee"),
		RecommendedFee: qrx.Str(fields, "recommended_fee"),
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
			Producer:  qrx.Str(e, "producer").Value,
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
			From:      qrx.Str(e, "from").Value,
			To:        qrx.Str(e, "to").Value,
			Value:     qrx.Str(e, "value", "amount").Value,
		})
	}
	return txs, nil
}

func (a *Adapter) GetValidatorStatus(ctx context.Context) (models.ValidatorStatus, error) {
	if !a.Capabilities(ctx)[adapters.CapValidatorStatus] {
		reason := "getvalidatorstatus not available from this QRX Core instance"
		return models.ValidatorStatus{
			Active:         models.Unsupp[bool](reason),
			Address:        models.Unsupp[string](reason),
			Stake:          models.Unsupp[string](reason),
			DelegatedStake: models.Unsupp[string](reason),
			VotingWeight:   models.Unsupp[string](reason),
		}, nil
	}
	raw, err := a.runner.Call(ctx, "getvalidatorstatus")
	if err != nil {
		return models.ValidatorStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.ValidatorStatus{}, err
	}
	return models.ValidatorStatus{
		Active:         qrx.Bool(fields, "active", "is_active"),
		Address:        qrx.Str(fields, "address", "validator_address"),
		Stake:          qrx.Str(fields, "stake"),
		DelegatedStake: qrx.Str(fields, "delegated_stake"),
		VotingWeight:   qrx.Str(fields, "voting_weight"),
	}, nil
}

func (a *Adapter) GetBlockProducerInfo(ctx context.Context) (models.BlockProducerStatus, error) {
	if !a.Capabilities(ctx)[adapters.CapBlockProducerInfo] {
		reason := "getblockproducerinfo not available from this QRX Core instance"
		return models.BlockProducerStatus{
			ProducerStatus: models.Unsupp[string](reason),
			BlocksProduced: models.Unsupp[int64](reason),
			MissedBlocks:   models.Unsupp[int64](reason),
		}, nil
	}
	raw, err := a.runner.Call(ctx, "getblockproducerinfo")
	if err != nil {
		return models.BlockProducerStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.BlockProducerStatus{}, err
	}
	return models.BlockProducerStatus{
		ProducerStatus: qrx.Str(fields, "status", "producer_status"),
		BlocksProduced: qrx.Int64(fields, "blocks_produced"),
		MissedBlocks:   qrx.Int64(fields, "missed_blocks"),
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
		Loaded:  qrx.Bool(fields, "loaded", "wallet_loaded"),
		Address: qrx.Str(fields, "address"),
		Balance: qrx.Str(fields, "balance"),
	}, nil
}

func (a *Adapter) GetVelocityInfo(ctx context.Context) (models.VelocityStatus, error) {
	if !a.Capabilities(ctx)[adapters.CapVelocity] {
		reason := "getvelocityinfo not available from this QRX Core instance"
		return models.VelocityStatus{
			EngineStatus:       models.Unsupp[string](reason),
			TransactionVersion: models.Unsupp[string](reason),
			SchedulerVersion:   models.Unsupp[string](reason),
			ParallelWidth:      models.Unsupp[int](reason),
			ExecutionWaves:     models.Unsupp[int64](reason),
			Conflicts:          models.Unsupp[int64](reason),
			SelectiveRetries:   models.Unsupp[int64](reason),
		}, nil
	}
	raw, err := a.runner.Call(ctx, "getvelocityinfo")
	if err != nil {
		return models.VelocityStatus{}, err
	}
	fields, err := qrx.Fields(raw)
	if err != nil {
		return models.VelocityStatus{}, err
	}
	return models.VelocityStatus{
		EngineStatus:       qrx.Str(fields, "engine_status", "status"),
		TransactionVersion: qrx.Str(fields, "transaction_version", "tx_version"),
		SchedulerVersion:   qrx.Str(fields, "scheduler_version"),
		ParallelWidth:      qrx.Int(fields, "parallel_width"),
		ExecutionWaves:     qrx.Int64(fields, "execution_waves"),
		Conflicts:          qrx.Int64(fields, "conflicts"),
		SelectiveRetries:   qrx.Int64(fields, "selective_retries"),
	}, nil
}

func (a *Adapter) GetNonceLanes(ctx context.Context) (models.NonceLanes, error) {
	if !a.Capabilities(ctx)[adapters.CapNonceLanes] {
		reason := "getnoncelanes not available from this QRX Core instance"
		return models.NonceLanes{
			LaneCount: models.Unsupp[int](reason),
			Lanes:     models.Unsupp[[]models.Lane](reason),
		}, nil
	}
	raw, err := a.runner.Call(ctx, "getnoncelanes")
	if err != nil {
		return models.NonceLanes{}, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		// Some QRX responses may wrap lanes in an object; degrade gracefully
		// rather than propagate a parse error the dashboard can't act on.
		return models.NonceLanes{
			LaneCount: models.Unavail[int]("unexpected getnoncelanes response shape"),
			Lanes:     models.Unavail[[]models.Lane]("unexpected getnoncelanes response shape"),
		}, nil
	}
	lanes := make([]models.Lane, 0, len(entries))
	for _, e := range entries {
		lanes = append(lanes, models.Lane{
			Index:     qrx.Int(e, "index").Value,
			NextNonce: qrx.Str(e, "next_nonce").Value,
			InFlight:  qrx.Int(e, "in_flight").Value,
		})
	}
	return models.NonceLanes{
		LaneCount: models.Avail(len(lanes)),
		Lanes:     models.Avail(lanes),
	}, nil
}
