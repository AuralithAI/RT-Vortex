package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/chat"
	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/prsync"
	"github.com/AuralithAI/rtvortex-server/internal/quota"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/validation"
	"github.com/AuralithAI/rtvortex-server/internal/vault"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
	"github.com/AuralithAI/rtvortex-server/internal/webhookq"
)

// ── Handler aggregates all dependencies needed by API endpoints ─────────────

// Handler holds all service dependencies for API endpoints.
type Handler struct {
	UserRepo       *store.UserRepository
	RepoRepo       *store.RepositoryRepo
	RepoMemberRepo *store.RepoMemberRepo
	ReviewRepo     *store.ReviewRepository
	OrgRepo        *store.OrgRepository
	WebhookRepo    *store.WebhookRepository
	PRRepo         *store.PullRequestRepo

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
	QuotaEnforcer   *quota.Enforcer
	DeliveryRepo    *webhookq.Repository
	PRSyncWorker    *prsync.Worker
	ChatRepo        *store.ChatRepository
	ChatService     *chat.Service
	Vault           *vault.FileVault       // shared file vault — user-scoped via vault token
	VCSPlatformRepo *store.VCSPlatformRepo // per-user VCS platform config (URLs, usernames)

	// Runtime embedding configuration — guarded by embedMu.
	embedMu     sync.RWMutex
	embedConfig embeddingRuntimeConfig
}

// embeddingRuntimeConfig holds the user-selected embedding configuration.
// It defaults to LOCAL_ONNX (built-in MiniLM-L6-v2).
type embeddingRuntimeConfig struct {
	UseBuiltin bool   `json:"use_builtin"` // true → LOCAL_ONNX
	Provider   string `json:"provider"`    // "openai", "cohere", "voyage" (only when UseBuiltin=false)
	Endpoint   string `json:"endpoint"`    // embedding API URL
	Model      string `json:"model"`       // e.g. "text-embedding-3-small"
	Dimensions uint32 `json:"dimensions"`  // e.g. 1536
	APIKey     string `json:"-"`           // never serialised
}

// DefaultEmbeddingConfig returns the default built-in embedding configuration.
func DefaultEmbeddingConfig() embeddingRuntimeConfig {
	return embeddingRuntimeConfig{
		UseBuiltin: true,
		Provider:   "",
		Endpoint:   "",
		Model:      "",
		Dimensions: 384,
	}
}

func init() {
	// Prevent "imported and not used" for json and sync:
	_ = json.Marshal
	_ = (*sync.Mutex)(nil)
}

// ─── Auth endpoints ─────────────────────────────────────────────────────────

// ListProviders returns available OAuth2 login providers.
// GET /api/v1/auth/providers
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	names := h.OAuthReg.List()
	type providerInfo struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Enabled     bool   `json:"enabled"`
	}
	displayNames := map[string]string{
		"github":    "GitHub",
		"gitlab":    "GitLab",
		"google":    "Google",
		"microsoft": "Microsoft",
		"bitbucket": "Bitbucket",
		"linkedin":  "LinkedIn",
		"apple":     "Apple",
		"x":         "X",
	}
	providers := make([]providerInfo, len(names))
	for i, n := range names {
		name := string(n)
		dn := displayNames[name]
		if dn == "" {
			dn = name
		}
		providers[i] = providerInfo{Name: name, DisplayName: dn, Enabled: true}
	}
	// Prevent browsers and proxies from caching the provider list so that
	// subsequent loads always hit the Go server and see the real set.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	writeJSON(w, http.StatusOK, providers)
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
		// Use proper query separator: & if URL already contains ?, else ?
		sep := "?"
		for _, ch := range redirectURL {
			if ch == '?' {
				sep = "&"
				break
			}
		}
		redirectTarget := redirectURL + sep + "token=" + tokenPair.AccessToken + "&refresh_token=" + tokenPair.RefreshToken
		http.Redirect(w, r, redirectTarget, http.StatusTemporaryRedirect)
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
	var req validation.UpdateUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
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
	if req.Email != "" {
		user.Email = req.Email
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
	limit, offset := parsePagination(r)
	orgs, total, err := h.OrgRepo.ListByUser(r.Context(), userID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": orgs, "total": total, "limit": limit, "offset": offset,
		"has_more": offset+limit < total,
	})
}

