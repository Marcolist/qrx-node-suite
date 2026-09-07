// Command gen-signing-key generates an Ed25519 keypair for signing update
// manifests (docs/updates.md#update-manifest). The private key must be
// kept off any running Agent -- see docs/security.md: signing happens on a
// release machine, verification happens on the Agent, and those are never
// the same key material flow.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	privPath := flag.String("priv", "signing-key.priv", "output path for the private key (base64, keep secret)")
	pubPath := flag.String("pub", "signing-key.pub", "output path for the public key (base64, goes in the Agent's config)")
	flag.Parse()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate key:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*privPath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write private key:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*pubPath, []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write public key:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (keep secret, offline) and %s (set as updates.public_key_base64 in Agent config)\n", *privPath, *pubPath)
}
