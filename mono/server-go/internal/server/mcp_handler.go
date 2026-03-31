package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
	// Manual token submission is deprecated — all MCP integrations should use
	// the OAuth redirect flow (GET /oauth/{provider}/authorize).
	// This endpoint is kept for internal/service-to-service use only.
	http.Error(w, `{"error":"manual token submission is disabled — use the OAuth connect flow instead","oauth_url":"/api/v1/integrations/oauth/{provider}/authorize"}`, http.StatusGone)
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

	// Store state in Redis along with the authenticated user's identity so
	// the callback (which runs without JWT) can associate the connection.
	settingsRedirect := r.URL.Query().Get("redirect_url")
	if settingsRedirect == "" {
		// No explicit redirect — derive the frontend origin from the Referer or
		// Origin header so we redirect back to the SPA, not the API server.
		frontendBase := ""
		if ref := r.Referer(); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				frontendBase = u.Scheme + "://" + u.Host
			}
		}
		if frontendBase == "" {
			if origin := r.Header.Get("Origin"); origin != "" {
				frontendBase = origin
			}
		}
		settingsRedirect = frontendBase + "/settings?tab=mcp&connected=" + providerName
	}
	userID := claims.UserID.String()
	orgID := ""
	if claims.OrgID != uuid.Nil {
		orgID = claims.OrgID.String()
	}
	if err := h.sessionMgr.StoreOAuthStateWithUser(r.Context(), state, "mcp:"+providerName, settingsRedirect, userID, orgID); err != nil {
		slog.Error("mcp: failed to store OAuth state", "error", err)
		http.Error(w, `{"error":"failed to store state"}`, http.StatusInternalServerError)
		return
	}

	// Additional oauth2 options for specific providers.
	var authOpts []oauth2.AuthCodeOption
	switch providerName {
	case "gmail", "google_calendar", "google_drive":
		authOpts = append(authOpts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	case "ms365":
		authOpts = append(authOpts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	case "jira", "confluence":
		// Atlassian requires audience param and prompt for refresh tokens.
		authOpts = append(authOpts,
			oauth2.SetAuthURLParam("audience", "api.atlassian.com"),
			oauth2.SetAuthURLParam("prompt", "consent"),
		)
	case "gitlab":
		authOpts = append(authOpts, oauth2.SetAuthURLParam("response_type", "code"))
	case "notion":
		// Notion uses basic auth in the token exchange; no special auth URL params.
	case "salesforce":
		authOpts = append(authOpts, oauth2.SetAuthURLParam("prompt", "login consent"))
	case "hubspot":
		// HubSpot uses standard auth code flow, no extra params.
	case "figma", "linear", "asana", "pagerduty", "stripe", "datadog", "zendesk":
		// Standard OAuth2 code flow, no extra params needed.
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

	storedProvider, redirectURL, userIDStr, orgIDStr, err := h.sessionMgr.ValidateOAuthStateWithUser(ctx, state)
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

	// Parse user identity from state.
	if userIDStr == "" {
		slog.Error("mcp: OAuth state missing user_id", "provider", providerName)
		http.Error(w, `{"error":"invalid state — user identity missing, please try again"}`, http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		slog.Error("mcp: OAuth state has invalid user_id", "user_id", userIDStr, "error", err)
		http.Error(w, `{"error":"invalid state — bad user identity"}`, http.StatusBadRequest)
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

	// Create MCP connection with the obtained tokens.
	conn := &store.MCPConnection{
		ID:       uuid.New(),
		UserID:   userID,
		Provider: providerName,
		Scopes:   oauthCfg.Scopes,
	}

	if orgIDStr != "" {
		if oid, err := uuid.Parse(orgIDStr); err == nil && oid != uuid.Nil {
			conn.OrgID = &oid
		}
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
		"user_id", userID,
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

// ── Custom MCP Templates ────────────────────────────────────────────────────

// CreateCustomTemplate validates and stores a new custom MCP template.
// POST /api/v1/integrations/custom-templates
func (h *mcpHandler) CreateCustomTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var body mcp.CustomMCPTemplate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	body.CreatedBy = claims.UserID.String()

	validationErrs, err := h.svc.CreateCustomTemplate(r.Context(), &body)
	if err != nil {
		slog.Error("mcp: failed to create custom template", "error", err)
		http.Error(w, `{"error":"failed to create template"}`, http.StatusInternalServerError)
		return
	}
	if len(validationErrs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"validation_errors": validationErrs,
		})
		return
	}

	writeJSON(w, http.StatusCreated, body)
}

// ListCustomTemplates returns custom templates visible to the user.
// GET /api/v1/integrations/custom-templates
func (h *mcpHandler) ListCustomTemplates(w http.ResponseWriter, r *http.Request) {
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

	templates, err := h.svc.ListCustomTemplates(r.Context(), claims.UserID, orgID)
	if err != nil {
		slog.Error("mcp: failed to list custom templates", "error", err)
		http.Error(w, `{"error":"failed to list templates"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, templates)
}

// DeleteCustomTemplate removes a custom template.
// DELETE /api/v1/integrations/custom-templates/{templateID}
func (h *mcpHandler) DeleteCustomTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	tmplID, err := uuid.Parse(chi.URLParam(r, "templateID"))
	if err != nil {
		http.Error(w, `{"error":"invalid template id"}`, http.StatusBadRequest)
		return
	}

	if err := h.svc.DeleteCustomTemplate(r.Context(), tmplID, claims.UserID); err != nil {
		if fmt.Sprintf("%v", err) == "forbidden: only the template creator can delete it" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		slog.Error("mcp: failed to delete custom template", "error", err)
		http.Error(w, `{"error":"failed to delete template"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": tmplID.String()})
}

// ValidateCustomTemplate validates a template without saving.
// POST /api/v1/integrations/custom-templates/validate
func (h *mcpHandler) ValidateCustomTemplate(w http.ResponseWriter, r *http.Request) {
	var body mcp.CustomMCPTemplate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	errs := h.svc.ValidateCustomTemplate(&body)
	if len(errs) > 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid":             false,
			"validation_errors": errs,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid": true,
	})
}

// SimulateCustomConnection tests connectivity for a custom template.
// POST /api/v1/integrations/custom-templates/simulate
func (h *mcpHandler) SimulateCustomConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseURL    string `json:"base_url"`
		Token      string `json:"token"`
		AuthType   string `json:"auth_type"`
		AuthHeader string `json:"auth_header"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.BaseURL == "" || body.Token == "" {
		http.Error(w, `{"error":"base_url and token are required"}`, http.StatusBadRequest)
		return
	}

	result, err := h.svc.SimulateCustomConnection(r.Context(), body.BaseURL, body.Token, body.AuthType, body.AuthHeader)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
