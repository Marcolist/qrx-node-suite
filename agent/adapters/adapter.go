// Package adapters defines the QRX Adapter boundary and hosts the
// AdapterRegistry. This is the only layer allowed to translate QRX Core
// output into agent/models types; see docs/architecture.md.
//
// Concrete adapters live in their own subpackages (mock, qrx007, legacy006,
// future) and self-register via Register() in an init() function, so the
// registry never hardcodes a list of adapters -- discovery is "which
// adapter packages did main.go import."
package adapters

import (
	"context"

	"qrx-node-suite/agent/models"
)

// Well-known capability keys for runtime capability detection
// (docs/architecture.md "Feature Capability Detection"). Adapters populate
// these based on what QRX Core actually answered, not on version number.
const (
	CapValidatorStatus   = "validator_status"
	CapBlockProducerInfo = "block_producer_info"
	CapRecentBlocks      = "recent_blocks"
	CapVelocity          = "velocity_info"
	CapNonceLanes        = "nonce_lanes"
)

// Adapter is the QRX Adapter interface. Method signatures intentionally
// mirror the QRX 0.0.7 command surface documented in
// docs/qrx-0.0.7-interface.md; adjust them if a real QRX response shape
// requires it, but never add a method for a command that hasn't been
// confirmed to exist.
type Adapter interface {
	// Name is the adapter's registry key (e.g. "qrx007").
	Name() string
	// Version is the adapter's own semantic version, independent of the QRX
	// Core version it targets (docs/updates.md#adapter-release-strategy).
	Version() string
	// SupportedQRXVersions is informational only. It is never used to decide
	// compatibility -- see version.Matrix, the authoritative source.
	SupportedQRXVersions() []string

	// Activate is called when this adapter becomes the active adapter. It
	// should be fast and must not block indefinitely; use ctx for timeout.
	Activate(ctx context.Context) error
	// Deactivate is called when this adapter stops being active (manual
	// switch or rollback). Must release any held resources.
	Deactivate(ctx context.Context) error
	// Health reports whether the adapter can currently reach QRX Core.
	Health(ctx context.Context) error
	// Capabilities performs runtime capability detection: which optional
	// commands actually answered successfully, keyed by the Cap* constants.
	// Implementations should cache this rather than re-probe on every call.
	Capabilities(ctx context.Context) map[string]bool

	GetNodeStatus(ctx context.Context) (models.NodeStatus, error)
	GetNetworkInfo(ctx context.Context) (models.NetworkStatus, error)
	GetBlockchainInfo(ctx context.Context) (models.BlockchainStatus, error)
	GetMempoolInfo(ctx context.Context) (models.MempoolStatus, error)
	GetFeeInfo(ctx context.Context) (models.FeeInfo, error)
	GetRecentBlocks(ctx context.Context, limit int) ([]models.RecentBlock, error)
	GetRecentTransactions(ctx context.Context, limit int) ([]models.RecentTransaction, error)
	GetValidatorStatus(ctx context.Context) (models.ValidatorStatus, error)
	GetBlockProducerInfo(ctx context.Context) (models.BlockProducerStatus, error)
	GetWalletInfo(ctx context.Context) (models.WalletSummary, error)
	GetVelocityInfo(ctx context.Context) (models.VelocityStatus, error)
	GetNonceLanes(ctx context.Context) (models.NonceLanes, error)
}

// Factory constructs a fresh, unactivated Adapter instance.
type Factory func() Adapter

var factories = map[string]Factory{}

// Register makes an adapter available for discovery. Call from an adapter
// package's init(); main.go blank-imports the packages it wants installed
// (docs/development.md), which is what "installed adapters" means for a
// single-binary Go Agent -- there is no dynamic plugin loading.
func Register(name string, f Factory) {
	if _, exists := factories[name]; exists {
		panic("adapters: duplicate registration for " + name)
	}
	factories[name] = f
}

// Installed returns the names of every adapter package that registered
// itself, sorted is left to the caller (Registry.Discover sorts).
func Installed() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	return names
}
