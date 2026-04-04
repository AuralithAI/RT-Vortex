package providers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// ── X (Twitter) OAuth 2.0 with PKCE ────────────────────────────────────────
// Implements the Authorization Code Flow with PKCE as required by the X API v2.
// Reference: https://docs.x.com/resources/fundamentals/authentication/oauth-2-0
//
// Confidential clients authenticate the token request using HTTP Basic Auth
// (base64(client_id:client_secret)) — the golang.org/x/oauth2 package handles
// this automatically via oauth2.Endpoint.AuthStyle = AuthStyleInHeader.
//
// PKCE flow:
//   1. Generate a random code_verifier (43-128 chars, URL-safe).
//   2. Derive code_challenge = BASE64URL(SHA256(code_verifier)).
//   3. Include code_challenge + code_challenge_method=S256 in the authorize URL.
//   4. Include the original code_verifier in the token exchange request.
// ────────────────────────────────────────────────────────────────────────────

// X OAuth2 endpoints — v2 API.
var xEndpoint = oauth2.Endpoint{
	AuthURL:   "https://x.com/i/oauth2/authorize",
	TokenURL:  "https://api.x.com/2/oauth2/token",
	AuthStyle: oauth2.AuthStyleInHeader, // Basic auth for confidential clients
}

const xUserInfoURL = "https://api.x.com/2/users/me?user.fields=id,name,username,profile_image_url"

type xUserResponse struct {
	Data struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Username        string `json:"username"`
		ProfileImageURL string `json:"profile_image_url"`
	} `json:"data"`
}

// XProvider implements OAuth2 for X (formerly Twitter) with PKCE.
type XProvider struct {
	name   auth.ProviderName
	config *oauth2.Config

	// PKCE verifier store: state → code_verifier.
	// The verifier is generated during AuthURL and consumed during Exchange.
	mu        sync.Mutex
	verifiers map[string]string
}

// NewXProvider creates an X OAuth2 provider with PKCE support.
func NewXProvider(cfg auth.OAuthProviderConfig) *XProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		// tweet.read + users.read are the minimum for user profile lookup.
		// offline.access is required to get a refresh_token (tokens expire in 2h).
		scopes = []string{"tweet.read", "users.read", "offline.access"}
	}
	return &XProvider{
		name: auth.ProviderX,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     xEndpoint,
		},
		verifiers: make(map[string]string),
	}
}

func (p *XProvider) Name() auth.ProviderName { return p.name }

// AuthURL generates the authorization URL with PKCE parameters.
// The code_verifier is stored in memory keyed by the CSRF state token and
// will be retrieved during Exchange.
func (p *XProvider) AuthURL(state string) string {
	verifier := generateCodeVerifier()
	challenge := deriveCodeChallenge(verifier)

	// Store the verifier so we can include it in the token exchange.
	p.mu.Lock()
	p.verifiers[state] = verifier
	p.mu.Unlock()

	return p.config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// Exchange trades the authorization code for an access token.
// It automatically includes the PKCE code_verifier that was stored during AuthURL.
func (p *XProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	// The state is not passed to Exchange by the generic handler, so we look
	// for the code_verifier that was most recently stored. To make this precise
	// we use the ExchangeWithState helper when available (see handler).
	// Fallback: pop the only verifier (works for single-flight flows).
	p.mu.Lock()
	var verifier string
	for s, v := range p.verifiers {
		verifier = v
		delete(p.verifiers, s)
		break
	}
	p.mu.Unlock()

	opts := []oauth2.AuthCodeOption{}
	if verifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", verifier))
	}
	return p.config.Exchange(ctx, code, opts...)
}

// ExchangeWithState exchanges the code using the PKCE verifier associated
// with the given state. This is the preferred path when the handler knows
// the state value.
func (p *XProvider) ExchangeWithState(ctx context.Context, code, state string) (*oauth2.Token, error) {
	p.mu.Lock()
	verifier := p.verifiers[state]
	delete(p.verifiers, state)
	p.mu.Unlock()

	opts := []oauth2.AuthCodeOption{}
	if verifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", verifier))
	}
	return p.config.Exchange(ctx, code, opts...)
}

func (p *XProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var xResp xUserResponse
	if err := auth.FetchJSON(ctx, token, xUserInfoURL, &xResp); err != nil {
		return nil, fmt.Errorf("x fetch user: %w", err)
	}
	return &auth.OAuthUser{
		ProviderID:   xResp.Data.ID,
		Provider:     string(auth.ProviderX),
		Email:        "", // X API v2 doesn't return email by default
		Name:         xResp.Data.Name,
		AvatarURL:    xResp.Data.ProfileImageURL,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}

// ── PKCE helpers ────────────────────────────────────────────────────────────

// generateCodeVerifier produces a 43-byte URL-safe random string (RFC 7636).
func generateCodeVerifier() string {
	buf := make([]byte, 32) // 32 bytes → 43 base64url chars
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// deriveCodeChallenge returns BASE64URL(SHA256(verifier)) per RFC 7636 §4.2.
func deriveCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
