# QRX 0.0.7 interface notes

**Verified source:** `phoenixkonsole/qrx` branch `0.0.7`, commit
`4a732c1a7d2b03fb299eabde437646c99c797e2d`.

The release pipeline builds Core with statically linked OpenSSL 3.6.4. QRX needs
OpenSSL 3.5 or newer for ML-DSA-65; Ubuntu 22.04/24.04 and Debian 12 do not meet
that requirement with their system OpenSSL.

`qrx-cli` accepts separate long options:

```text
qrx-cli --network alpha --datadir /var/lib/qrx --wallet node getnodestatus
```

It connects by HTTP to the loopback RPC port selected by the network profile:
mainnet 37660, alpha 37661, testnet 37662, and regtest 37663. Credentials come
from `QRX_RPC_USER` and `QRX_RPC_PASSWORD`. Successful replies use this envelope:

```json
{"ok":true,"method":"getnodestatus","result":{"network":"alpha","local_height":0}}
```

Confirmed status methods include `getbuildinfo`, `getnodestatus`,
`getblockchaininfo`, `getnetworkinfo`, `getuptime`, `getmempoolinfo`,
`getrecentblocks`, `getrecenttransactions`, `getvalidatorstatus`,
`getblockproducerinfo`, `getfeeinfo`, `getwalletinfo`, `getvelocityinfo`, and
`getnoncelanes <address>`. Recent lists are nested under `result.blocks` and
`result.transactions`; validator state is nested under `result.staking`.

The upstream CLI calls `qrx_ensure_node()` before every RPC request. That writes
chain configuration and can create a wallet, causing a reproducible race with a
freshly starting daemon (`read genesis hash failed`). The Suite release applies
`installer/core/qrx-0.0.7-cli-readonly.patch`: the CLI only connects to RPC and
never initializes local state. Ten consecutive clean daemon starts passed after
this patch; the unpatched build failed intermittently during concurrent startup.

The upstream daemon accepts `--rpc-user` and `--rpc-password` but, unlike its
CLI, does not read the documented `QRX_RPC_USER` and `QRX_RPC_PASSWORD`
environment variables. Passing secrets as service command-line arguments exposes
them through process metadata. The Suite patch adds the missing daemon-side
environment lookup so systemd can load both credentials from a root-only file.

The upstream CLI also exits successfully for structured RPC failures because it
discards the HTTP status and treats every completed TCP exchange as success. The
Suite patch returns a nonzero exit code when the response contains
`"ok":false`, so bad credentials and RPC errors cannot pass health checks.

The public network described by the Core README is `alpha` on P2P port 26661.
The `mainnet` profile is named “QRX Mainnet Preview” and contains no seed nodes.
Only `seed1.qrxchain.org` currently resolves; reachability and a nonzero peer count
must be checked separately from process health.
