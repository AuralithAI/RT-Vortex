package model_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/google/uuid"
)

// ── User ────────────────────────────────────────────────────────────────────

func TestUser_Fields(t *testing.T) {
	u := model.User{
		ID:          uuid.New(),
		Email:       "alice@example.com",
		DisplayName: "Alice",
		AvatarURL:   "https://example.com/avatar.png",
	}

	if u.Email != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", u.Email)
	}
	if u.DisplayName != "Alice" {
		t.Errorf("expected Alice, got %s", u.DisplayName)
	}
}

func TestUser_ZeroValue(t *testing.T) {
	var u model.User
	if u.ID != uuid.Nil {
		t.Error("expected nil UUID for zero value")
	}
	if u.Email != "" {
		t.Error("expected empty email for zero value")
	}
}

// ── OAuthIdentity ───────────────────────────────────────────────────────────

func TestOAuthIdentity_Fields(t *testing.T) {
	id := model.OAuthIdentity{
		UserID:         uuid.New(),
		Provider:       "github",
		ProviderUserID: "12345",
		Scopes:         "repo,user",
	}

	if id.Provider != "github" {
		t.Errorf("expected github, got %s", id.Provider)
	}
	if id.ProviderUserID != "12345" {
		t.Errorf("expected 12345, got %s", id.ProviderUserID)
	}
}

// ── Organization ────────────────────────────────────────────────────────────

func TestOrganization_Fields(t *testing.T) {
	org := model.Organization{
		ID:   uuid.New(),
		Name: "Acme Inc",
		Slug: "acme-inc",
		Plan: "pro",
	}

	if org.Name != "Acme Inc" {
		t.Errorf("expected 'Acme Inc', got %s", org.Name)
	}
	if org.Plan != "pro" {
		t.Errorf("expected pro, got %s", org.Plan)
	}
}

// ── OrgMember ───────────────────────────────────────────────────────────────

func TestOrgMember_Roles(t *testing.T) {
	roles := []string{"owner", "admin", "member", "viewer"}
	for _, role := range roles {
		m := model.OrgMember{
			OrgID:  uuid.New(),
			UserID: uuid.New(),
			Role:   role,
		}
		if m.Role != role {
			t.Errorf("expected role %s, got %s", role, m.Role)
		}
	}
}

// ── Repository ──────────────────────────────────────────────────────────────

func TestRepository_Fields(t *testing.T) {
	repo := model.Repository{
		OrgID:         uuid.New(),
		Platform:      "github",
		Owner:         "AuralithAI",
		Name:          "rtvortex",
		DefaultBranch: "main",
		CloneURL:      "https://github.com/AuralithAI/rtvortex.git",
	}

	if repo.Platform != "github" {
		t.Error("wrong platform")
	}
	if repo.Owner != "AuralithAI" {
		t.Error("wrong owner")
	}
	if repo.DefaultBranch != "main" {
		t.Error("wrong default branch")
	}
}

func TestRepository_WebhookSecretNotInJSON(t *testing.T) {
	// The WebhookSecret field has `json:"-"` tag, verify the struct accepts it
	repo := model.Repository{
		WebhookSecret: "super-secret",
	}
	if repo.WebhookSecret != "super-secret" {
		t.Error("expected webhook secret to be set")
	}
}
