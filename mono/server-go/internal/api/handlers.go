package api

import (
	"bytes"
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
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

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
	"github.com/AuralithAI/rtvortex-server/internal/vault/keychain"
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
	VCSResolver  *vcs.Resolver
	EngineClient *engine.Client

	ReviewPipeline   *review.Pipeline
	IndexingService  *indexing.Service
	AuditLogger      *audit.Logger
	QuotaEnforcer    *quota.Enforcer
	DeliveryRepo     *webhookq.Repository
	PRSyncWorker     *prsync.Worker
	ChatRepo         *store.ChatRepository
	ChatService      *chat.Service
	AssetRepo        *store.AssetRepository    // multimodal asset persistence (repo_assets)
	KeychainService  *keychain.Service         // encrypted keychain — primary secret store for all per-user secrets
	VCSPlatformRepo  *store.VCSPlatformRepo    // per-user VCS platform config (URLs, usernames)
	MetricsCollector *engine.MetricsCollector  // engine metrics stream consumer
	EmbedCache       *engine.EmbedCacheService // Redis-backed L2 embedding cache

	// Runtime embedding configuration — guarded by embedMu.
	embedMu     sync.RWMutex
	embedConfig embeddingRuntimeConfig
}

// userVaultFor returns a per-user SecretStore backed by the keychain.
// Returns nil if the keychain service is not available.
func (h *Handler) userVaultFor(userID uuid.UUID) vault.SecretStore {
	if h.KeychainService != nil {
		return h.KeychainService.ForUser(userID)
	}
	return nil
}

// embeddingRuntimeConfig holds the user-selected embedding configuration.
// It defaults to LOCAL_ONNX with bge-m3.
type embeddingRuntimeConfig struct {
	UseBuiltin   bool   `json:"use_builtin"`   // true → LOCAL_ONNX
	BuiltinModel string `json:"builtin_model"` // "bge-m3" or "minilm"
	Provider     string `json:"provider"`      // "openai", "cohere", "voyage" (only when UseBuiltin=false)
	Endpoint     string `json:"endpoint"`      // embedding API URL
	Model        string `json:"model"`         // e.g. "text-embedding-3-small"
	Dimensions   uint32 `json:"dimensions"`    // e.g. 1536
	APIKey       string `json:"-"`             // never serialised
}

// DefaultEmbeddingConfig returns the default built-in embedding configuration.
func DefaultEmbeddingConfig() embeddingRuntimeConfig {
	return embeddingRuntimeConfig{
		UseBuiltin:   true,
		BuiltinModel: "bge-m3",
		Provider:     "",
		Endpoint:     "",
		Model:        "",
		Dimensions:   1024,
	}
}

func init() {
	// Prevent "imported and not used" for json and sync:
	_ = json.Marshal
	_ = (*sync.Mutex)(nil)
}

