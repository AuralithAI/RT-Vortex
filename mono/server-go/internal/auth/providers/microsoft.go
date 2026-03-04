package providers

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	msEndpoint "golang.org/x/oauth2/microsoft"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

const microsoftGraphMeURL = "https://graph.microsoft.com/v1.0/me"

type msUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

// MicrosoftProvider implements OAuth2 for Microsoft / Azure AD.
type MicrosoftProvider struct {
	name   auth.ProviderName
	config *oauth2.Config
}

// NewMicrosoftProvider creates a Microsoft OAuth2 provider.
func NewMicrosoftProvider(cfg auth.OAuthProviderConfig) *MicrosoftProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email", "User.Read"}
	}
	return &MicrosoftProvider{
		name: auth.ProviderMicrosoft,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     msEndpoint.AzureADEndpoint("common"),
		},
	}
}

func (p *MicrosoftProvider) Name() auth.ProviderName { return p.name }

func (p *MicrosoftProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (p *MicrosoftProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

func (p *MicrosoftProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*auth.OAuthUser, error) {
	var mu msUser
	if err := auth.FetchJSON(ctx, token, microsoftGraphMeURL, &mu); err != nil {
		return nil, fmt.Errorf("microsoft fetch user: %w", err)
	}
	email := mu.Mail
	if email == "" {
		email = mu.UserPrincipalName
	}
	return &auth.OAuthUser{
		ProviderID:   mu.ID,
		Provider:     string(auth.ProviderMicrosoft),
		Email:        email,
		Name:         mu.DisplayName,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
	}, nil
}
