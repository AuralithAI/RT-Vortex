package providers

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

const (
	bitbucketAuthURL  = "https://bitbucket.org/site/oauth2/authorize"
	bitbucketTokenURL = "https://bitbucket.org/site/oauth2/access_token"
	bitbucketUserURL  = "https://api.bitbucket.org/2.0/user"
	bitbucketEmailURL = "https://api.bitbucket.org/2.0/user/emails"
)

type bitbucketUser struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Links       struct {
		Avatar struct {
			Href string `json:"href"`
		} `json:"avatar"`
	} `json:"links"`
}

type bitbucketEmail struct {
	Values []struct {
		Email     string `json:"email"`
		IsPrimary bool   `json:"is_primary"`
	} `json:"values"`
}

// BitbucketProvider implements OAuth2 for Bitbucket.
type BitbucketProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewBitbucketProvider creates a Bitbucket OAuth2 provider.
func NewBitbucketProvider(cfg auth.OAuthProviderConfig) *BitbucketProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"account", "repository"}
	}
	return &BitbucketProvider{
		name: auth.ProviderBitbucket,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  bitbucketAuthURL,
				TokenURL: bitbucketTokenURL,
			},
		},
	}
}

func (p *BitbucketProvider) Name() auth.ProviderName { return p.name }

func (p *BitbucketProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

func (p *BitbucketProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *BitbucketProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var bu bitbucketUser
	if err := auth.FetchJSON(ctx, token, bitbucketUserURL, &bu); err != nil {
		return nil, fmt.Errorf("bitbucket fetch user: %w", err)
	}

	// Bitbucket requires a separate API call for the primary email.
	var be bitbucketEmail
	if err := auth.FetchJSON(ctx, token, bitbucketEmailURL, &be); err != nil {
		return nil, fmt.Errorf("bitbucket fetch emails: %w", err)
	}
	email := ""
	for _, e := range be.Values {
		if e.IsPrimary {
			email = e.Email
			break
		}
	}
	if email == "" && len(be.Values) > 0 {
		email = be.Values[0].Email
	}

	return &auth.OAuthUser{
		ProviderID:   bu.UUID,
		Provider:     string(auth.ProviderBitbucket),
		Email:        email,
		Name:         bu.DisplayName,
		AvatarURL:    bu.Links.Avatar.Href,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
