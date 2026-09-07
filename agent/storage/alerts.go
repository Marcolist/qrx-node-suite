package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"qrx-node-suite/agent/models"
)

// AlertStore persists models.Alert to the alerts table. It implements
// agent/alerts.Store (that package defines the interface, to avoid a
// storage<->alerts import cycle and keep alerts.Engine trivially testable
// with a fake).
type AlertStore struct {
	db *sql.DB
}

func NewAlertStore(db *sql.DB) *AlertStore {
	return &AlertStore{db: db}
}

// Create inserts a new alert and returns it with its assigned ID.
func (s *AlertStore) Create(ctx context.Context, a models.Alert) (models.Alert, error) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO alerts (rule_id, severity, title, message, created_at, resolved)
		VALUES (?, ?, ?, ?, ?, 0)`,
		a.RuleID, string(a.Severity), a.Title, a.Message, a.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return models.Alert{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Alert{}, err
	}
	a.ID = id
	return a, nil
}

// OpenByRule returns the currently unresolved alert for a rule, if any.
func (s *AlertStore) OpenByRule(ctx context.Context, ruleID string) (models.Alert, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, rule_id, severity, title, message, created_at
		FROM alerts WHERE rule_id = ? AND resolved = 0
		ORDER BY created_at DESC LIMIT 1`, ruleID)
	var a models.Alert
	var severity, createdAt string
	err := row.Scan(&a.ID, &a.RuleID, &severity, &a.Title, &a.Message, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Alert{}, false, nil
	}
	if err != nil {
		return models.Alert{}, false, err
	}
	a.Severity = models.AlertSeverity(severity)
	ts, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return models.Alert{}, false, err
	}
	a.CreatedAt = ts
	return a, true, nil
}

// ResolveByRule marks the currently open alert(s) for a rule resolved.
func (s *AlertStore) ResolveByRule(ctx context.Context, ruleID string, resolvedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE alerts SET resolved = 1, resolved_at = ?
		WHERE rule_id = ? AND resolved = 0`,
		resolvedAt.Format(time.RFC3339Nano), ruleID)
	return err
}

// List returns the most recent alerts, newest first, optionally filtered to
// only unresolved ones. Used by GET /api/v1/alerts.
func (s *AlertStore) List(ctx context.Context, unresolvedOnly bool, limit int) ([]models.Alert, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, rule_id, severity, title, message, created_at, resolved_at, resolved FROM alerts`
	if unresolvedOnly {
		query += ` WHERE resolved = 0`
	}
	query += ` ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Alert
	for rows.Next() {
		var a models.Alert
		var severity, createdAt string
		var resolvedAt sql.NullString
		var resolved bool
		if err := rows.Scan(&a.ID, &a.RuleID, &severity, &a.Title, &a.Message, &createdAt, &resolvedAt, &resolved); err != nil {
			return nil, err
		}
		a.Severity = models.AlertSeverity(severity)
		ts, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		a.CreatedAt = ts
		a.Resolved = resolved
		if resolvedAt.Valid {
			rt, err := time.Parse(time.RFC3339Nano, resolvedAt.String)
			if err != nil {
				return nil, err
			}
			a.ResolvedAt = &rt
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
