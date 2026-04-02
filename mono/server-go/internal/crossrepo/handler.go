package crossrepo

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── Audit action constants for cross-repo events ────────────────────────────

const (
	AuditActionLinkCreated     = "crossrepo.link_created"
	AuditActionLinkUpdated     = "crossrepo.link_updated"
	AuditActionLinkDeleted     = "crossrepo.link_deleted"
	AuditActionAccessDenied    = "crossrepo.access_denied"
	AuditActionAccessGranted   = "crossrepo.access_granted"
)

// ── Handler ─────────────────────────────────────────────────────────────────

// Handler provides HTTP endpoints for cross-repo link management.
type Handler struct {
	authorizer   *Authorizer
	repoLinkRepo *store.RepoLinkRepo
	repoRepo     *store.RepositoryRepo
	auditLogger  *audit.Logger
}

// NewHandler creates a new cross-repo Handler.
func NewHandler(
	authorizer *Authorizer,
	repoLinkRepo *store.RepoLinkRepo,
	repoRepo *store.RepositoryRepo,
	auditLogger *audit.Logger,
) *Handler {
	return &Handler{
		authorizer:   authorizer,
		repoLinkRepo: repoLinkRepo,
		repoRepo:     repoRepo,
		auditLogger:  auditLogger,
	}
}

// RegisterRoutes mounts the cross-repo link management routes on the given chi.Router.
// Expected mount point: /api/v1/repos/{repoID}/links
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/", h.CreateLink)
	r.Get("/", h.ListLinks)
	r.Get("/{linkID}", h.GetLink)
	r.Put("/{linkID}", h.UpdateLink)
	r.Delete("/{linkID}", h.DeleteLink)
	r.Get("/{linkID}/events", h.ListLinkEvents)
}

// RegisterOrgRoutes mounts org-level cross-repo routes.
// Expected mount point: /api/v1/orgs/{orgID}/links
func (h *Handler) RegisterOrgRoutes(r chi.Router) {
	r.Get("/", h.ListOrgLinks)
	r.Get("/events", h.ListOrgEvents)
}

// ── CreateLink ──────────────────────────────────────────────────────────────

type createLinkRequest struct {
	TargetRepoID string `json:"target_repo_id"`
	ShareProfile string `json:"share_profile"`
	Label        string `json:"label"`
}