// builtinModelDimensions returns the embedding dimension for a builtin model name.
func builtinModelDimensions(name string) uint32 {
	switch name {
	case "minilm":
		return 384
	case "bge-m3":
		return 1024
	default:
		return 1024
	}
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
	// Some providers (e.g., X/Twitter) use PKCE and need the original state
	// to retrieve the code_verifier. Use the stateful exchange when available.
	var token *oauth2.Token
	type stateExchanger interface {
		ExchangeWithState(ctx context.Context, code, state string) (*oauth2.Token, error)
	}
	if se, ok := provider.(stateExchanger); ok {
		token, err = se.ExchangeWithState(ctx, code, state)
	} else {
		token, err = provider.Exchange(ctx, code)
	}
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

	// Set refreshed tokens in cookies so the browser picks them up
	// automatically — mirrors the cookie-setting logic in OAuthCallback.
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
// Accepts optional JSON body: {"action": "index"|"reindex"|"reclone", "target_branch": "..."}
func (h *Handler) TriggerIndex(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}

	// Parse optional request body for action & target_branch.
	type triggerBody struct {
		Action       string `json:"action"`        // "index" (default), "reindex", "reclone"
		TargetBranch string `json:"target_branch"` // optional branch override
	}
	var body triggerBody
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	// Default action
	if body.Action == "" {
		body.Action = "index"
	}
	// Validate action
	switch body.Action {
	case "index", "reindex", "reclone":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid action: must be index, reindex, or reclone")
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
	if ec.UseBuiltin {
		engineCfg.EmbeddingProvider = "LOCAL_ONNX"
		model := ec.BuiltinModel
		if model == "" {
			model = "bge-m3"
		}
		engineCfg.EmbeddingModel = model
		dims := builtinModelDimensions(model)
		engineCfg.EmbeddingDimensions = dims
	} else if ec.Provider != "" {
		engineCfg.EmbeddingProvider = "HTTP"
		engineCfg.EmbeddingEndpoint = ec.Endpoint
		engineCfg.EmbeddingModel = ec.Model
		engineCfg.EmbeddingDimensions = ec.Dimensions
		engineCfg.EmbeddingAPIKey = ec.APIKey
	}

	// ── Resolve VCS clone token from the user's keychain ────────────────
	// The C++ engine uses this to authenticate `git clone` for private repos.
	// Token is resolved server-side and passed transiently — never persisted.
	platform := repo.Platform
	if platform == "" && repo.CloneURL != "" {
		platform = detectPlatformFromURL(repo.CloneURL)
	}
	if platform != "" {
		userID, ok := auth.UserIDFromContext(r.Context())
		if ok {
			if uv := h.userVaultFor(userID); uv != nil {
				tokenKey := "vcs." + platform + ".token"
				if platform == "azure_devops" {
					tokenKey = "vcs.azure_devops.pat"
				}
				if cloneToken, _ := uv.Get(tokenKey); cloneToken != "" {
					engineCfg.CloneToken = cloneToken
					slog.Info("resolved VCS clone token from keychain",
						"platform", platform, "repo_id", repoID,
						"token_len", len(cloneToken))
				}
			}
		}
	}

	// Set the action and optional branch on the engine config.
	engineCfg.IndexAction = body.Action
	if body.TargetBranch != "" {
		engineCfg.TargetBranch = body.TargetBranch
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
	slog.Info("indexing triggered", "repo_id", repoID, "job_id", jobID, "action", body.Action)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "status": "accepted", "action": body.Action})
}

// ListBranches returns the remote branches of a repository via git ls-remote.
// GET /api/v1/repos/{repoID}/branches
func (h *Handler) ListBranches(w http.ResponseWriter, r *http.Request) {
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

	// Resolve VCS clone token for authenticated ls-remote
	var cloneToken string
	platform := repo.Platform
	if platform == "" && repo.CloneURL != "" {
		platform = detectPlatformFromURL(repo.CloneURL)
	}
	if platform != "" {
		userID, ok := auth.UserIDFromContext(r.Context())
		if ok {
			if uv := h.userVaultFor(userID); uv != nil {
				tokenKey := "vcs." + platform + ".token"
				if platform == "azure_devops" {
					tokenKey = "vcs.azure_devops.pat"
				}
				cloneToken, _ = uv.Get(tokenKey)
			}
		}
	}

	// Run git ls-remote --heads to list remote branches.
	// Try unauthenticated first (works for public repos), fall back to token.
	gitCmd := fmt.Sprintf("git ls-remote --heads %s", repo.CloneURL)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", gitCmd)
	output, err := cmd.Output()

	if err != nil && cloneToken != "" && strings.HasPrefix(repo.CloneURL, "https://") {
		// Unauthenticated failed — retry with token (private repo)
		slog.Info("unauthenticated ls-remote failed, retrying with VCS token",
			"repo_id", repoID)
		gitCmd = fmt.Sprintf("git -c http.extraHeader='Authorization: Bearer %s' ls-remote --heads %s",
			cloneToken, repo.CloneURL)
		cmd = exec.CommandContext(ctx, "sh", "-c", gitCmd)
		output, err = cmd.Output()
	}

	if err != nil {
		slog.Error("failed to list branches", "repo_id", repoID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list remote branches")
		return
	}

	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			ref := parts[1]
			branch := strings.TrimPrefix(ref, "refs/heads/")
			branches = append(branches, branch)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"branches":       branches,
		"default_branch": repo.DefaultBranch,
		"count":          len(branches),
	})
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

// ─── Embedding Statistics endpoint ───────────────────────────────────────────

