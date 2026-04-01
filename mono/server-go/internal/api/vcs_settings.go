package api

// ─── VCS Settings Handlers ──────────────────────────────────────────────────
// Per-user VCS platform credential management.
//
// Non-secret fields (URLs, usernames, org names) are stored in PostgreSQL
// in the user_vcs_platforms table. Actual secrets (tokens, passwords, webhook
// secrets) are stored in the encrypted file vault, scoped by the user's
// crypto-random vault token.
//
// The vault token is never exposed to the frontend — it's resolved server-side
// from the authenticated user record.
//
// Supported platforms:
//   - github:       token, webhook_secret │ base_url, api_url
//   - gitlab:       token, webhook_secret │ base_url
//   - bitbucket:    token, app_password, webhook_secret │ base_url, api_url, username
//   - azure_devops: pat, webhook_secret, client_secret │ base_url, organization, tenant_id, client_id
// ─────────────────────────────────────────────────────────────────────────────

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/vault"
)

// ── Field Definitions ───────────────────────────────────────────────────────

// vcsFieldDef describes a single configurable field for a VCS platform.
type vcsFieldDef struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Secret       bool   `json:"secret"`        // true → stored in vault; false → stored in DB
	DefaultValue string `json:"default_value"` // default for URL fields (empty = no default)
	Hint         string `json:"hint"`          // UI hint text
}

var vcsPlatformDefs = map[string]struct {
	DisplayName string
	Fields      []vcsFieldDef
}{
	"github": {
		DisplayName: "GitHub",
		Fields: []vcsFieldDef{
			{Key: "token", Label: "Personal Access Token (PAT)", Secret: true},
			{Key: "webhook_secret", Label: "Webhook Secret", Secret: true},
			{Key: "base_url", Label: "Base URL", Secret: false, DefaultValue: "https://github.com", Hint: "Change only for GitHub Enterprise Server"},
			{Key: "api_url", Label: "API URL", Secret: false, DefaultValue: "https://api.github.com", Hint: "Change only for GitHub Enterprise Server"},
		},
	},
	"gitlab": {
		DisplayName: "GitLab",
		Fields: []vcsFieldDef{
			{Key: "token", Label: "Personal Access Token", Secret: true},
			{Key: "webhook_secret", Label: "Webhook Secret", Secret: true},
			{Key: "base_url", Label: "Base URL", Secret: false, DefaultValue: "https://gitlab.com", Hint: "Change only for self-hosted GitLab"},
		},
	},
	"bitbucket": {
		DisplayName: "Bitbucket",
		Fields: []vcsFieldDef{
			{Key: "token", Label: "Access Token", Secret: true},
			{Key: "app_password", Label: "App Password", Secret: true},
			{Key: "webhook_secret", Label: "Webhook Secret", Secret: true},
			{Key: "username", Label: "Username", Secret: false, Hint: "Required for App Password authentication"},
			{Key: "base_url", Label: "Base URL", Secret: false, DefaultValue: "https://bitbucket.org", Hint: "Change only for Bitbucket Server / Data Center"},
			{Key: "api_url", Label: "API URL", Secret: false, DefaultValue: "https://api.bitbucket.org/2.0", Hint: "Change only for Bitbucket Server / Data Center"},
		},
	},
	"azure_devops": {
		DisplayName: "Azure DevOps",
		Fields: []vcsFieldDef{
			{Key: "pat", Label: "Personal Access Token", Secret: true},
			{Key: "webhook_secret", Label: "Webhook Secret", Secret: true},
			{Key: "client_secret", Label: "Azure AD Client Secret", Secret: true},
			{Key: "organization", Label: "Organization", Secret: false, Hint: "Your Azure DevOps organization name"},
			{Key: "base_url", Label: "Base URL", Secret: false, DefaultValue: "https://dev.azure.com", Hint: "Change only for Azure DevOps Server (on-premises)"},
			{Key: "tenant_id", Label: "Azure AD Tenant ID", Secret: false, Hint: "Required for Azure AD authentication"},
			{Key: "client_id", Label: "Azure AD Client ID", Secret: false, Hint: "Required for Azure AD authentication"},
		},
	},
}

