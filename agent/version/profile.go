package version

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CompatibilityProfile is an independently updateable, versioned document
// describing what a specific QRX Core version actually offers: known
// commands, feature flags, required adapter, known bugs, and switching
// safety flags. Profiles are data, not code -- they can be shipped via OTA
// (agent/updates) without an Agent release. See docs/qrx-compatibility.md.
type CompatibilityProfile struct {
	ProfileVersion int    `json:"profile_version"`
	QRXCoreVersion string `json:"qrx_core_version"`

	KnownCLICommands []string        `json:"known_cli_commands"`
	KnownRPCMethods  []string        `json:"known_rpc_methods"`
	FeatureFlags     map[string]bool `json:"feature_flags"`

	RequiredAdapter            string   `json:"required_adapter"`
	OptionalFeatures           []string `json:"optional_features,omitempty"`
	KnownBugs                  []string `json:"known_bugs,omitempty"`
	PlatformNotes              string   `json:"platform_notes,omitempty"`
	MinRecommendedSuiteVersion string   `json:"min_recommended_suite_version,omitempty"`

	// CoreVersionSwitchSafety records what is known about switching an
	// already-installed node onto this QRX Core version in place. Any field
	// left at its zero value ("") is treated as Unknown by
	// agent/updates.CoreSwitchSafetyCheck, which then blocks the switch unless
	// an expert override is supplied (docs/updates.md#core-version-switching-safety).
	CoreVersionSwitchSafety SwitchSafety `json:"core_version_switch_safety"`
}

// SwitchCompat is a tri-state compatibility judgement: it is deliberately not
// a bool, because "we don't know" must never collapse into "yes" or "no".
type SwitchCompat string

const (
	SwitchCompatible    SwitchCompat = "compatible"
	SwitchIncompatible  SwitchCompat = "incompatible"
	SwitchUnknownCompat SwitchCompat = "" // zero value == unknown, blocks by default
)

// SwitchSafety captures the five checks docs/updates.md requires before
// switching an installed node onto a different QRX Core version.
type SwitchSafety struct {
	BlockchainData SwitchCompat `json:"blockchain_data"`
	Configuration  SwitchCompat `json:"configuration"`
	Wallet         SwitchCompat `json:"wallet"`
	Adapter        SwitchCompat `json:"adapter"`
	Network        SwitchCompat `json:"network"`
}

// AllKnown reports whether every switch-safety dimension has an explicit
// compatible/incompatible judgement (none left as unknown).
func (s SwitchSafety) AllKnown() bool {
	for _, c := range []SwitchCompat{s.BlockchainData, s.Configuration, s.Wallet, s.Adapter, s.Network} {
		if c != SwitchCompatible && c != SwitchIncompatible {
			return false
		}
	}
	return true
}

// AnyIncompatible reports whether any dimension is explicitly incompatible.
func (s SwitchSafety) AnyIncompatible() bool {
	for _, c := range []SwitchCompat{s.BlockchainData, s.Configuration, s.Wallet, s.Adapter, s.Network} {
		if c == SwitchIncompatible {
			return true
		}
	}
	return false
}

// HasCapability reports whether a feature flag is explicitly enabled. Missing
// flags default to false -- callers that need to distinguish "known absent"
// from "never checked" should inspect FeatureFlags directly.
func (p *CompatibilityProfile) HasCapability(flag string) bool {
	if p.FeatureFlags == nil {
		return false
	}
	return p.FeatureFlags[flag]
}

// LoadCompatibilityProfile reads and parses a compatibility profile document.
func LoadCompatibilityProfile(r io.Reader) (*CompatibilityProfile, error) {
	var p CompatibilityProfile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("parse compatibility profile: %w", err)
	}
	return &p, nil
}

// LoadCompatibilityProfileFile loads a compatibility profile from a file path.
func LoadCompatibilityProfileFile(path string) (*CompatibilityProfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open compatibility profile %s: %w", path, err)
	}
	defer f.Close()
	return LoadCompatibilityProfile(f)
}
