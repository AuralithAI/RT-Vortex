package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// ── Handler aggregates all dependencies needed by API endpoints ─────────────

// Handler holds all service dependencies for API endpoints.
type Handler struct {
	UserRepo    *store.UserRepository
	RepoRepo    *store.RepositoryRepo
	ReviewRepo  *store.ReviewRepository
	OrgRepo     *store.OrgRepository
	WebhookRepo *store.WebhookRepository

	SessionMgr   *session.Manager
	JWTMgr       *auth.JWTManager
	OAuthReg     *auth.ProviderRegistry
	TokenEnc     *rtcrypto.TokenEncryptor
	LLMRegistry  *llm.Registry
	VCSRegistry  *vcs.PlatformRegistry
	EngineClient *engine.Client

	ReviewPipeline  *review.Pipeline
	IndexingService *indexing.Service
	AuditLogger     *audit.Logger
}

// ─── Auth endpoints ─────────────────────────────────────────────────────────

// ListProviders returns available OAuth2 login providers.
// GET /api/v1/auth/providers
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	names := h.OAuthReg.List()
	providers := make([]string, len(names))
	for i, n := range names {
		providers[i] = string(n)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": providers,
	})
}

// OAuthLogin redirects the user to the OAuth2 provider's authorization page.
// GET /api/v1/auth/login/{provider}
func (h *Handler) OAuthLogin(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	provider, ok := h.OAuthReg.Get(auth.ProviderName(providerName))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported OAuth provider: "+providerName)
		return
	}

	// Generate random state for CSRF protection.
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}
	state := hex.EncodeToString(stateBytes)

	// Store state in Redis.
	redirectURL := r.URL.Query().Get("redirect_url")
	if redirectURL == "" {
		redirectURL = "/"
	}
	if err := h.SessionMgr.StoreOAuthState(r.Context(), state, providerName, redirectURL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store OAuth state")
		return
	}

	authURL := provider.AuthURL(state)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// OAuthCallback handles the OAuth2 callback from the provider.
// GET /api/v1/auth/callback/{provider}
func (h *Handler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Validate state parameter.
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		writeError(w, http.StatusBadRequest, "missing state or code parameter")
		return
	}

	providerName, redirectURL, err := h.SessionMgr.ValidateOAuthState(ctx, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired OAuth state")
		return
	}

	provider, ok := h.OAuthReg.Get(auth.ProviderName(providerName))
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported provider: "+providerName)
		return
	}

	// Exchange code for token.
	token, err := provider.Exchange(ctx, code)
	if err != nil {
		slog.Error("OAuth token exchange failed", "provider", providerName, "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange OAuth code")
		return
	}

	// Fetch user info from provider.
	oauthUser, err := provider.FetchUser(ctx, token)
	if err != nil {
		slog.Error("OAuth user fetch failed", "provider", providerName, "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user from provider")
		return
	}

	// Find or create the user in our database.
	user, err := h.UserRepo.GetByEmail(ctx, oauthUser.Email)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		// Create new user.
		user = &model.User{
			Email:       oauthUser.Email,
			DisplayName: oauthUser.Name,
			AvatarURL:   oauthUser.AvatarURL,
		}
		if err := h.UserRepo.Create(ctx, user); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
		slog.Info("new user created via OAuth", "user_id", user.ID, "provider", providerName, "email", user.Email)
	}

	// Upsert OAuth identity link.
	encAccessToken, err := h.TokenEnc.Encrypt(oauthUser.AccessToken)
	if err != nil {
		slog.Error("failed to encrypt access token", "error", err)
		writeError(w, http.StatusInternalServerError, "token encryption failed")
		return
	}
	encRefreshToken, err := h.TokenEnc.Encrypt(token.RefreshToken)
	if err != nil {
		slog.Error("failed to encrypt refresh token", "error", err)
		writeError(w, http.StatusInternalServerError, "token encryption failed")
		return
	}

	identity := &model.OAuthIdentity{
		UserID:          user.ID,
		Provider:        providerName,
		ProviderUserID:  oauthUser.ProviderID,
		AccessTokenEnc:  encAccessToken,
		RefreshTokenEnc: encRefreshToken,
		Scopes:          "read",
	}
	if token.Expiry.After(time.Time{}) {
		identity.ExpiresAt = &token.Expiry
	}
	if err := h.UserRepo.UpsertOAuthIdentity(ctx, identity); err != nil {
		slog.Error("failed to upsert OAuth identity", "error", err)
	}

	// Generate JWT token pair.
	tokenPair, err := h.JWTMgr.GenerateTokenPair(user.ID, user.Email, user.DisplayName, "user", uuid.Nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	// Set tokens in secure cookies.
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    tokenPair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(tokenPair.ExpiresIn),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    tokenPair.RefreshToken,
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   7 * 24 * 60 * 60, // 7 days
	})

	// If redirectURL is set, redirect (SPA flow).
	if redirectURL != "" && redirectURL != "/" {
		h.AuditLogger.LogRequest(r, audit.ActionLogin, "user", user.ID.String(), map[string]interface{}{
			"provider": providerName, "email": user.Email,
		})
		http.Redirect(w, r, redirectURL+"?token="+tokenPair.AccessToken, http.StatusTemporaryRedirect)
		return
	}

	h.AuditLogger.LogRequest(r, audit.ActionLogin, "user", user.ID.String(), map[string]interface{}{
		"provider": providerName, "email": user.Email,
	})
	writeJSON(w, http.StatusOK, tokenPair)
}