// CreateOrg creates a new organization.
// POST /api/v1/orgs
func (h *Handler) CreateOrg(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req validation.CreateOrgRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
		return
	}
	org := &model.Organization{Name: req.Name, Slug: req.Slug, Plan: "free"}
	if err := h.OrgRepo.Create(r.Context(), org); err != nil {
		slog.Error("failed to create organization", "error", err)
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
	var req validation.UpdateOrgRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
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
	limit, offset := parsePagination(r)
	members, total, err := h.OrgRepo.ListMembers(r.Context(), orgID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members, "total": total, "limit": limit, "offset": offset,
	})
}

// InviteOrgMember invites a user to an organization.
// POST /api/v1/orgs/{orgID}/members
func (h *Handler) InviteOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org ID")
		return
	}
	var req validation.InviteMemberRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	// Check member quota.
	if h.QuotaEnforcer != nil {
		org, orgErr := h.OrgRepo.GetByID(r.Context(), orgID)
		if orgErr == nil {
			result, qErr := h.QuotaEnforcer.CheckMemberQuota(r.Context(), orgID, org.Plan)
			if qErr == nil && !result.Allowed {
				writeError(w, http.StatusForbidden, result.Reason)
				return
			}
		}
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

// ListRepos returns repositories for an organization, or all user-accessible repos.
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

	var repos []*model.Repository
	var total int
	var err error

	if orgID != uuid.Nil {
		repos, total, err = h.RepoRepo.ListByOrg(r.Context(), orgID, limit, offset)
	} else {
		// No org filter — list all repos accessible by the user via org memberships.
		repos, total, err = h.RepoRepo.ListByUser(r.Context(), claims.UserID, limit, offset)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repositories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": repos, "total": total, "limit": limit, "offset": offset,
		"has_more": offset+limit < total,
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
	var req validation.RegisterRepoRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Auto-extract owner and repo name from clone_url when not provided.
	if req.CloneURL != "" && (req.Owner == "" || req.Name == "") {
		if parsed, err := parseRepoURL(req.CloneURL); err == nil {
			if req.Owner == "" {
				req.Owner = parsed.owner
			}
			if req.Name == "" {
				req.Name = parsed.name
			}
		}
	}

	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
		return
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}

	// Resolve org: use explicit OrgID, then JWT claim, then auto-create a personal org.
	orgID := claims.OrgID
	if req.OrgID != "" {
		parsed, err := uuid.Parse(req.OrgID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid org_id")
			return
		}
		orgID = parsed
	}
	if orgID == uuid.Nil {
		// Find or create a personal org for the user.
		userID := claims.UserID
		orgs, _, err := h.OrgRepo.ListByUser(r.Context(), userID, 1, 0)
		if err == nil && len(orgs) > 0 {
			orgID = orgs[0].ID
		} else {
			// Auto-create a personal org.
			// Derive a URL-safe slug from the email (replace @ and . with hyphens).
			slug := strings.ToLower(claims.Email)
			slug = strings.NewReplacer("@", "-", ".", "-").Replace(slug)
			personalOrg := &model.Organization{Name: claims.Email, Slug: slug, Plan: "free"}
			if createErr := h.OrgRepo.Create(r.Context(), personalOrg); createErr != nil {
				slog.Error("failed to create personal organization", "error", createErr)
				writeError(w, http.StatusInternalServerError, "failed to create personal organization")
				return
			}
			_ = h.OrgRepo.AddMember(r.Context(), personalOrg.ID, userID, "owner")
			orgID = personalOrg.ID
			slog.Info("auto-created personal org", "org_id", orgID, "user_id", userID)
		}
	}

	// Check repo quota.
	if h.QuotaEnforcer != nil && orgID != uuid.Nil {
		org, orgErr := h.OrgRepo.GetByID(r.Context(), orgID)
		if orgErr == nil {
			result, qErr := h.QuotaEnforcer.CheckRepoQuota(r.Context(), orgID, org.Plan)
			if qErr == nil && !result.Allowed {
				writeError(w, http.StatusForbidden, result.Reason)
				return
			}
		}
	}
	// Auto-generate external_id from owner/name when not provided.
	if req.ExternalID == "" {
		req.ExternalID = fmt.Sprintf("%s/%s", req.Owner, req.Name)
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
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") {
			writeError(w, http.StatusConflict, fmt.Sprintf("Repository %s/%s is already connected", req.Owner, req.Name))
			return
		}
		slog.Error("failed to register repository", "error", err)
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
	var req validation.UpdateRepoRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
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

// ─── Repository Member Access ────────────────────────────────────────────────

// ListRepoMembers returns users with explicit access to a repository.
// GET /api/v1/repos/{repoID}/members
func (h *Handler) ListRepoMembers(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	members, err := h.RepoMemberRepo.ListMembers(r.Context(), repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repo members")
		return
	}
	if members == nil {
		members = []*store.RepoMemberInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":  members,
		"total": len(members),
	})
}

