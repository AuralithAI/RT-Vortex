package providers

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

const (
	gitlabAuthURL  = "https://gitlab.com/oauth/authorize"
	gitlabTokenURL = "https://gitlab.com/oauth/token"
	gitlabUserURL  = "https://gitlab.com/api/v4/user"
)

type gitlabUser struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// GitLabProvider implements OAuth2 for GitLab.
type GitLabProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewGitLabProvider creates a GitLab OAuth2 provider.
func NewGitLabProvider(cfg auth.OAuthProviderConfig) *GitLabProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"read_user", "api"}
	}
	return &GitLabProvider{
		name: auth.ProviderGitLab,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  gitlabAuthURL,
				TokenURL: gitlabTokenURL,
			},
		},
	}
}

func (p *GitLabProvider) Name() auth.ProviderName { return p.name }

func (p *GitLabProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (p *GitLabProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *GitLabProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var gu gitlabUser
	if err := auth.FetchJSON(ctx, token, gitlabUserURL, &gu); err != nil {
		return nil, fmt.Errorf("gitlab fetch user: %w", err)
	}
	name := gu.Name
	if name == "" {
		name = gu.Username
	}
	return &auth.OAuthUser{
		ProviderID:   strconv.Itoa(gu.ID),
		Provider:     string(auth.ProviderGitLab),
		Email:        gu.Email,
		Name:         name,
		AvatarURL:    gu.AvatarURL,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