// RefreshToken refreshes an expired JWT using a refresh token.
// POST /api/v1/auth/refresh
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := readJSON(r, &req); err != nil {
		// Also check cookie.
		cookie, cerr := r.Cookie("refresh_token")
		if cerr != nil {
			writeError(w, http.StatusBadRequest, "refresh_token required")
			return
		}
		req.RefreshToken = cookie.Value
	}

	claims, err := h.JWTMgr.ValidateToken(req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if claims.Type != auth.RefreshToken {
		writeError(w, http.StatusUnauthorized, "not a refresh token")
		return
	}

	user, err := h.UserRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}

	tokenPair, err := h.JWTMgr.GenerateTokenPair(user.ID, user.Email, user.DisplayName, "user", uuid.Nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate tokens")
		return
	}

	writeJSON(w, http.StatusOK, tokenPair)
}

// Logout invalidates the current session.
// POST /api/v1/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.AuditLogger.LogRequest(r, audit.ActionLogout, "user", "", nil)
	http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "", Path: "/api/v1/auth/refresh", HttpOnly: true, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// ─── User endpoints ─────────────────────────────────────────────────────────

// GetCurrentUser returns the authenticated user's profile.
// GET /api/v1/user/me
func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.UserRepo.GetByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// UpdateCurrentUser updates the authenticated user's profile.
// PUT /api/v1/user/me
func (h *Handler) UpdateCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := h.UserRepo.GetByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if req.DisplayName != "" {
		user.DisplayName = req.DisplayName
	}
	if req.AvatarURL != "" {
		user.AvatarURL = req.AvatarURL
	}
	if err := h.UserRepo.Update(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ─── Organization endpoints ─────────────────────────────────────────────────

// ListOrgs returns organizations the current user belongs to.
// GET /api/v1/orgs
func (h *Handler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	orgs, err := h.OrgRepo.ListByUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"organizations": orgs, "count": len(orgs)})
}

// CreateOrg creates a new organization.
// POST /api/v1/orgs
func (h *Handler) CreateOrg(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	org := &model.Organization{Name: req.Name, Slug: req.Slug, Plan: "free"}
	if err := h.OrgRepo.Create(r.Context(), org); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}
	if err := h.OrgRepo.AddMember(r.Context(), org.ID, userID, "owner"); err != nil {
		slog.Error("failed to add creator as org owner", "error", err)
	}
	h.AuditLogger.LogRequest(r, audit.ActionOrgCreate, "org", org.ID.String(), map[string]interface{}{
		"name": req.Name, "slug": req.Slug,
	})
	writeJSON(w, http.StatusCreated, org)
}

