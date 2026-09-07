# Configuration

The Agent reads a single JSON config file (`-config path` or
`QRX_AGENT_CONFIG=path`); every field within that file is optional and
falls back to `config.Default()` (Mock mode). Not passing `-config`/
`QRX_AGENT_CONFIG` at all is the same thing, deliberately, for local Mock-
mode development -- but passing a *path* that doesn't exist or isn't
readable is a hard startup failure (`cmd/agentd/main.go`'s
`requireConfigPathExists`), not a silent fallback to the same defaults: a
real, intended config was named and couldn't be read, which is not the
same situation as none being requested at all. See `agent/config/config.go`
for the authoritative field list. Configuration is itself versioned
(`config_schema_version`) and migrated on load -- see
`docs/updates.md#configuration-and-database-migrations`.

## Key sections

```json
{
  "listen_addr": "127.0.0.1:8787",
  "data_dir": "./var",
  "dashboard_dir": "./dashboard/dist",
  "adapter": {
    "name": "qrx007",
    "manual_override": false,
    "cli_path": "/usr/local/bin/qrx-cli",
    "network": "mainnet",
    "assumed_qrx_core_version": "0.0.7"
  },
  "admin_token": "",
  "telegram": { "enabled": false, "token": "", "chat_id": "" },
  "updates": {
    "public_key_base64": "",
    "source_kind": "github",
    "github_owner": "your-org",
    "github_repo": "qrx-node-suite",
    "retain_versions": 2,
    "max_boot_attempts": 3
  },
  "poll": { "node_status_seconds": 5, "network_seconds": 10, "system_seconds": 5, "version_hours": 6 },
  "is_validator_node": false,
  "log_format": "text",
  "log_level": "info"
}
```

- `adapter.name` empty means automatic selection
  (`docs/qrx-compatibility.md`); set it to force a specific adapter
  (`manual_override: true` to bypass a matrix-disabled one, with the
  warnings that implies).
- `admin_token` empty **disables** every administrative endpoint (403, not
  open) -- see `docs/security.md`.
- `updates.source_kind` is one of `github`/`static`/`local`/`development`
  (`agent/updates/sources`); `development` (the default) uses no network at
  all, useful for local testing.
- `is_validator_node: true` hard-disables automatic QRX Core updates
  regardless of any channel setting -- see
  `docs/updates.md#qrx-core-updates`.
- `updates.max_boot_attempts` bounds how many consecutive failed boots a
  self-binary update (`agent`, `adapter_*`) gets before `updates.BootGuard`
  forces an automatic rollback -- see
  `docs/updates.md#self-binary-components-agent-adapters`.

## Polling

`poll.node_status_seconds` etc. control `cmd/agentd`'s single centralized
polling loop (`Poller` in `agent/cmd/agentd/poller.go`) -- API handlers
never poll the adapter themselves, they read `api.Cache`
(docs/architecture.md's "Data flow" section). Recommended defaults per the
original design brief: node/blockchain 5s, network/validator/velocity/mempool
10s, system 5s, recent activity 15s, version checks every 6 hours. The
current implementation uses one shared interval
(`poll.node_status_seconds`) for the whole tick rather than per-domain
intervals -- splitting these out is a reasonable follow-up once real-world
polling cost data exists.

## Retention

SQLite tables (`metrics`, `node_snapshots`, etc. -- see
`agent/storage/migrations/0001_init.sql`) are not yet pruned automatically;
implementing the suggested retention policy (48h raw / 30d 5-minute
aggregates / 365d hourly aggregates) is tracked as follow-up work, not yet
implemented in this codebase.
