package models

import "time"

// AlertSeverity classifies an alert's urgency.
type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)

// Alert is a persisted, user-facing condition raised by agent/alerts.
type Alert struct {
	ID         int64         `json:"id"`
	RuleID     string        `json:"rule_id"`
	Severity   AlertSeverity `json:"severity"`
	Title      string        `json:"title"`
	Message    string        `json:"message"`
	CreatedAt  time.Time     `json:"created_at"`
	ResolvedAt *time.Time    `json:"resolved_at,omitempty"`
	Resolved   bool          `json:"resolved"`
}
