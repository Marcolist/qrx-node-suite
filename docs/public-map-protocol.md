# Public node map protocol (future)

**Status: not implemented in this codebase yet.** Reserved for a future
opt-in public map of QRX nodes/validators (analogous to public block
explorers' node maps), built on top of `docs/telemetry.md`'s opt-in
telemetry client once that exists -- a node only appears on a public map if
its operator has explicitly opted into both telemetry and map listing,
separately (opting into telemetry must not silently opt a node into public
listing).

Nothing in this repository implements or assumes this protocol yet; it is
listed here only so the doc tree matches the architecture this project is
building toward (`docs/architecture.md`).