// GetOrg returns organization details.
// GET /api/v1/orgs/{orgID}
func (h *Handler) GetOrg(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org ID")
		return
	}
	org, err := h.OrgRepo.GetByID(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "organization not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

// UpdateOrg updates organization settings.
// PUT /api/v1/orgs/{orgID}
func (h *Handler) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org ID")
		return
	}
	org, err := h.OrgRepo.GetByID(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "organization not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	var req struct {
		Name     string                 `json:"name"`
		Settings map[string]interface{} `json:"settings"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != "" {
		org.Name = req.Name
	}
	if req.Settings != nil {
		org.Settings = req.Settings
	}
	if err := h.OrgRepo.Update(r.Context(), org); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update organization")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

// ListOrgMembers returns members of an organization.
// GET /api/v1/orgs/{orgID}/members
func (h *Handler) ListOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org ID")
		return
	}
	members, err := h.OrgRepo.ListMembers(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"members": members, "count": len(members)})
}

// InviteOrgMember invites a user to an organization.
// POST /api/v1/orgs/{orgID}/members
func (h *Handler) InviteOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org ID")
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	user, err := h.UserRepo.GetByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found with that email")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if err := h.OrgRepo.AddMember(r.Context(), orgID, user.ID, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	h.AuditLogger.LogRequest(r, audit.ActionOrgMemberInvite, "org", orgID.String(), map[string]interface{}{
		"invited_user_id": user.ID.String(), "email": req.Email, "role": req.Role,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "invited", "user_id": user.ID.String()})
}

// RemoveOrgMember removes a user from an organization.
// DELETE /api/v1/orgs/{orgID}/members/{userID}
func (h *Handler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org ID")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID")
		return
	}
	if err := h.OrgRepo.RemoveMember(r.Context(), orgID, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	h.AuditLogger.LogRequest(r, audit.ActionOrgMemberRemove, "org", orgID.String(), map[string]interface{}{
		"removed_user_id": userID.String(),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ─── Repository endpoints ───────────────────────────────────────────────────

// ListRepos returns repositories for an organization.
// GET /api/v1/repos
func (h *Handler) ListRepos(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	limit, offset := parsePagination(r)

	orgID := claims.OrgID
	if orgID == uuid.Nil {
		if s := r.URL.Query().Get("org_id"); s != "" {
			parsed, err := uuid.Parse(s)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid org_id")
				return
			}
			orgID = parsed
		}
	}
	if orgID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "org_id is required")
		return
	}

	repos, total, err := h.RepoRepo.ListByOrg(r.Context(), orgID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repositories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"repositories": repos, "total": total, "limit": limit, "offset": offset,
	})
}

// RegisterRepo registers a repository for review.
// POST /api/v1/repos
func (h *Handler) RegisterRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Platform      string `json:"platform"`
		Owner         string `json:"owner"`
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		CloneURL      string `json:"clone_url"`
		ExternalID    string `json:"external_id"`
		OrgID         string `json:"org_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Platform == "" || req.Owner == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "platform, owner, and name are required")
		return
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	orgID := claims.OrgID
	if req.OrgID != "" {
		parsed, err := uuid.Parse(req.OrgID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid org_id")
			return
		}
		orgID = parsed
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate webhook secret")
		return
	}
	repo := &model.Repository{
		OrgID: orgID, Platform: req.Platform, ExternalID: req.ExternalID,
		Owner: req.Owner, Name: req.Name, DefaultBranch: req.DefaultBranch,
		CloneURL: req.CloneURL, WebhookSecret: hex.EncodeToString(secretBytes),
	}
	if err := h.RepoRepo.Create(r.Context(), repo); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register repository")
		return
	}
	slog.Info("repository registered", "repo_id", repo.ID, "name", fmt.Sprintf("%s/%s", req.Owner, req.Name))
	h.AuditLogger.LogRequest(r, audit.ActionRepoCreate, "repo", repo.ID.String(), map[string]interface{}{
		"platform": req.Platform, "owner": req.Owner, "name": req.Name,
	})
	writeJSON(w, http.StatusCreated, repo)
}

