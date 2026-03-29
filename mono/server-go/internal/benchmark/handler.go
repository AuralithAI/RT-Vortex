package benchmark

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler exposes the benchmark REST API.
type Handler struct {
	runner *Runner
	logger *slog.Logger
}

// NewHandler creates a benchmark HTTP handler.
func NewHandler(runner *Runner, logger *slog.Logger) *Handler {
	return &Handler{runner: runner, logger: logger}
}

// RegisterRoutes mounts benchmark routes on the chi router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/tasks", h.ListTasks)
	r.Get("/runs", h.ListRuns)
	r.Post("/runs", h.StartRun)
	r.Get("/runs/{runID}", h.GetRun)
	r.Get("/runs/{runID}/report", h.GetRunReport)
	r.Get("/ratings", h.GetRatings)
	r.Get("/summary", h.GetSummary)
}

// ListTasks returns all registered benchmark tasks.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.runner.ListTasks()
	writeJSON(w, http.StatusOK, tasks)
}

// ListRuns returns all benchmark runs.
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	runs := h.runner.ListRuns()
	writeJSON(w, http.StatusOK, runs)
}

// StartRun starts a new benchmark run.
func (h *Handler) StartRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Name == "" {
		req.Name = "run-" + time.Now().Format("20060102-150405")
	}
	if req.Mode == "" {
		req.Mode = ModeBoth
	}

	h.logger.Info("starting benchmark run",
		"name", req.Name,
		"mode", req.Mode,
		"task_ids", req.TaskIDs,
		"category", req.Category,
	)

	// Run synchronously for now; handler is called from a goroutine by chi.
	run, err := h.runner.StartRun(req)
	if err != nil {
		h.logger.Error("benchmark run failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, run)
}

// GetRun returns a specific benchmark run.
func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "runID")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid run ID"})
		return
	}

	run, ok := h.runner.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}

	writeJSON(w, http.StatusOK, run)
}

// GetRunReport returns the downloadable JSON report for a benchmark run.
func (h *Handler) GetRunReport(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "runID")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid run ID"})
		return
	}

	run, ok := h.runner.GetRun(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}

	report := map[string]any{
		"report_version": "1.0",
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"run":            run,
		"ratings":        h.runner.GetRatings(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="benchmark-report-`+id.String()[:8]+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

// GetRatings returns current ELO ratings for all modes.
func (h *Handler) GetRatings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.runner.GetRatings())
}

// GetSummary returns a combined view of latest run + ratings for the dashboard.
func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	runs := h.runner.ListRuns()
	var latestRun *Run
	if len(runs) > 0 {
		latestRun = &runs[0]
	}

	summary := map[string]any{
		"latest_run": latestRun,
		"ratings":    h.runner.GetRatings(),
		"total_runs": len(runs),
		"total_tasks": len(h.runner.ListTasks()),
	}

	writeJSON(w, http.StatusOK, summary)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
