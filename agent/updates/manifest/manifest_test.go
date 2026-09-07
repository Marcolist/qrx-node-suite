package manifest_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qrx-node-suite/agent/updates/manifest"
)

func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return pub, priv
}

func writeTempArtifact(t *testing.T, content string) (path string, sha256Hex string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	sum := sha256Sum(t, path)
	return path, sum
}

func sha256Sum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	h := sha256Hex(b)
	return h
}

func buildSignedManifest(t *testing.T, priv ed25519.PrivateKey, component string, artifactPath, artifactSHA256 string) *manifest.Manifest {
	t.Helper()
	sig, err := manifest.SignComponentChecksum(artifactSHA256, priv)
	if err != nil {
		t.Fatalf("SignComponentChecksum: %v", err)
	}
	m := &manifest.Manifest{
		ManifestVersion: 1,
		Channel:         "stable",
		SuiteVersion:    "0.2.0",
		ReleasedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Components: map[string]manifest.ComponentUpdate{
			component: {
				Version:   "0.2.0",
				URL:       "file://" + artifactPath,
				SHA256:    artifactSHA256,
				Signature: sig,
			},
		},
	}
	manifest.SignManifest(m, priv)
	return m
}

func TestManifestSignatureRoundTrip(t *testing.T) {
	pub, priv := testKeypair(t)
	path, sum := writeTempArtifact(t, "agent-binary-v0.2.0")
	m := buildSignedManifest(t, priv, "agent", path, sum)

	if err := manifest.VerifyManifestSignature(m, pub); err != nil {
		t.Fatalf("VerifyManifestSignature: %v", err)
	}
}

func TestTamperedManifestFieldFailsVerification(t *testing.T) {
	pub, priv := testKeypair(t)
	path, sum := writeTempArtifact(t, "agent-binary-v0.2.0")
	m := buildSignedManifest(t, priv, "agent", path, sum)

	// Tamper with a field the signature covers -- suite_version.
	m.SuiteVersion = "9.9.9"
	if err := manifest.VerifyManifestSignature(m, pub); err == nil {
		t.Fatal("expected VerifyManifestSignature to fail after tampering, got nil")
	}
}

func TestTamperedComponentFailsVerification(t *testing.T) {
	pub, priv := testKeypair(t)
	path, sum := writeTempArtifact(t, "agent-binary-v0.2.0")
	m := buildSignedManifest(t, priv, "agent", path, sum)

	c := m.Components["agent"]
	c.Version = "99.0.0" // splice in a different version without re-signing
	m.Components["agent"] = c

	if err := manifest.VerifyManifestSignature(m, pub); err == nil {
		t.Fatal("expected VerifyManifestSignature to fail after component tampering, got nil")
	}
}

func TestVerifyArtifactChecksumMismatch(t *testing.T) {
	pub, priv := testKeypair(t)
	path, sum := writeTempArtifact(t, "original content")
	c := manifest.ComponentUpdate{Version: "1.0.0", URL: "file://" + path, SHA256: sum}
	sig, _ := manifest.SignComponentChecksum(sum, priv)
	c.Signature = sig

	// Corrupt the downloaded artifact after the manifest was signed.
	if err := os.WriteFile(path, []byte("corrupted content"), 0o644); err != nil {
		t.Fatalf("corrupt artifact: %v", err)
	}

	err := manifest.VerifyArtifact(path, c, pub)
	if err == nil {
		t.Fatal("expected checksum mismatch, got nil")
	}
}

func TestVerifyArtifactWrongSigningKey(t *testing.T) {
	_, priv := testKeypair(t)
	otherPub, _ := testKeypair(t) // a different keypair -- simulates a compromised/wrong signer
	path, sum := writeTempArtifact(t, "content")
	sig, _ := manifest.SignComponentChecksum(sum, priv)
	c := manifest.ComponentUpdate{Version: "1.0.0", URL: "file://" + path, SHA256: sum, Signature: sig}

	if err := manifest.VerifyArtifact(path, c, otherPub); err == nil {
		t.Fatal("expected signature verification to fail against the wrong public key")
	}
}

func TestCheckNotDowngradeBlocksByDefault(t *testing.T) {
	if err := manifest.CheckNotDowngrade("0.2.0", "0.1.0", false); err == nil {
		t.Fatal("expected downgrade to be blocked by default")
	}
	if err := manifest.CheckNotDowngrade("0.2.0", "0.1.0", true); err != nil {
		t.Fatalf("expected explicit allowDowngrade to permit it, got %v", err)
	}
	if err := manifest.CheckNotDowngrade("0.2.0", "0.3.0", false); err != nil {
		t.Fatalf("upgrade should never be blocked as a downgrade: %v", err)
	}
	if err := manifest.CheckNotDowngrade("", "0.1.0", false); err != nil {
		t.Fatalf("no current version installed should never be treated as a downgrade: %v", err)
	}
}

func TestCheckCompatibilityBlocksUnmetQRXRequirement(t *testing.T) {
	c := manifest.ComponentUpdate{
		Version: "2.0.0",
		ReleaseNotes: &manifest.ReleaseNotes{
			RequiredQRXVersion: ">=0.0.8",
		},
	}
	err := manifest.CheckCompatibility(c, "0.1.0", "0.0.7", "1.0.0")
	if err == nil {
		t.Fatal("expected update requiring QRX Core >=0.0.8 to be blocked when running 0.0.7")
	}

	okErr := manifest.CheckCompatibility(c, "0.1.0", "0.0.8", "1.0.0")
	if okErr != nil {
		t.Errorf("expected no error when QRX Core 0.0.8 satisfies the requirement, got %v", okErr)
	}
}

func TestCheckNotReplayedRejectsStaleManifest(t *testing.T) {
	pub, priv := testKeypair(t)
	path, sum := writeTempArtifact(t, "content")
	m := buildSignedManifest(t, priv, "agent", path, sum)
	_ = pub

	// Same released_at as last-seen must be ALLOWED: reusing one manifest
	// fetch to install several components must not trip replay protection.
	if err := manifest.CheckNotReplayed(m, m.ReleasedAt); err != nil {
		t.Errorf("expected a manifest with the same released_at as last-seen to be allowed (reused for a second component), got %v", err)
	}

	older := &manifest.Manifest{ReleasedAt: "2025-01-01T00:00:00Z"}
	if err := manifest.CheckNotReplayed(older, m.ReleasedAt); err == nil {
		t.Fatal("expected a manifest older than the last one seen to be rejected as replayed")
	}

	newer := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	m.ReleasedAt = newer
	if err := manifest.CheckNotReplayed(m, "2026-01-01T00:00:00Z"); err != nil {
		t.Errorf("expected a genuinely newer manifest to pass, got %v", err)
	}
}

func TestUnsignedManifestFailsValidation(t *testing.T) {
	m := &manifest.Manifest{
		ManifestVersion: 1,
		Channel:         "stable",
		SuiteVersion:    "0.1.0",
		ReleasedAt:      time.Now().UTC().Format(time.RFC3339),
		Components: map[string]manifest.ComponentUpdate{
			"agent": {Version: "0.1.0", URL: "https://example.invalid/a", SHA256: "ab", Signature: "cd"},
		},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected Validate to reject a manifest with no manifest_signature")
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