// vcsPlatformOrder is the display order.
var vcsPlatformOrder = []string{"github", "gitlab", "bitbucket", "azure_devops"}

// getDBFieldValue extracts a non-secret field value from the DB model.
func getDBFieldValue(p *model.UserVCSPlatform, key string) string {
	if p == nil {
		return ""
	}
	switch key {
	case "base_url":
		return p.BaseURL
	case "api_url":
		return p.APIURL
	case "organization":
		return p.Organization
	case "username":
		return p.Username
	case "tenant_id":
		return p.TenantID
	case "client_id":
		return p.ClientID
	default:
		return ""
	}
}

// setDBFieldValue sets a non-secret field value on the DB model.
func setDBFieldValue(p *model.UserVCSPlatform, key, val string) {
	switch key {
	case "base_url":
		p.BaseURL = val
	case "api_url":
		p.APIURL = val
	case "organization":
		p.Organization = val
	case "username":
		p.Username = val
	case "tenant_id":
		p.TenantID = val
	case "client_id":
		p.ClientID = val
	}
}

// ── Handlers ────────────────────────────────────────────────────────────────

// ListVCSPlatforms returns all supported VCS platforms with their fields and
// the user's current configuration status (configured or not).
// GET /api/v1/vcs/platforms
func (h *Handler) ListVCSPlatforms(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Load user-scoped vault via keychain.
	var userVault vault.SecretStore
	if uv := h.userVaultFor(userID); uv != nil {
		userVault = uv
	}

	// Load all user VCS platform configs from DB.
	var dbConfigs map[string]*model.UserVCSPlatform
	if h.VCSPlatformRepo != nil {
		platforms, _ := h.VCSPlatformRepo.ListByUser(r.Context(), userID)
		dbConfigs = make(map[string]*model.UserVCSPlatform, len(platforms))
		for _, p := range platforms {
			dbConfigs[p.Platform] = p
		}
	}

	type fieldInfo struct {
		Key          string `json:"key"`
		Label        string `json:"label"`
		Secret       bool   `json:"secret"`
		HasValue     bool   `json:"has_value"`
		Value        string `json:"value"`
		DefaultValue string `json:"default_value,omitempty"`
		Hint         string `json:"hint,omitempty"`
	}

	type platformInfo struct {
		Name        string      `json:"name"`
		DisplayName string      `json:"display_name"`
		Configured  bool        `json:"configured"`
		Fields      []fieldInfo `json:"fields"`
	}

	result := make([]platformInfo, 0, len(vcsPlatformOrder))
	for _, name := range vcsPlatformOrder {
		def := vcsPlatformDefs[name]
		info := platformInfo{
			Name:        name,
			DisplayName: def.DisplayName,
		}

		dbConfig := dbConfigs[name] // may be nil

		for _, field := range def.Fields {
			fi := fieldInfo{
				Key:          field.Key,
				Label:        field.Label,
				Secret:       field.Secret,
				DefaultValue: field.DefaultValue,
				Hint:         field.Hint,
			}

			if field.Secret {
				// Secret fields — read from vault.
				if userVault != nil {
					vaultKey := fmt.Sprintf("vcs.%s.%s", name, field.Key)
					val, _ := userVault.Get(vaultKey)
					if val != "" {
						fi.HasValue = true
						// Mask: show last 4 chars.
						if len(val) > 4 {
							fi.Value = strings.Repeat("•", len(val)-4) + val[len(val)-4:]
						} else {
							fi.Value = strings.Repeat("•", len(val))
						}
						info.Configured = true
					}
				}
			} else {
				// Non-secret fields — read from DB.
				dbVal := getDBFieldValue(dbConfig, field.Key)
				if dbVal != "" {
					fi.HasValue = true
					fi.Value = dbVal
				}
			}

			info.Fields = append(info.Fields, fi)
		}

		result = append(result, info)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"platforms": result,
	})
}

