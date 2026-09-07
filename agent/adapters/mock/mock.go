// Package mock implements a simulated QRX Core adapter for development and
// CI, with no real qrxd required. Every NodeStatus it returns has
// SimulationMode set to true; agent/api and the dashboard must render a
// persistent "SIMULATION MODE" indicator whenever the active adapter reports
// it (docs/architecture.md, principle 6) -- mock output must never be
// visually indistinguishable from a real node.
package mock

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/models"
)

func init() {
	adapters.Register("mock", func() adapters.Adapter { return New() })
}

const adapterVersion = "1.0.0"

// Adapter simulates a QRX 0.0.7-shaped node. Every optional QRX 0.0.7
// capability is turned on so the dashboard can be developed against a full
// data set; state is used elsewhere.
type Adapter struct {
	mu      sync.Mutex
	rng     *rand.Rand
	started time.Time

	online         bool
	height         int64
	finalizedLag   int64
	peers          models.PeerSummary
	mempoolTx      int64
	validatorOn    bool
	blocksProduced int64
	missedBlocks   int64

	stop chan struct{}
}

// New builds a mock adapter. It does not start simulating until Activate is
// called.
func New() *Adapter {
	return &Adapter{
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
		height:       1_204_500,
		finalizedLag: 2,
		peers:        models.PeerSummary{Connected: 12, Inbound: 5, Outbound: 7},
		validatorOn:  true,
	}
}

func (a *Adapter) Name() string    { return "mock" }
func (a *Adapter) Version() string { return adapterVersion }
func (a *Adapter) SupportedQRXVersions() []string {
	return []string{"0.0.6", "0.0.7"} // simulates whichever profile the caller asks for
}

func (a *Adapter) Activate(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stop != nil {
		return nil // already active
	}
	a.online = true
	a.started = time.Now()
	a.stop = make(chan struct{})
	go a.simulate(a.stop)
	return nil
}

func (a *Adapter) Deactivate(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stop != nil {
		close(a.stop)
		a.stop = nil
	}
	a.online = false
	return nil
}

func (a *Adapter) Health(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.online {
		return errOffline
	}
	return nil
}

func (a *Adapter) Capabilities(ctx context.Context) map[string]bool {
	return map[string]bool{
		adapters.CapValidatorStatus:   true,
		adapters.CapBlockProducerInfo: true,
		adapters.CapRecentBlocks:      true,
		adapters.CapVelocity:          true,
		adapters.CapNonceLanes:        true,
	}
}

// simulate advances chain state every 2 seconds: height increases, peers
// drift, mempool churns, validator occasionally goes idle -- enough surface
// for the dashboard to visibly react without needing a real node.
func (a *Adapter) simulate(stop chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			a.mu.Lock()
			a.height++
			a.mempoolTx = int64(a.rng.Intn(40))
			if a.rng.Intn(20) == 0 {
				delta := a.rng.Intn(5) - 2
				a.peers.Connected += delta
				if a.peers.Connected < 1 {
					a.peers.Connected = 1
				}
			}
			if a.validatorOn {
				if a.rng.Intn(15) == 0 {
					a.missedBlocks++
				} else {
					a.blocksProduced++
				}
			}
			a.mu.Unlock()
		}
	}
}

type simError string

func (e simError) Error() string { return string(e) }

const errOffline = simError("mock: adapter not active")

func (a *Adapter) GetNodeStatus(ctx context.Context) (models.NodeStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	return models.NodeStatus{
		Online:             a.online,
		Network:            models.Avail("testnet-simulation"),
		Height:             models.Avail(a.height),
		FinalizedHeight:    models.Avail(a.height - a.finalizedLag),
		Sync:               models.Avail(models.SyncStatus{Syncing: false, CurrentHeight: a.height}),
		Peers:              models.Avail(a.peers),
		MempoolTxCount:     models.Avail(a.mempoolTx),
		QRXVersion:         models.Avail("0.0.7-simulated"),
		NodeUptimeSeconds:  models.Avail(int64(now.Sub(a.started).Seconds())),
		LastSuccessfulPoll: &now,
		SimulationMode:     true,
		AdapterName:        "mock",
	}, nil
}

func (a *Adapter) GetNetworkInfo(ctx context.Context) (models.NetworkStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return models.NetworkStatus{
		Network:         models.Avail("testnet-simulation"),
		PeerCount:       models.Avail(a.peers.Connected),
		ProtocolVersion: models.Avail("70015-sim"),
		ListenAddresses: models.Avail([]string{"0.0.0.0:8333"}),
	}, nil
}