// AddRepoMember grants a user access to a repository.
// POST /api/v1/repos/{repoID}/members
func (h *Handler) AddRepoMember(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
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
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.Role == "" {
		req.Role = "viewer"
	}
	validRoles := map[string]bool{"admin": true, "reviewer": true, "viewer": true}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "role must be admin, reviewer, or viewer")
		return
	}

	// Resolve user by email.
	user, err := h.UserRepo.GetByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found with that email")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up user")
		return
	}

	if err := h.RepoMemberRepo.AddMember(r.Context(), repoID, user.ID, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add repo member")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"repo_id": repoID,
		"user_id": user.ID,
		"role":    req.Role,
		"status":  "added",
	})
}

// RemoveRepoMember revokes a user's access to a repository.
// DELETE /api/v1/repos/{repoID}/members/{userID}
func (h *Handler) RemoveRepoMember(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID")
		return
	}
	if err := h.RepoMemberRepo.RemoveMember(r.Context(), repoID, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to remove repo member")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
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
	// Check indexing quota.
	if h.QuotaEnforcer != nil {
		org, orgErr := h.OrgRepo.GetByID(r.Context(), repo.OrgID)
		if orgErr == nil {
			result := h.QuotaEnforcer.CheckIndexingAllowed(org.Plan)
			if !result.Allowed {
				writeError(w, http.StatusForbidden, result.Reason)
				return
			}
		}
	}
	// Build engine index config with the user's current embedding choice.
	h.embedMu.RLock()
	ec := h.embedConfig
	h.embedMu.RUnlock()

	engineCfg := engine.IndexConfig{
		MaxFileSizeKB: 512, ChunkSize: 1024, ChunkOverlap: 128, EnableASTChunking: true,
	}
	if !ec.UseBuiltin && ec.Provider != "" {
		engineCfg.EmbeddingProvider = "HTTP"
		engineCfg.EmbeddingEndpoint = ec.Endpoint
		engineCfg.EmbeddingModel = ec.Model
		engineCfg.EmbeddingDimensions = ec.Dimensions
		engineCfg.EmbeddingAPIKey = ec.APIKey
	}

	jobID, err := h.IndexingService.StartFullIndex(r.Context(), indexing.FullIndexRequest{
		RepoID:   repoID.String(),
		RepoPath: repo.CloneURL,
		Config:   engineCfg,
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

	// If a specific job_id is requested, return that job's status
	if jobID := r.URL.Query().Get("job_id"); jobID != "" {
		status, ok := h.IndexingService.GetJobStatus(jobID)
		if !ok {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	// Check for active indexing job on this repo
	if activeJob, ok := h.IndexingService.GetActiveJobForRepo(repoID.String()); ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"repo_id":         repoID,
			"indexed":         false,
			"status":          "indexing",
			"job_id":          activeJob.JobID,
			"progress":        activeJob.Progress,
			"phase":           activeJob.Phase,
			"message":         activeJob.Message,
			"files_processed": activeJob.FilesProcessed,
			"files_total":     activeJob.FilesTotal,
			"current_file":    activeJob.CurrentFile,
			"eta_seconds":     activeJob.ETASeconds,
			"started_at":      activeJob.StartedAt,
			"error":           activeJob.Error,
		})
		return
	}

	// No active job — check engine for existing index stats
	stats, err := h.IndexingService.GetIndexInfo(r.Context(), repoID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get index info")
		return
	}
	if stats == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"repo_id": repoID,
			"indexed": false,
			"status":  "idle",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"repo_id":  repoID,
		"indexed":  true,
		"status":   "completed",
		"progress": 100,
		"stats":    stats,
	})
}

