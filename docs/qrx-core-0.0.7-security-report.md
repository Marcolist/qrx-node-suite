# QRX Core 0.0.7 integration and security report

This report is written as a repair brief for Claude Code or a QRX Core
maintainer. It covers commit
`4a732c1a7d2b03fb299eabde437646c99c797e2d` from branch `0.0.7`, tested on
Linux amd64 with OpenSSL 3.6.4. The Node Suite carries one narrow compatibility patch so it
can ship safely now; the underlying fixes should be made and tested upstream.

## P0: CLI changes persistent state and races the daemon

`qrx-core/src/qrx_cli.c` calls `qrx_ensure_node()` before every RPC request.
An RPC client therefore creates or rewrites chain configuration, genesis and
wallet state. Starting `qrx-cli` beside `qrxd` on an empty data directory
intermittently failed with `read genesis hash failed` (reproduced in one of five
fresh starts). An offline monitoring request also created a data directory.

Fix: make `qrx-cli` a read-only RPC client. It must calculate the RPC endpoint
without calling node or wallet initialization. Add tests that run the daemon and
CLI concurrently against at least 100 fresh data directories, and assert that an
offline CLI request creates no files. The suite currently applies
`installer/core/qrx-0.0.7-cli-readonly.patch`; ten consecutive fresh-start runs
passed after that patch.

## P0: daemon cannot consume RPC credentials without argv leakage

`qrx-cli` reads `QRX_RPC_USER` and `QRX_RPC_PASSWORD`, while `qrxd` only accepts
the corresponding command-line switches. A service manager must therefore put
the password in process arguments, which can expose it through process metadata.

Fix: have `qrxd` read the same environment variables when command-line values
are absent, preserving explicit argument precedence. Add tests for no
credentials, wrong credentials and correct environment credentials. The suite
applies this behavior in the same patch. Live verification showed unauthenticated
and wrongly authenticated RPC requests rejected, with the correct credentials
accepted.

## P1: CLI returns success for RPC errors

`qrx-cli` strips the HTTP headers and exits zero whenever the TCP exchange
succeeds. HTTP 401 and JSON `{"ok":false,...}` responses therefore look like
successful commands to scripts and health checks.

Fix: exit nonzero for non-2xx HTTP responses and for structured RPC errors,
write the error body to stderr, and preserve distinct exit codes where useful.
Add authentication and invalid-method CLI tests. Node Suite currently treats an
`"ok":false` response as a failed CLI invocation in its bundled patch.

## P1: `connections` does not count connections

The reported connection count is derived from configured lines in peer files,
not established inbound or outbound sessions. A node reported four connections
while its configured public seed was unreachable. This makes node health and
sync status unsafe to interpret.

Fix: track live session state and return separate `connected`, `inbound` and
`outbound` counts. Keep configured/discovered address counts under differently
named fields. Until Core exposes real sessions, Node Suite marks peer count as
unavailable.

## P1: zero-height isolated node reports fully synchronized

At height zero with no confirmed live peer, Core reports 100 percent sync and
`initial_block_download=false`. Monitoring cannot distinguish an isolated fresh
node from a synchronized chain.

Fix: define sync against a trusted observed tip and active peer state. Return an
explicit reason such as `no_live_peers` or `tip_unknown` when no comparison is
possible. Add a test for a fresh isolated node.

## P1: network bootstrap data is inconsistent

The alpha profile contains loopback pseudo-seeds. Documentation/configuration
refers to `seed1.qrxchain.org`, `seed2.qrxchain.org` and
`seed3.qrxchain.org`; during testing only seed1 resolved, and its alpha P2P port
was unreachable. The mainnet profile identifies itself as a preview and has no
seeds.

Fix: publish an authoritative bootstrap list per network, add DNS and TCP health
checks in CI/operations, and keep unreachable names out of shipped defaults.
Expose bootstrap attempts and their current connection state through RPC.

## P1: listen configuration persists surprising first-run values

Node initialization writes `node.conf`; later `--listen` values do not reliably
replace the persisted `host` and `external_host`. A first initialization on
loopback can leave a VPS permanently listening only on loopback despite a later
public `--listen` argument.

Fix: document precedence and make explicit CLI options override persisted
values, or fail on conflicts. Add a restart test that changes the bind address.
Node Suite currently initializes with the requested public P2P bind and safely
updates the Core-owned config as the Core service user.

## P2: protocol version definitions disagree

Compilation warns that `QRX_PROTOCOL_VERSION` is redefined: the public header
defines 2 while `qrx.c` defines 6. Runtime RPC reports protocol version 6.

Fix: keep one authoritative definition, remove the duplicate, and add a compile
assertion or test connecting the serialized handshake version to the RPC value.

## P2: recovery phrase is printed to standard output

First wallet creation emits `recovery_phrase=...` to daemon output. Under a
normal service this becomes a durable journal secret.

Fix: provide an explicit root-owned output file descriptor/path or an
interactive one-time retrieval command; never log recovery material. Node Suite
captures first initialization privately and refuses persistent service startup
until the wallet already exists.

## P2: response shapes and versioning are unstable for operators

RPC payloads wrap data under `result`; recent blocks and transactions add
another named collection; validator data adds `staking`; nonce lanes are encoded
as strings such as `lane=0 nonce=0`. The build identifies as
`0.0.7-velocity-phase4` although compatibility is published as 0.0.7.

Fix: publish a versioned JSON schema, return typed nonce lane objects, and expose
both semantic compatibility version and build identifier as separate fields.
Add schema fixtures to Core CI.

## P2: compiler warnings indicate unchecked path handling

The reviewed release build produced 193 compiler warnings, including truncation
warnings around `snprintf` and misleading-indentation warnings. These do not by themselves prove exploitation,
but they occur in path and configuration handling that processes operator or
network-derived values.

Fix: enable `-Wall -Wextra -Werror` in CI, replace silent truncation with checked
length failures, add long-path/boundary tests, and resolve every existing
warning. Fuzz configuration and RPC parsing with ASan/UBSan builds.

## Acceptance checks

1. `qrx-cli` against an absent daemon writes no files.
2. 100 concurrent fresh daemon/CLI starts produce no corrupted genesis state.
3. RPC authentication works from environment variables and secrets are absent
   from `/proc/<pid>/cmdline`.
4. An unreachable configured seed yields zero live connections and a clearly
   indeterminate sync state.
5. Explicit bind options take effect after restart even when `node.conf` exists.
6. The build is warning-free under `-Wall -Wextra -Werror` and passes ASan/UBSan
   tests for configuration, RPC and peer message parsers.
