package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrSettingNotFound is returned by SettingsStore.Get/GetJSON when no row
// exists for the key.
var ErrSettingNotFound = errors.New("storage: setting not found")

// SettingsStore is a generic key/value store backing agent/updates' channel
// selection, version pins, update lock, and scheduled update window --
// anything that's operator-configurable state rather than a typed table of
// its own.
type SettingsStore struct {
	db *sql.DB
}

func NewSettingsStore(db *sql.DB) *SettingsStore {
	return &SettingsStore{db: db}
}

// Set stores a raw string value.
func (s *SettingsStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Get returns a raw string value, or ErrSettingNotFound.
func (s *SettingsStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSettingNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetJSON marshals v and stores it under key.
func (s *SettingsStore) SetJSON(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(ctx, key, string(b))
}

// GetJSON reads key and unmarshals it into v. Returns ErrSettingNotFound if
// the key doesn't exist, leaving v untouched -- callers should apply their
// own default in that case.
func (s *SettingsStore) GetJSON(ctx context.Context, key string, v any) error {
	raw, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

// Delete removes a key. Deleting a nonexistent key is not an error.
func (s *SettingsStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

// All returns every stored key. Used by the Settings API endpoint
// (GET /api/v1/settings).
func (s *SettingsStore) All(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
