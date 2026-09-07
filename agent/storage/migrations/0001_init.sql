-- Initial schema. Applied atomically by agent/storage.Migrate inside its own
-- transaction -- do NOT put BEGIN/COMMIT in this file.
--
-- Tables follow docs/architecture.md's "Local database" section. This file
-- is append-only once released (see CONTRIBUTING.md); a schema change is a
-- new numbered migration file, never an edit to this one.

CREATE TABLE update_history (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    component        TEXT    NOT NULL,
    from_version     TEXT,
    to_version       TEXT    NOT NULL,
    channel          TEXT    NOT NULL,
    timestamp        TEXT    NOT NULL,
    status           TEXT    NOT NULL,
    rollback_used    INTEGER NOT NULL DEFAULT 0,
    checksum         TEXT,
    manifest_version INTEGER,
    error_message    TEXT
);
CREATE INDEX idx_update_history_component ON update_history(component, timestamp);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp  TEXT NOT NULL,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    component  TEXT,
    details    TEXT,
    ip_address TEXT
);
CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp);

-- Generic key/value settings: update channels per component, version pins,
-- update lock, scheduled update window. See agent/updates/settings.go.
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE metrics (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    metric    TEXT    NOT NULL,
    value     REAL    NOT NULL,
    timestamp TEXT    NOT NULL
);
CREATE INDEX idx_metrics_metric_time ON metrics(metric, timestamp);

CREATE TABLE node_snapshots (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp        TEXT    NOT NULL,
    height           INTEGER,
    finalized_height INTEGER,
    peer_count       INTEGER,
    mempool_tx_count INTEGER,
    online           INTEGER NOT NULL
);
CREATE INDEX idx_node_snapshots_time ON node_snapshots(timestamp);

CREATE TABLE network_snapshots (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp  TEXT NOT NULL,
    peer_count INTEGER,
    network    TEXT
);
CREATE INDEX idx_network_snapshots_time ON network_snapshots(timestamp);

CREATE TABLE validator_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL,
    active          INTEGER,
    blocks_produced INTEGER,
    missed_blocks   INTEGER
);
CREATE INDEX idx_validator_snapshots_time ON validator_snapshots(timestamp);

CREATE TABLE velocity_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL,
    parallel_width  INTEGER,
    execution_waves INTEGER,
    conflicts       INTEGER
);
CREATE INDEX idx_velocity_snapshots_time ON velocity_snapshots(timestamp);

CREATE TABLE alerts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id     TEXT    NOT NULL,
    severity    TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    created_at  TEXT    NOT NULL,
    resolved_at TEXT,
    resolved    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_alerts_resolved ON alerts(resolved, created_at);

CREATE TABLE events (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    type      TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    data      TEXT
);
CREATE INDEX idx_events_time ON events(timestamp);

CREATE TABLE service_events (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    service   TEXT NOT NULL,
    event     TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    detail    TEXT
);
CREATE INDEX idx_service_events_time ON service_events(timestamp);

CREATE TABLE restart_history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    service      TEXT NOT NULL,
    timestamp    TEXT NOT NULL,
    reason       TEXT,
    triggered_by TEXT NOT NULL -- 'guardian' | 'manual' | 'update'
);
CREATE INDEX idx_restart_history_time ON restart_history(timestamp);

CREATE TABLE paired_devices (
    id           TEXT    PRIMARY KEY,
    name         TEXT,
    paired_at    TEXT    NOT NULL,
    last_seen_at TEXT,
    read_only    INTEGER NOT NULL DEFAULT 1,
    revoked      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE telemetry_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
