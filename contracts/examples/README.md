# Contract examples

- `component-version-model.json` -- an example `agent/version.ComponentVersionModel`.
- `update-manifest.json` -- a genuinely, verifiably signed example manifest
  (docs/updates.md#update-manifest), generated and signed with this
  project's own tools (`agent/cmd/gen-signing-key`, `agent/cmd/sign-manifest`)
  against `example-signing-public-key.txt`'s keypair. This is a
  **throwaway example key, generated solely for this file** -- never use it
  for anything real; its private half is not in this repository at all.

Verify it yourself:

```sh
cd agent
cat > /tmp/verify.go << 'EOF'
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"

	"qrx-node-suite/agent/updates/manifest"
)

func main() {
	pubB64, _ := os.ReadFile("../contracts/examples/example-signing-public-key.txt")
	pub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pubB64)))
	f, _ := os.Open("../contracts/examples/update-manifest.json")
	m, err := manifest.Parse(f)
	if err != nil { panic(err) }
	if err := manifest.VerifyManifestSignature(m, ed25519.PublicKey(pub)); err != nil {
		panic(err)
	}
	println("OK")
}
EOF
go run /tmp/verify.go
```

- `compatibility-matrix.json` -- see `agent/data/compatibility-matrix.json`,
  the real one this project ships and tests against; not duplicated here.
