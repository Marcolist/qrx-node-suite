package api

import (
	"net/http"

	"qrx-node-suite/agent/adapters"
	"qrx-node-suite/agent/updates"
	"qrx-node-suite/agent/version"
)

// handleUpdatesStatus is GET /api/v1/updates -- read-only, per-component
// current/latest/channel/update_available, per docs/updates.md#update-status.
func (d *Deps) handleUpdatesStatus(w http.ResponseWriter, r *http.Request) {
	if d.Updates == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	results, err := d.Updates.Plan(r.Context(), d.UpdateComponents)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make(map[string]updates.CheckResult, len(results))
	for _, res := range results {
		out[res.Component] = res
	}
	if d.QRXCore != nil {
		auto, _ := d.QRXCore.AutomaticEnabled(r.Context())
		policy := "manual"
		if auto {
			policy = "automatic"
		}
		out["qrx_core_policy"] = updates.CheckResult{Component: "qrx_core", Channel: policy}
	}
	writeJSON(w, http.StatusOK, out)
}

type checkRequest struct {
	Component string `json:"component"`
}

func (d *Deps) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if d.Updates == nil {
		httpError(w, http.StatusServiceUnavailable, "update manager not configured")
		return
	}
	result, err := d.Updates.Check(r.Context(), req.Component)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type planRequest struct {
	Components []string `json:"components"`
}

func (d *Deps) handleUpdatesPlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if len(req.Components) == 0 {
		req.Components = d.UpdateComponents
	}
	if d.Updates == nil {
		httpError(w, http.StatusServiceUnavailable, "update manager not configured")
		return
	}
	results, err := d.Updates.Plan(r.Context(), req.Components)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

type installRequest struct {
	Component      string `json:"component"`
	Channel        string `json:"channel"`
	AllowDowngrade bool   `json:"allow_downgrade"`
	Force          bool   `json:"force"`
}

// handleUpdatesInstall is POST /api/v1/updates/install [admin]. Manual
// (operator-triggered) is always true here -- there is no automatic
// caller of this HTTP endpoint.
func (d *Deps) handleUpdatesInstall(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if d.Updates == nil {
		httpError(w, http.StatusServiceUnavailable, "update manager not configured")
		return
	}
	actor := actorFromContext(r.Context())
	result, err := d.Updates.Install(r.Context(), req.Component, updates.InstallOptions{
		Channel: req.Channel, AllowDowngrade: req.AllowDowngrade, Manual: true, Force: req.Force, Actor: actor,
	})
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type rollbackRequest struct {
	Component string `json:"component"`
}

// handleUpdatesRollback is POST /api/v1/updates/rollback [admin]. QRX Core
// is not routed through this endpoint -- see POST /api/v1/qrx-core/switch,
// which requires a target version and its compatibility profile (QRX
// Core's rollback is "switch to whatever version was previously current",
// which the dashboard resolves via GET /api/v1/versions before calling
// switch).
func (d *Deps) handleUpdatesRollback(w http.ResponseWriter, r *http.Request) {
	var req rollbackRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Component == updates.QRXCoreComponent {
		httpError(w, http.StatusBadRequest, "QRX Core rollback goes through POST /api/v1/qrx-core/switch with the previous version and its compatibility profile")
		return
	}
	if d.Updates == nil {
		httpError(w, http.StatusServiceUnavailable, "update manager not configured")
		return
	}
	actor := actorFromContext(r.Context())
	result, err := d.Updates.Rollback(r.Context(), req.Component, actor)
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type activateVersionRequest struct {
	Version          string `json:"version"`
	AllowUnsupported bool   `json:"allow_unsupported"`
}

// handleActivateVersion is
// POST /api/v1/components/{component}/activate-version [admin]. In this
// build, "component" must name an installed adapter (see
// agent/adapters.Registry) -- manual adapter selection
// (docs/updates.md#manual-adapter-selection). Activating an
// already-installed agent/dashboard version by name (as opposed to
// installing a new one via /updates/install) is not yet wired to an
// endpoint; see docs/updates.md's noted scope.
func (d *Deps) handleActivateVersion(w http.ResponseWriter, r *http.Request) {
	component := r.PathValue("component")
	var req activateVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if d.Registry == nil {
		httpError(w, http.StatusServiceUnavailable, "adapter registry not configured")
		return
	}
	if err := d.Registry.Activate(r.Context(), component, adapters.ActivateOptions{AllowUnsupported: req.AllowUnsupported}); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Record(r.Context(), auditEvent(actorFromContext(r.Context()), "ADMIN_SWITCH_ADAPTER", component, req.Version))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated", "component": component})
}

type qrxCoreSwitchRequest struct {
	Version        string `json:"version"`
	ExpertOverride bool   `json:"expert_override"`
	ProfilePath    string `json:"profile_path"` // path to the target version's compatibility profile JSON
}

// handleQRXCoreSwitch is POST /api/v1/qrx-core/switch [admin]:
// docs/updates.md#qrx-core-version-selection's "Switch Version"/"Rollback"
// actions, both of which are "activate an already-installed QRX Core
// version" -- the only difference is which version the caller names.
func (d *Deps) handleQRXCoreSwitch(w http.ResponseWriter, r *http.Request) {
	var req qrxCoreSwitchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if d.QRXCore == nil {
		httpError(w, http.StatusServiceUnavailable, "QRX Core update manager not configured")
		return
	}
	if req.ProfilePath == "" {
		httpError(w, http.StatusBadRequest, "profile_path is required: QRX Core switches always require a compatibility profile, see docs/updates.md#core-version-switching-safety")
		return
	}
	profile, err := version.LoadCompatibilityProfileFile(req.ProfilePath)
	if err != nil {
		httpError(w, http.StatusBadRequest, "load compatibility profile: "+err.Error())
		return
	}
	rec, err := d.QRXCore.SwitchVersion(r.Context(), req.Version, updates.SwitchOptions{
		Profile: profile, ExpertOverride: req.ExpertOverride, Actor: actorFromContext(r.Context()),
	})
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}
