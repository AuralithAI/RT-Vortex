package providers

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// X (Twitter) OAuth2 endpoints (v2 API with PKCE)
var xEndpoint = oauth2.Endpoint{
	AuthURL:  "https://twitter.com/i/oauth2/authorize",
	TokenURL: "https://api.twitter.com/2/oauth2/token",
}

const xUserInfoURL = "https://api.twitter.com/2/users/me?user.fields=id,name,username,profile_image_url"

type xUserResponse struct {
	Data struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Username        string `json:"username"`
		ProfileImageURL string `json:"profile_image_url"`
	} `json:"data"`
}

// XProvider implements OAuth2 for X (formerly Twitter).
type XProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewXProvider creates an X OAuth2 provider.
func NewXProvider(cfg auth.OAuthProviderConfig) *XProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"tweet.read", "users.read"}
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
	}
}

func (p *XProvider) Name() auth.ProviderName { return p.name }

func (p *XProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p *XProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
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
