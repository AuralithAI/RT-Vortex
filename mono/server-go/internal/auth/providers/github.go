package providers

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubUserURL  = "https://api.github.com/user"
)

type githubUser struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubProvider implements OAuth2 for GitHub.
type GitHubProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewGitHubProvider creates a GitHub OAuth2 provider.
func NewGitHubProvider(cfg auth.OAuthProviderConfig) *GitHubProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"user:email", "read:org", "repo"}
	}
	return &GitHubProvider{
		name: auth.ProviderGitHub,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  githubAuthURL,
				TokenURL: githubTokenURL,
			},
		},
	}
}

func (p *GitHubProvider) Name() auth.ProviderName { return p.name }

func (p *GitHubProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *GitHubProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var gu githubUser
	if err := auth.FetchJSON(ctx, token, githubUserURL, &gu); err != nil {
		return nil, fmt.Errorf("github fetch user: %w", err)
	}
	name := gu.Name
	if name == "" {
		name = gu.Login
	}
	return &auth.OAuthUser{
		ProviderID:   strconv.Itoa(gu.ID),
		Provider:     string(auth.ProviderGitHub),
		Email:        gu.Email,
		Name:         name,
		AvatarURL:    gu.AvatarURL,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
