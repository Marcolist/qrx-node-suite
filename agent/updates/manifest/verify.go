package manifest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"qrx-node-suite/agent/version"
)

var (
	// ErrInvalidManifestSignature means the manifest's own signature does
	// not verify -- the manifest is untrusted in its entirety and must be
	// discarded, not partially used.
	ErrInvalidManifestSignature = errors.New("manifest: signature verification failed")
	// ErrChecksumMismatch means a downloaded artifact's SHA256 does not
	// match the manifest -- the download is corrupted or tampered and must
	// not be installed.
	ErrChecksumMismatch = errors.New("manifest: artifact checksum mismatch")
	// ErrInvalidArtifactSignature means a component's per-artifact
	// signature does not verify.
	ErrInvalidArtifactSignature = errors.New("manifest: artifact signature verification failed")
	// ErrDowngradeBlocked means the target version is not newer than the
	// currently installed version and downgrade was not explicitly allowed.
	ErrDowngradeBlocked = errors.New("manifest: downgrade blocked (automatic downgrades are never allowed; manual downgrade requires explicit confirmation)")
	// ErrIncompatible means ReleaseNotes declares a requirement (QRX Core
	// version, adapter version, minimum Suite version) the running system
	// does not currently meet.
	ErrIncompatible = errors.New("manifest: update is incompatible with the currently running system")
	// ErrReplayed means this manifest is not newer than the last one this
	// Agent already saw for this channel -- possible replay of a stale,
	// still-validly-signed manifest.
	ErrReplayed = errors.New("manifest: manifest is not newer than the last one seen for this channel (possible replay)")
)

// VerifyManifestSignature checks the manifest's own Ed25519 signature
// against pub. This must pass before ANY field of the manifest (including
// per-component entries) is trusted -- see docs/security.md, "fake
// manifest".
func VerifyManifestSignature(m *Manifest, pub ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(m.ManifestSignature)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidManifestSignature, err)
	}
	if !ed25519.Verify(pub, CanonicalPayload(m), sig) {
		return ErrInvalidManifestSignature
	}
	return nil
}

// SignManifest computes and sets m.ManifestSignature. Used by
// scripts/sign-manifest and tests; the Agent itself never signs, only
// verifies (docs/security.md: signing keys never touch a running node).
func SignManifest(m *Manifest, priv ed25519.PrivateKey) {
	sig := ed25519.Sign(priv, CanonicalPayload(m))
	m.ManifestSignature = base64.StdEncoding.EncodeToString(sig)
}

// SignComponentChecksum signs a component's SHA256 digest (decoded from its
// hex sha256 field) and returns the base64 signature to place in that
// component's Signature field.
func SignComponentChecksum(sha256Hex string, priv ed25519.PrivateKey) (string, error) {
	digest, err := hex.DecodeString(sha256Hex)
	if err != nil {
		return "", fmt.Errorf("decode sha256: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, digest)), nil
}

// VerifyArtifact checks a downloaded artifact at path against a component
// entry: SHA256 checksum, then the component's own Ed25519 signature over
// that checksum. Both must pass. The manifest's own signature
// (VerifyManifestSignature) must already have been checked before this is
// called -- an unsigned/untampered artifact entry inside a forged manifest
// is not meaningful on its own.
func VerifyArtifact(path string, c ComponentUpdate, pub ed25519.PublicKey) error {
	digest, err := sha256File(path)
	if err != nil {
		return err
	}
	digestHex := hex.EncodeToString(digest)
	if digestHex != c.SHA256 {
		return fmt.Errorf("%w: got %s, manifest declares %s", ErrChecksumMismatch, digestHex, c.SHA256)
	}
	sig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil {
		return fmt.Errorf("%w: invalid base64: %v", ErrInvalidArtifactSignature, err)
	}
	if !ed25519.Verify(pub, digest, sig) {
		return ErrInvalidArtifactSignature
	}
	return nil
}

func sha256File(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hash artifact: %w", err)
	}
	return h.Sum(nil), nil
}

// CheckNotDowngrade blocks installing targetVersion when it is not newer
// than currentVersion, unless allowDowngrade is set. Per
// docs/updates.md#downgrade-protection: automatic installs must never pass
// allowDowngrade=true; only an explicit, confirmed manual downgrade request
// may.
func CheckNotDowngrade(currentVersion, targetVersion string, allowDowngrade bool) error {
	if currentVersion == "" {
		return nil // nothing installed yet -- not a downgrade
	}
	if allowDowngrade {
		return nil
	}
	if version.Compare(targetVersion, currentVersion) < 0 {
		return fmt.Errorf("%w: %s -> %s", ErrDowngradeBlocked, currentVersion, targetVersion)
	}
	return nil
}

// CheckCompatibility enforces docs/updates.md#breaking-update-protection:
// an update whose ReleaseNotes declare a requirement the running system
// does not meet is blocked, never auto-installed. Empty requirement fields
// are treated as "no constraint." currentSuiteVersion/currentQRXVersion/
// currentAdapterVersion are the versions actually running right now.
func CheckCompatibility(c ComponentUpdate, currentSuiteVersion, currentQRXVersion, currentAdapterVersion string) error {
	rn := c.ReleaseNotes
	if rn == nil {
		return nil
	}
	if rn.RequiredQRXVersion != "" && currentQRXVersion != "" && !version.Satisfies(currentQRXVersion, rn.RequiredQRXVersion) {
		return fmt.Errorf("%w: requires QRX Core %s, running %s", ErrIncompatible, rn.RequiredQRXVersion, currentQRXVersion)
	}
	if rn.RequiredAdapterVersion != "" && currentAdapterVersion != "" && !version.Satisfies(currentAdapterVersion, rn.RequiredAdapterVersion) {
		return fmt.Errorf("%w: requires adapter %s, running %s", ErrIncompatible, rn.RequiredAdapterVersion, currentAdapterVersion)
	}
	if rn.MinSuiteVersion != "" && currentSuiteVersion != "" && version.Compare(currentSuiteVersion, rn.MinSuiteVersion) < 0 {
		return fmt.Errorf("%w: requires Node Suite >= %s, running %s", ErrIncompatible, rn.MinSuiteVersion, currentSuiteVersion)
	}
	return nil
}

// CheckNotReplayed blocks a manifest that is OLDER than the last one already
// seen for this channel, per docs/security.md "replay attack".
// lastSeenReleasedAt may be "" if none has been recorded yet. A manifest
// with the SAME released_at as the last one seen is allowed -- the same
// current manifest is legitimately reused to install several components in
// one fetch, or refetched on retry; only a manifest strictly older than one
// already acted on is a replay.
func CheckNotReplayed(m *Manifest, lastSeenReleasedAt string) error {
	if lastSeenReleasedAt == "" {
		return nil
	}
	if m.ReleasedAt < lastSeenReleasedAt {
		// RFC3339 timestamps compare correctly as strings when the
		// timezone/format is consistent, which SignManifest/our own
		// released_at generation guarantees (UTC, fixed layout). A
		// manifest from an untrusted source with a nonstandard timestamp
		// format is rejected by Manifest.Validate's RFC3339 parse
		// elsewhere before this check runs.
		return fmt.Errorf("%w: released_at %s < last seen %s", ErrReplayed, m.ReleasedAt, lastSeenReleasedAt)
	}
	return nil
}
