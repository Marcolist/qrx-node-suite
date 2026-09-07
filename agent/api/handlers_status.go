package api

import (
	"net/http"
	"time"

	"qrx-node-suite/agent/models"
)

// statusResponse is GET /api/v1/status's aggregate view -- everything the
// dashboard Overview page needs in one round trip, per
// docs/architecture.md section 17.
type statusResponse struct {
	Health      models.HealthState  `json:"health"`
	Node        models.NodeStatus   `json:"node"`
	System      models.SystemStatus `json:"system"`
	AdapterName string              `json:"adapter_name"`
	UpdatedAt   string              `json:"updated_at"`
}

func (d *Deps) handleStatus(w http.ResponseWriter, r *http.Request) {
	s := d.Cache.Get()
	writeJSON(w, http.StatusOK, statusResponse{
		Health: s.Health, Node: s.Node, System: s.System,
		AdapterName: s.AdapterName, UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	})
}

func (d *Deps) handleNode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Cache.Get().Node)
}

func (d *Deps) handleNetwork(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Cache.Get().Network)
}

func (d *Deps) handleBlockchain(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Cache.Get().Blockchain)
}

func (d *Deps) handleMempool(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Cache.Get().Mempool)
}

func (d *Deps) handleValidator(w http.ResponseWriter, r *http.Request) {
	s := d.Cache.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"validator":      s.Validator,
		"block_producer": s.BlockProducer,
	})
}

func (d *Deps) handleVelocity(w http.ResponseWriter, r *http.Request) {
	s := d.Cache.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"velocity":    s.Velocity,
		"nonce_lanes": s.NonceLanes,
	})
}

func (d *Deps) handleSystem(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Cache.Get().System)
}

func (d *Deps) handleActivity(w http.ResponseWriter, r *http.Request) {
	s := d.Cache.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"recent_blocks":       s.RecentBlocks,
		"recent_transactions": s.RecentTransactions,
	})
}

func (d *Deps) handleAlertsList(w http.ResponseWriter, r *http.Request) {
	if d.Alerts == nil {
		writeJSON(w, http.StatusOK, []models.Alert{})
		return
	}
	unresolvedOnly := r.URL.Query().Get("unresolved") == "true"
	list, err := d.Alerts.List(r.Context(), unresolvedOnly, 100)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (d *Deps) handleServices(w http.ResponseWriter, r *http.Request) {
	s := d.Cache.Get()
	state := models.ServiceRunning
	if s.Health == models.HealthOffline {
		state = models.ServiceStopped
	}
	writeJSON(w, http.StatusOK, []models.ServiceStatus{
		{Name: "qrxd", State: state},
	})
}

func (d *Deps) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	if d.Settings == nil {
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}
	all, err := d.Settings.All(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, all)
}

func (d *Deps) handleEvents(w http.ResponseWriter, r *http.Request) {
	if d.Bus == nil {
		httpError(w, http.StatusServiceUnavailable, "event bus not configured")
		return
	}
	d.Bus.ServeSSE(w, r)
}