// ConfigureVCSPlatform saves credentials for a VCS platform.
// Secret fields go to the vault; non-secret fields go to PostgreSQL.
// PUT /api/v1/vcs/platforms/{platform}
func (h *Handler) ConfigureVCSPlatform(w http.ResponseWriter, r *http.Request) {
	platformName := chi.URLParam(r, "platform")
	def, ok := vcsPlatformDefs[platformName]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported platform: "+platformName)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	userVault := h.userVaultFor(userID)
	if userVault == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain not configured")
		return
	}
	if h.VCSPlatformRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "VCS platform store not configured")
		return
	}

	var req map[string]string
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Build set of allowed field keys with their types.
	allowedSecret := make(map[string]bool, len(def.Fields))
	allowedDB := make(map[string]bool, len(def.Fields))
	for _, f := range def.Fields {
		if f.Secret {
			allowedSecret[f.Key] = true
		} else {
			allowedDB[f.Key] = true
		}
	}

	// Load existing DB config or create new one.
	dbPlatform, err := h.VCSPlatformRepo.GetByUserAndPlatform(r.Context(), userID, platformName)
	if errors.Is(err, store.ErrNotFound) {
		dbPlatform = &model.UserVCSPlatform{
			UserID:   userID,
			Platform: platformName,
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load VCS config")
		return
	}

	savedSecrets := 0
	savedDB := 0
	for key, val := range req {
		if allowedSecret[key] {
			// Secret field → vault.
			vaultKey := fmt.Sprintf("vcs.%s.%s", platformName, key)
			if val == "" {
				_ = userVault.Delete(vaultKey)
			} else {
				if err := userVault.Set(vaultKey, val); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to save credential")
					return
				}
			}
			savedSecrets++
		} else if allowedDB[key] {
			// Non-secret field → DB.
			setDBFieldValue(dbPlatform, key, val)
			savedDB++
		}
	}

	// Persist DB fields if any were provided.
	if savedDB > 0 {
		if err := h.VCSPlatformRepo.Upsert(r.Context(), dbPlatform); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save VCS config")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"platform":      platformName,
		"saved_secrets": savedSecrets,
		"saved_config":  savedDB,
	})
}

// DeleteVCSPlatform removes all credentials for a VCS platform.
// DELETE /api/v1/vcs/platforms/{platform}
func (h *Handler) DeleteVCSPlatform(w http.ResponseWriter, r *http.Request) {
	platformName := chi.URLParam(r, "platform")
	def, ok := vcsPlatformDefs[platformName]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported platform: "+platformName)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	userVault := h.userVaultFor(userID)
	if userVault == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain not configured")
		return
	}

	// Delete all secret fields from vault.
	deletedVault := 0
	for _, f := range def.Fields {
		if f.Secret {
			vaultKey := fmt.Sprintf("vcs.%s.%s", platformName, f.Key)
			if err := userVault.Delete(vaultKey); err == nil {
				deletedVault++
			}
		}
	}

	// Delete DB config.
	if h.VCSPlatformRepo != nil {
		_ = h.VCSPlatformRepo.DeleteByUserAndPlatform(r.Context(), userID, platformName)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"platform": platformName,
		"deleted":  deletedVault,
	})
}

// TestVCSPlatform tests connectivity to a VCS platform using the user's credentials.
// POST /api/v1/vcs/platforms/{platform}/test
func (h *Handler) TestVCSPlatform(w http.ResponseWriter, r *http.Request) {
	platformName := chi.URLParam(r, "platform")
	if _, ok := vcsPlatformDefs[platformName]; !ok {
		writeError(w, http.StatusBadRequest, "unsupported platform: "+platformName)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	userVault := h.userVaultFor(userID)
	if userVault == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain not configured")
		return
	}

	// Load token from vault.
	token, _ := userVault.Get(fmt.Sprintf("vcs.%s.token", platformName))
	if platformName == "azure_devops" {
		pat, _ := userVault.Get("vcs.azure_devops.pat")
		if pat != "" {
			token = pat
		}
	}

	if token == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"platform": platformName,
			"success":  false,
			"error":    "No token configured. Please save your credentials first.",
		})
		return
	}

	// Load non-secret config from DB for URL resolution.
	var dbConfig *model.UserVCSPlatform
	if h.VCSPlatformRepo != nil {
		dbConfig, _ = h.VCSPlatformRepo.GetByUserAndPlatform(r.Context(), userID, platformName)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	success, message := testVCSToken(ctx, platformName, token, dbConfig)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"platform": platformName,
		"success":  success,
		"message":  message,
	})
}

