package storage

import (
	"context"
	"database/sql"
	"time"
)

// UpdateStatus is the outcome of one update attempt, per docs/updates.md
// "Update history".
type UpdateStatus string

const (
	UpdateStatusStarted    UpdateStatus = "started"
	UpdateStatusSucceeded  UpdateStatus = "succeeded"
	UpdateStatusFailed     UpdateStatus = "failed"
	UpdateStatusRolledBack UpdateStatus = "rolled_back"
	UpdateStatusBlocked    UpdateStatus = "blocked" // e.g. compatibility check failed before install
)

// UpdateHistoryRecord is one row of the update_history table.
type UpdateHistoryRecord struct {
	ID              int64
	Component       string
	FromVersion     string
	ToVersion       string
	Channel         string
	Timestamp       time.Time
	Status          UpdateStatus
	RollbackUsed    bool
	Checksum        string
	ManifestVersion int
	ErrorMessage    string
}

// UpdateHistoryStore persists agent/updates' CHECK/DOWNLOAD/.../COMMIT or
// ROLLBACK outcomes, per docs/updates.md#update-history.
type UpdateHistoryStore struct {
	db *sql.DB
}

func NewUpdateHistoryStore(db *sql.DB) *UpdateHistoryStore {
	return &UpdateHistoryStore{db: db}
}

// Insert records one update attempt outcome and returns its row id.
func (s *UpdateHistoryStore) Insert(ctx context.Context, r UpdateHistoryRecord) (int64, error) {
	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO update_history
			(component, from_version, to_version, channel, timestamp, status, rollback_used, checksum, manifest_version, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Component, r.FromVersion, r.ToVersion, r.Channel,
		r.Timestamp.Format(time.RFC3339Nano), string(r.Status), r.RollbackUsed,
		r.Checksum, r.ManifestVersion, r.ErrorMessage,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListForComponent returns the most recent update_history rows for a
// component, newest first, bounded by limit.
func (s *UpdateHistoryStore) ListForComponent(ctx context.Context, component string, limit int) ([]UpdateHistoryRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, component, from_version, to_version, channel, timestamp, status, rollback_used, checksum, manifest_version, error_message
		FROM update_history WHERE component = ? ORDER BY timestamp DESC, id DESC LIMIT ?`,
		component, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUpdateHistory(rows)
}

// Latest returns the most recent update_history row for a component, or
// (nil, nil) if there is none yet.
func (s *UpdateHistoryStore) Latest(ctx context.Context, component string) (*UpdateHistoryRecord, error) {
	records, err := s.ListForComponent(ctx, component, 1)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func scanUpdateHistory(rows *sql.Rows) ([]UpdateHistoryRecord, error) {
	var out []UpdateHistoryRecord
	for rows.Next() {
		var r UpdateHistoryRecord
		var ts, status string
		var fromVersion, checksum, errMsg sql.NullString
		var manifestVersion sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Component, &fromVersion, &r.ToVersion, &r.Channel,
			&ts, &status, &r.RollbackUsed, &checksum, &manifestVersion, &errMsg); err != nil {
			return nil, err
		}
		r.FromVersion = fromVersion.String
		r.Checksum = checksum.String
		r.ErrorMessage = errMsg.String
		r.ManifestVersion = int(manifestVersion.Int64)
		r.Status = UpdateStatus(status)
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, err
		}
		r.Timestamp = parsed
		out = append(out, r)
	}
	return out, rows.Err()
}
