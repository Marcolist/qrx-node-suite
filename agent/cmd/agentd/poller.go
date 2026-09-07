package main

import (
	"context"
	"log/slog"
	"time"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/alerts"
	"qrx-node-suite/agent/api"
	"qrx-node-suite/agent/events"
	"qrx-node-suite/agent/guardian"
	"qrx-node-suite/agent/models"
	"qrx-node-suite/agent/monitoring"
)

// Poller is the Agent's one centralized polling loop
// (docs/configuration.md#polling: "Use centralized polling. Dashboard
// requests should read cached state where possible instead of invoking
// qrx-cli repeatedly."). It is the only thing that calls the active
// adapter's Get* methods; every API handler reads api.Cache instead.
type Poller struct {
	Registry   *adapters.Registry
	Monitoring monitoring.Collector
	Cache      *api.Cache
	Bus        *events.Bus
	Guardian   *guardian.Guardian
	Alerts     *alerts.Engine
	Interval   time.Duration
	Logger     *slog.Logger

	lastSuccess time.Time
}

// Run blocks, ticking until ctx is done.
func (p *Poller) Run(ctx context.Context) {
	if p.Interval <= 0 {
		p.Interval = 5 * time.Second
	}
	p.tick(ctx) // populate the cache immediately, don't make the first request wait a full interval
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	prev := p.Cache.Get()
	snap := &api.Snapshot{UpdatedAt: time.Now()}

	active := p.Registry.Active()
	if active == nil {
		snap.Node = models.NodeStatus{Online: false, LastError: "no active adapter"}
	} else {
		snap.AdapterName = active.Name()
		p.collect(ctx, active, prev, snap)
	}
	if p.Monitoring != nil {
		snap.System = p.Monitoring.Collect(ctx)
	}
	if snap.Node.Online {
		p.lastSuccess = time.Now()
	}

	var adapterHealthy bool
	if active != nil {
		adapterHealthy = active.Health(ctx) == nil
	}
	state := p.Guardian.Evaluate(ctx, guardian.Signals{
		QRXProcessRunning:  active != nil,
		NodeOnline:         snap.Node.Online,
		AdapterHealthy:     adapterHealthy,
		LastSuccessfulPoll: p.lastSuccess,
	})
	snap.Health = state

	p.Cache.Set(snap)
	p.publishDiffs(prev, snap)

	if p.Alerts != nil {
		if err := p.Alerts.Evaluate(ctx, alerts.Snapshot{Health: state, System: snap.System, Node: snap.Node}); err != nil {
			p.Logger.Error("alert evaluation failed", "error", err)
		}
	}
}

// collect calls every adapter method for one poll tick, tolerating
// individual failures: a field that fails to fetch keeps its previous
// cached value (with the adapter's error noted on NodeStatus) rather than
// blanking the whole snapshot to zero values.
func (p *Poller) collect(ctx context.Context, a adapters.Adapter, prev *api.Snapshot, snap *api.Snapshot) {
	if ns, err := a.GetNodeStatus(ctx); err == nil {
		snap.Node = ns
	} else {
		snap.Node = prev.Node
		snap.Node.Online = false
		snap.Node.LastError = err.Error()
		p.Logger.Warn("GetNodeStatus failed", "error", err)
	}
	if v, err := a.GetNetworkInfo(ctx); err == nil {
		snap.Network = v
	} else {
		snap.Network = prev.Network
	}
	if v, err := a.GetBlockchainInfo(ctx); err == nil {
		snap.Blockchain = v
	} else {
		snap.Blockchain = prev.Blockchain
	}
	if v, err := a.GetMempoolInfo(ctx); err == nil {
		snap.Mempool = v
	} else {
		snap.Mempool = prev.Mempool
	}
	if v, err := a.GetFeeInfo(ctx); err == nil {
		snap.Fees = v
	} else {
		snap.Fees = prev.Fees
	}
	if v, err := a.GetValidatorStatus(ctx); err == nil {
		snap.Validator = v
	} else {
		snap.Validator = prev.Validator
	}
	if v, err := a.GetBlockProducerInfo(ctx); err == nil {
		snap.BlockProducer = v
	} else {
		snap.BlockProducer = prev.BlockProducer
	}
	if v, err := a.GetVelocityInfo(ctx); err == nil {
		snap.Velocity = v
	} else {
		snap.Velocity = prev.Velocity
	}
	if v, err := a.GetNonceLanes(ctx); err == nil {
		snap.NonceLanes = v
	} else {
		snap.NonceLanes = prev.NonceLanes
	}
	if v, err := a.GetWalletInfo(ctx); err == nil {
		snap.Wallet = v
	} else {
		snap.Wallet = prev.Wallet
	}
	if v, err := a.GetRecentBlocks(ctx, 10); err == nil {
		snap.RecentBlocks = v
	} else {
		snap.RecentBlocks = prev.RecentBlocks
	}
	if v, err := a.GetRecentTransactions(ctx, 10); err == nil {
		snap.RecentTransactions = v
	} else {
		snap.RecentTransactions = prev.RecentTransactions
	}
}

// publishDiffs emits a representative subset of docs/architecture.md
// section 14's event vocabulary -- every meaningful state CHANGE, not a
// firehose of every field on every tick.
func (p *Poller) publishDiffs(prev, snap *api.Snapshot) {
	if p.Bus == nil {
		return
	}
	now := time.Now()
	if prev.Node.Online != snap.Node.Online {
		t := models.EventNodeOffline
		if snap.Node.Online {
			t = models.EventNodeOnline
		}
		p.Bus.Publish(models.Event{Type: t, Timestamp: now})
	}
	if prev.Node.Height.Ok() && snap.Node.Height.Ok() && prev.Node.Height.Value != snap.Node.Height.Value {
		p.Bus.Publish(models.Event{Type: models.EventNodeHeightChanged, Timestamp: now, Data: snap.Node.Height.Value})
	}
	if prev.Node.Peers.Ok() && snap.Node.Peers.Ok() && prev.Node.Peers.Value.Connected != snap.Node.Peers.Value.Connected {
		p.Bus.Publish(models.Event{Type: models.EventNodePeerCountChanged, Timestamp: now, Data: snap.Node.Peers.Value})
	}
	if prev.Node.MempoolTxCount.Ok() && snap.Node.MempoolTxCount.Ok() && prev.Node.MempoolTxCount.Value != snap.Node.MempoolTxCount.Value {
		p.Bus.Publish(models.Event{Type: models.EventNodeMempoolChanged, Timestamp: now, Data: snap.Node.MempoolTxCount.Value})
	}
	// A Guardian health-state change publishes its own event via the
	// onEvent callback wired in main.go, not duplicated here.
}
