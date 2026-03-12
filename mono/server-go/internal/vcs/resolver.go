// Package vcs — VCSResolver resolves VCS platform credentials per-repo from
// the user vault (secrets) and PostgreSQL (non-secret config).
//
// Every repo belongs to an org; the org owner's vault is
// used to resolve platform tokens and webhook secrets.
//
// Token resolution order:
//  1. Find the repository by ID → get org_id + platform
//  2. Find the org owner (role = "owner") → get vault_token
//  3. Read the platform token from the owner's vault ("vcs.<platform>.token")
//  4. Read non-secret config (base_url, api_url, etc.) from user_vcs_platforms
//  5. Construct an ephemeral Platform client for the request
//
// Webhook secret resolution:
//   - The per-repo webhook secret is stored in repositories.webhook_secret
//     (generated at repo registration time).  Webhook handlers validate
//     signatures against this DB-stored secret without needing any XML config.
package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Resolver ────────────────────────────────────────────────────────────────

// VaultReader abstracts the file vault so that the VCS package does not
// import the vault package directly.
type VaultReader interface {
	// GetScoped reads a secret from the vault using the user's vault token.
	GetScoped(vaultToken, key string) (string, error)
}

// ResolvedCreds holds the credentials resolved for a specific platform+user.
type ResolvedCreds struct {
	Platform      PlatformType
	Token         string // PAT / OAuth token
	WebhookSecret string // per-repo webhook secret (from DB)
	BaseURL       string // platform base URL override
	APIURL        string // API URL override
	Organization  string // Azure DevOps org name
	Username      string // Bitbucket username
}

// Resolver resolves VCS credentials per-repo from vault + DB.
// It replaces the static PlatformRegistry for runtime operations.
type Resolver struct {
	db    *pgxpool.Pool
	vault VaultReader
}

// NewResolver creates a VCS credential resolver.
func NewResolver(db *pgxpool.Pool, vault VaultReader) *Resolver {
	return &Resolver{db: db, vault: vault}
}

// ForRepo resolves VCS credentials for a repository and returns a ready-to-use
// Platform client.  The credentials come from the org owner's vault and the
// per-user VCS platform config in PostgreSQL.
func (r *Resolver) ForRepo(ctx context.Context, repoID uuid.UUID) (Platform, error) {
	// 1. Look up the repo to get platform + org_id.
	var platform, webhookSecret string
	var orgID uuid.UUID
	err := r.db.QueryRow(ctx,
		`SELECT platform, org_id, webhook_secret FROM repositories WHERE id = $1`, repoID,
	).Scan(&platform, &orgID, &webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("vcs resolver: repo lookup: %w", err)
	}

	creds, err := r.resolveOrgOwnerCreds(ctx, orgID, PlatformType(platform))
	if err != nil {
		return nil, err
	}
	creds.WebhookSecret = webhookSecret

	return r.buildClient(creds)
}

// ForRepoDirect resolves VCS credentials given a pre-loaded repo's fields,
// avoiding an extra DB round-trip when the caller already has the repo.
func (r *Resolver) ForRepoDirect(ctx context.Context, orgID uuid.UUID, platform PlatformType, webhookSecret string) (Platform, error) {
	creds, err := r.resolveOrgOwnerCreds(ctx, orgID, platform)
	if err != nil {
		return nil, err
	}
	creds.WebhookSecret = webhookSecret
	return r.buildClient(creds)
}

