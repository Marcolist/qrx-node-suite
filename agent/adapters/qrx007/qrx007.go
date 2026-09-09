// Package qrx007 wraps the QRX 0.0.7 command surface documented in
// docs/qrx-0.0.7-interface.md.
//
// Response fields are based on the pinned phoenixkonsole/qrx 0.0.7 source
// and are exercised against a real local daemon in release validation.
package qrx007

import (
	"context"
	"encoding/json"
	"fmt"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/qrx"
)

func init() {
	adapters.Register("qrx007", func() adapters.Adapter { return New(qrx.Config{}) })
}

const adapterVersion = "1.1.0"

var supportedQRXVersions = []string{"0.0.7"}

// Adapter wraps qrx-cli for a QRX 0.0.7 node via the centralized
// qrx.Runner (agent/qrx), the only place allowed to spawn qrx-cli.
type Adapter struct {
	cfg           qrx.Config
	runner        *qrx.Runner
	caps          map[string]bool
	walletAddress string
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

// Config returns the connection config currently in effect -- read-only
// access for diagnostics and tests (e.g. proving cmd/agentd actually
// applied the operator's configured cli_path/network/wallet_name/data_dir
// before activation; see the F11 fix in cmd/agentd/main.go).
func (a *Adapter) Config() qrx.Config { return a.cfg }

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
	if raw, err := a.runner.Call(ctx, "getwalletinfo"); err == nil {
		if fields, fieldErr := qrx.Fields(raw); fieldErr == nil {
			if address := qrx.Str(fields, "address"); address.Ok() {
				a.walletAddress = address.Value
				probe(adapters.CapNonceLanes, "getnoncelanes", address.Value)
			}
		}
	}
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
	var buildFields, uptimeFields, mempoolFields map[string]json.RawMessage
	if buildInfo, err := a.runner.Call(ctx, "getbuildinfo"); err == nil {
		buildFields, _ = qrx.Fields(buildInfo)
	}
	if uptime, err := a.runner.Call(ctx, "getuptime"); err == nil {
		uptimeFields, _ = qrx.Fields(uptime)
	}
	if mempool, err := a.runner.Call(ctx, "getmempoolinfo"); err == nil {
		mempoolFields, _ = qrx.Fields(mempool)
	}

	height := qrx.Int64(fields, "local_height", "height", "blocks", "block_height")
	// Core reports 100% synchronized at height zero even when none of the
	// configured peer addresses is connected. Without live peer telemetry,
	// its best_peer_height/blocks_behind values cannot prove synchronization.
	syncStatus := models.Unavail[models.SyncStatus]("QRX 0.0.7 cannot establish sync state without confirmed live peer telemetry")
	return models.NodeStatus{
		Online:            true,
		Network:           qrx.Str(fields, "network", "chain"),
		Height:            height,
		FinalizedHeight:   qrx.Int64(fields, "finalized_height", "finalizedheight"),
		Sync:              syncStatus,
		Peers:             models.Unavail[models.PeerSummary]("QRX 0.0.7 reports configured peer entries, not confirmed live connections"),
		MempoolTxCount:    qrx.Int64(mempoolFields, "txs", "size", "tx_count"),
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
	network := models.Unavail[string]("network was not configured")
	if a.cfg.Network != "" {
		network = models.Avail(a.cfg.Network)
	}
	return models.NetworkStatus{
		Network:         network,
		PeerCount:       models.Unavail[int]("QRX 0.0.7 connections counts configured entries, not confirmed live peers"),
		ProtocolVersion: qrx.Str(fields, "protocolversion", "protocol_version"),
		ListenAddresses: models.Unavail[[]string]("QRX 0.0.7 reports only a listening boolean, not bound addresses"),
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
		BestBlockHash:   qrx.Str(fields, "bestblockhash", "best_block_hash"),
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
		TxCount:   qrx.Int64(fields, "txs", "size", "tx_count"),
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
	result, err := qrx.Result(raw)
	if err != nil {
		return nil, err
	}
	var resultObject map[string]json.RawMessage
	if err := json.Unmarshal(result, &resultObject); err != nil {
		return nil, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(resultObject["blocks"], &entries); err != nil {
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
	result, err := qrx.Result(raw)
	if err != nil {
		return nil, err
	}
	var resultObject map[string]json.RawMessage
	if err := json.Unmarshal(result, &resultObject); err != nil {
		return nil, err
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(resultObject["transactions"], &entries); err != nil {
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
	var staking map[string]json.RawMessage
	_ = json.Unmarshal(fields["staking"], &staking)
	power := qrx.Str(staking, "validator_power")
	active := models.Unavail[bool]("validator power not present in qrx-cli response")
	if rawPower, ok := staking["validator_power"]; ok {
		var n int64
		if json.Unmarshal(rawPower, &n) == nil {
			active = models.Avail(n > 0)
		}
	}
	return models.ValidatorStatus{
		Active:         active,
		Address:        qrx.Str(staking, "address"),
		Stake:          qrx.Str(staking, "self_stake"),
		DelegatedStake: qrx.Str(staking, "delegated_to_me"),
		VotingWeight:   power,
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
	producerStatus := models.Unavail[string]("enabled field not present in qrx-cli response")
	if enabled := qrx.Bool(fields, "enabled"); enabled.Ok() {
		producerStatus = models.Avail("disabled")
		if enabled.Value {
			producerStatus = models.Avail("enabled")
		}
	}
	return models.BlockProducerStatus{
		ProducerStatus: producerStatus,
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
	address := qrx.Str(fields, "address")
	loaded := qrx.Bool(fields, "loaded", "wallet_loaded")
	if !loaded.Ok() && address.Ok() {
		loaded = models.Avail(true)
	}
	return models.WalletSummary{
		Loaded:  loaded,
		Address: address,
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
		EngineStatus:       qrx.Str(fields, "core_track", "engine_status", "status"),
		TransactionVersion: qrx.Str(fields, "velocity_tx_version", "transaction_version", "tx_version"),
		SchedulerVersion:   qrx.Str(fields, "parallel_execution_model", "deterministic_mempool_order", "scheduler_version"),
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
	if a.walletAddress == "" {
		return models.NonceLanes{
			LaneCount: models.Unavail[int]("wallet address unavailable"),
			Lanes:     models.Unavail[[]models.Lane]("wallet address unavailable"),
		}, nil
	}
	raw, err := a.runner.Call(ctx, "getnoncelanes", a.walletAddress)
	if err != nil {
		return models.NonceLanes{}, err
	}
	result, resultErr := qrx.Result(raw)
	if resultErr != nil {
		return models.NonceLanes{}, resultErr
	}
	var resultObject map[string]json.RawMessage
	if err := json.Unmarshal(result, &resultObject); err != nil {
		return models.NonceLanes{}, err
	}
	var rawLanes []string
	if err := json.Unmarshal(resultObject["lanes"], &rawLanes); err != nil {
		// Some QRX responses may wrap lanes in an object; degrade gracefully
		// rather than propagate a parse error the dashboard can't act on.
		return models.NonceLanes{
			LaneCount: models.Unavail[int]("unexpected getnoncelanes response shape"),
			Lanes:     models.Unavail[[]models.Lane]("unexpected getnoncelanes response shape"),
		}, nil
	}
	lanes := make([]models.Lane, 0, len(rawLanes))
	for _, rawLane := range rawLanes {
		var index int
		var nonce string
		if _, err := fmt.Sscanf(rawLane, "lane=%d nonce=%s", &index, &nonce); err != nil {
			continue
		}
		lanes = append(lanes, models.Lane{Index: index, NextNonce: nonce})
	}
	return models.NonceLanes{
		LaneCount: models.Avail(len(lanes)),
		Lanes:     models.Avail(lanes),
	}, nil
}
