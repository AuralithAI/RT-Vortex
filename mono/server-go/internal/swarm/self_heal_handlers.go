package swarm

// Self-Healing Pipeline HTTP handlers.
//
// Internal (agent JWT):
//   POST /internal/swarm/self-heal/provider-outcome  — agent reports provider success/failure
//   GET  /internal/swarm/self-heal/provider-status    — check if a provider is available
//
// User-facing (user JWT, /api/v1/swarm):
//   GET  /api/v1/swarm/self-heal/summary              — full dashboard summary
//   GET  /api/v1/swarm/self-heal/events               — paginated event log
//   POST /api/v1/swarm/self-heal/events/{id}/resolve  — mark event resolved
//   POST /api/v1/swarm/self-heal/circuits/{provider}/reset — manual circuit reset
//   GET  /api/v1/swarm/self-heal/circuits              — list all circuit states

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ── Internal Handlers (agent JWT) ───────────────────────────────────────────

// HandleProviderOutcome handles POST /internal/swarm/self-heal/provider-outcome.
// Agents call this after every LLM call to report success/failure.
func (h *Handler) HandleProviderOutcome(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var report ProviderOutcomeReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if report.Provider == "" {
		http.Error(w, `{"error":"provider is required"}`, http.StatusBadRequest)
		return
	}

	if err := h.SelfHealSvc.RecordProviderOutcome(r.Context(), report); err != nil {
		slog.Error("self-heal: failed to record provider outcome", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleProviderStatus handles GET /internal/swarm/self-heal/provider-status?provider=xxx.
// Returns whether the provider is available (circuit breaker check).
func (h *Handler) HandleProviderStatus(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	provider := r.URL.Query().Get("provider")
	if provider == "" {
		http.Error(w, `{"error":"provider query param required"}`, http.StatusBadRequest)
		return
	}

	available := h.SelfHealSvc.IsProviderAvailable(provider)
	state := h.SelfHealSvc.GetProviderState(provider)

	resp := map[string]interface{}{
		"provider":  provider,
		"available": available,
	}
	if state != nil {
		resp["state"] = state.State
		resp["consecutive_failures"] = state.ConsecutiveFailures
		resp["open_until"] = state.OpenUntil
	} else {
		resp["state"] = "closed"
		resp["consecutive_failures"] = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ── User-facing Handlers (user JWT) ─────────────────────────────────────────

// HandleSelfHealSummary handles GET /api/v1/swarm/self-heal/summary.
func (h *Handler) HandleSelfHealSummary(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 25
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	summary, err := h.SelfHealSvc.GetSummary(r.Context(), limit)
	if err != nil {
		slog.Error("self-heal: summary error", "error", err)
		http.Error(w, `{"error":"failed to fetch summary"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// HandleSelfHealEvents handles GET /api/v1/swarm/self-heal/events.
// Query params: event_type, severity, provider, limit, offset.
func (h *Handler) HandleSelfHealEvents(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	eventType := q.Get("event_type")
	severity := q.Get("severity")
	provider := q.Get("provider")
	limit := 50
	offset := 0
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v >= 0 {
		offset = v
	}

	events, total, err := h.SelfHealSvc.GetEvents(r.Context(), eventType, severity, provider, limit, offset)
	if err != nil {
		slog.Error("self-heal: events error", "error", err)
		http.Error(w, `{"error":"failed to fetch events"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// HandleResolveEvent handles POST /api/v1/swarm/self-heal/events/{id}/resolve.
func (h *Handler) HandleResolveEvent(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "id")
	eventID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid event id"}`, http.StatusBadRequest)
		return
	}

	if err := h.SelfHealSvc.ResolveEvent(r.Context(), eventID); err != nil {
		slog.Error("self-heal: resolve error", "event_id", eventID, "error", err)
		http.Error(w, `{"error":"failed to resolve event"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleResetCircuit handles POST /api/v1/swarm/self-heal/circuits/{provider}/reset.
func (h *Handler) HandleResetCircuit(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	provider := chi.URLParam(r, "provider")
	if provider == "" {
		http.Error(w, `{"error":"provider is required"}`, http.StatusBadRequest)
		return
	}

	if err := h.SelfHealSvc.ResetCircuit(r.Context(), provider); err != nil {
		slog.Error("self-heal: reset circuit error", "provider", provider, "error", err)
		http.Error(w, `{"error":"failed to reset circuit"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleListCircuits handles GET /api/v1/swarm/self-heal/circuits.
func (h *Handler) HandleListCircuits(w http.ResponseWriter, r *http.Request) {
	if h.SelfHealSvc == nil {
		http.Error(w, `{"error":"self-heal service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	circuits, err := h.SelfHealSvc.listCircuitStates(r.Context())
	if err != nil {
		slog.Error("self-heal: list circuits error", "error", err)
		http.Error(w, `{"error":"failed to list circuits"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"circuits": circuits,
	})
}
