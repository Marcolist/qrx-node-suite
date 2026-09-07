package api

import (
	"net/http"

	"qrx-node-suite/agent/storage"
)

func auditEvent(actor, action, component, details string) storage.AuditEvent {
	return storage.AuditEvent{Actor: actor, Action: action, Component: component, Details: details}
}

// handleServiceRestart is POST /api/v1/services/qrx/restart [admin]. Per
// docs/architecture.md section 13: administrative endpoints must be
// disabled unless configured, require authentication, and be audited. This
// one is additionally disabled (404) unless RestartQRXService is wired up
// -- distinct from the 401/403 an unauthenticated/wrong-token request gets,
// see Deps.RestartQRXService's doc comment.
func (d *Deps) handleServiceRestart(w http.ResponseWriter, r *http.Request) {
	if d.RestartQRXService == nil {
		httpError(w, http.StatusNotFound, "service restart is not configured on this Agent")
		return
	}
	actor := actorFromContext(r.Context())
	if err := d.RestartQRXService(r.Context()); err != nil {
		if d.Audit != nil {
			d.Audit.Record(r.Context(), auditEvent(actor, storage.ActionServiceRestart, "qrxd", "failed: "+err.Error()))
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Record(r.Context(), auditEvent(actor, storage.ActionServiceRestart, "qrxd", "succeeded"))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}
