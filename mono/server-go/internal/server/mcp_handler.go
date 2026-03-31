package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/config"
	"github.com/AuralithAI/rtvortex-server/internal/mcp"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

type mcpHandler struct {
	svc        *mcp.Service
	repo       *store.MCPRepository
	sessionMgr *session.Manager
	mcpCfg     config.MCPConfig
	serverBase string
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

// ── MCP OAuth Flow ──────────────────────────────────────────────────────────

// InitiateOAuth redirects the user to the MCP provider's OAuth consent screen.
// GET /api/v1/integrations/oauth/{provider}/authorize
func (h *mcpHandler) InitiateOAuth(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	providerName := chi.URLParam(r, "provider")
	oauthCfg, exists := h.mcpCfg.OAuthProviders[providerName]
	if !exists {
		http.Error(w, `{"error":"OAuth not configured for this provider"}`, http.StatusBadRequest)
		return
	}

	// Build oauth2.Config.
	redirectURL := fmt.Sprintf("%s/api/v1/integrations/oauth/%s/callback", h.serverBase, providerName)
	oc := &oauth2.Config{
		ClientID:     oauthCfg.ClientID,
		ClientSecret: oauthCfg.ClientSecret,
		Scopes:       oauthCfg.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  oauthCfg.AuthURL,
			TokenURL: oauthCfg.TokenURL,
		},
		RedirectURL: redirectURL,
	}

	// Generate CSRF state.
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		slog.Error("mcp: failed to generate OAuth state", "error", err)
		http.Error(w, `{"error":"failed to generate state"}`, http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	// Store state in Redis. We re-use the session manager's OAuth state mechanism.
	// redirect_url points back to the settings page.
	settingsRedirect := r.URL.Query().Get("redirect_url")
	if settingsRedirect == "" {
		settingsRedirect = "/settings?tab=mcp&connected=" + providerName
	}
	if err := h.sessionMgr.StoreOAuthState(r.Context(), state, "mcp:"+providerName, settingsRedirect); err != nil {
		slog.Error("mcp: failed to store OAuth state", "error", err)
		http.Error(w, `{"error":"failed to store state"}`, http.StatusInternalServerError)
		return
	}

	// Additional oauth2 options for specific providers.
	var authOpts []oauth2.AuthCodeOption
	if providerName == "gmail" {
		authOpts = append(authOpts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	}
	if providerName == "ms365" {
		authOpts = append(authOpts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	}

	authURL := oc.AuthCodeURL(state, authOpts...)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// OAuthCallback handles the callback from the MCP provider OAuth consent screen.
// GET /api/v1/integrations/oauth/{provider}/callback
func (h *mcpHandler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Validate state + code.
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, `{"error":"missing state or code parameter"}`, http.StatusBadRequest)
		return
	}

	storedProvider, redirectURL, err := h.sessionMgr.ValidateOAuthState(ctx, state)
	if err != nil {
		slog.Error("mcp: invalid OAuth state", "error", err)
		http.Error(w, `{"error":"invalid or expired OAuth state"}`, http.StatusBadRequest)
		return
	}

	// storedProvider is "mcp:{provider}", extract the provider name.
	providerName := chi.URLParam(r, "provider")
	expectedKey := "mcp:" + providerName
	if storedProvider != expectedKey {
		http.Error(w, `{"error":"state/provider mismatch"}`, http.StatusBadRequest)
		return
	}

	oauthCfg, exists := h.mcpCfg.OAuthProviders[providerName]
	if !exists {
		http.Error(w, `{"error":"OAuth not configured for this provider"}`, http.StatusBadRequest)
		return
	}

	// Build the same oauth2.Config.
	callbackURL := fmt.Sprintf("%s/api/v1/integrations/oauth/%s/callback", h.serverBase, providerName)
	oc := &oauth2.Config{
		ClientID:     oauthCfg.ClientID,
		ClientSecret: oauthCfg.ClientSecret,
		Scopes:       oauthCfg.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  oauthCfg.AuthURL,
			TokenURL: oauthCfg.TokenURL,
		},
		RedirectURL: callbackURL,
	}

	// Exchange code for token.
	token, err := oc.Exchange(ctx, code)
	if err != nil {
		slog.Error("mcp: OAuth code exchange failed", "provider", providerName, "error", err)
		http.Error(w, `{"error":"failed to exchange OAuth code for token"}`, http.StatusBadGateway)
		return
	}

	// Get user claims from JWT to associate the connection.
	claims, ok := auth.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		// For the callback, the user may not have a JWT cookie since the browser
		// navigated to the provider. Try to get user from session cookie instead.
		// We'll fall through and try; if claims is nil we error.
		http.Error(w, `{"error":"unauthorized — please log in and try again"}`, http.StatusUnauthorized)
		return
	}

	// Create MCP connection with the obtained tokens.
	conn := &store.MCPConnection{
		ID:       uuid.New(),
		UserID:   claims.UserID,
		Provider: providerName,
		Scopes:   oauthCfg.Scopes,
	}

	if claims.OrgID != uuid.Nil {
		oid := claims.OrgID
		conn.OrgID = &oid
	}

	if !token.Expiry.IsZero() {
		exp := token.Expiry
		conn.ExpiresAt = &exp
	}

	if err := h.svc.CreateConnection(ctx, conn, token.AccessToken, token.RefreshToken); err != nil {
		slog.Error("mcp: failed to create OAuth connection", "provider", providerName, "error", err)
		http.Error(w, `{"error":"failed to store connection"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("mcp: OAuth connection created",
		"provider", providerName,
		"user_id", claims.UserID,
		"connection_id", conn.ID,
	)

	// Redirect the user back to settings.
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// OAuthStatus returns which MCP providers have OAuth configured (for the UI to show OAuth vs manual).
// GET /api/v1/integrations/oauth/status
func (h *mcpHandler) OAuthStatus(w http.ResponseWriter, r *http.Request) {
	status := make(map[string]bool)
	for name := range h.mcpCfg.OAuthProviders {
		status[name] = true
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"oauth_enabled": status,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
