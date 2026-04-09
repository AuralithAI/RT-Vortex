package swarm

// Observability Dashboard HTTP handlers.
//
// User-facing (user JWT, /api/v1/swarm):
//   GET  /api/v1/swarm/observability/dashboard     — full dashboard (current + time-series + health)
//   GET  /api/v1/swarm/observability/time-series    — metric snapshot time-series
//   GET  /api/v1/swarm/observability/providers      — per-provider performance data
//   GET  /api/v1/swarm/observability/providers/{provider} — single provider perf time-series
//   GET  /api/v1/swarm/observability/cost           — cost summary
//   GET  /api/v1/swarm/observability/health         — health score breakdown
//   PUT  /api/v1/swarm/observability/budget         — set cost budget

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ── Dashboard ───────────────────────────────────────────────────────────────

// HandleObservabilityDashboard handles GET /api/v1/swarm/observability/dashboard.
// Query params: hours (default 24).
func (h *Handler) HandleObservabilityDashboard(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	hours := 24
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 720 {
		hours = v
	}

	dash, err := h.ObservabilitySvc.GetDashboard(r.Context(), hours)
	if err != nil {
		slog.Error("observability: dashboard error", "error", err)
		http.Error(w, `{"error":"failed to fetch dashboard"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dash)
}

// ── Time-Series ─────────────────────────────────────────────────────────────

// HandleObservabilityTimeSeries handles GET /api/v1/swarm/observability/time-series.
// Query params: hours (default 24).
func (h *Handler) HandleObservabilityTimeSeries(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	hours := 24
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 720 {
		hours = v
	}

	series, err := h.ObservabilitySvc.GetTimeSeries(r.Context(), hours)
	if err != nil {
		slog.Error("observability: time-series error", "error", err)
		http.Error(w, `{"error":"failed to fetch time-series"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"snapshots": series,
		"count":     len(series),
		"hours":     hours,
	})
}

// ── Provider Performance ────────────────────────────────────────────────────

// HandleObservabilityProviders handles GET /api/v1/swarm/observability/providers.
// Query params: hours (default 24).
func (h *Handler) HandleObservabilityProviders(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	hours := 24
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 720 {
		hours = v
	}

	perf, err := h.ObservabilitySvc.GetProviderPerf(r.Context(), "", hours)
	if err != nil {
		slog.Error("observability: providers error", "error", err)
		http.Error(w, `{"error":"failed to fetch provider performance"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": perf,
		"count":     len(perf),
	})
}

// HandleObservabilityProviderDetail handles GET /api/v1/swarm/observability/providers/{provider}.
// Query params: hours (default 24).
func (h *Handler) HandleObservabilityProviderDetail(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	provider := chi.URLParam(r, "provider")
	if provider == "" {
		http.Error(w, `{"error":"provider is required"}`, http.StatusBadRequest)
		return
	}

	hours := 24
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 720 {
		hours = v
	}

	perf, err := h.ObservabilitySvc.GetProviderPerf(r.Context(), provider, hours)
	if err != nil {
		slog.Error("observability: provider detail error", "error", err, "provider", provider)
		http.Error(w, `{"error":"failed to fetch provider data"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider":    provider,
		"data_points": perf,
		"count":       len(perf),
	})
}

// ── Cost ────────────────────────────────────────────────────────────────────

// HandleObservabilityCost handles GET /api/v1/swarm/observability/cost.
func (h *Handler) HandleObservabilityCost(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	summary, err := h.ObservabilitySvc.getCostSummary(r.Context())
	if err != nil {
		slog.Error("observability: cost error", "error", err)
		http.Error(w, `{"error":"failed to fetch cost data"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// ── Health ──────────────────────────────────────────────────────────────────

// HandleObservabilityHealth handles GET /api/v1/swarm/observability/health.
func (h *Handler) HandleObservabilityHealth(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	h.ObservabilitySvc.mu.RLock()
	snap := h.ObservabilitySvc.latestSnapshot
	h.ObservabilitySvc.mu.RUnlock()

	if snap == nil {
		// Fall back to a live collection.
		snap = h.ObservabilitySvc.collectMetrics(r.Context())
		snap.HealthScore = h.ObservabilitySvc.computeHealthScore(snap)
	}

	breakdown := h.ObservabilitySvc.buildHealthBreakdown(snap)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(breakdown)
}

// ── Budget ──────────────────────────────────────────────────────────────────

// HandleObservabilitySetBudget handles PUT /api/v1/swarm/observability/budget.
func (h *Handler) HandleObservabilitySetBudget(w http.ResponseWriter, r *http.Request) {
	if h.ObservabilitySvc == nil {
		http.Error(w, `{"error":"observability service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var body struct {
		BudgetUSD      float64 `json:"budget_usd"`
		AlertThreshold float64 `json:"alert_threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.BudgetUSD <= 0 {
		http.Error(w, `{"error":"budget_usd must be positive"}`, http.StatusBadRequest)
		return
	}

	if err := h.ObservabilitySvc.SetCostBudget(r.Context(), body.BudgetUSD, body.AlertThreshold); err != nil {
		slog.Error("observability: set budget error", "error", err)
		http.Error(w, `{"error":"failed to set budget"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