// CreateLink creates a new cross-repo link from the current repo to a target repo.
// POST /api/v1/repos/{repoID}/links
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	sourceRepoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		http.Error(w, `{"error":"invalid repo ID"}`, http.StatusBadRequest)
		return
	}

	var req createLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	targetRepoID, err := uuid.Parse(req.TargetRepoID)
	if err != nil {
		http.Error(w, `{"error":"invalid target_repo_id"}`, http.StatusBadRequest)
		return
	}

	if sourceRepoID == targetRepoID {
		http.Error(w, `{"error":"cannot link a repo to itself"}`, http.StatusBadRequest)
		return
	}

	// Default share profile.
	if req.ShareProfile == "" {
		req.ShareProfile = model.ShareMetadata
	}
	if !model.ValidShareProfiles[req.ShareProfile] {
		http.Error(w, `{"error":"invalid share_profile; allowed: full, symbols, metadata, none"}`, http.StatusBadRequest)
		return
	}

	// Look up source repo to get org ID.
	sourceRepo, err := h.repoRepo.GetByID(r.Context(), sourceRepoID)
	if err != nil {
		http.Error(w, `{"error":"source repository not found"}`, http.StatusNotFound)
		return
	}

	// Authorize: only org admin/owner can create links.
	decision := h.authorizer.AuthorizeLink(r.Context(), userID, sourceRepo.OrgID)
	if !decision.Allowed {
		h.auditLogger.LogRequest(r, AuditActionAccessDenied, "repo_link", sourceRepoID.String(),
			map[string]interface{}{"action": "create", "reason": decision.Reason})
		jsonError(w, decision.Reason, http.StatusForbidden)
		return
	}

	// Check link budget.
	budget := h.authorizer.CheckOrgLinkBudget(r.Context(), sourceRepo.OrgID)
	if !budget.Allowed {
		jsonError(w, budget.Reason, http.StatusForbidden)
		return
	}

	// Verify target repo exists and is in the same org.
	targetRepo, err := h.repoRepo.GetByID(r.Context(), targetRepoID)
	if err != nil {
		http.Error(w, `{"error":"target repository not found"}`, http.StatusNotFound)
		return
	}
	if sourceRepo.OrgID != targetRepo.OrgID {
		http.Error(w, `{"error":"target repository must be in the same organization"}`, http.StatusBadRequest)
		return
	}

	// Create the link.
	link := &model.RepoLink{
		OrgID:        sourceRepo.OrgID,
		SourceRepoID: sourceRepoID,
		TargetRepoID: targetRepoID,
		ShareProfile: req.ShareProfile,
		Label:        req.Label,
		CreatedBy:    userID,
	}

	if err := h.repoLinkRepo.Create(r.Context(), link); err != nil {
		if errors.Is(err, store.ErrConflict) {
			http.Error(w, `{"error":"link already exists between these repositories"}`, http.StatusConflict)
			return
		}
		slog.Error("failed to create repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Log audit event.
	h.logLinkEvent(r, link, AuditActionLinkCreated, userID, map[string]interface{}{
		"share_profile": req.ShareProfile,
		"label":         req.Label,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(link)
}

// ── ListLinks ───────────────────────────────────────────────────────────────

// ListLinks returns all links for a specific repo (as source or target).
// GET /api/v1/repos/{repoID}/links
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		http.Error(w, `{"error":"invalid repo ID"}`, http.StatusBadRequest)
		return
	}

	links, err := h.repoLinkRepo.ListByRepo(r.Context(), repoID)
	if err != nil {
		slog.Error("failed to list repo links", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"links": links,
		"total": len(links),
	})
}

// ── GetLink ─────────────────────────────────────────────────────────────────

// GetLink returns a single link by ID.
// GET /api/v1/repos/{repoID}/links/{linkID}
func (h *Handler) GetLink(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		http.Error(w, `{"error":"invalid link ID"}`, http.StatusBadRequest)
		return
	}

	link, err := h.repoLinkRepo.GetByID(r.Context(), linkID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, `{"error":"link not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("failed to get repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// ── UpdateLink ──────────────────────────────────────────────────────────────

type updateLinkRequest struct {
	ShareProfile string `json:"share_profile"`
	Label        string `json:"label"`
}

// UpdateLink updates the share profile and/or label of an existing link.
// PUT /api/v1/repos/{repoID}/links/{linkID}
func (h *Handler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		http.Error(w, `{"error":"invalid link ID"}`, http.StatusBadRequest)
		return
	}

	var req updateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.ShareProfile != "" && !model.ValidShareProfiles[req.ShareProfile] {
		http.Error(w, `{"error":"invalid share_profile; allowed: full, symbols, metadata, none"}`, http.StatusBadRequest)
		return
	}

	// Load existing link.
	link, err := h.repoLinkRepo.GetByID(r.Context(), linkID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, `{"error":"link not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("failed to get repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Authorize: only org admin/owner can modify links.
	decision := h.authorizer.AuthorizeLink(r.Context(), userID, link.OrgID)
	if !decision.Allowed {
		h.auditLogger.LogRequest(r, AuditActionAccessDenied, "repo_link", linkID.String(),
			map[string]interface{}{"action": "update", "reason": decision.Reason})
		jsonError(w, decision.Reason, http.StatusForbidden)
		return
	}

	// Apply defaults: keep existing values if not provided.
	newProfile := link.ShareProfile
	if req.ShareProfile != "" {
		newProfile = req.ShareProfile
	}
	newLabel := link.Label
	if req.Label != "" {
		newLabel = req.Label
	}

	oldProfile := link.ShareProfile
	if err := h.repoLinkRepo.UpdateShareProfile(r.Context(), linkID, newProfile, newLabel); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"link not found"}`, http.StatusNotFound)
			return
		}
		slog.Error("failed to update repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Log audit event.
	h.logLinkEvent(r, link, AuditActionLinkUpdated, userID, map[string]interface{}{
		"old_share_profile": oldProfile,
		"new_share_profile": newProfile,
		"label":             newLabel,
	})

	link.ShareProfile = newProfile
	link.Label = newLabel

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// ── DeleteLink ──────────────────────────────────────────────────────────────

// DeleteLink removes a cross-repo link.
// DELETE /api/v1/repos/{repoID}/links/{linkID}
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		http.Error(w, `{"error":"invalid link ID"}`, http.StatusBadRequest)
		return
	}

	// Load existing link for audit + auth.
	link, err := h.repoLinkRepo.GetByID(r.Context(), linkID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, `{"error":"link not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("failed to get repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Authorize: only org admin/owner can delete links.
	decision := h.authorizer.AuthorizeLink(r.Context(), userID, link.OrgID)
	if !decision.Allowed {
		h.auditLogger.LogRequest(r, AuditActionAccessDenied, "repo_link", linkID.String(),
			map[string]interface{}{"action": "delete", "reason": decision.Reason})
		jsonError(w, decision.Reason, http.StatusForbidden)
		return
	}

	if err := h.repoLinkRepo.Delete(r.Context(), linkID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"link not found"}`, http.StatusNotFound)
			return
		}
		slog.Error("failed to delete repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Log audit event.
	h.logLinkEvent(r, link, AuditActionLinkDeleted, userID, map[string]interface{}{
		"share_profile": link.ShareProfile,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ── ListLinkEvents ──────────────────────────────────────────────────────────

// ListLinkEvents returns audit events for a specific link.
// GET /api/v1/repos/{repoID}/links/{linkID}/events
func (h *Handler) ListLinkEvents(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		http.Error(w, `{"error":"invalid link ID"}`, http.StatusBadRequest)
		return
	}

	// Load link to get org ID.
	link, err := h.repoLinkRepo.GetByID(r.Context(), linkID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, `{"error":"link not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("failed to get repo link", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	limit := parseIntParam(r, "limit", 50)
	events, err := h.repoLinkRepo.ListEvents(r.Context(), link.OrgID, &linkID, limit)
	if err != nil {
		slog.Error("failed to list link events", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"total":  len(events),
	})
}

// ── Org-level routes ────────────────────────────────────────────────────────

// ListOrgLinks returns all links within an organization.
// GET /api/v1/orgs/{orgID}/links
func (h *Handler) ListOrgLinks(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		http.Error(w, `{"error":"invalid org ID"}`, http.StatusBadRequest)
		return
	}

	limit := parseIntParam(r, "limit", 50)
	offset := parseIntParam(r, "offset", 0)

	links, total, err := h.repoLinkRepo.ListByOrg(r.Context(), orgID, limit, offset)
	if err != nil {
		slog.Error("failed to list org links", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"links":  links,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ListOrgEvents returns all link events for an organization.
// GET /api/v1/orgs/{orgID}/links/events
func (h *Handler) ListOrgEvents(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		http.Error(w, `{"error":"invalid org ID"}`, http.StatusBadRequest)
		return
	}

	limit := parseIntParam(r, "limit", 50)

	events, err := h.repoLinkRepo.ListEvents(r.Context(), orgID, nil, limit)
	if err != nil {
		slog.Error("failed to list org link events", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"total":  len(events),
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// logLinkEvent writes both to the main audit_log and to the repo_link_events table.
func (h *Handler) logLinkEvent(r *http.Request, link *model.RepoLink, action string, actorID uuid.UUID, meta map[string]interface{}) {
	// Main audit log (fire-and-forget).
	h.auditLogger.LogRequest(r, action, "repo_link", link.ID.String(), meta)

	// Dedicated link event log.
	evt := &model.RepoLinkEvent{
		LinkID:       &link.ID,
		OrgID:        link.OrgID,
		SourceRepoID: link.SourceRepoID,
		TargetRepoID: link.TargetRepoID,
		Action:       action,
		ActorID:      &actorID,
		Metadata:     meta,
	}
	go func() {
		if err := h.repoLinkRepo.LogEvent(context.Background(), evt); err != nil {
			slog.Warn("failed to log repo link event",
				"action", action, "link_id", link.ID, "error", err)
		}
	}()
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// parseIntParam reads an integer query parameter with a default value.
func parseIntParam(r *http.Request, name string, defaultVal int) int {
	s := r.URL.Query().Get(name)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}