func (a *Adapter) GetBlockchainInfo(ctx context.Context) (models.BlockchainStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return models.BlockchainStatus{
		Network:         models.Avail("testnet-simulation"),
		Height:          models.Avail(a.height),
		FinalizedHeight: models.Avail(a.height - a.finalizedLag),
		BestBlockHash:   models.Avail(simHash(a.height)),
	}, nil
}

func (a *Adapter) GetMempoolInfo(ctx context.Context) (models.MempoolStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return models.MempoolStatus{
		TxCount:   models.Avail(a.mempoolTx),
		SizeBytes: models.Avail(a.mempoolTx * 250),
	}, nil
}

func (a *Adapter) GetFeeInfo(ctx context.Context) (models.FeeInfo, error) {
	return models.FeeInfo{
		MinFee:         models.Avail("0.00001"),
		RecommendedFee: models.Avail("0.00005"),
	}, nil
}

func (a *Adapter) GetRecentBlocks(ctx context.Context, limit int) ([]models.RecentBlock, error) {
	a.mu.Lock()
	h := a.height
	a.mu.Unlock()
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	blocks := make([]models.RecentBlock, 0, limit)
	now := time.Now().Unix()
	for i := 0; i < limit; i++ {
		height := h - int64(i)
		blocks = append(blocks, models.RecentBlock{
			Height:    height,
			Hash:      simHash(height),
			Timestamp: now - int64(i*6),
			TxCount:   intPtr(a.rng.Intn(30)),
			Producer:  "sim-producer-1",
		})
	}
	return blocks, nil
}

func (a *Adapter) GetRecentTransactions(ctx context.Context, limit int) ([]models.RecentTransaction, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	now := time.Now().Unix()
	txs := make([]models.RecentTransaction, 0, limit)
	for i := 0; i < limit; i++ {
		txs = append(txs, models.RecentTransaction{
			Hash:      simHash(int64(i) + now),
			Timestamp: now - int64(i*3),
			From:      "sim1qsender",
			To:        "sim1qreceiver",
			Value:     "1.25",
		})
	}
	return txs, nil
}

func (a *Adapter) GetValidatorStatus(ctx context.Context) (models.ValidatorStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return models.ValidatorStatus{
		Active:         models.Avail(a.validatorOn),
		Address:        models.Avail("sim1qvalidatoraddress"),
		Stake:          models.Avail("100000.0"),
		DelegatedStake: models.Avail("25000.0"),
		VotingWeight:   models.Avail("0.042"),
	}, nil
}

func (a *Adapter) GetBlockProducerInfo(ctx context.Context) (models.BlockProducerStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	status := "idle"
	if a.validatorOn {
		status = "producing"
	}
	return models.BlockProducerStatus{
		ProducerStatus: models.Avail(status),
		BlocksProduced: models.Avail(a.blocksProduced),
		MissedBlocks:   models.Avail(a.missedBlocks),
	}, nil
}

func (a *Adapter) GetWalletInfo(ctx context.Context) (models.WalletSummary, error) {
	return models.WalletSummary{
		Loaded:  models.Avail(true),
		Address: models.Avail("sim1qwalletaddress"),
		Balance: models.Avail("4213.7788"),
	}, nil
}

func (a *Adapter) GetVelocityInfo(ctx context.Context) (models.VelocityStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return models.VelocityStatus{
		EngineStatus:       models.Avail("running"),
		TransactionVersion: models.Avail("2"),
		SchedulerVersion:   models.Avail("1"),
		ParallelWidth:      models.Avail(4 + a.rng.Intn(4)),
		ExecutionWaves:     models.Avail(a.height % 1000),
		Conflicts:          models.Avail(int64(a.rng.Intn(3))),
		SelectiveRetries:   models.Avail(int64(a.rng.Intn(2))),
	}, nil
}

func (a *Adapter) GetNonceLanes(ctx context.Context) (models.NonceLanes, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lanes := make([]models.Lane, 0, 4)
	for i := 0; i < 4; i++ {
		lanes = append(lanes, models.Lane{Index: i, NextNonce: simHash(int64(i) + a.height)[:8], InFlight: a.rng.Intn(3)})
	}
	return models.NonceLanes{
		LaneCount: models.Avail(len(lanes)),
		Lanes:     models.Avail(lanes),
	}, nil
}

func simHash(seed int64) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 64)
	x := seed*2654435761 + 1
	for i := range b {
		x = x*6364136223846793005 + 1442695040888963407
		b[i] = hex[(x>>uint(i%56))&0xf]
	}
	return string(b)
}

func intPtr(v int) *int { return &v }
