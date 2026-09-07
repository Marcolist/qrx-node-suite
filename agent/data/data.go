// Package data embeds the compatibility matrix and compatibility profiles
// that ship with the Agent binary (ADR-001: single static binary, no
// runtime dependency on a relative filesystem path to find its own data).
// Operators can still override these at runtime with OTA-updated
// compatibility_profile components (docs/updates.md) staged elsewhere.
package data

import (
	"embed"
	"fmt"

	"qrx-node-suite/agent/version"
)

//go:embed compatibility-matrix.json compatibility-profiles/*.json
var FS embed.FS

// LoadMatrix parses the embedded compatibility matrix.
func LoadMatrix() (*version.Matrix, error) {
	f, err := FS.Open("compatibility-matrix.json")
	if err != nil {
		return nil, fmt.Errorf("data: open embedded compatibility matrix: %w", err)
	}
	defer f.Close()
	return version.LoadMatrix(f)
}

// LoadProfile parses the embedded compatibility profile for a QRX Core
// version, e.g. LoadProfile("0.0.7") reads compatibility-profiles/qrx-0.0.7.json.
func LoadProfile(qrxCoreVersion string) (*version.CompatibilityProfile, error) {
	f, err := FS.Open("compatibility-profiles/qrx-" + qrxCoreVersion + ".json")
	if err != nil {
		return nil, fmt.Errorf("data: open embedded compatibility profile for %s: %w", qrxCoreVersion, err)
	}
	defer f.Close()
	return version.LoadCompatibilityProfile(f)
}
