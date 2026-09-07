// Package manifest defines QRX Node Suite's signed, versioned update
// manifest format (docs/updates.md#update-manifest) and its verification
// (docs/updates.md#update-verification, docs/security.md's OTA threat
// model). It has no dependency on the OTA orchestrator (agent/updates) or
// any transport (agent/updates/sources), so both can depend on it without a
// cycle.
package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// Manifest is one release: a channel, a suite version, and every component
// available at that release. Manifests are never trusted unsigned -- see
// Verify in verify.go.
type Manifest struct {
	ManifestVersion int                        `json:"manifest_version"`
	Channel         string                     `json:"channel"`
	SuiteVersion    string                     `json:"suite_version"`
	ReleasedAt      string                     `json:"released_at"` // RFC3339
	Components      map[string]ComponentUpdate `json:"components"`

	// ManifestSignature is an Ed25519 signature (base64 standard encoding)
	// over CanonicalPayload(m), covering channel/suite_version/released_at
	// and every component's version+sha256. This is stronger than the
	// literal example in docs/updates.md, which only shows a per-component
	// signature: without a whole-manifest signature, an attacker who
	// controls the update server could still swap which components are
	// listed together, or splice in an old (individually still
	// validly-signed) component entry from a prior manifest, without
	// forging any single signature. See docs/security.md "fake manifest".
	ManifestSignature string `json:"manifest_signature"`
}

// ComponentUpdate is one component's entry in a Manifest.
type ComponentUpdate struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`    // hex-encoded
	Signature string `json:"signature"` // Ed25519 signature (base64) over the raw SHA256 digest bytes

	ReleaseNotes *ReleaseNotes `json:"release_notes,omitempty"`
}

// ReleaseNotes is shown to the operator before install
// (docs/updates.md#release-notes) and drives breaking-update protection
// (docs/updates.md#breaking-update-protection).
type ReleaseNotes struct {
	ReleasedAt      string   `json:"released_at,omitempty"`
	Changes         []string `json:"changes,omitempty"`
	BreakingChanges []string `json:"breaking_changes,omitempty"`
	SecurityFixes   []string `json:"security_fixes,omitempty"`

	// RequiredQRXVersion/RequiredAdapterVersion/MinSuiteVersion are version
	// ranges (agent/version.Satisfies syntax) the running system must
	// already meet for this update to be installable. Empty means no
	// constraint.
	RequiredQRXVersion     string `json:"required_qrx_version,omitempty"`
	RequiredAdapterVersion string `json:"required_adapter_version,omitempty"`
	MinSuiteVersion        string `json:"min_suite_version,omitempty"`
}

// Parse decodes and structurally validates a manifest. It does NOT verify
// signatures -- call Verify (verify.go) separately, since that requires a
// public key the parser doesn't have.
func Parse(r io.Reader) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks structural well-formedness: required fields present,
// non-empty components, no empty checksums/signatures. It does not touch
// cryptography.
func (m *Manifest) Validate() error {
	if m.ManifestVersion <= 0 {
		return fmt.Errorf("manifest: manifest_version must be positive")
	}
	if m.Channel == "" {
		return fmt.Errorf("manifest: channel is required")
	}
	if m.SuiteVersion == "" {
		return fmt.Errorf("manifest: suite_version is required")
	}
	if m.ReleasedAt == "" {
		return fmt.Errorf("manifest: released_at is required")
	}
	if _, err := time.Parse(time.RFC3339, m.ReleasedAt); err != nil {
		return fmt.Errorf("manifest: released_at must be RFC3339: %w", err)
	}
	if m.ManifestSignature == "" {
		return fmt.Errorf("manifest: manifest_signature is required -- unsigned manifests are never trusted")
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("manifest: at least one component is required")
	}
	for name, c := range m.Components {
		if c.Version == "" {
			return fmt.Errorf("manifest: component %q missing version", name)
		}
		if c.URL == "" {
			return fmt.Errorf("manifest: component %q missing url", name)
		}
		if c.SHA256 == "" {
			return fmt.Errorf("manifest: component %q missing sha256", name)
		}
		if c.Signature == "" {
			return fmt.Errorf("manifest: component %q missing signature -- unsigned artifacts are never trusted", name)
		}
	}
	return nil
}

// CanonicalPayload builds the deterministic byte sequence the manifest
// signature is computed over: channel, suite_version, released_at, then
// every component's (name, version, sha256) sorted by name. Using a fixed
// field list rather than raw JSON bytes avoids canonical-JSON edge cases
// (key ordering, whitespace) while still committing to everything that
// matters for integrity.
func CanonicalPayload(m *Manifest) []byte {
	names := make([]string, 0, len(m.Components))
	for name := range m.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf []byte
	write := func(s string) {
		buf = append(buf, []byte(s)...)
		buf = append(buf, 0) // NUL separator: prevents field-boundary ambiguity
	}
	write(fmt.Sprintf("%d", m.ManifestVersion))
	write(m.Channel)
	write(m.SuiteVersion)
	write(m.ReleasedAt)
	for _, name := range names {
		c := m.Components[name]
		write(name)
		write(c.Version)
		write(c.SHA256)
	}
	return buf
}
