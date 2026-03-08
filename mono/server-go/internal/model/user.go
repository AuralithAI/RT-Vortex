// Package model defines the core domain types used across the application.
package model

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ─── User ────────────────────────────────────────────────────────────────────

// User represents a registered user.
type User struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Email       string    `json:"email" db:"email"`
	DisplayName string    `json:"display_name" db:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" db:"avatar_url"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// MarshalJSON emits both "name" and "display_name" so the web frontend
// (which uses `user.name`) and the canonical API field are both populated.
func (u User) MarshalJSON() ([]byte, error) {
	type alias User
	return json.Marshal(struct {
		alias
		Name string `json:"name"`
	}{
		alias: alias(u),
		Name:  u.DisplayName,
	})
}

// OAuthIdentity links a user to an OAuth2 provider account.
type OAuthIdentity struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          uuid.UUID  `json:"user_id" db:"user_id"`
	Provider        string     `json:"provider" db:"provider"` // github, google, microsoft, linkedin, gitlab, bitbucket
	ProviderUserID  string     `json:"provider_user_id" db:"provider_user_id"`
	AccessTokenEnc  string     `json:"-" db:"access_token_enc"` // encrypted, never exposed in JSON
	RefreshTokenEnc string     `json:"-" db:"refresh_token_enc"`
	Scopes          string     `json:"scopes,omitempty" db:"scopes"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// ─── Organization ────────────────────────────────────────────────────────────

// Organization represents a team or company.
type Organization struct {
	ID        uuid.UUID              `json:"id" db:"id"`
	Name      string                 `json:"name" db:"name"`
	Slug      string                 `json:"slug" db:"slug"`
	Plan      string                 `json:"plan" db:"plan"`                   // free, pro, enterprise
	Settings  map[string]interface{} `json:"settings,omitempty" db:"settings"` // JSONB
	CreatedAt time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt time.Time              `json:"updated_at" db:"updated_at"`
}

// OrgMember represents a user's membership in an organization.
type OrgMember struct {
	OrgID    uuid.UUID `json:"org_id" db:"org_id"`
	UserID   uuid.UUID `json:"user_id" db:"user_id"`
	Role     string    `json:"role" db:"role"` // owner, admin, member, viewer
	JoinedAt time.Time `json:"joined_at" db:"joined_at"`
}

// ─── Repository ──────────────────────────────────────────────────────────────

// Repository represents a VCS repository registered for review.
type Repository struct {
	ID            uuid.UUID              `json:"id" db:"id"`
	OrgID         uuid.UUID              `json:"org_id" db:"org_id"`
	Platform      string                 `json:"platform" db:"platform"` // github, gitlab, bitbucket, azure_devops
	ExternalID    string                 `json:"external_id" db:"external_id"`
	Owner         string                 `json:"owner" db:"owner"`
	Name          string                 `json:"name" db:"name"`
	DefaultBranch string                 `json:"default_branch" db:"default_branch"`
	CloneURL      string                 `json:"clone_url,omitempty" db:"clone_url"`
	WebhookSecret string                 `json:"-" db:"webhook_secret"`        // never expose
	Config        map[string]interface{} `json:"config,omitempty" db:"config"` // JSONB
	IndexedAt     *time.Time             `json:"indexed_at,omitempty" db:"indexed_at"`
	CreatedAt     time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at" db:"updated_at"`
}

// FullName returns "owner/name".
func (r *Repository) FullName() string {
	if r.Owner != "" && r.Name != "" {
		return fmt.Sprintf("%s/%s", r.Owner, r.Name)
	}
	return r.Name
}

// MarshalJSON adds computed fields the dashboard expects:
//   - full_name:       "owner/name"
//   - is_indexed:      true when indexed_at is set
//   - last_indexed_at: alias for indexed_at (the key the UI reads)
func (r Repository) MarshalJSON() ([]byte, error) {
	type Alias Repository // prevent infinite recursion
	return json.Marshal(&struct {
		Alias
		FullName      string     `json:"full_name"`
		IsIndexed     bool       `json:"is_indexed"`
		LastIndexedAt *time.Time `json:"last_indexed_at"`
	}{
		Alias:         Alias(r),
		FullName:      r.FullName(),
		IsIndexed:     r.IndexedAt != nil,
		LastIndexedAt: r.IndexedAt,
	})
}