// ─── Review endpoints ───────────────────────────────────────────────────────

// ListReviews returns reviews for a repository, or all user-accessible reviews.
// GET /api/v1/reviews
func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	var reviews []*model.Review
	var total int
	var err error

	repoIDStr := r.URL.Query().Get("repo_id")
	if repoIDStr != "" {
		var repoID uuid.UUID
		repoID, err = uuid.Parse(repoIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid repo_id")
			return
		}
		reviews, total, err = h.ReviewRepo.ListByRepo(r.Context(), repoID, limit, offset)
	} else {
		// No repo filter — list all reviews accessible by the user.
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		reviews, total, err = h.ReviewRepo.ListByUser(r.Context(), userID, limit, offset)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reviews")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": reviews, "total": total, "limit": limit, "offset": offset,
		"has_more": offset+limit < total,
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
	var req validation.TriggerReviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ve := req.Validate(); ve != nil {
		writeValidationError(w, ve)
		return
	}
	repoID, err := uuid.Parse(req.RepoID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo_id")
		return
	}
	// Check review quota.
	if h.QuotaEnforcer != nil {
		repo, repoErr := h.RepoRepo.GetByID(r.Context(), repoID)
		if repoErr == nil {
			org, orgErr := h.OrgRepo.GetByID(r.Context(), repo.OrgID)
			if orgErr == nil {
				result, qErr := h.QuotaEnforcer.CheckReviewQuota(r.Context(), org.ID, org.Plan)
				if qErr == nil && !result.Allowed {
					writeError(w, http.StatusForbidden, result.Reason)
					return
				}
			}
		}
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

// ListLLMProviders returns all pre-registered LLM providers with rich metadata.
// GET /api/v1/llm/providers
func (h *Handler) ListLLMProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providerNames := h.LLMRegistry.ListProviders()

	type providerInfo struct {
		Name         string   `json:"name"`
		DisplayName  string   `json:"display_name"`
		BaseURL      string   `json:"base_url"`
		DefaultModel string   `json:"default_model"`
		Configured   bool     `json:"configured"`
		RequiresKey  bool     `json:"requires_key"`
		Healthy      bool     `json:"healthy"`
		Models       []string `json:"models"`
	}

	providers := make([]providerInfo, 0, len(providerNames))
	for _, name := range providerNames {
		p, ok := h.LLMRegistry.Get(name)
		meta, hasMeta := h.LLMRegistry.GetMeta(name)

		info := providerInfo{Name: name}
		if hasMeta {
			info.DisplayName = meta.DisplayName
			info.BaseURL = meta.BaseURL
			info.DefaultModel = meta.DefaultModel
			info.Configured = meta.Configured
			info.RequiresKey = meta.RequiresKey
		}

		if ok {
			info.Healthy = p.Healthy(ctx)
			models, err := p.ListModels(ctx)
			if err == nil {
				info.Models = models
			}
		}

		providers = append(providers, info)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": providers,
		"primary":   h.LLMRegistry.PrimaryName(),
		"count":     len(providers),
	})
}

