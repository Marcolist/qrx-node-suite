// Package qrx is the one place in this module allowed to spawn qrx-cli or
// speak to a QRX Core transport. Nothing outside agent/adapters/* may import
// it -- see docs/architecture.md and agent/adapters/README.md.
package qrx

import "time"

// Config describes how to reach a locally installed QRX Core. Nothing here
// is guessed at runtime beyond an optional PATH lookup for CLIPath; explicit
// configuration always wins (see docs/configuration.md).
type Config struct {
	// CLIPath is the path to the qrx-cli binary. If empty, "qrx-cli" is
	// resolved from PATH at call time.
	CLIPath string
	// DataDir is passed as --datadir PATH when non-empty.
	DataDir string
	// Network selects mainnet/testnet/regtest etc, passed as --network VALUE when
	// non-empty. Never assumed -- an empty value means "let qrx-cli decide",
	// which callers should treat as Unknown rather than "mainnet".
	Network string
	// WalletName is passed as --wallet NAME when non-empty.
	WalletName string
	// Timeout bounds a single command invocation. Defaults to 5s if zero.
	Timeout time.Duration
}

func (c Config) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Second
	}
	return c.Timeout
}

func (c Config) cliPath() string {
	if c.CLIPath == "" {
		return "qrx-cli"
	}
	return c.CLIPath
}
