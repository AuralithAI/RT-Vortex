package providers

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	googleOAuth "golang.org/x/oauth2/google"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

const googleUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

type googleUser struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// GoogleProvider implements OAuth2 for Google.
type GoogleProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewGoogleProvider creates a Google OAuth2 provider.
func NewGoogleProvider(cfg auth.OAuthProviderConfig) *GoogleProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		}
	}
	return &GoogleProvider{
		name: auth.ProviderGoogle,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     googleOAuth.Endpoint,
		},
	}
}

func (p *GoogleProvider) Name() auth.ProviderName { return p.name }

func (p *GoogleProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
}

func (p *GoogleProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *GoogleProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var gu googleUser
	if err := auth.FetchJSON(ctx, token, googleUserInfoURL, &gu); err != nil {
		return nil, fmt.Errorf("google fetch user: %w", err)
	}
	return &auth.OAuthUser{
		ProviderID:   gu.ID,
		Provider:     string(auth.ProviderGoogle),
		Email:        gu.Email,
		Name:         gu.Name,
		AvatarURL:    gu.Picture,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
