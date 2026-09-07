// Command sign-checksums signs a release SHA256SUMS file (or any file) with
// an Ed25519 private key from gen-signing-key, producing a base64 detached
// signature alongside it. The release pipeline (.github/workflows/release.yml)
// runs this over the SHA256SUMS it generates for a tagged release, and the
// root install.sh verifies that signature against the same trusted public
// key baked into the Agent's OTA verification (docs/updates.md#update-manifest)
// before trusting any checksum in the file -- see docs/installer.md#release-security.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	keyPath := flag.String("key", "", "path to the base64 private key file (from gen-signing-key -priv)")
	inPath := flag.String("in", "", "file to sign (e.g. SHA256SUMS)")
	outPath := flag.String("out", "", "output path for the base64 detached signature (default: -in + .sig)")
	flag.Parse()

	if *keyPath == "" || *inPath == "" {
		fmt.Fprintln(os.Stderr, "usage: sign-checksums -key priv.key -in SHA256SUMS [-out SHA256SUMS.sig]")
		os.Exit(2)
	}
	if *outPath == "" {
		*outPath = *inPath + ".sig"
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
		fail("read %s: %v", *inPath, err)
	}

	sig := ed25519.Sign(priv, data)
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	if err := os.WriteFile(*outPath, []byte(sigB64+"\n"), 0o644); err != nil {
		fail("write signature: %v", err)
	}
	fmt.Println("signature written to", *outPath)
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