// GetRepo returns repository details.
// GET /api/v1/repos/{repoID}
func (h *Handler) GetRepo(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	repo, err := h.RepoRepo.GetByID(r.Context(), repoID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

// UpdateRepo updates repository configuration.
// PUT /api/v1/repos/{repoID}
func (h *Handler) UpdateRepo(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	repo, err := h.RepoRepo.GetByID(r.Context(), repoID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	var req struct {
		DefaultBranch string                 `json:"default_branch"`
		Config        map[string]interface{} `json:"config"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DefaultBranch != "" {
		repo.DefaultBranch = req.DefaultBranch
	}
	if req.Config != nil {
		repo.Config = req.Config
	}
	if err := h.RepoRepo.Update(r.Context(), repo); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update repository")
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

// DeleteRepo removes a repository from review.
// DELETE /api/v1/repos/{repoID}
func (h *Handler) DeleteRepo(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	if err := h.IndexingService.DeleteIndex(r.Context(), repoID.String()); err != nil {
		slog.Warn("failed to delete index from engine", "error", err)
	}
	if err := h.RepoRepo.Delete(r.Context(), repoID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete repository")
		return
	}
	h.AuditLogger.LogRequest(r, audit.ActionRepoDelete, "repo", repoID.String(), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TriggerIndex triggers indexing of a repository.
// POST /api/v1/repos/{repoID}/index
func (h *Handler) TriggerIndex(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	repo, err := h.RepoRepo.GetByID(r.Context(), repoID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	jobID, err := h.IndexingService.StartFullIndex(r.Context(), indexing.FullIndexRequest{
		RepoID:   repoID.String(),
		RepoPath: repo.CloneURL,
		Config: engine.IndexConfig{
			MaxFileSizeKB: 512, ChunkSize: 1024, ChunkOverlap: 128, EnableASTChunking: true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start indexing")
		return
	}
	slog.Info("indexing triggered", "repo_id", repoID, "job_id", jobID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "status": "accepted"})
}

// GetIndexStatus returns the indexing status of a repository.
// GET /api/v1/repos/{repoID}/index/status
func (h *Handler) GetIndexStatus(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	if jobID := r.URL.Query().Get("job_id"); jobID != "" {
		status, ok := h.IndexingService.GetJobStatus(jobID)
		if !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}
	stats, err := h.IndexingService.GetIndexInfo(r.Context(), repoID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get index info")
		return
	}
	if stats == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"indexed": false, "repo_id": repoID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"indexed": true, "repo_id": repoID, "stats": stats})
}

// ─── Review endpoints ───────────────────────────────────────────────────────

// ListReviews returns reviews for a repository.
// GET /api/v1/reviews
func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
	repoIDStr := r.URL.Query().Get("repo_id")
	if repoIDStr == "" {
		writeError(w, http.StatusBadRequest, "repo_id query parameter is required")
		return
	}
	repoID, err := uuid.Parse(repoIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo_id")
		return
	}
	limit, offset := parsePagination(r)
	reviews, total, err := h.ReviewRepo.ListByRepo(r.Context(), repoID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reviews")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reviews": reviews, "total": total, "limit": limit, "offset": offset,
	})
}

// TriggerReview manually triggers a review.
// POST /api/v1/reviews
func (h *Handler) TriggerReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		RepoID   string `json:"repo_id"`
		PRNumber int    `json:"pr_number"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RepoID == "" || req.PRNumber == 0 {
		writeError(w, http.StatusBadRequest, "repo_id and pr_number are required")
		return
	}
	repoID, err := uuid.Parse(req.RepoID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo_id")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		result, err := h.ReviewPipeline.Execute(ctx, review.Request{
			RepoID: repoID, PRNumber: req.PRNumber, TriggeredBy: userID,
		})
		if err != nil {
			slog.Error("review pipeline failed", "error", err, "repo_id", repoID, "pr", req.PRNumber)
			return
		}
		slog.Info("review completed", "review_id", result.ReviewID, "comments", result.CommentsCount)
	}()
	h.AuditLogger.LogRequest(r, audit.ActionReviewTrigger, "review", repoID.String(), map[string]interface{}{
		"pr_number": req.PRNumber, "source": "manual",
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "message": "Review has been queued"})
}

// GetReview returns review details.
// GET /api/v1/reviews/{reviewID}
func (h *Handler) GetReview(w http.ResponseWriter, r *http.Request) {
	reviewID, err := uuid.Parse(chi.URLParam(r, "reviewID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid review ID")
		return
	}
	rev, err := h.ReviewRepo.GetByID(r.Context(), reviewID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "review not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

// GetReviewComments returns comments for a review.
// GET /api/v1/reviews/{reviewID}/comments
func (h *Handler) GetReviewComments(w http.ResponseWriter, r *http.Request) {
	reviewID, err := uuid.Parse(chi.URLParam(r, "reviewID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid review ID")
		return
	}
	comments, err := h.ReviewRepo.ListCommentsByReview(r.Context(), reviewID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"comments": comments, "count": len(comments)})
}

// ─── LLM endpoints ──────────────────────────────────────────────────────────

// ListLLMProviders returns configured LLM providers.
// GET /api/v1/llm/providers
func (h *Handler) ListLLMProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providerNames := h.LLMRegistry.ListProviders()
	type providerInfo struct {
		Name    string `json:"name"`
		Healthy bool   `json:"healthy"`
	}
	providers := make([]providerInfo, 0, len(providerNames))
	for _, name := range providerNames {
		p, ok := h.LLMRegistry.Get(name)
		healthy := false
		if ok {
			healthy = p.Healthy(ctx)
		}
		providers = append(providers, providerInfo{Name: name, Healthy: healthy})
	}
	primary, _ := h.LLMRegistry.Primary()
	primaryName := ""
	if primary != nil {
		primaryName = primary.Name()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": providers, "primary": primaryName, "count": len(providers),
	})
}

// TestLLMProvider tests connectivity to an LLM provider.
// POST /api/v1/llm/providers/test
func (h *Handler) TestLLMProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	provider, ok := h.LLMRegistry.Get(req.Provider)
	if !ok {
		writeError(w, http.StatusNotFound, "provider not found: "+req.Provider)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := provider.Complete(ctx, &llm.CompletionRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "Respond with exactly: OK"}},
		MaxTokens: 10, Temperature: 0,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": req.Provider, "healthy": false, "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": req.Provider, "healthy": true, "model": resp.Model,
		"response": resp.Content, "usage": resp.Usage,
	})
}

