// Package config loads and versions QRX Node Suite's own configuration
// (docs/updates.md#configuration-migrations, docs/configuration.md).
// config_schema_version is tracked independently of every other version
// domain (agent/version.ComponentVersionModel).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// CurrentSchemaVersion is the config schema this build of the Agent
// expects. Migrate advances an on-disk config from any older version to
// this one; see migrate.go.
const CurrentSchemaVersion = 1

// Config is the Agent's own configuration. Every field has a safe default
// so the Agent can start in Mock mode with zero configuration for
// development (docs/development.md).
type Config struct {
	SchemaVersion int `json:"config_schema_version"`

	ListenAddr   string `json:"listen_addr"`   // default 127.0.0.1:8787
	DataDir      string `json:"data_dir"`      // holds the SQLite DB + OTA component stores
	DashboardDir string `json:"dashboard_dir"` // built dashboard static assets (npm run build's output); see docs/development.md

	Adapter AdapterConfig `json:"adapter"`

	// AdminToken gates every administrative endpoint (docs/updates.md#admin-authorization).
	// Empty means administrative endpoints are entirely disabled, not
	// "open" -- see docs/security.md.
	AdminToken string `json:"admin_token"`

	Telegram TelegramConfig `json:"telegram"`

	Updates UpdatesConfig `json:"updates"`

	Poll PollConfig `json:"poll"`

	// IsValidatorNode gates automatic QRX Core updates off unconditionally
	// (docs/updates.md#qrx-core-updates) and is surfaced in the dashboard.
	IsValidatorNode bool `json:"is_validator_node"`

	// QRXCoreServiceUseSudo makes agent/platform.Systemd prepend "sudo" to
	// every qrxd.service systemctl call, for the shipped install (an
	// unprivileged qrx-agent user with the narrowly-scoped sudoers rule
	// install.sh installs -- installer/linux/qrx-agent-sudoers -- limited
	// to exactly "systemctl {start,stop,restart,is-active} qrxd.service",
	// nothing else). Defaults to false: local/dev runs (docs/development.md,
	// `go run ./cmd/agentd`) have neither that sudoers rule nor necessarily
	// a passwordless sudo session, so defaulting this on would make Core
	// service control hang on a password prompt or fail outright instead
	// of just calling systemctl directly as whatever user is already
	// running the process. F12 fix, external security audit: this field
	// existed as agent/platform.Systemd.UseSudo already, but nothing ever
	// set it, so Core service control could not work at all against a real
	// qrxd.service owned by a different user -- the Agent was never meant
	// to run as root just to manage it.
	QRXCoreServiceUseSudo bool `json:"qrx_core_service_use_sudo"`

	LogFormat string `json:"log_format"` // "json" or "text"
	LogLevel  string `json:"log_level"`  // debug/info/warn/error
}

type AdapterConfig struct {
	// Name selects which adapter to activate manually. Empty means
	// automatic selection (agent/adapters.Registry.SelectAutomatic).
	Name           string `json:"name"`
	ManualOverride bool   `json:"manual_override"`

	CLIPath    string `json:"cli_path"`
	DataDir    string `json:"data_dir"`
	Network    string `json:"network"`
	WalletName string `json:"wallet_name"`

	// AssumedQRXCoreVersion is used ONLY when live detection (calling
	// qrx-cli getbuildinfo) fails -- e.g. no real qrxd is reachable. It is
	// never treated as confirmed; cmd/agentd logs a warning whenever it
	// falls back to this value. Defaults to "0.0.7" for Mock-mode
	// development.
	AssumedQRXCoreVersion string `json:"assumed_qrx_core_version"`
}

type TelegramConfig struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	ChatID  string `json:"chat_id"`
}

type UpdatesConfig struct {
	// PublicKeyBase64 verifies update manifests -- see agent/updates/manifest.
	PublicKeyBase64           string `json:"public_key_base64"`
	SourceKind                string `json:"source_kind"` // "github" | "static" | "local" | "development"
	GitHubOwner               string `json:"github_owner"`
	GitHubRepo                string `json:"github_repo"`
	StaticURLTemplate         string `json:"static_url_template"`
	LocalManifestPathTemplate string `json:"local_manifest_path_template"`
	RetainVersions            int    `json:"retain_versions"`

	// MaxBootAttempts is how many consecutive times a self-binary update
	// (agent, adapter_*) is allowed to fail to boot far enough to reach
	// Manager.ResumeSelfUpdate before updates.BootGuard forces an
	// automatic rollback to the previous version. <=0 means BootGuard's
	// own default (3). See docs/updates.md's "self-binary components"
	// section.
	MaxBootAttempts int `json:"max_boot_attempts"`
}

type PollConfig struct {
	NodeStatusSeconds int `json:"node_status_seconds"`
	NetworkSeconds    int `json:"network_seconds"`
	SystemSeconds     int `json:"system_seconds"`
	VersionHours      int `json:"version_hours"`
}

// Default returns a Config usable for local development with no external
// configuration: mock adapter, in-memory-friendly paths, no admin token
// (administrative endpoints disabled), no Telegram.
func Default() Config {
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		ListenAddr:    "127.0.0.1:8787",
		DataDir:       "./var",
		DashboardDir:  "./dashboard/dist",
		Adapter:       AdapterConfig{Name: "mock", AssumedQRXCoreVersion: "0.0.7"},
		Updates: UpdatesConfig{
			SourceKind:      "development",
			RetainVersions:  2,
			MaxBootAttempts: 3,
		},
		Poll: PollConfig{
			NodeStatusSeconds: 5,
			NetworkSeconds:    10,
			SystemSeconds:     5,
			VersionHours:      6,
		},
		LogFormat: "text",
		LogLevel:  "info",
	}
}

// Load reads a JSON config file, applying Default() for any zero-value
// field left unset is NOT performed (explicit config wins entirely) --
// callers that want defaults-plus-overrides should start from Default()
// and unmarshal on top of it, which LoadInto does.
func Load(path string) (Config, error) {
	cfg := Default()
	if err := LoadInto(path, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadInto reads path's JSON, migrates it to CurrentSchemaVersion if
// needed (see migrate.go), and unmarshals the result on top of an existing
// Config (so any field the file doesn't set keeps whatever cfg already had
// -- typically Default()'s values).
func LoadInto(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no config file -- Default()'s values stand
		}
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	migrated, err := MigrateRaw(data)
	if err != nil {
		return fmt.Errorf("config: migrate %s: %w", path, err)
	}
	if err := json.Unmarshal(migrated, cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	return nil
}

// Save writes cfg as JSON to path, used after Migrate persists an upgraded
// config (docs/updates.md#configuration-migrations: "activate" step).
func Save(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp := path + ".tmp-" + time.Now().Format("20060102150405")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("config: activate: %w", err)
	}
	return nil
}
