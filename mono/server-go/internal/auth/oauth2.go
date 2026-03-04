package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// ── OAuth2 Types ────────────────────────────────────────────────────────────

// ProviderName identifies an OAuth2 provider.
type ProviderName string

const (
	ProviderGoogle    ProviderName = "google"
	ProviderMicrosoft ProviderName = "microsoft"
	ProviderGitHub    ProviderName = "github"
	ProviderGitLab    ProviderName = "gitlab"
	ProviderBitbucket ProviderName = "bitbucket"
	ProviderLinkedIn  ProviderName = "linkedin"
)

// OAuthUser contains the normalized user profile returned by a provider.
type OAuthUser struct {
	ProviderID   string `json:"provider_id"`
	Provider     string `json:"provider"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url"`
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
}

// OAuthProviderConfig holds client credentials and URLs for one provider.
type OAuthProviderConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// ── Provider Interface ──────────────────────────────────────────────────────

// OAuthProvider defines the contract every OAuth2 provider must implement.
type OAuthProvider interface {
	Name() ProviderName
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	FetchUser(ctx context.Context, token *oauth2.Token) (*OAuthUser, error)
}

// ── Provider Registry ───────────────────────────────────────────────────────

// ProviderRegistry manages all configured OAuth2 providers.
type ProviderRegistry struct {
	providers map[ProviderName]OAuthProvider
}

// NewProviderRegistry creates an empty registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[ProviderName]OAuthProvider),
	}
}

// Register adds a provider to the registry.
func (r *ProviderRegistry) Register(p OAuthProvider) {
	r.providers[p.Name()] = p
}

// Get returns a provider by name.
func (r *ProviderRegistry) Get(name ProviderName) (OAuthProvider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// List returns all registered provider names.
func (r *ProviderRegistry) List() []ProviderName {
	names := make([]ProviderName, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// ── Base Provider ───────────────────────────────────────────────────────────

// baseProvider implements common OAuth2 logic shared across providers.
type baseProvider struct {
	name   ProviderName
	config *oauth2.Config
}

func (b *baseProvider) Name() ProviderName {
	return b.name
}

func (b *baseProvider) AuthURL(state string) string {
	return b.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (b *baseProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return b.config.Exchange(ctx, code)
}

// FetchJSON is a helper that fetches a URL with the token and decodes JSON.
func FetchJSON(ctx context.Context, token *oauth2.Token, url string, dest interface{}) error {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ── State Manager ───────────────────────────────────────────────────────────

var ErrInvalidState = errors.New("invalid or expired OAuth state")
