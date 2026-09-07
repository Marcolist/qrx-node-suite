// Package api implements the Agent's versioned REST API
// (docs/architecture.md section 13) and SSE event stream (section 14).
// Handlers never call the active adapter directly -- they read from Cache,
// which cmd/agentd's poller keeps current on a schedule
// (docs/configuration.md#polling: "Dashboard requests should read cached
// state where possible instead of invoking qrx-cli repeatedly").
package api

import (
	"sync/atomic"
	"time"

	"qrx-node-suite/agent/models"
)

// Snapshot is one consistent, point-in-time read of everything the API
// serves from cache. It is immutable once published -- Cache.Set swaps in
// a whole new *Snapshot atomically, so readers never see a mix of old and
// new fields.
type Snapshot struct {
	Node               models.NodeStatus
	Network            models.NetworkStatus
	Blockchain         models.BlockchainStatus
	Mempool            models.MempoolStatus
	Fees               models.FeeInfo
	Validator          models.ValidatorStatus
	BlockProducer      models.BlockProducerStatus
	Velocity           models.VelocityStatus
	NonceLanes         models.NonceLanes
	Wallet             models.WalletSummary
	System             models.SystemStatus
	RecentBlocks       []models.RecentBlock
	RecentTransactions []models.RecentTransaction
	Health             models.HealthState
	AdapterName        string
	UpdatedAt          time.Time
}

// Cache holds the latest Snapshot, safe for concurrent read/write via an
// atomic pointer swap -- no lock needed on the (much more frequent) read
// path.
type Cache struct {
	ptr atomic.Pointer[Snapshot]
}

func NewCache() *Cache {
	c := &Cache{}
	c.ptr.Store(&Snapshot{Health: models.HealthOffline})
	return c
}

// Get returns the current snapshot. Never nil.
func (c *Cache) Get() *Snapshot {
	return c.ptr.Load()
}

// Set publishes a new snapshot, replacing the old one atomically.
func (c *Cache) Set(s *Snapshot) {
	c.ptr.Store(s)
}