// ─── Admin endpoints ────────────────────────────────────────────────────────

// GetSystemStats returns system-wide statistics.
// GET /api/v1/admin/stats
func (h *Handler) GetSystemStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h.AuditLogger.LogRequest(r, audit.ActionAdminStats, "system", "", nil)
	var engineDiag *engine.DiagnosticsResult
	diag, err := h.EngineClient.GetDiagnostics(ctx, true, true)
	if err == nil {
		engineDiag = diag
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"engine": engineDiag, "llm_providers": h.LLMRegistry.ListProviders(),
		"vcs_platforms": h.VCSRegistry.List(),
	})
}

// GetDetailedHealth returns detailed health for all subsystems.
// GET /api/v1/admin/health/detailed
func (h *Handler) GetDetailedHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	health := map[string]interface{}{}
	engineHealth, err := h.EngineClient.HealthCheck(ctx)
	if err != nil {
		health["engine"] = map[string]interface{}{"healthy": false, "error": err.Error()}
	} else {
		health["engine"] = engineHealth
	}
	llmHealth := map[string]bool{}
	for _, name := range h.LLMRegistry.ListProviders() {
		p, ok := h.LLMRegistry.Get(name)
		if ok {
			llmHealth[name] = p.Healthy(ctx)
		}
	}
	health["llm_providers"] = llmHealth
	writeJSON(w, http.StatusOK, health)
}

// ─── Webhook endpoints ──────────────────────────────────────────────────────

