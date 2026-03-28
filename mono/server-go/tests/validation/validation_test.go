package validation_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/validation"
)

// ── Primitive Validators ────────────────────────────────────────────────────

func TestRequired(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"non-empty", "hello", false},
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"single char", "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.Required(ve, "field", tt.value)
			if tt.wantErr && !ve.HasErrors() {
				t.Error("expected validation error for empty value")
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestMinLength(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		min     int
		wantErr bool
	}{
		{"meets minimum", "ab", 2, false},
		{"exceeds minimum", "abc", 2, false},
		{"below minimum", "a", 2, true},
		{"empty vs 1", "", 1, true},
		{"unicode runes", "日本語", 3, false},
		{"unicode below min", "日本", 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.MinLength(ve, "field", tt.value, tt.min)
			if tt.wantErr && !ve.HasErrors() {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected error: %v", ve.Errors)
			}
		})
	}
}

func TestMaxLength(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		max     int
		wantErr bool
	}{
		{"within limit", "ab", 5, false},
		{"at limit", "abcde", 5, false},
		{"exceeds limit", "abcdef", 5, true},
		{"empty", "", 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.MaxLength(ve, "field", tt.value, tt.max)
			if tt.wantErr && !ve.HasErrors() {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected error: %v", ve.Errors)
			}
		})
	}
}

func TestSlug(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid simple", "my-org", false},
		{"valid numeric", "team123", false},
		{"valid with hyphens", "my-cool-team", false},
		{"invalid uppercase", "MyOrg", true},
		{"invalid spaces", "my org", true},
		{"invalid underscore", "my_org", true},
		{"invalid starts with hyphen", "-team", true},
		{"invalid ends with hyphen", "team-", true},
		{"invalid double hyphen", "my--team", true},
		{"empty skipped", "", false},
		{"single char", "a", false},
		{"numbers only", "123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.Slug(ve, "slug", tt.value)
			if tt.wantErr && !ve.HasErrors() {
				t.Errorf("expected validation error for slug %q", tt.value)
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected error for slug %q: %v", tt.value, ve.Errors)
			}
		})
	}
}

func TestEmail(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid simple", "user@example.com", false},
		{"valid subdomain", "user@sub.domain.com", false},
		{"valid plus", "user+tag@example.com", false},
		{"invalid no at", "userexample.com", true},
		{"invalid no domain", "user@", true},
		{"invalid no tld", "user@domain", true},
		{"invalid spaces", "user @example.com", true},
		{"empty skipped", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.Email(ve, "email", tt.value)
			if tt.wantErr && !ve.HasErrors() {
				t.Errorf("expected validation error for email %q", tt.value)
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected error for email %q: %v", tt.value, ve.Errors)
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid http", "http://example.com", false},
		{"valid https", "https://example.com/path", false},
		{"valid with port", "https://localhost:8080/path", false},
		{"invalid no scheme", "example.com", true},
		{"invalid ftp", "ftp://example.com", true},
		{"empty skipped", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.URL(ve, "url", tt.value)
			if tt.wantErr && !ve.HasErrors() {
				t.Errorf("expected validation error for url %q", tt.value)
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected error for url %q: %v", tt.value, ve.Errors)
			}
		})
	}
}

func TestPlatform(t *testing.T) {
	valid := []string{"github", "gitlab", "bitbucket", "azure_devops"}
	for _, v := range valid {
		ve := &validation.ValidationError{}
		validation.Platform(ve, "platform", v)
		if ve.HasErrors() {
			t.Errorf("expected %q to be valid platform", v)
		}
	}
	invalid := []string{"GitHub", "svn", "mercurial", "random"}
	for _, v := range invalid {
		ve := &validation.ValidationError{}
		validation.Platform(ve, "platform", v)
		if !ve.HasErrors() {
			t.Errorf("expected %q to be invalid platform", v)
		}
	}
}