// ConfigureLLMProvider updates the API key, model, and/or base URL for a provider at runtime.
// PUT /api/v1/llm/providers/{provider}
func (h *Handler) ConfigureLLMProvider(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		writeError(w, http.StatusBadRequest, "provider name required")
		return
	}

	var req struct {
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, ok := h.LLMRegistry.Get(providerName); !ok {
		writeError(w, http.StatusNotFound, "provider not found: "+providerName)
		return
	}

	if req.APIKey != "" {
		if !h.LLMRegistry.UpdateAPIKey(providerName, req.APIKey) {
			writeError(w, http.StatusInternalServerError, "failed to update API key")
			return
		}
	}
	if req.Model != "" {
		h.LLMRegistry.UpdateModel(providerName, req.Model)
	}
	if req.BaseURL != "" {
		h.LLMRegistry.UpdateBaseURL(providerName, req.BaseURL)
	}

	// Re-check health after configuration.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p, _ := h.LLMRegistry.Get(providerName)
	healthy := p.Healthy(ctx)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider":   providerName,
		"configured": true,
		"healthy":    healthy,
	})
}

// SetPrimaryLLMProvider changes the primary LLM provider.
// PUT /api/v1/llm/primary
func (h *Handler) SetPrimaryLLMProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := h.LLMRegistry.Get(req.Provider); !ok {
		writeError(w, http.StatusNotFound, "provider not found: "+req.Provider)
		return
	}
	h.LLMRegistry.SetPrimary(req.Provider)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"primary": req.Provider,
	})
}

