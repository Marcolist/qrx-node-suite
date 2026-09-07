package storage

import (
	"context"
	"database/sql"
	"time"
)

// Administrative audit actions, per docs/updates.md#admin-authorization.
const (
	ActionUpdateAgent       = "ADMIN_UPDATE_AGENT"
	ActionUpdateDashboard   = "ADMIN_UPDATE_DASHBOARD"
	ActionUpdateAdapter     = "ADMIN_UPDATE_ADAPTER"
	ActionSwitchAdapter     = "ADMIN_SWITCH_ADAPTER"
	ActionSwitchQRXCore     = "ADMIN_SWITCH_QRX_CORE"
	ActionRollbackAgent     = "ADMIN_ROLLBACK_AGENT"
	ActionRollbackDashboard = "ADMIN_ROLLBACK_DASHBOARD"
	ActionRollbackAdapter   = "ADMIN_ROLLBACK_ADAPTER"
	ActionRollbackQRXCore   = "ADMIN_ROLLBACK_QRX_CORE"
	ActionPinVersion        = "ADMIN_PIN_VERSION"
	ActionSetChannel        = "ADMIN_SET_CHANNEL"
	ActionSetUpdateLock     = "ADMIN_SET_UPDATE_LOCK"
	ActionServiceRestart    = "ADMIN_SERVICE_RESTART"
)

// AuditEvent is one row of the audit_log table: every administrative action
// (docs/updates.md#admin-authorization) must record one of these.
type AuditEvent struct {
	ID        int64
	Timestamp time.Time
	Actor     string // authenticated principal, e.g. "admin" or a user id
	Action    string
	Component string
	Details   string
	IPAddress string
}

// AuditLogStore persists AuditEvents.
type AuditLogStore struct {
	db *sql.DB
}

func NewAuditLogStore(db *sql.DB) *AuditLogStore {
	return &AuditLogStore{db: db}
}

// Record inserts one audit event.
func (s *AuditLogStore) Record(ctx context.Context, e AuditEvent) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (timestamp, actor, action, component, details, ip_address)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.Timestamp.Format(time.RFC3339Nano), e.Actor, e.Action, e.Component, e.Details, e.IPAddress,
	)
	return err
}

// List returns the most recent audit events, newest first.
func (s *AuditLogStore) List(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, actor, action, component, details, ip_address
		FROM audit_log ORDER BY timestamp DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var ts string
		var component, details, ip sql.NullString
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &component, &details, &ip); err != nil {
			return nil, err
		}
		e.Component = component.String
		e.Details = details.String
		e.IPAddress = ip.String
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, err
		}
		e.Timestamp = parsed
		out = append(out, e)
	}
	return out, rows.Err()
}