// testVCSToken makes a lightweight authenticated request to verify the token works.
// URLs are resolved from the DB config (with defaults for well-known platforms).
func testVCSToken(ctx context.Context, platform, token string, dbConfig *model.UserVCSPlatform) (bool, string) {
	var apiURL, authHeader string

	// Helper to get DB field with a fallback default.
	getURL := func(key, fallback string) string {
		val := getDBFieldValue(dbConfig, key)
		if val == "" {
			return fallback
		}
		return val
	}

	switch platform {
	case "github":
		base := getURL("api_url", "https://api.github.com")
		apiURL = strings.TrimSuffix(base, "/") + "/user"
		authHeader = "Bearer " + token

	case "gitlab":
		base := getURL("base_url", "https://gitlab.com")
		apiURL = strings.TrimSuffix(base, "/") + "/api/v4/user"
		authHeader = "Bearer " + token

	case "bitbucket":
		base := getURL("api_url", "https://api.bitbucket.org/2.0")
		isBBServer := !strings.Contains(base, "bitbucket.org")
		if isBBServer {
			// Bitbucket Server / Data Center REST API
			// base should be the full base URL (e.g. https://bb.example.com)
			// The REST endpoint is /rest/api/1.0/users on the base host.
			serverBase := getURL("base_url", base)
			apiURL = strings.TrimSuffix(serverBase, "/") + "/rest/api/1.0/users?limit=1"
			// Bitbucket Server HTTP Access Tokens use Bearer auth.
			authHeader = "Bearer " + token
		} else {
			// Bitbucket Cloud
			apiURL = strings.TrimSuffix(base, "/") + "/user"
			authHeader = "Bearer " + token
		}

	case "azure_devops":
		org := getDBFieldValue(dbConfig, "organization")
		base := getURL("base_url", "https://dev.azure.com")
		if org == "" {
			return false, "Organization is required for Azure DevOps."
		}
		apiURL = fmt.Sprintf("%s/%s/_apis/projects?api-version=7.1&$top=1", strings.TrimSuffix(base, "/"), org)
		authHeader = "Basic " + basicAuth("", token)

	default:
		return false, "Unknown platform"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, "Failed to create request: " + err.Error()
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, "Connection failed: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, fmt.Sprintf("Connected successfully (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return false, fmt.Sprintf("Authentication failed (HTTP %d). Please check your token.", resp.StatusCode)
	}
	return false, fmt.Sprintf("Unexpected response (HTTP %d)", resp.StatusCode)
}

// basicAuth encodes user:pass for Basic auth header.
func basicAuth(user, pass string) string {
	creds := user + ":" + pass
	return base64.StdEncoding.EncodeToString([]byte(creds))
}

// ── Token Capability Awareness ──────────────────────────────────────────────

// tokenCapability describes what operations a particular token type supports.
type tokenCapability struct {
	TokenType  string   `json:"token_type"`
	Label      string   `json:"label"`
	CanClone   bool     `json:"can_clone"`
	CanReview  bool     `json:"can_review"`  // post comments, approve PRs
	CanWebhook bool     `json:"can_webhook"` // manage webhooks
	CanReadPR  bool     `json:"can_read_pr"` // list/read pull requests
	Scopes     []string `json:"scopes"`      // required scopes/permissions
	SetupGuide string   `json:"setup_guide"` // short instructions
}

// platformTokenCapabilities defines per-provider token types and their capabilities.
var platformTokenCapabilities = map[string][]tokenCapability{
	"github": {
		{
			TokenType:  "fine_grained_pat",
			Label:      "Fine-Grained Personal Access Token (recommended)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"Contents: Read", "Pull requests: Read & Write", "Webhooks: Read & Write"},
			SetupGuide: "Settings → Developer settings → Personal access tokens → Fine-grained tokens. Select specific repositories and grant Contents, Pull requests, and Webhooks permissions.",
		},
		{
			TokenType:  "classic_pat",
			Label:      "Classic Personal Access Token",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"repo", "admin:repo_hook"},
			SetupGuide: "Settings → Developer settings → Personal access tokens → Tokens (classic). Select 'repo' and 'admin:repo_hook' scopes.",
		},
		{
			TokenType:  "github_app",
			Label:      "GitHub App Installation Token",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"contents:read", "pull_requests:write", "webhooks"},
			SetupGuide: "Install the RTVortex GitHub App on your organization. Tokens are managed automatically.",
		},
		{
			TokenType:  "oauth_token",
			Label:      "OAuth Token (SSO login)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: false,
			CanReadPR:  true,
			Scopes:     []string{"repo"},
			SetupGuide: "OAuth tokens are issued during GitHub SSO login. They may not include webhook management permissions.",
		},
	},
	"gitlab": {
		{
			TokenType:  "personal_access_token",
			Label:      "Personal Access Token",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"api", "read_repository", "write_repository"},
			SetupGuide: "Preferences → Access Tokens → Add new token. Select 'api' scope for full access, or 'read_repository' + 'write_repository' for limited access.",
		},
		{
			TokenType:  "project_access_token",
			Label:      "Project Access Token",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: false,
			CanReadPR:  true,
			Scopes:     []string{"api"},
			SetupGuide: "Project → Settings → Access Tokens. Scoped to a single project — cannot manage webhooks at the group level.",
		},
		{
			TokenType:  "group_access_token",
			Label:      "Group Access Token",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"api"},
			SetupGuide: "Group → Settings → Access Tokens. Covers all projects in the group.",
		},
	},
	"bitbucket": {
		{
			TokenType:  "http_access_token",
			Label:      "HTTP Access Token (Bitbucket Server / Data Center)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: false,
			CanReadPR:  true,
			Scopes:     []string{"Repository Read", "Repository Write"},
			SetupGuide: "Bitbucket Server → Manage account → HTTP access tokens → Create token. Grant 'Repository Read' and 'Repository Write' permissions. Set the Base URL to your Bitbucket Server URL (e.g. https://bitbucket.example.com).",
		},
		{
			TokenType:  "app_password",
			Label:      "App Password (Bitbucket Cloud)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"Repositories: Read", "Repositories: Write", "Pull requests: Read", "Pull requests: Write", "Webhooks: Read and write"},
			SetupGuide: "Personal settings → App passwords → Create app password. Select Repository (Read/Write), Pull requests (Read/Write), and Webhooks permissions.",
		},
		{
			TokenType:  "oauth_consumer",
			Label:      "OAuth Consumer (Bitbucket Cloud)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: false,
			CanReadPR:  true,
			Scopes:     []string{"repository", "pullrequest"},
			SetupGuide: "Workspace settings → OAuth consumers → Add consumer. Webhook management requires App passwords.",
		},
		{
			TokenType:  "repository_access_token",
			Label:      "Repository Access Token (Bitbucket Cloud)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: false,
			CanReadPR:  true,
			Scopes:     []string{"repository:read", "repository:write", "pullrequest:read", "pullrequest:write"},
			SetupGuide: "Repository → Repository settings → Access tokens. Scoped to one repository.",
		},
	},
	"azure_devops": {
		{
			TokenType:  "personal_access_token",
			Label:      "Personal Access Token (PAT)",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"Code (Read & Write)", "Pull Request Threads (Read & Write)", "Service Hooks (Read & Manage)"},
			SetupGuide: "User settings → Personal access tokens → New Token. Select 'Code (Read & Write)', 'Pull Request Threads (Read & Write)', and 'Service Hooks (Read & Manage)' scopes.",
		},
		{
			TokenType:  "service_principal",
			Label:      "Azure AD Service Principal",
			CanClone:   true,
			CanReview:  true,
			CanWebhook: true,
			CanReadPR:  true,
			Scopes:     []string{"vso.code_write", "vso.hooks_write"},
			SetupGuide: "Azure Portal → App registrations → New registration. Grant Azure DevOps API permissions. Configure client_id, tenant_id, and client_secret.",
		},
	},
}