// GetEmbedStats returns embedding health and performance metrics for a repository.
// GET /api/v1/repos/{repoID}/embed-stats
func (h *Handler) GetEmbedStats(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}

	if h.EngineClient == nil {
		writeError(w, http.StatusServiceUnavailable, "engine not connected")
		return
	}

	stats, err := h.EngineClient.GetEmbedStats(r.Context(), repoID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get embed stats: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// ─── Intra-Repo File Map endpoint ───────────────────────────────────────────

// GetRepoFileMap returns the knowledge graph nodes and edges for a repository.
// GET /api/v1/repos/{repoID}/file-map
func (h *Handler) GetRepoFileMap(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo ID")
		return
	}

	if h.EngineClient == nil {
		writeError(w, http.StatusServiceUnavailable, "engine not connected")
		return
	}

	// Parse optional filter query params
	nodeTypes := r.URL.Query()["node_type"]
	edgeTypes := r.URL.Query()["edge_type"]

	// Parse optional max_nodes cap.
	// 0 = no cap (engine returns all matching nodes — safe when node_type filter is used).
	// Default when not specified: 0 (no cap). Hard max: 5000.
	var maxNodes uint32 = 0
	if v := r.URL.Query().Get("max_nodes"); v != "" {
		if n, parseErr := strconv.ParseUint(v, 10, 32); parseErr == nil {
			maxNodes = uint32(n)
		}
	}
	if maxNodes > 5000 {
		maxNodes = 5000
	}

	fileMap, err := h.EngineClient.GetRepoFileMap(r.Context(), repoID.String(), nodeTypes, edgeTypes, maxNodes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get file map: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, fileMap)
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

	// Lazy-rehydrate: if any provider is not yet configured in the in-memory
	// registry, check the requesting user's keychain for persisted API keys.
	// This handles the post-restart case where the keychain has the keys but
	// the in-memory registry was initialized with env vars only.
	h.rehydrateLLMFromKeychain(r)

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
		"routes":    h.LLMRegistry.GetRoutes(),
	})
}

// rehydrateLLMFromKeychain checks the requesting user's keychain for any
// persisted LLM API keys that are not yet loaded into the in-memory registry.
// This covers the post-restart scenario where keys live in the keychain but
// the registry was initialised from environment variables only.
// It is intentionally idempotent — once a provider is marked Configured the
// keychain lookup is skipped.
func (h *Handler) rehydrateLLMFromKeychain(r *http.Request) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		return
	}
	uv := h.userVaultFor(uid)
	if uv == nil {
		return
	}

	for _, name := range h.LLMRegistry.ListProviders() {
		meta, hasMeta := h.LLMRegistry.GetMeta(name)
		if !hasMeta || meta.Configured {
			continue // already configured (env var or previous rehydration)
		}

		// Try to restore the API key from keychain.
		apiKey, err := uv.Get(fmt.Sprintf("llm.%s.api_key", name))
		if err != nil || apiKey == "" {
			continue
		}

		// Apply model and base_url overrides first so ApplyAPIKey re-creates
		// the provider with the full config. Use vault-free Apply* variants
		// since the values already live in this user's keychain.
		if model, e := uv.Get(fmt.Sprintf("llm.%s.model", name)); e == nil && model != "" {
			h.LLMRegistry.ApplyModel(name, model)
		}
		if base, e := uv.Get(fmt.Sprintf("llm.%s.base_url", name)); e == nil && base != "" {
			h.LLMRegistry.ApplyBaseURL(name, base)
		}

		// Now set the API key — this marks the provider as Configured.
		// Use ApplyAPIKey (no vault write-back) since the key already lives
		// in this user's keychain and the registry vault may belong to a
		// different user.
		h.LLMRegistry.ApplyAPIKey(name, apiKey)

		slog.Info("llm: rehydrated provider from keychain",
			"provider", name, "user", uid)
	}

	// Restore primary provider choice if stored.
	if primary, err := uv.Get("llm.primary"); err == nil && primary != "" {
		if _, exists := h.LLMRegistry.Get(primary); exists {
			h.LLMRegistry.SetPrimary(primary)
		}
	}
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
		// Persist the key in the user's keychain for cross-restart survival.
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			if uv := h.userVaultFor(uid); uv != nil {
				vaultKey := fmt.Sprintf("llm.%s.api_key", providerName)
				if err := uv.Set(vaultKey, req.APIKey); err != nil {
					slog.Warn("failed to persist LLM API key in keychain", "provider", providerName, "error", err)
				}
			}
		}
	}
	if req.Model != "" {
		h.LLMRegistry.UpdateModel(providerName, req.Model)
		// Persist model choice to keychain so it survives restarts.
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			if uv := h.userVaultFor(uid); uv != nil {
				if err := uv.Set(fmt.Sprintf("llm.%s.model", providerName), req.Model); err != nil {
					slog.Warn("failed to persist LLM model choice in keychain", "provider", providerName, "error", err)
				}
			}
		}
	}
	if req.BaseURL != "" {
		h.LLMRegistry.UpdateBaseURL(providerName, req.BaseURL)
		// Persist base URL to keychain so it survives restarts.
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			if uv := h.userVaultFor(uid); uv != nil {
				if err := uv.Set(fmt.Sprintf("llm.%s.base_url", providerName), req.BaseURL); err != nil {
					slog.Warn("failed to persist LLM base URL in keychain", "provider", providerName, "error", err)
				}
			}
		}
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

	// Persist primary choice to the user's keychain.
	if uid, ok := auth.UserIDFromContext(r.Context()); ok {
		if uv := h.userVaultFor(uid); uv != nil {
			_ = uv.Set("llm.primary", req.Provider)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"primary": req.Provider,
	})
}

