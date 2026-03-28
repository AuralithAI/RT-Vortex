package providers

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

const (
	linkedinUserInfoURL = "https://api.linkedin.com/v2/userinfo"
	linkedinAuthURL     = "https://www.linkedin.com/oauth/v2/authorization"
	linkedinTokenURL    = "https://www.linkedin.com/oauth/v2/accessToken"
)

type linkedinUserInfo struct {
	Sub     string `json:"sub"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Picture string `json:"picture"`
}

// LinkedInProvider implements OAuth2 for LinkedIn (OpenID Connect).
type LinkedInProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewLinkedInProvider creates a LinkedIn OAuth2 provider.
func NewLinkedInProvider(cfg auth.OAuthProviderConfig) *LinkedInProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	return &LinkedInProvider{
		name: auth.ProviderLinkedIn,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  linkedinAuthURL,
				TokenURL: linkedinTokenURL,
			},
		},
	}
}

func (p *LinkedInProvider) Name() auth.ProviderName { return p.name }

func (p *LinkedInProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p *LinkedInProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *LinkedInProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var lu linkedinUserInfo
	if err := auth.FetchJSON(ctx, token, linkedinUserInfoURL, &lu); err != nil {
		return nil, fmt.Errorf("linkedin fetch user: %w", err)
	}
	return &auth.OAuthUser{
		ProviderID:   lu.Sub,
		Provider:     string(auth.ProviderLinkedIn),
		Email:        lu.Email,
		Name:         lu.Name,
		AvatarURL:    lu.Picture,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
