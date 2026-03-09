package api

import (
	"net/http"
)

// ── Engine Metrics REST Endpoint ────────────────────────────────────────────

// GetEngineMetrics returns the latest engine metrics snapshot.
// GET /api/v1/engine/metrics
func (h *Handler) GetEngineMetrics(w http.ResponseWriter, r *http.Request) {
	if h.MetricsCollector == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "engine metrics collector not configured",
		})
		return
	}

	snap := h.MetricsCollector.LatestSnapshot()
	if snap == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "no engine metrics available yet",
		})
		return
	}

	writeJSON(w, http.StatusOK, snap)
}

// GetEngineHealth returns engine health with metrics-readiness info.
// GET /api/v1/engine/health
func (h *Handler) GetEngineHealth(w http.ResponseWriter, r *http.Request) {
	if h.EngineClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "engine client not configured",
		})
		return
	}

	hs, err := h.EngineClient.HealthCheck(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "engine health check failed: " + err.Error(),
		})
		return
	}

	type healthResp struct {
		Healthy             bool              `json:"healthy"`
		Version             string            `json:"version"`
		UptimeSeconds       uint64            `json:"uptime_seconds"`
		Components          map[string]string `json:"components"`
		MetricsEnabled      bool              `json:"metrics_enabled"`
		ActiveMetricStreams uint32            `json:"active_metric_streams"`
		HasLatestSnapshot   bool              `json:"has_latest_snapshot"`
	}

	resp := healthResp{
		Healthy:             hs.Healthy,
		Version:             hs.Version,
		UptimeSeconds:       hs.UptimeSeconds,
		Components:          hs.Components,
		MetricsEnabled:      hs.MetricsEnabled,
		ActiveMetricStreams: hs.ActiveMetricStreams,
	}

	if h.MetricsCollector != nil {
		resp.HasLatestSnapshot = h.MetricsCollector.LatestSnapshot() != nil
	}

	writeJSON(w, http.StatusOK, resp)
}
