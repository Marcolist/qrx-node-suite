# QRX 0.0.7 interface notes

**Status: UNVERIFIED.** This document exists to make the boundary between
"confirmed" and "assumed" explicit, per docs/architecture.md principle 11 and
CONTRIBUTING.md. Nothing in this file should be read as a specification of
QRX 0.0.7's actual behavior.

## Why this is unverified

`agent/adapters/qrx007` was written in an environment with:

- No copy of the QRX Core source tree.
- No running `qrxd`/`qrx-cli` to execute commands against and observe real
  output.
- No outbound network access to fetch either of the above (this sandbox's
  egress policy blocks arbitrary hosts; see the repository's own network
  notes if you're reading this from a similar environment).

The only input available was the command name list from the product
specification this repository was built against (reproduced below and in
`agent/data/compatibility-profiles/qrx-0.0.7.json`). Field names, response
envelopes, error formats, and transport details are **not confirmed**.

## What is claimed to be true (the command list)

QRX 0.0.7 is documented to expose these commands, presumably via `qrx-cli`
forwarding to `qrxd` (transport not confirmed — see below):

```
getnodestatus          getvalidatorstatus       getnewaddress
getblockchaininfo       getblockproducerinfo      listaddresses
getnetworkinfo          getfeeinfo                 getaddressnonce
getuptime               getwalletinfo               sendrawtransaction
getbuildinfo            getnoncelanes               getvelocityinfo
getmempoolinfo           getrecentblocks             createvelocitytransaction
                          getrecenttransactions
```

## What is NOT confirmed

- **Exact JSON field names** for any command's response. `agent/qrx/fields.go`
  and `agent/adapters/qrx007` deliberately try multiple plausible key names
  (e.g. `"height"`, `"blocks"`, `"block_height"`) and fall back to
  `models.Unavailable` rather than assume one is correct.
- **Whether responses are JSON at all**, and if so, whether they're a bare
  object, wrapped in a `result`/`data` envelope, or JSON-RPC shaped
  (`{"result": ..., "error": ...}`).
- **Error format**: what `qrx-cli` prints on a bad command, a node that's
  still syncing, a missing wallet, etc. `agent/qrx.CommandError.UnknownCommand()`
  is a heuristic guess at recognizing "command doesn't exist," not a
  confirmed match against real stderr text.
- **Transport**: whether `qrx-cli` talks to `qrxd` over a local RPC/HTTP
  port, a Unix domain socket, or forwards to some other local control
  interface. `agent/qrx.Runner` currently only shells out to a `qrx-cli`
  binary (`docs/architecture.md`'s "QRX command execution" section);
  direct-RPC support is not implemented.
- **Network/wallet selection flags**: `-network=`, `-datadir=`, `-wallet=` in
  `agent/qrx/config.go` are a guess at conventional Bitcoin-derivative CLI
  flag naming, not confirmed against QRX 0.0.7's actual flag names.
- **Validator/VELOCITY response shapes**: `getvalidatorstatus`,
  `getblockproducerinfo`, `getvelocityinfo`, `getnoncelanes` responses are
  entirely unverified; the fields `agent/models` defines for them
  (`ValidatorStatus`, `BlockProducerStatus`, `VelocityStatus`, `NonceLanes`)
  are a reasonable guess at what such an engine would report, not a
  transcription of real output.

## What this means for the codebase

`agent/adapters/qrx007` is written defensively specifically because of the
above:

1. Every field extraction goes through `agent/qrx.Str`/`Int64`/`Int`/`Bool`,
   which try several plausible key names and degrade to
   `models.Unavailable` (never a zero value, never a panic) when none match.
2. Optional command groups (`getvalidatorstatus`, `getblockproducerinfo`,
   `getvelocityinfo`, `getnoncelanes`) are runtime-capability-probed
   (`Adapter.Capabilities`) rather than assumed present just because the
   detected QRX Core version is 0.0.7 — see
   `docs/architecture.md#feature-capability-detection`.
3. `agent/data/compatibility-profiles/qrx-0.0.7.json`'s `platform_notes`
   field and `core_version_switch_safety` block are explicitly left as
   "unverified" / unknown, which makes `version.SwitchSafety.AllKnown()`
   false and therefore blocks any automatic QRX Core version switch onto
   0.0.7 until a human confirms it (`docs/updates.md#core-version-switching-safety`).

## What must happen before this is trustworthy

Before running `agent/adapters/qrx007` against a real validator:

1. Get access to QRX 0.0.7 source (or a running `qrxd`) and run each command
   above, capturing real request/response pairs.
2. Update this document with confirmed field names, error shapes, and
   transport details, citing the source location or command transcript.
3. Tighten `agent/adapters/qrx007`'s field extraction to the confirmed key
   names (keep the multi-key fallback only where the field name has
   genuinely changed between point releases).
4. Fill in `agent/data/compatibility-profiles/qrx-0.0.7.json`'s
   `core_version_switch_safety` block with real compatible/incompatible
   judgements once tested, so automatic switching can be enabled for that
   specific transition.
5. Remove this "UNVERIFIED" banner and replace it with confirmed interface
   documentation.