// ListVCSTokenCapabilities returns token type capabilities for each platform.
// GET /api/v1/vcs/token-capabilities
func (h *Handler) ListVCSTokenCapabilities(w http.ResponseWriter, r *http.Request) {
	// Optional: filter by platform
	platformFilter := r.URL.Query().Get("platform")

	result := make(map[string][]tokenCapability)
	for platform, caps := range platformTokenCapabilities {
		if platformFilter != "" && platform != platformFilter {
			continue
		}
		result[platform] = caps
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"capabilities": result,
	})
}

// CheckClonePermission verifies that the user's stored VCS token can clone a specific repo.
// POST /api/v1/vcs/platforms/{platform}/check-clone
func (h *Handler) CheckClonePermission(w http.ResponseWriter, r *http.Request) {
	platformName := chi.URLParam(r, "platform")
	if _, ok := vcsPlatformDefs[platformName]; !ok {
		writeError(w, http.StatusBadRequest, "unsupported platform: "+platformName)
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		CloneURL string `json:"clone_url"`
	}
	if err := readJSON(r, &req); err != nil || req.CloneURL == "" {
		writeError(w, http.StatusBadRequest, "clone_url is required")
		return
	}

	userVault := h.userVaultFor(userID)
	if userVault == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"platform":  platformName,
			"can_clone": false,
			"reason":    "Keychain not configured. Cannot verify token.",
			"has_token": false,
		})
		return
	}

	// Resolve token
	tokenKey := "vcs." + platformName + ".token"
	if platformName == "azure_devops" {
		tokenKey = "vcs.azure_devops.pat"
	}
	token, _ := userVault.Get(tokenKey)

	if token == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"platform":        platformName,
			"can_clone":       false,
			"reason":          "No token configured for " + platformName + ". Please add your credentials in VCS settings.",
			"has_token":       false,
			"needs_different": false,
		})
		return
	}

	// Quick check: try an authenticated lightweight API call to verify repo access.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	canClone, reason := checkRepoAccess(ctx, platformName, token, req.CloneURL)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"platform":        platformName,
		"can_clone":       canClone,
		"reason":          reason,
		"has_token":       true,
		"needs_different": !canClone && token != "",
	})
}

