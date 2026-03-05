package auth_test

import (
	"context"
	"testing"

	"golang.org/x/oauth2"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// ── mockOAuthProvider ───────────────────────────────────────────────────────

// mockOAuthProvider is a minimal OAuthProvider for registry tests.
type mockOAuthProvider struct {
	name auth.ProviderName
}

func (m *mockOAuthProvider) Name() auth.ProviderName { return m.name }

func (m *mockOAuthProvider) AuthURL(state string) string {
	return "https://example.com/auth?state=" + state
}

func (m *mockOAuthProvider) Exchange(_ context.Context, _ string) (*oauth2.Token, error) {
	return &oauth2.Token{}, nil
}

func (m *mockOAuthProvider) FetchUser(_ context.Context, _ *oauth2.Token) (*auth.OAuthUser, error) {
	return &auth.OAuthUser{}, nil
}

// ── ProviderRegistry ────────────────────────────────────────────────────────

func TestProviderRegistry_Empty(t *testing.T) {
	reg := auth.NewProviderRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}

	list := reg.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d items", len(list))
	}
}

func TestProviderRegistry_GetUnregistered(t *testing.T) {
	reg := auth.NewProviderRegistry()
	_, ok := reg.Get("github")
	if ok {
		t.Error("expected provider not found")
	}
}

func TestProviderRegistry_RegisterAndGet(t *testing.T) {
	reg := auth.NewProviderRegistry()
	provider := &mockOAuthProvider{name: "github"}
	reg.Register(provider)

	got, ok := reg.Get("github")
	if !ok {
		t.Fatal("expected provider to be found")
	}
	if got.Name() != "github" {
		t.Errorf("expected name github, got %s", got.Name())
	}
}

func TestProviderRegistry_List(t *testing.T) {
	reg := auth.NewProviderRegistry()
	reg.Register(&mockOAuthProvider{name: "github"})
	reg.Register(&mockOAuthProvider{name: "gitlab"})

	list := reg.List()
	if len(list) != 2 {
		t.Errorf("expected 2 providers, got %d", len(list))
	}

	names := make(map[auth.ProviderName]bool)
	for _, n := range list {
		names[n] = true
	}
	if !names["github"] {
		t.Error("expected github in list")
	}
	if !names["gitlab"] {
		t.Error("expected gitlab in list")
	}
}

func TestProviderRegistry_OverwriteExisting(t *testing.T) {
	reg := auth.NewProviderRegistry()
	reg.Register(&mockOAuthProvider{name: "github"})
	reg.Register(&mockOAuthProvider{name: "github"}) // overwrite

	list := reg.List()
	if len(list) != 1 {
		t.Errorf("expected 1 provider after overwrite, got %d", len(list))
	}
}

func TestProviderRegistry_AuthURL(t *testing.T) {
	reg := auth.NewProviderRegistry()
	reg.Register(&mockOAuthProvider{name: "github"})

	p, ok := reg.Get("github")
	if !ok {
		t.Fatal("expected provider")
	}
	url := p.AuthURL("test-state-123")
	if url == "" {
		t.Fatal("expected non-empty auth URL")
	}
}