// resolveOrgOwnerCreds finds the org owner and reads their vault + DB config.
func (r *Resolver) resolveOrgOwnerCreds(ctx context.Context, orgID uuid.UUID, platform PlatformType) (*ResolvedCreds, error) {
	// 2. Find the org owner → get their vault_token.
	var ownerID uuid.UUID
	var vaultToken string
	err := r.db.QueryRow(ctx,
		`SELECT u.id, u.vault_token
		   FROM users u
		   JOIN org_members om ON om.user_id = u.id
		  WHERE om.org_id = $1 AND om.role = 'owner'
		  LIMIT 1`, orgID,
	).Scan(&ownerID, &vaultToken)
	if err != nil {
		return nil, fmt.Errorf("vcs resolver: org owner lookup: %w", err)
	}

	if vaultToken == "" {
		return nil, fmt.Errorf("vcs resolver: org owner %s has no vault token", ownerID)
	}

	// 3. Read the platform token from the owner's vault.
	tokenKey := "vcs." + string(platform) + ".token"
	if platform == PlatformAzureDevOps {
		tokenKey = "vcs.azure_devops.pat"
	}

	token, err := r.vault.GetScoped(vaultToken, tokenKey)
	if err != nil {
		slog.Warn("vcs resolver: failed to read token from vault",
			"platform", platform, "owner_id", ownerID, "error", err)
		// Non-fatal — token may not be configured yet.
	}

	// Also try to read the webhook secret from vault (for webhook validation
	// when repo-level secret isn't available).
	whKey := "vcs." + string(platform) + ".webhook_secret"
	vaultWebhookSecret, _ := r.vault.GetScoped(vaultToken, whKey)

	// 4. Read non-secret config from user_vcs_platforms.
	var baseURL, apiURL, organization, username string
	_ = r.db.QueryRow(ctx,
		`SELECT COALESCE(base_url,''), COALESCE(api_url,''), COALESCE(organization,''), COALESCE(username,'')
		   FROM user_vcs_platforms
		  WHERE user_id = $1 AND platform = $2`, ownerID, string(platform),
	).Scan(&baseURL, &apiURL, &organization, &username)
	// Not found is OK — defaults will be used.

	return &ResolvedCreds{
		Platform:      platform,
		Token:         token,
		WebhookSecret: vaultWebhookSecret,
		BaseURL:       baseURL,
		APIURL:        apiURL,
		Organization:  organization,
		Username:      username,
	}, nil
}

// buildClient creates an ephemeral VCS Platform client from resolved creds.
// The returned client is not cached — it's created fresh for each request
// so that credential changes in the vault take effect immediately.
func (r *Resolver) buildClient(creds *ResolvedCreds) (Platform, error) {
	switch creds.Platform {
	case PlatformGitHub:
		return newGitHubFromCreds(creds), nil
	case PlatformGitLab:
		return newGitLabFromCreds(creds), nil
	case PlatformBitbucket:
		return newBitbucketFromCreds(creds), nil
	case PlatformAzureDevOps:
		return newAzureDevOpsFromCreds(creds), nil
	default:
		return nil, fmt.Errorf("vcs resolver: unsupported platform: %s", creds.Platform)
	}
}

// ── Webhook Validation (standalone, no Platform client needed) ──────────────

// ValidateWebhookHMAC validates an HMAC-SHA256 webhook signature against a
// secret.  Used by GitHub and Bitbucket webhooks.
func ValidateWebhookHMAC(payload []byte, signature, secret string) bool {
	if secret == "" {
		slog.Warn("webhook: secret is empty, skipping validation")
		return true
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// ValidateWebhookToken validates a plaintext webhook token comparison.
// Used by GitLab (X-Gitlab-Token) and Azure DevOps (X-Vss-Token).
func ValidateWebhookToken(token, secret string) bool {
	if secret == "" {
		slog.Warn("webhook: secret is empty, skipping validation")
		return true
	}
	return token == secret
}

// ResolveRepoWebhookSecret looks up the per-repo webhook secret from the DB.
// Returns empty string if not found.
func (r *Resolver) ResolveRepoWebhookSecret(ctx context.Context, platform, externalID string) string {
	var secret string
	err := r.db.QueryRow(ctx,
		`SELECT webhook_secret FROM repositories WHERE platform = $1 AND external_id = $2`,
		platform, externalID,
	).Scan(&secret)
	if err != nil {
		return ""
	}
	return secret
}

// ResolveWebhookSecretForOrg looks up the webhook secret from the org owner's
// vault for a given platform.  Used as fallback when per-repo secret is empty.
func (r *Resolver) ResolveWebhookSecretForOrg(ctx context.Context, orgID uuid.UUID, platform PlatformType) string {
	creds, err := r.resolveOrgOwnerCreds(ctx, orgID, platform)
	if err != nil {
		return ""
	}
	return creds.WebhookSecret
}