// TestLLMProvider tests connectivity to an LLM provider.
// POST /api/v1/llm/providers/test
// Accepts optional api_key / model / base_url so the user can test before
// clicking "Save". If provided, the registry is updated first so the
// provider instance uses the new credentials.
func (h *Handler) TestLLMProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
		BaseURL  string `json:"base_url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, ok := h.LLMRegistry.Get(req.Provider); !ok {
		writeError(w, http.StatusNotFound, "provider not found: "+req.Provider)
		return
	}

	// Apply any unsaved config so the test uses the latest values from the UI.
	if req.APIKey != "" {
		h.LLMRegistry.UpdateAPIKey(req.Provider, req.APIKey)
	}
	if req.Model != "" {
		h.LLMRegistry.UpdateModel(req.Provider, req.Model)
	}
	if req.BaseURL != "" {
		h.LLMRegistry.UpdateBaseURL(req.Provider, req.BaseURL)
	}

	provider, _ := h.LLMRegistry.Get(req.Provider)

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

// ─── Embeddings endpoints ───────────────────────────────────────────────────

// GetEmbeddingsConfig returns the current embeddings configuration.
// GET /api/v1/embeddings/config
func (h *Handler) GetEmbeddingsConfig(w http.ResponseWriter, r *http.Request) {
	h.embedMu.RLock()
	ec := h.embedConfig
	h.embedMu.RUnlock()

	// If embedConfig is zero-valued, initialise with the default.
	if !ec.UseBuiltin && ec.Provider == "" && ec.Dimensions == 0 {
		ec = DefaultEmbeddingConfig()
	}

	// Check which external providers are configured based on the LLM registry API keys.
	externalProviders := []map[string]interface{}{
		{
			"name": "openai", "display_name": "OpenAI Embeddings",
			"model": "text-embedding-3-small", "dimensions": 1536,
			"endpoint":     "https://api.openai.com/v1/embeddings",
			"configured":   h.isLLMKeySet("openai"),
			"requires_key": true,
		},
		{
			"name": "cohere", "display_name": "Cohere Embed",
			"model": "embed-english-v3.0", "dimensions": 1024,
			"endpoint":     "https://api.cohere.ai/v1/embed",
			"configured":   false,
			"requires_key": true,
		},
		{
			"name": "voyage", "display_name": "Voyage AI",
			"model": "voyage-code-3", "dimensions": 1024,
			"endpoint":     "https://api.voyageai.com/v1/embeddings",
			"configured":   false,
			"requires_key": true,
		},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"use_builtin":     ec.UseBuiltin,
		"active_provider": ec.Provider,
		"builtin_model": map[string]interface{}{
			"name":        "MiniLM-L6-v2",
			"provider":    "Sentence Transformers (HuggingFace)",
			"dimensions":  384,
			"description": "Lightweight local embedding model — no API key required. Runs on the C++ engine via ONNX Runtime.",
		},
		"external_providers": externalProviders,
	})
}

// isLLMKeySet checks whether a given LLM provider has an API key configured.
func (h *Handler) isLLMKeySet(name string) bool {
	m, ok := h.LLMRegistry.GetMeta(name)
	return ok && m.Configured
}

// UpdateEmbeddingsConfig updates the embedding provider selection.
// PUT /api/v1/embeddings/config
func (h *Handler) UpdateEmbeddingsConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UseBuiltin bool   `json:"use_builtin"`
		Provider   string `json:"provider"`   // "openai", "cohere", "voyage"
		Endpoint   string `json:"endpoint"`   // embedding API URL
		Model      string `json:"model"`      // e.g. "text-embedding-3-small"
		Dimensions uint32 `json:"dimensions"` // e.g. 1536
		APIKey     string `json:"api_key"`    // optional — re-use LLM key if empty
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// If no dedicated embedding API key was provided, try to inherit from the
	// LLM registry (e.g. OpenAI embeddings share the same key as OpenAI LLM).
	apiKey := req.APIKey
	if apiKey == "" && req.Provider != "" {
		if meta, ok := h.LLMRegistry.GetMeta(req.Provider); ok && meta.Configured {
			apiKey = meta.APIKey
		}
	}

	h.embedMu.Lock()
	h.embedConfig = embeddingRuntimeConfig{
		UseBuiltin: req.UseBuiltin,
		Provider:   req.Provider,
		Endpoint:   req.Endpoint,
		Model:      req.Model,
		Dimensions: req.Dimensions,
		APIKey:     apiKey,
	}
	h.embedMu.Unlock()

	slog.Info("embedding config updated",
		"use_builtin", req.UseBuiltin,
		"provider", req.Provider,
		"model", req.Model,
		"dimensions", req.Dimensions,
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"use_builtin": req.UseBuiltin,
		"provider":    req.Provider,
		"model":       req.Model,
		"dimensions":  req.Dimensions,
	})
}

// CheckLLMBalance checks the credit/token balance for a cloud LLM provider.
// POST /api/v1/llm/providers/{provider}/balance
func (h *Handler) CheckLLMBalance(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	if providerName == "" {
		writeError(w, http.StatusBadRequest, "provider name required")
		return
	}

	meta, ok := h.LLMRegistry.GetMeta(providerName)
	if !ok {
		writeError(w, http.StatusNotFound, "provider not found: "+providerName)
		return
	}
	if !meta.Configured {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": providerName, "status": "not_configured",
		})
		return
	}

	// Provider-specific balance checks.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	switch providerName {
	case "openai":
		h.checkOpenAIBalance(ctx, w, meta)
	case "anthropic":
		h.checkAnthropicBalance(ctx, w, meta)
	case "gemini":
		// Gemini uses Google Cloud billing — check via a test request's rate limit headers.
		h.checkGenericBalance(ctx, w, providerName, meta)
	case "grok":
		h.checkGenericBalance(ctx, w, providerName, meta)
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": providerName, "status": "unknown",
			"message": "Balance checking is not supported for this provider.",
		})
	}
}

// checkOpenAIBalance checks the OpenAI credit balance via their billing API.
func (h *Handler) checkOpenAIBalance(ctx context.Context, w http.ResponseWriter, meta llm.ProviderMeta) {
	// OpenAI doesn't expose a public balance API anymore — we infer from
	// a lightweight test call and check response headers / errors for rate
	// limit or billing issues.
	p, ok := h.LLMRegistry.Get("openai")
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": "openai", "status": "error", "message": "provider not registered",
		})
		return
	}
	_, err := p.Complete(ctx, &llm.CompletionRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		MaxTokens: 1, Temperature: 0,
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "insufficient_quota") || strings.Contains(errStr, "billing") {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"provider": "openai", "status": "low_balance",
				"warning": "Your OpenAI account has insufficient credits. Please recharge your API usage at https://platform.openai.com/account/billing",
			})
			return
		}
		if strings.Contains(errStr, "rate_limit") {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"provider": "openai", "status": "rate_limited",
				"warning": "Rate limited — your account is active but hitting usage limits.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": "openai", "status": "error", "message": errStr,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": "openai", "status": "ok", "message": "API key is valid and has available credits.",
	})
}

// checkAnthropicBalance checks the Anthropic credit balance.
func (h *Handler) checkAnthropicBalance(ctx context.Context, w http.ResponseWriter, meta llm.ProviderMeta) {
	p, ok := h.LLMRegistry.Get("anthropic")
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": "anthropic", "status": "error", "message": "provider not registered",
		})
		return
	}
	_, err := p.Complete(ctx, &llm.CompletionRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		MaxTokens: 1, Temperature: 0,
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "credit") || strings.Contains(errStr, "billing") || strings.Contains(errStr, "overloaded") {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"provider": "anthropic", "status": "low_balance",
				"warning": "Your Anthropic account may have insufficient credits. Please recharge at https://console.anthropic.com/settings/billing",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": "anthropic", "status": "error", "message": errStr,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": "anthropic", "status": "ok", "message": "API key is valid and has available credits.",
	})
}

// checkGenericBalance does a lightweight test call and infers balance from errors.
func (h *Handler) checkGenericBalance(ctx context.Context, w http.ResponseWriter, name string, meta llm.ProviderMeta) {
	p, ok := h.LLMRegistry.Get(name)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": name, "status": "error", "message": "provider not registered",
		})
		return
	}
	_, err := p.Complete(ctx, &llm.CompletionRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		MaxTokens: 1, Temperature: 0,
	})
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "quota") || strings.Contains(errStr, "billing") ||
			strings.Contains(errStr, "credit") || strings.Contains(errStr, "insufficient") {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"provider": name, "status": "low_balance",
				"warning": fmt.Sprintf("Your %s account may have insufficient credits. Please check your billing settings.", meta.DisplayName),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": name, "status": "error", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": name, "status": "ok", "message": "API key is valid and has available credits.",
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
			// Record delivery failure for retry.
			if h.DeliveryRepo != nil {
				d := &webhookq.Delivery{
					WebhookEventID: evt.ID, RepoID: repo.ID,
					Platform: "github", PRNumber: payload.PullRequest.Number,
				}
				_ = h.DeliveryRepo.Create(context.Background(), d)
			}
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

// ─── URL Parsing Helper ────────────────────────────────────────────────────

type repoURLParts struct {
	owner string
	name  string
}

// parseRepoURL extracts owner and repo name from a clone URL.
// Supports HTTPS (https://github.com/owner/repo.git) and SSH (git@github.com:owner/repo.git).
func parseRepoURL(rawURL string) (repoURLParts, error) {
	// Handle SSH URLs like git@github.com:owner/repo.git
	if strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		idx := strings.LastIndex(rawURL, ":")
		path := rawURL[idx+1:]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return repoURLParts{owner: parts[0], name: parts[1]}, nil
		}
	}

	// Handle HTTPS URLs
	u, err := url.Parse(rawURL)
	if err != nil {
		return repoURLParts{}, fmt.Errorf("invalid url: %w", err)
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return repoURLParts{owner: parts[0], name: parts[1]}, nil
	}
	return repoURLParts{}, fmt.Errorf("cannot extract owner/name from URL")
}
