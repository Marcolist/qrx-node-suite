package config

import (
	"encoding/json"
	"fmt"
)

// rawMigration transforms raw JSON config data (a generic map, not the
// current Go Config struct) from one schema_version to the next. Operating
// on the raw map -- not a strictly-typed struct -- means a migration can
// rename or restructure a field that no longer exists in the current
// struct at all, which a struct-to-struct migration could not represent.
type rawMigration struct {
	From, To int
	// Apply mutates data in place. It must set/advance
	// data["config_schema_version"] itself is NOT required -- MigrateRaw
	// does that once Apply succeeds.
	Apply func(data map[string]any) error
}

// migrations is empty because CurrentSchemaVersion (1) is the first schema
// this project has ever shipped -- there is nothing to migrate FROM yet.
// When a future change needs one, add an entry here, e.g.:
//
//	{From: 1, To: 2, Apply: func(data map[string]any) error {
//	    // e.g. data["telegram"] restructured into a list of channels:
//	    if old, ok := data["telegram"]; ok {
//	        data["notification_channels"] = []any{old}
//	        delete(data, "telegram")
//	    }
//	    return nil
//	}},
//
// Never edit a migration that has shipped -- add a new one instead
// (CONTRIBUTING.md: "migrations are append-only").
var migrations []rawMigration

// MigrateRaw implements docs/updates.md#configuration-migrations: backup
// (the caller's original data slice is never mutated) -> validate old
// (must parse as JSON) -> migrate (apply every registered step in order) ->
// validate new (the result must unmarshal into Config) -> return the
// migrated bytes for the caller to activate (write back / use). Any
// failure returns an error and the ORIGINAL data is left completely
// untouched on disk -- "rollback" here means "never wrote the change,"
// which is simpler and just as safe as writing-then-reverting for an
// in-memory transform.
func MigrateRaw(data []byte) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("validate old config: %w", err)
	}

	version := CurrentSchemaVersion
	if v, ok := raw["config_schema_version"]; ok {
		if f, ok := v.(float64); ok {
			version = int(f)
		}
	} else {
		// No version field at all means "the first schema this project
		// ever had," which IS CurrentSchemaVersion today (1) -- nothing to
		// migrate.
		version = CurrentSchemaVersion
	}

	for {
		if version == CurrentSchemaVersion {
			break
		}
		var next *rawMigration
		for i := range migrations {
			if migrations[i].From == version {
				next = &migrations[i]
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("config schema version %d has no migration path to %d", version, CurrentSchemaVersion)
		}
		if err := next.Apply(raw); err != nil {
			return nil, fmt.Errorf("migrate config %d -> %d: %w", next.From, next.To, err)
		}
		raw["config_schema_version"] = float64(next.To)
		version = next.To
	}

	out, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal migrated config: %w", err)
	}
	var check Config
	if err := json.Unmarshal(out, &check); err != nil {
		return nil, fmt.Errorf("validate migrated config: %w", err)
	}
	return out, nil
}
