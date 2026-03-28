// Package validation provides structured input validation for API requests.
//
// It wraps go-playground/validator with domain-specific rules and returns
// structured 422 error responses with per-field detail.
package validation

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ── Field Error ─────────────────────────────────────────────────────────────

// FieldError represents a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Tag     string `json:"tag,omitempty"` // e.g. "required", "max", "slug"
}

// Error implements the error interface.
func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationError represents one or more field-level validation failures.
// It is designed to produce structured 422 responses.
type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

// Error implements the error interface.
func (ve *ValidationError) Error() string {
	msgs := make([]string, len(ve.Errors))
	for i, e := range ve.Errors {
		msgs[i] = e.Error()
	}
	return "validation failed: " + strings.Join(msgs, "; ")
}

// HasErrors returns true if at least one field error exists.
func (ve *ValidationError) HasErrors() bool {
	return len(ve.Errors) > 0
}

// Add appends a field error.
func (ve *ValidationError) Add(field, message, tag string) {
	ve.Errors = append(ve.Errors, FieldError{Field: field, Message: message, Tag: tag})
}

// StatusCode returns 422 Unprocessable Entity.
func (ve *ValidationError) StatusCode() int {
	return http.StatusUnprocessableEntity
}

// ── Validator ───────────────────────────────────────────────────────────────

// Patterns used for validation.
var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	emailPattern    = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	urlPattern      = regexp.MustCompile(`^https?://[^\s]+$`)
	platformPattern = regexp.MustCompile(`^(github|gitlab|bitbucket|azure_devops)$`)
	rolePattern     = regexp.MustCompile(`^(owner|admin|member|viewer)$`)
	branchPattern   = regexp.MustCompile(`^[a-zA-Z0-9._/\-]+$`)
)

// Required checks that a string field is non-empty.
func Required(ve *ValidationError, field, value string) {
	if strings.TrimSpace(value) == "" {
		ve.Add(field, field+" is required", "required")
	}
}

// MinLength checks minimum string length (rune count).
func MinLength(ve *ValidationError, field, value string, min int) {
	if utf8.RuneCountInString(value) < min {
		ve.Add(field, fmt.Sprintf("%s must be at least %d characters", field, min), "min")
	}
}

// MaxLength checks maximum string length (rune count).
func MaxLength(ve *ValidationError, field, value string, max int) {
	if utf8.RuneCountInString(value) > max {
		ve.Add(field, fmt.Sprintf("%s must be at most %d characters", field, max), "max")
	}
}

// Slug validates that a value matches a URL-safe slug pattern (lowercase alphanumeric with hyphens).
func Slug(ve *ValidationError, field, value string) {
	if value == "" {
		return // skip if empty; use Required() separately
	}
	if !slugPattern.MatchString(value) {
		ve.Add(field, field+" must contain only lowercase letters, numbers, and hyphens", "slug")
	}
}

// Email validates email format.
func Email(ve *ValidationError, field, value string) {
	if value == "" {
		return
	}
	if !emailPattern.MatchString(value) {
		ve.Add(field, field+" must be a valid email address", "email")
	}
}

// URL validates HTTP(S) URL format.
func URL(ve *ValidationError, field, value string) {
	if value == "" {
		return
	}
	if !urlPattern.MatchString(value) {
		ve.Add(field, field+" must be a valid HTTP(S) URL", "url")
	}
}

// Platform validates VCS platform name.
func Platform(ve *ValidationError, field, value string) {
	if value == "" {
		return
	}
	if !platformPattern.MatchString(value) {
		ve.Add(field, field+" must be one of: github, gitlab, bitbucket, azure_devops", "platform")
	}
}

// Role validates organization membership role.
func Role(ve *ValidationError, field, value string) {
	if value == "" {
		return
	}
	if !rolePattern.MatchString(value) {
		ve.Add(field, field+" must be one of: owner, admin, member, viewer", "role")
	}
}

// Branch validates a git branch name.
func Branch(ve *ValidationError, field, value string) {
	if value == "" {
		return
	}
	if !branchPattern.MatchString(value) {
		ve.Add(field, field+" contains invalid characters", "branch")
	}
}

