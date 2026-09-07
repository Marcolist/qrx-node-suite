# Security Policy

QRX Node Suite manages infrastructure that may sit next to validator keys and node
operators' funds. Treat any report responsibly.

## Reporting a vulnerability

Do not open a public GitHub issue for a suspected vulnerability. Instead, email the
maintainers privately (see the repository's GitHub profile for a current contact) with:

- A description of the issue and its impact
- Steps to reproduce, or a proof of concept
- The affected component(s) and version(s)

We aim to acknowledge reports within 5 business days.

## Scope

In scope:

- The QRX Agent (`agent/`), including the REST API, adapter loading, OTA update pipeline,
  and admin authentication/authorization.
- The Dashboard (`dashboard/`) as served by the Agent.
- Installer scripts (`installer/`).
- The update manifest / signing scheme (`contracts/json-schema/update-manifest.schema.json`,
  `agent/updates/`).

Out of scope:

- QRX Core itself (report upstream to the QRX Core project).
- Vulnerabilities requiring an attacker to already have root on the host, unless they
  defeat a specific privilege boundary this project claims to enforce (see
  [`docs/security.md`](docs/security.md)).

## Hard guarantees this project makes

These are enforced in code and reviewed on every change touching them:

1. QRX Node Suite never stores wallet seeds, private keys, validator signing keys, or
   wallet passphrases, and never logs or transmits them.
2. Remote control and mobile pairing default to **disabled**; mobile access defaults to
   **read-only** when enabled.
3. Public telemetry is **opt-in** and disabled by default.
4. Update manifests are rejected unless their checksum and signature verify against a
   pinned public key. Unsigned or unverifiable manifests are never installed.
5. Automatic downgrades are never performed. Manual downgrades require explicit
   confirmation.
6. Administrative API endpoints (updates, version switching, service control) require
   authentication and are audit-logged; they are unreachable anonymously over the LAN.

See [`docs/security.md`](docs/security.md) for the full OTA threat model (malicious update
server, compromised release account, modified download, downgrade attack, replay attack,
fake manifest, unsigned adapter, malicious dashboard asset, rollback abuse) and how each is
mitigated.
