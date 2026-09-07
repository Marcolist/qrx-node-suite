// Command sign-manifest signs an update manifest and every component entry
// in it (docs/updates.md#update-manifest), given a private key from
// gen-signing-key. Input is a JSON manifest with manifest_version, channel,
// suite_version, released_at, and components already filled in (version,
// url, sha256 per component) but signature/manifest_signature blank --
// this command fills those in and writes the signed manifest.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"qrx-node-suite/agent/updates/manifest"
)

func main() {
	keyPath := flag.String("key", "", "path to the base64 private key file (from gen-signing-key -priv)")
	inPath := flag.String("manifest", "", "path to the unsigned manifest JSON")
	outPath := flag.String("out", "", "output path (default: overwrite -manifest)")
	flag.Parse()

	if *keyPath == "" || *inPath == "" {
		fmt.Fprintln(os.Stderr, "usage: sign-manifest -key priv.key -manifest manifest.json [-out signed.json]")
		os.Exit(2)
	}
	if *outPath == "" {
		*outPath = *inPath
	}

	keyData, err := os.ReadFile(*keyPath)
	if err != nil {
		fail("read key: %v", err)
	}
	priv, err := decodePrivateKey(keyData)
	if err != nil {
		fail("decode key: %v", err)
	}

	data, err := os.ReadFile(*inPath)
	if err != nil {
		fail("read manifest: %v", err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		fail("parse manifest: %v", err)
	}

	for name, c := range m.Components {
		if c.SHA256 == "" {
			fail("component %q has no sha256 -- compute it first (e.g. sha256sum the artifact)", name)
		}
		sig, err := manifest.SignComponentChecksum(c.SHA256, priv)
		if err != nil {
			fail("sign component %q: %v", name, err)
		}
		c.Signature = sig
		m.Components[name] = c
	}
	manifest.SignManifest(&m, priv)

	out, err := json.MarshalIndent(&m, "", "  ")
	if err != nil {
		fail("marshal signed manifest: %v", err)
	}
	if err := os.WriteFile(*outPath, out, 0o644); err != nil {
		fail("write signed manifest: %v", err)
	}
	fmt.Println("signed manifest written to", *outPath)
}

func decodePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	trimmed := trimTrailingNewline(data)
	raw, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}

func trimTrailingNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
