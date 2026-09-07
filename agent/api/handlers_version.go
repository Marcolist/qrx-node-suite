package api

import (
	"net/http"

	"qrx-node-suite/agent/models"
)

func (d *Deps) handleVersion(w http.ResponseWriter, r *http.Request) {
	if d.VersionInfo == nil {
		writeJSON(w, http.StatusOK, models.VersionInfo{})
		return
	}
	writeJSON(w, http.StatusOK, d.VersionInfo())
}

// handleVersions is GET /api/v1/versions: the Installed/Active/Recommended/
// Pinned/Compatible per-component detail behind the dashboard's Version
// Selection UX (docs/updates.md#version-selection-ux). It reuses
// updates.Manager.Plan, which already computes exactly this.
func (d *Deps) handleVersions(w http.ResponseWriter, r *http.Request) {
	if d.Updates == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	results, err := d.Updates.Plan(r.Context(), d.UpdateComponents)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}