// GetLLMRoutes returns the role-based model routing table.
// GET /api/v1/llm/routes
func (h *Handler) GetLLMRoutes(w http.ResponseWriter, _ *http.Request) {
	routes := h.LLMRegistry.GetRoutes()

	type routeInfo struct {
		Role     string `json:"role"`
		Provider string `json:"provider"`
		Model    string `json:"model,omitempty"`
	}

	out := make([]routeInfo, 0, len(routes))
	for role, route := range routes {
		out = append(out, routeInfo{
			Role:     role,
			Provider: route.Provider,
			Model:    route.Model,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"routes":  out,
		"primary": h.LLMRegistry.PrimaryName(),
	})
}

// SetLLMRoutes updates the role-based model routing table.
// PUT /api/v1/llm/routes
//
//	Request body:
//	  { "routes": [{ "role": "orchestrator", "provider": "anthropic", "model": "..." }, ...] }
func (h *Handler) SetLLMRoutes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Routes []struct {
			Role     string `json:"role"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"routes"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	routes := make(map[string]llm.ModelRoute, len(req.Routes))
	for _, rt := range req.Routes {
		if rt.Role == "" || rt.Provider == "" {
			continue
		}
		// Verify the provider exists.
		if _, ok := h.LLMRegistry.Get(rt.Provider); !ok {
			writeError(w, http.StatusBadRequest, "unknown provider: "+rt.Provider)
			return
		}
		routes[rt.Role] = llm.ModelRoute{Provider: rt.Provider, Model: rt.Model}
	}

	h.LLMRegistry.SetRoutes(routes)

	// Persist routes to the user's keychain as JSON.
	if uid, ok := auth.UserIDFromContext(r.Context()); ok {
		if uv := h.userVaultFor(uid); uv != nil {
			if data, err := json.Marshal(routes); err == nil {
				_ = uv.Set("llm.routes", string(data))
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"routes": len(routes),
		"ok":     true,
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

	// Check vault for dedicated embedding API keys.
	var userVault vault.SecretStore
	if uid, ok := auth.UserIDFromContext(r.Context()); ok {
		userVault = h.userVaultFor(uid)
	}
	embedKeySet := func(provider string) bool {
		if userVault != nil {
			if key, err := userVault.Get("embed_key." + provider); err == nil && key != "" {
				return true
			}
		}
		return h.isLLMKeySet(provider)
	}

	// Available models per provider (model catalog).
	externalProviders := []map[string]interface{}{
		{
			"name": "openai", "display_name": "OpenAI Embeddings",
			"model": "text-embedding-3-small", "dimensions": 1536,
			"endpoint":     "https://api.openai.com/v1/embeddings",
			"configured":   embedKeySet("openai"),
			"requires_key": true,
			"available_models": []map[string]interface{}{
				{"name": "text-embedding-3-small", "dimensions": 1536, "description": "Cost-effective, high-quality embeddings"},
				{"name": "text-embedding-3-large", "dimensions": 3072, "description": "Highest quality, larger vectors"},
				{"name": "text-embedding-ada-002", "dimensions": 1536, "description": "Legacy model, widely deployed"},
			},
		},
		{
			"name": "cohere", "display_name": "Cohere Embed",
			"model": "embed-english-v3.0", "dimensions": 1024,
			"endpoint":     "https://api.cohere.ai/v1/embed",
			"configured":   embedKeySet("cohere"),
			"requires_key": true,
			"available_models": []map[string]interface{}{
				{"name": "embed-english-v3.0", "dimensions": 1024, "description": "English-optimised, best for code"},
				{"name": "embed-multilingual-v3.0", "dimensions": 1024, "description": "Multilingual support"},
				{"name": "embed-english-light-v3.0", "dimensions": 384, "description": "Lightweight, faster inference"},
			},
		},
		{
			"name": "voyage", "display_name": "Voyage AI",
			"model": "voyage-code-3", "dimensions": 1024,
			"endpoint":     "https://api.voyageai.com/v1/embeddings",
			"configured":   embedKeySet("voyage"),
			"requires_key": true,
			"available_models": []map[string]interface{}{
				{"name": "voyage-code-3", "dimensions": 1024, "description": "Optimised for code retrieval"},
				{"name": "voyage-3", "dimensions": 1024, "description": "General-purpose, high quality"},
				{"name": "voyage-3-lite", "dimensions": 512, "description": "Lightweight, cost-effective"},
			},
		},
		{
			"name": "custom", "display_name": "Custom / Self-hosted (Ollama, vLLM)",
			"model": "nomic-embed-text", "dimensions": 768,
			"endpoint":     "http://localhost:11434/api/embeddings",
			"configured":   false,
			"requires_key": false,
			"available_models": []map[string]interface{}{
				{"name": "nomic-embed-text", "dimensions": 768, "description": "Ollama default embedding model"},
				{"name": "mxbai-embed-large", "dimensions": 1024, "description": "High-quality open-source embeddings"},
				{"name": "all-minilm", "dimensions": 384, "description": "Lightweight, fast local model"},
			},
		},
	}

	// If a provider is actively selected, update its default model to match.
	if ec.Provider != "" {
		for i := range externalProviders {
			if externalProviders[i]["name"] == ec.Provider {
				if ec.Model != "" {
					externalProviders[i]["model"] = ec.Model
				}
				if ec.Dimensions > 0 {
					externalProviders[i]["dimensions"] = ec.Dimensions
				}
				if ec.Endpoint != "" {
					externalProviders[i]["endpoint"] = ec.Endpoint
				}
			}
		}
	}

	activeBuiltin := ec.BuiltinModel
	if activeBuiltin == "" {
		activeBuiltin = "bge-m3"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"use_builtin":          ec.UseBuiltin,
		"active_provider":      ec.Provider,
		"active_model":         ec.Model,
		"active_builtin_model": activeBuiltin,
		"builtin_models": []map[string]interface{}{
			{
				"name":         "bge-m3",
				"display_name": "BGE-M3",
				"provider":     "BAAI (HuggingFace)",
				"dimensions":   1024,
				"size_mb":      2300,
				"description":  "High-quality multilingual embedding model — 1024 dimensions. Best retrieval accuracy. Downloaded on first use (~2.3 GB).",
			},
			{
				"name":         "minilm",
				"display_name": "MiniLM-L6-v2",
				"provider":     "Sentence Transformers (HuggingFace)",
				"dimensions":   384,
				"size_mb":      87,
				"description":  "Lightweight local embedding model — 384 dimensions. Fast inference, lower memory. Bundled with the engine.",
			},
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
		UseBuiltin   bool   `json:"use_builtin"`
		BuiltinModel string `json:"builtin_model"` // "bge-m3" or "minilm"
		Provider     string `json:"provider"`      // "openai", "cohere", "voyage", "custom"
		Endpoint     string `json:"endpoint"`      // embedding API URL
		Model        string `json:"model"`         // e.g. "text-embedding-3-small"
		Dimensions   uint32 `json:"dimensions"`    // e.g. 1536
		APIKey       string `json:"api_key"`       // optional — re-use LLM key if empty
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve API key: dedicated embedding key → keychain → LLM registry fallback.
	apiKey := req.APIKey

	var uv vault.SecretStore
	if uid, ok := auth.UserIDFromContext(r.Context()); ok {
		uv = h.userVaultFor(uid)
	}

	// If the user provided a new API key, store it in the keychain.
	if apiKey != "" && uv != nil && req.Provider != "" {
		if err := uv.Set("embed_key."+req.Provider, apiKey); err != nil {
			slog.Warn("failed to store embedding API key in keychain", "provider", req.Provider, "error", err)
		}
	}

	// If no key was provided, try keychain first, then LLM registry.
	if apiKey == "" && req.Provider != "" {
		if uv != nil {
			if key, err := uv.Get("embed_key." + req.Provider); err == nil && key != "" {
				apiKey = key
			}
		}
		if apiKey == "" {
			if meta, ok := h.LLMRegistry.GetMeta(req.Provider); ok && meta.Configured {
				apiKey = meta.APIKey
			}
		}
	}

	h.embedMu.Lock()
	h.embedConfig = embeddingRuntimeConfig{
		UseBuiltin:   req.UseBuiltin,
		BuiltinModel: req.BuiltinModel,
		Provider:     req.Provider,
		Endpoint:     req.Endpoint,
		Model:        req.Model,
		Dimensions:   req.Dimensions,
		APIKey:       apiKey,
	}
	h.embedMu.Unlock()

	slog.Info("embedding config updated",
		"use_builtin", req.UseBuiltin,
		"builtin_model", req.BuiltinModel,
		"provider", req.Provider,
		"model", req.Model,
		"dimensions", req.Dimensions,
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"use_builtin":   req.UseBuiltin,
		"builtin_model": req.BuiltinModel,
		"provider":      req.Provider,
		"model":         req.Model,
		"dimensions":    req.Dimensions,
		"configured":    apiKey != "" || req.UseBuiltin || req.Provider == "custom",
	})
}

// TestEmbeddingProvider tests connectivity to an embedding API provider.
// POST /api/v1/embeddings/test
func (h *Handler) TestEmbeddingProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "endpoint is required")
		return
	}

	// Resolve API key from request → keychain → LLM registry.
	apiKey := req.APIKey
	if apiKey == "" && req.Provider != "" {
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			if uv := h.userVaultFor(uid); uv != nil {
				if key, err := uv.Get("embed_key." + req.Provider); err == nil && key != "" {
					apiKey = key
				}
			}
		}
		if apiKey == "" {
			if meta, ok := h.LLMRegistry.GetMeta(req.Provider); ok && meta.Configured {
				apiKey = meta.APIKey
			}
		}
	}

	// Determine if this is a Cohere-style API.
	isCohere := strings.Contains(req.Endpoint, "cohere")

	// Build a minimal embedding request with a short test string.
	var payload map[string]interface{}
	if isCohere {
		payload = map[string]interface{}{
			"model":      req.Model,
			"texts":      []string{"test embedding connection"},
			"input_type": "search_document",
			"truncate":   "END",
		}
	} else {
		payload = map[string]interface{}{
			"model": req.Model,
			"input": []string{"test embedding connection"},
		}
	}

	payloadBytes, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.Endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": req.Provider, "healthy": false, "error": "invalid endpoint URL",
		})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": req.Provider, "healthy": false, "error": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != 200 {
		errMsg := string(body)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200]
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider":    req.Provider,
			"healthy":     false,
			"error":       fmt.Sprintf("HTTP %d: %s", resp.StatusCode, errMsg),
			"status_code": resp.StatusCode,
		})
		return
	}

	// Parse response to confirm we got real embeddings back.
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": req.Provider, "healthy": false, "error": "invalid JSON response",
		})
		return
	}

	// Extract dimension count from response.
	dims := 0
	if isCohere {
		if embs, ok := result["embeddings"].([]interface{}); ok && len(embs) > 0 {
			if first, ok := embs[0].([]interface{}); ok {
				dims = len(first)
			}
		}
	} else {
		if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
			if item, ok := data[0].(map[string]interface{}); ok {
				if emb, ok := item["embedding"].([]interface{}); ok {
					dims = len(emb)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider":   req.Provider,
		"healthy":    true,
		"model":      req.Model,
		"dimensions": dims,
	})
}

// CheckEmbeddingCredits checks credit / billing status for an embedding provider.
// POST /api/v1/embeddings/credits
func (h *Handler) CheckEmbeddingCredits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"api_key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve API key.
	apiKey := req.APIKey
	if apiKey == "" && req.Provider != "" {
		if uid, ok := auth.UserIDFromContext(r.Context()); ok {
			if uv := h.userVaultFor(uid); uv != nil {
				if key, err := uv.Get("embed_key." + req.Provider); err == nil && key != "" {
					apiKey = key
				}
			}
		}
		if apiKey == "" {
			if meta, ok := h.LLMRegistry.GetMeta(req.Provider); ok && meta.Configured {
				apiKey = meta.APIKey
			}
		}
	}

	if apiKey == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": req.Provider, "status": "not_configured",
			"message": "No API key configured for this provider.",
		})
		return
	}

	// Provider-specific credit checks.
	switch req.Provider {
	case "openai":
		// OpenAI doesn't expose a balance API; test with a tiny call.
		h.checkEmbeddingHealthViaTest(r.Context(), w, req.Provider, "https://api.openai.com/v1/embeddings",
			"text-embedding-3-small", apiKey, false)
	case "cohere":
		h.checkEmbeddingHealthViaTest(r.Context(), w, req.Provider, "https://api.cohere.ai/v1/embed",
			"embed-english-v3.0", apiKey, true)
	case "voyage":
		h.checkEmbeddingHealthViaTest(r.Context(), w, req.Provider, "https://api.voyageai.com/v1/embeddings",
			"voyage-code-3", apiKey, false)
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": req.Provider, "status": "unknown",
			"message": "Credit checking is not supported for this provider.",
		})
	}
}

// checkEmbeddingHealthViaTest infers billing status from a lightweight embedding call.
func (h *Handler) checkEmbeddingHealthViaTest(ctx context.Context, w http.ResponseWriter,
	provider, endpoint, model, apiKey string, isCohere bool) {

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var payload map[string]interface{}
	if isCohere {
		payload = map[string]interface{}{
			"model": model, "texts": []string{"credit check"}, "input_type": "search_document", "truncate": "END",
		}
	} else {
		payload = map[string]interface{}{"model": model, "input": []string{"credit check"}}
	}

	payloadBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": provider, "status": "error", "message": err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch {
	case resp.StatusCode == 200:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": provider, "status": "ok", "message": "API key is valid and billing is active.",
		})
	case resp.StatusCode == 401:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": provider, "status": "error", "message": "Invalid API key (401 Unauthorized).",
		})
	case resp.StatusCode == 429:
		errMsg := string(body)
		if strings.Contains(errMsg, "insufficient_quota") || strings.Contains(errMsg, "billing") {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"provider": provider, "status": "low_balance",
				"message": "Quota exhausted or billing issue detected.",
			})
		} else {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"provider": provider, "status": "rate_limited",
				"message": "Rate limited — credits appear available.",
			})
		}
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"provider": provider, "status": "error",
			"message": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)[:min(len(body), 200)]),
		})
	}
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
		"vcs_platforms": []string{"github", "gitlab", "bitbucket", "azure_devops"},
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
//
// Webhook signature validation uses the per-repo webhook secret stored in the
// repositories table.  The repo is identified from the payload before
// validation so that each repo can have its own secret.
func (h *Handler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	evt := &model.WebhookEvent{Platform: "github", EventType: r.Header.Get("X-GitHub-Event"), Payload: body}
	_ = h.WebhookRepo.RecordEvent(ctx, evt)

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

	// Look up the repo to get its per-repo webhook secret.
	repo, err := h.RepoRepo.GetByPlatformAndExternalID(ctx, "github", fmt.Sprintf("%d", payload.Repository.ID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not registered"})
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Validate signature using the per-repo webhook secret.
	signature := r.Header.Get("X-Hub-Signature-256")
	whSecret := repo.WebhookSecret
	if whSecret == "" {
		// Fallback: try the org owner's vault webhook secret.
		whSecret = h.VCSResolver.ResolveWebhookSecretForOrg(ctx, repo.OrgID, vcs.PlatformGitHub)
	}
	if !vcs.ValidateWebhookHMAC(body, signature, whSecret) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	if payload.Action != "opened" && payload.Action != "synchronize" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "action": payload.Action})
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

	// Look up the repo to get its per-repo webhook secret.
	repo, err := h.RepoRepo.GetByPlatformAndExternalID(ctx, "gitlab", fmt.Sprintf("%d", payload.Project.ID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not registered"})
			return
		}
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Validate token using per-repo or org-owner webhook secret.
	token := r.Header.Get("X-Gitlab-Token")
	whSecret := repo.WebhookSecret
	if whSecret == "" {
		whSecret = h.VCSResolver.ResolveWebhookSecretForOrg(ctx, repo.OrgID, vcs.PlatformGitLab)
	}
	if !vcs.ValidateWebhookToken(token, whSecret) {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}

	if payload.ObjectAttributes.Action != "open" && payload.ObjectAttributes.Action != "update" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "action": payload.ObjectAttributes.Action})
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

	// Validate HMAC signature using per-repo webhook secret.
	signature := r.Header.Get("X-Hub-Signature")
	whSecret := repo.WebhookSecret
	if whSecret == "" {
		whSecret = h.VCSResolver.ResolveWebhookSecretForOrg(ctx, repo.OrgID, vcs.PlatformBitbucket)
	}
	if !vcs.ValidateWebhookHMAC(body, signature, whSecret) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
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

	// Validate token using per-repo webhook secret.
	token := r.Header.Get("X-Vss-Token")
	whSecret := repo.WebhookSecret
	if whSecret == "" {
		whSecret = h.VCSResolver.ResolveWebhookSecretForOrg(ctx, repo.OrgID, vcs.PlatformAzureDevOps)
	}
	if !vcs.ValidateWebhookToken(token, whSecret) {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
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
// Supports:
//   - HTTPS:            https://github.com/owner/repo.git
//   - SSH:              git@github.com:owner/repo.git
//   - Bitbucket Server: https://bb.example.com/scm/PROJECT/repo.git
func parseRepoURL(rawURL string) (repoURLParts, error) {
	// Handle SSH URLs like git@github.com:owner/repo.git
	if strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		idx := strings.LastIndex(rawURL, ":")
		path := rawURL[idx+1:]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return repoURLParts{owner: parts[len(parts)-2], name: parts[len(parts)-1]}, nil
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

	// Bitbucket Server clone URLs: /scm/{project}/{repo}
	// Skip the "scm" prefix to get project + repo.
	if len(parts) >= 3 && parts[0] == "scm" {
		return repoURLParts{owner: parts[1], name: parts[2]}, nil
	}

	// Standard: /{owner}/{repo} (last two segments)
	if len(parts) >= 2 {
		return repoURLParts{owner: parts[len(parts)-2], name: parts[len(parts)-1]}, nil
	}
	return repoURLParts{}, fmt.Errorf("cannot extract owner/name from URL")
}

// detectPlatformFromURL infers the VCS platform from a clone URL.
// Used as a fallback when the repository record has no explicit platform field.
func detectPlatformFromURL(cloneURL string) string {
	lower := strings.ToLower(cloneURL)
	switch {
	case strings.Contains(lower, "github.com"):
		return "github"
	case strings.Contains(lower, "gitlab.com") || strings.Contains(lower, "gitlab"):
		return "gitlab"
	case strings.Contains(lower, "bitbucket.org") || strings.Contains(lower, "bitbucket"):
		return "bitbucket"
	case strings.Contains(lower, "/scm/"):
		// Bitbucket Server clone URLs use /scm/ path prefix
		return "bitbucket"
	case strings.Contains(lower, "dev.azure.com") || strings.Contains(lower, "visualstudio.com"):
		return "azure_devops"
	default:
		return ""
	}
}