func TestRole(t *testing.T) {
	valid := []string{"owner", "admin", "member", "viewer"}
	for _, v := range valid {
		ve := &validation.ValidationError{}
		validation.Role(ve, "role", v)
		if ve.HasErrors() {
			t.Errorf("expected %q to be valid role", v)
		}
	}
	invalid := []string{"superadmin", "root", "guest", ""}
	for _, v := range invalid {
		ve := &validation.ValidationError{}
		validation.Role(ve, "role", v)
		// Empty should be skipped (no error).
		if v == "" && ve.HasErrors() {
			t.Error("expected empty role to be skipped")
		}
		if v != "" && !ve.HasErrors() {
			t.Errorf("expected %q to be invalid role", v)
		}
	}
}

func TestBranch(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid main", "main", false},
		{"valid with slash", "feature/my-feature", false},
		{"valid with dots", "release/v1.2.3", false},
		{"invalid spaces", "my branch", true},
		{"invalid special", "branch@home", true},
		{"empty skipped", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := &validation.ValidationError{}
			validation.Branch(ve, "branch", tt.value)
			if tt.wantErr && !ve.HasErrors() {
				t.Errorf("expected validation error for branch %q", tt.value)
			}
			if !tt.wantErr && ve.HasErrors() {
				t.Errorf("unexpected error for branch %q: %v", tt.value, ve.Errors)
			}
		})
	}
}

func TestPositiveInt(t *testing.T) {
	ve := &validation.ValidationError{}
	validation.PositiveInt(ve, "count", 1)
	if ve.HasErrors() {
		t.Error("1 should be positive")
	}

	ve = &validation.ValidationError{}
	validation.PositiveInt(ve, "count", 0)
	if !ve.HasErrors() {
		t.Error("0 should fail positive check")
	}

	ve = &validation.ValidationError{}
	validation.PositiveInt(ve, "count", -5)
	if !ve.HasErrors() {
		t.Error("-5 should fail positive check")
	}
}

// ── Request Validators ──────────────────────────────────────────────────────

func TestCreateOrgRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.CreateOrgRequest
		wantErr bool
	}{
		{"valid", validation.CreateOrgRequest{Name: "My Org", Slug: "my-org"}, false},
		{"empty name", validation.CreateOrgRequest{Name: "", Slug: "my-org"}, true},
		{"empty slug", validation.CreateOrgRequest{Name: "My Org", Slug: ""}, true},
		{"slug too short", validation.CreateOrgRequest{Name: "My Org", Slug: "a"}, true},
		{"slug uppercase", validation.CreateOrgRequest{Name: "My Org", Slug: "MyOrg"}, true},
		{"slug with underscore", validation.CreateOrgRequest{Name: "My Org", Slug: "my_org"}, true},
		{"name too long", validation.CreateOrgRequest{
			Name: string(make([]byte, 101)),
			Slug: "my-org",
		}, true},
		{"slug too long", validation.CreateOrgRequest{
			Name: "My Org",
			Slug: string(make([]byte, 51)),
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestUpdateOrgRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.UpdateOrgRequest
		wantErr bool
	}{
		{"valid name", validation.UpdateOrgRequest{Name: "New Name"}, false},
		{"empty (all optional)", validation.UpdateOrgRequest{}, false},
		{"name too short", validation.UpdateOrgRequest{Name: "a"}, true},
		{"name too long", validation.UpdateOrgRequest{
			Name: string(make([]byte, 101)),
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestUpdateUserRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.UpdateUserRequest
		wantErr bool
	}{
		{"valid", validation.UpdateUserRequest{DisplayName: "Alice", AvatarURL: "https://example.com/avatar.png"}, false},
		{"empty (all optional)", validation.UpdateUserRequest{}, false},
		{"invalid avatar url", validation.UpdateUserRequest{AvatarURL: "not-a-url"}, true},
		{"avatar too long", validation.UpdateUserRequest{
			AvatarURL: "https://example.com/" + string(make([]byte, 2050)),
		}, true},
		{"display name too long", validation.UpdateUserRequest{
			DisplayName: string(make([]byte, 101)),
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestRegisterRepoRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.RegisterRepoRequest
		wantErr bool
	}{
		{"valid", validation.RegisterRepoRequest{
			Platform: "github", Owner: "octocat", Name: "hello-world",
		}, false},
		{"valid with all fields", validation.RegisterRepoRequest{
			Platform: "gitlab", Owner: "user", Name: "repo",
			DefaultBranch: "develop", CloneURL: "https://gitlab.com/user/repo.git",
		}, false},
		{"missing platform", validation.RegisterRepoRequest{
			Owner: "octocat", Name: "hello-world",
		}, true},
		{"invalid platform", validation.RegisterRepoRequest{
			Platform: "svn", Owner: "octocat", Name: "hello-world",
		}, true},
		{"missing owner", validation.RegisterRepoRequest{
			Platform: "github", Name: "hello-world",
		}, true},
		{"missing name", validation.RegisterRepoRequest{
			Platform: "github", Owner: "octocat",
		}, true},
		{"invalid branch", validation.RegisterRepoRequest{
			Platform: "github", Owner: "octocat", Name: "repo",
			DefaultBranch: "my branch",
		}, true},
		{"owner too long", validation.RegisterRepoRequest{
			Platform: "github", Owner: string(make([]byte, 256)), Name: "repo",
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestUpdateRepoRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.UpdateRepoRequest
		wantErr bool
	}{
		{"valid", validation.UpdateRepoRequest{DefaultBranch: "develop"}, false},
		{"empty (all optional)", validation.UpdateRepoRequest{}, false},
		{"invalid branch", validation.UpdateRepoRequest{DefaultBranch: "bad branch"}, true},
		{"branch too long", validation.UpdateRepoRequest{
			DefaultBranch: string(make([]byte, 101)),
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestInviteMemberRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.InviteMemberRequest
		wantErr bool
	}{
		{"valid", validation.InviteMemberRequest{Email: "user@example.com", Role: "member"}, false},
		{"valid no role", validation.InviteMemberRequest{Email: "user@example.com"}, false},
		{"missing email", validation.InviteMemberRequest{Role: "member"}, true},
		{"invalid email", validation.InviteMemberRequest{Email: "notanemail"}, true},
		{"invalid role", validation.InviteMemberRequest{Email: "user@example.com", Role: "superadmin"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

func TestTriggerReviewRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     validation.TriggerReviewRequest
		wantErr bool
	}{
		{"valid", validation.TriggerReviewRequest{RepoID: "some-id", PRNumber: 42}, false},
		{"missing repo_id", validation.TriggerReviewRequest{PRNumber: 42}, true},
		{"zero pr_number", validation.TriggerReviewRequest{RepoID: "some-id", PRNumber: 0}, true},
		{"negative pr_number", validation.TriggerReviewRequest{RepoID: "some-id", PRNumber: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ve := tt.req.Validate()
			if tt.wantErr && ve == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && ve != nil {
				t.Errorf("unexpected validation error: %v", ve.Errors)
			}
		})
	}
}

// ── Structured Error Tests ──────────────────────────────────────────────────

func TestValidationError_Interface(t *testing.T) {
	ve := &validation.ValidationError{}
	ve.Add("name", "name is required", "required")
	ve.Add("slug", "slug must contain only lowercase letters", "slug")

	if !ve.HasErrors() {
		t.Error("expected HasErrors to be true")
	}
	if len(ve.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(ve.Errors))
	}
	if ve.StatusCode() != 422 {
		t.Errorf("expected status 422, got %d", ve.StatusCode())
	}

	errStr := ve.Error()
	if errStr == "" {
		t.Error("Error() should return non-empty string")
	}
}

func TestFieldError_Error(t *testing.T) {
	fe := &validation.FieldError{Field: "email", Message: "must be valid", Tag: "email"}
	s := fe.Error()
	if s != "email: must be valid" {
		t.Errorf("expected 'email: must be valid', got %q", s)
	}
}

func TestValidationError_NoErrors(t *testing.T) {
	ve := &validation.ValidationError{}
	if ve.HasErrors() {
		t.Error("empty ValidationError should not have errors")
	}
}
