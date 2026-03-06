package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// Apple OAuth2 endpoints
var appleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://appleid.apple.com/auth/authorize",
	TokenURL: "https://appleid.apple.com/auth/token",
}

// AppleProvider implements OAuth2 for Sign in with Apple.
type AppleProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewAppleProvider creates an Apple OAuth2 provider.
func NewAppleProvider(cfg auth.OAuthProviderConfig) *AppleProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"name", "email"}
	}
	return &AppleProvider{
		name: auth.ProviderApple,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     appleEndpoint,
		},
	}
}

func (p *AppleProvider) Name() auth.ProviderName { return p.name }

func (p *AppleProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("response_mode", "form_post"),
		oauth2.SetAuthURLParam("response_type", "code"),
	)
}

func (p *AppleProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *AppleProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	// Apple returns user info in the ID token (JWT), not via a separate API.
	// Parse the id_token claim from the token response.
	idToken, ok := token.Extra("id_token").(string)
	if !ok || idToken == "" {
		return nil, fmt.Errorf("apple: no id_token in token response")
	}

	// Decode the JWT payload (middle segment) without verification
	// (token was just exchanged over TLS, so it's trusted).
	parts := strings.SplitN(idToken, ".", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("apple: malformed id_token")
	}
	payload, err := auth.Base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("apple: decode id_token payload: %w", err)
	}

	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("apple: parse id_token claims: %w", err)
	}

	return &auth.OAuthUser{
		ProviderID:   claims.Sub,
		Provider:     string(auth.ProviderApple),
		Email:        claims.Email,
		Name:         "", // Apple only sends name on first authorization
		AvatarURL:    "",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
