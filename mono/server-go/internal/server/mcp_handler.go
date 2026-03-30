package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/mcp"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

type mcpHandler struct {
	svc  *mcp.Service
	repo *store.MCPRepository
}

func (h *mcpHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers := h.svc.ListProviders()
	writeJSON(w, http.StatusOK, providers)
}

func (h *mcpHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var orgID *uuid.UUID
	if claims.OrgID != uuid.Nil {
		oid := claims.OrgID
		orgID = &oid
	}

	connections, err := h.svc.ListConnections(r.Context(), claims.UserID, orgID)
	if err != nil {
		slog.Error("mcp: failed to list connections", "error", err)
		http.Error(w, `{"error":"failed to list connections"}`, http.StatusInternalServerError)
		return
	}

	safe := make([]connectionResponse, 0, len(connections))
	for _, c := range connections {
		safe = append(safe, toConnectionResponse(c))
	}

	writeJSON(w, http.StatusOK, safe)
}

func (h *mcpHandler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		Provider     string   `json:"provider"`
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		Scopes       []string `json:"scopes"`
		IsOrgLevel   bool     `json:"is_org_level"`
		ExpiresIn    int      `json:"expires_in"`
		Metadata     string   `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.Provider == "" || body.AccessToken == "" {
		http.Error(w, `{"error":"provider and access_token are required"}`, http.StatusBadRequest)
		return
	}

	conn := &store.MCPConnection{
		ID:       uuid.New(),
		UserID:   claims.UserID,
		Provider: body.Provider,
		Scopes:   body.Scopes,
		Metadata: body.Metadata,
	}

	if body.IsOrgLevel && claims.OrgID != uuid.Nil {
		oid := claims.OrgID
		conn.OrgID = &oid
		conn.IsOrgLevel = true
	}

	if body.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
		conn.ExpiresAt = &exp
	}

	if err := h.svc.CreateConnection(r.Context(), conn, body.AccessToken, body.RefreshToken); err != nil {
		slog.Error("mcp: failed to create connection", "error", err)
		http.Error(w, `{"error":"failed to create connection"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, toConnectionResponse(*conn))
}

func (h *mcpHandler) DeleteConnection(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	connID, err := uuid.Parse(chi.URLParam(r, "connectionID"))
	if err != nil {
		http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
		return
	}

	conn, err := h.svc.GetConnection(r.Context(), connID)
	if err != nil {
		http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
		return
	}

	if conn.UserID != claims.UserID {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	if err := h.svc.DeleteConnection(r.Context(), connID); err != nil {
		slog.Error("mcp: failed to delete connection", "error", err)
		http.Error(w, `{"error":"failed to delete connection"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected", "id": connID.String()})
}

func (h *mcpHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	connID, err := uuid.Parse(chi.URLParam(r, "connectionID"))
	if err != nil {
		http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
		return
	}

	result, err := h.svc.TestConnection(r.Context(), connID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *mcpHandler) GetCallLog(w http.ResponseWriter, r *http.Request) {
	connID, err := uuid.Parse(chi.URLParam(r, "connectionID"))
	if err != nil {
		http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
		return
	}

	entries, err := h.repo.ListCallLog(r.Context(), connID, 100)
	if err != nil {
		slog.Error("mcp: failed to list call log", "error", err)
		http.Error(w, `{"error":"failed to list call log"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

type connectionResponse struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	OrgID       *uuid.UUID `json:"org_id,omitempty"`
	IsOrgLevel  bool       `json:"is_org_level"`
	Provider    string     `json:"provider"`
	Status      string     `json:"status"`
	Scopes      []string   `json:"scopes"`
	Metadata    string     `json:"metadata,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ConnectedAt time.Time  `json:"connected_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func toConnectionResponse(c store.MCPConnection) connectionResponse {
	return connectionResponse{
		ID:          c.ID,
		UserID:      c.UserID,
		OrgID:       c.OrgID,
		IsOrgLevel:  c.IsOrgLevel,
		Provider:    c.Provider,
		Status:      c.Status,
		Scopes:      c.Scopes,
		Metadata:    c.Metadata,
		LastUsedAt:  c.LastUsedAt,
		ConnectedAt: c.ConnectedAt,
		ExpiresAt:   c.ExpiresAt,
		CreatedAt:   c.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