// PositiveInt checks that an integer is > 0.
func PositiveInt(ve *ValidationError, field string, value int) {
	if value <= 0 {
		ve.Add(field, fmt.Sprintf("%s must be a positive integer", field), "positive")
	}
}

// ── Request Validators ──────────────────────────────────────────────────────

// CreateOrgRequest is the validated input for creating an organization.
type CreateOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Validate validates the create org request.
func (r *CreateOrgRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	Required(ve, "name", r.Name)
	MinLength(ve, "name", r.Name, 2)
	MaxLength(ve, "name", r.Name, 100)
	Required(ve, "slug", r.Slug)
	MinLength(ve, "slug", r.Slug, 2)
	MaxLength(ve, "slug", r.Slug, 50)
	Slug(ve, "slug", r.Slug)
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// UpdateOrgRequest is the validated input for updating an organization.
type UpdateOrgRequest struct {
	Name     string                 `json:"name"`
	Settings map[string]interface{} `json:"settings"`
}

// Validate validates the update org request.
func (r *UpdateOrgRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	if r.Name != "" {
		MinLength(ve, "name", r.Name, 2)
		MaxLength(ve, "name", r.Name, 100)
	}
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// UpdateUserRequest is the validated input for updating user profile.
type UpdateUserRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	AvatarURL   string `json:"avatar_url"`
}

// Validate validates the update user request.
func (r *UpdateUserRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	if r.DisplayName != "" {
		MinLength(ve, "display_name", r.DisplayName, 1)
		MaxLength(ve, "display_name", r.DisplayName, 100)
	}
	if r.Email != "" {
		Email(ve, "email", r.Email)
		MaxLength(ve, "email", r.Email, 255)
	}
	if r.AvatarURL != "" {
		URL(ve, "avatar_url", r.AvatarURL)
		MaxLength(ve, "avatar_url", r.AvatarURL, 2048)
	}
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// RegisterRepoRequest is the validated input for repository registration.
type RegisterRepoRequest struct {
	Platform      string `json:"platform"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	ExternalID    string `json:"external_id"`
	OrgID         string `json:"org_id"`
}

// Validate validates the register repo request.
func (r *RegisterRepoRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	Required(ve, "platform", r.Platform)
	Platform(ve, "platform", r.Platform)
	Required(ve, "owner", r.Owner)
	MinLength(ve, "owner", r.Owner, 1)
	MaxLength(ve, "owner", r.Owner, 255)
	Required(ve, "name", r.Name)
	MinLength(ve, "name", r.Name, 1)
	MaxLength(ve, "name", r.Name, 255)
	if r.DefaultBranch != "" {
		Branch(ve, "default_branch", r.DefaultBranch)
		MaxLength(ve, "default_branch", r.DefaultBranch, 100)
	}
	if r.CloneURL != "" {
		MaxLength(ve, "clone_url", r.CloneURL, 2048)
	}
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// UpdateRepoRequest is the validated input for updating repository config.
type UpdateRepoRequest struct {
	DefaultBranch string                 `json:"default_branch"`
	Config        map[string]interface{} `json:"config"`
}

// Validate validates the update repo request.
func (r *UpdateRepoRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	if r.DefaultBranch != "" {
		Branch(ve, "default_branch", r.DefaultBranch)
		MaxLength(ve, "default_branch", r.DefaultBranch, 100)
	}
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// InviteMemberRequest is the validated input for inviting an org member.
type InviteMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Validate validates the invite member request.
func (r *InviteMemberRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	Required(ve, "email", r.Email)
	Email(ve, "email", r.Email)
	if r.Role != "" {
		Role(ve, "role", r.Role)
	}
	if ve.HasErrors() {
		return ve
	}
	return nil
}

// TriggerReviewRequest is the validated input for triggering a review.
type TriggerReviewRequest struct {
	RepoID   string `json:"repo_id"`
	PRNumber int    `json:"pr_number"`
}

// Validate validates the trigger review request.
func (r *TriggerReviewRequest) Validate() *ValidationError {
	ve := &ValidationError{}
	Required(ve, "repo_id", r.RepoID)
	PositiveInt(ve, "pr_number", r.PRNumber)
	if ve.HasErrors() {
		return ve
	}
	return nil
}