// checkRepoAccess makes a lightweight API call to verify the token has read access to the repository.
func checkRepoAccess(ctx context.Context, platform, token, cloneURL string) (bool, string) {
	// Parse owner/repo from the clone URL
	parsed, err := parseRepoURL(cloneURL)
	if err != nil {
		return false, "Could not parse repository URL"
	}

	var apiURL, authHeader string
	switch platform {
	case "github":
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/%s", parsed.owner, parsed.name)
		authHeader = "Bearer " + token
	case "gitlab":
		// GitLab needs URL-encoded project path
		project := parsed.owner + "/" + parsed.name
		apiURL = fmt.Sprintf("https://gitlab.com/api/v4/projects/%s", strings.ReplaceAll(project, "/", "%%2F"))
		authHeader = "Bearer " + token
	case "bitbucket":
		// Detect Bitbucket Server vs Cloud from the clone URL host.
		isBBServer := !strings.Contains(cloneURL, "bitbucket.org")
		if isBBServer {
			// Bitbucket Server REST API: /rest/api/1.0/projects/{PROJECT}/repos/{SLUG}
			// Extract the base URL from the clone URL itself.
			u, _ := url.Parse(cloneURL)
			if u != nil && u.Host != "" {
				serverBase := u.Scheme + "://" + u.Host
				apiURL = fmt.Sprintf("%s/rest/api/1.0/projects/%s/repos/%s",
					serverBase, strings.ToUpper(parsed.owner), parsed.name)
			} else {
				return false, "Could not determine Bitbucket Server base URL from clone URL"
			}
			authHeader = "Bearer " + token
		} else {
			apiURL = fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", parsed.owner, parsed.name)
			authHeader = "Bearer " + token
		}
	case "azure_devops":
		// For Azure DevOps the URL structure is different — do a basic auth check
		apiURL = fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories?api-version=7.1", parsed.owner, parsed.name)
		authHeader = "Basic " + basicAuth("", token)
	default:
		return false, "Unknown platform"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, "Failed to create request"
	}
	httpReq.Header.Set("Authorization", authHeader)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return false, "Connection failed: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, "Token has access to this repository"
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return false, "Token does not have access to this repository. You may need a token with broader scope or access to this specific repo."
	}
	if resp.StatusCode == 404 {
		return false, "Repository not found or token lacks read access. For private repos, ensure your token includes repository read permissions."
	}
	return false, fmt.Sprintf("Unexpected response (HTTP %d)", resp.StatusCode)
}