// HandleGitHubWebhook processes incoming GitHub webhook events.
// POST /api/v1/webhooks/github
func (h *Handler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	evt := &model.WebhookEvent{Platform: "github", EventType: r.Header.Get("X-GitHub-Event"), Payload: body}
	_ = h.WebhookRepo.RecordEvent(ctx, evt)

	signature := r.Header.Get("X-Hub-Signature-256")
	ghPlatform, ok := h.VCSRegistry.Get(vcs.PlatformGitHub)
	if !ok {
		writeError(w, http.StatusInternalServerError, "GitHub platform not configured")
		return
	}
	if !ghPlatform.ValidateWebhookSignature(body, signature) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	if event != "pull_request" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "event": event})
		return
	}
	payload, err := parseGitHubPRPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse payload")
		return
	}
	if payload.Action != "opened" && payload.Action != "synchronize" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "action": payload.Action})
		return
	}
	repo, err := h.RepoRepo.GetByPlatformAndExternalID(ctx, "github", fmt.Sprintf("%d", payload.Repository.ID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not registered"})
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	go func() {
		reviewCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		result, err := h.ReviewPipeline.Execute(reviewCtx, review.Request{
			RepoID: repo.ID, PRNumber: payload.PullRequest.Number, TriggeredBy: uuid.Nil,
		})
		if err != nil {
			slog.Error("webhook review failed", "error", err)
			return
		}
		slog.Info("webhook review completed", "review_id", result.ReviewID, "comments", result.CommentsCount)
	}()
	_ = h.WebhookRepo.MarkProcessed(ctx, evt.ID, "")
	h.AuditLogger.LogRequest(r, audit.ActionWebhookReceived, "webhook", repo.ID.String(), map[string]interface{}{
		"platform": "github", "event": "pull_request", "pr_number": payload.PullRequest.Number,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// HandleGitLabWebhook processes incoming GitLab webhook events.
// POST /api/v1/webhooks/gitlab
func (h *Handler) HandleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	evt := &model.WebhookEvent{Platform: "gitlab", EventType: r.Header.Get("X-Gitlab-Event"), Payload: body}
	_ = h.WebhookRepo.RecordEvent(ctx, evt)

	token := r.Header.Get("X-Gitlab-Token")
	glPlatform, ok := h.VCSRegistry.Get(vcs.PlatformGitLab)
	if !ok {
		writeError(w, http.StatusInternalServerError, "GitLab platform not configured")
		return
	}
	if !glPlatform.ValidateWebhookSignature(body, token) {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}
	eventType := r.Header.Get("X-Gitlab-Event")
	if eventType != "Merge Request Hook" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "event": eventType})
		return
	}
	payload, err := parseGitLabMRPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse payload")
		return
	}
	if payload.ObjectAttributes.Action != "open" && payload.ObjectAttributes.Action != "update" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "action": payload.ObjectAttributes.Action})
		return
	}
	repo, err := h.RepoRepo.GetByPlatformAndExternalID(ctx, "gitlab", fmt.Sprintf("%d", payload.Project.ID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not registered"})
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	go func() {
		reviewCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, err := h.ReviewPipeline.Execute(reviewCtx, review.Request{
			RepoID: repo.ID, PRNumber: payload.ObjectAttributes.IID, TriggeredBy: uuid.Nil,
		})
		if err != nil {
			slog.Error("gitlab webhook review failed", "error", err)
		}
	}()
	_ = h.WebhookRepo.MarkProcessed(ctx, evt.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// HandleBitbucketWebhook processes incoming Bitbucket webhook events.
// POST /api/v1/webhooks/bitbucket
func (h *Handler) HandleBitbucketWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	eventKey := r.Header.Get("X-Event-Key")
	evt := &model.WebhookEvent{Platform: "bitbucket", EventType: eventKey, Payload: body}
	_ = h.WebhookRepo.RecordEvent(ctx, evt)
	if eventKey != "pullrequest:created" && eventKey != "pullrequest:updated" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "event": eventKey})
		return
	}
	payload, err := parseBitbucketPRPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse payload")
		return
	}
	repo, err := h.RepoRepo.GetByPlatformAndExternalID(ctx, "bitbucket", payload.Repository.UUID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not registered"})
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	go func() {
		reviewCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, err := h.ReviewPipeline.Execute(reviewCtx, review.Request{
			RepoID: repo.ID, PRNumber: payload.PullRequest.ID, TriggeredBy: uuid.Nil,
		})
		if err != nil {
			slog.Error("bitbucket webhook review failed", "error", err)
		}
	}()
	_ = h.WebhookRepo.MarkProcessed(ctx, evt.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// HandleAzureDevOpsWebhook processes incoming Azure DevOps webhook events.
// POST /api/v1/webhooks/azure-devops
func (h *Handler) HandleAzureDevOpsWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	evt := &model.WebhookEvent{Platform: "azure_devops", EventType: "pull_request", Payload: body}
	_ = h.WebhookRepo.RecordEvent(ctx, evt)
	payload, err := parseAzureDevOpsPRPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse payload")
		return
	}
	if payload.EventType != "git.pullrequest.created" && payload.EventType != "git.pullrequest.updated" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "event": payload.EventType})
		return
	}
	repo, err := h.RepoRepo.GetByPlatformAndExternalID(ctx, "azure_devops", payload.Resource.Repository.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not registered"})
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	go func() {
		reviewCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, err := h.ReviewPipeline.Execute(reviewCtx, review.Request{
			RepoID: repo.ID, PRNumber: payload.Resource.PullRequestID, TriggeredBy: uuid.Nil,
		})
		if err != nil {
			slog.Error("azure devops webhook review failed", "error", err)
		}
	}()
	_ = h.WebhookRepo.MarkProcessed(ctx, evt.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// ─── Pagination Helper ──────────────────────────────────────────────────────

func parsePagination(r *http.Request) (limit, offset int) {
	limit, offset = 20, 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}
