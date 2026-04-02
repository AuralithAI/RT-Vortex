package crossrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/crossrepo"
	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── Unit tests for RepoLink.AllowsAction ────────────────────────────────────

func TestRepoLink_AllowsAction(t *testing.T) {
	tests := []struct {
		name     string
		profile  string
		action   string
		expected bool
	}{
		// Full profile allows everything.
		{"full+search", model.ShareFull, model.ActionCrossRepoSearch, true},
		{"full+symbols", model.ShareFull, model.ActionCrossRepoSymbols, true},
		{"full+metadata", model.ShareFull, model.ActionCrossRepoMetadata, true},
		{"full+file_read", model.ShareFull, model.ActionCrossRepoFileRead, true},
		{"full+graph_view", model.ShareFull, model.ActionCrossRepoGraphView, true},
		{"full+chat_mention", model.ShareFull, model.ActionCrossRepoChatMention, true},

		// Symbols profile allows symbols, metadata, graph, chat — NOT search, file_read.
		{"symbols+search", model.ShareSymbols, model.ActionCrossRepoSearch, false},
		{"symbols+symbols", model.ShareSymbols, model.ActionCrossRepoSymbols, true},
		{"symbols+metadata", model.ShareSymbols, model.ActionCrossRepoMetadata, true},
		{"symbols+file_read", model.ShareSymbols, model.ActionCrossRepoFileRead, false},
		{"symbols+graph_view", model.ShareSymbols, model.ActionCrossRepoGraphView, true},
		{"symbols+chat_mention", model.ShareSymbols, model.ActionCrossRepoChatMention, true},

		// Metadata profile allows only metadata and graph_view.
		{"metadata+search", model.ShareMetadata, model.ActionCrossRepoSearch, false},
		{"metadata+symbols", model.ShareMetadata, model.ActionCrossRepoSymbols, false},
		{"metadata+metadata", model.ShareMetadata, model.ActionCrossRepoMetadata, true},
		{"metadata+file_read", model.ShareMetadata, model.ActionCrossRepoFileRead, false},
		{"metadata+graph_view", model.ShareMetadata, model.ActionCrossRepoGraphView, true},
		{"metadata+chat_mention", model.ShareMetadata, model.ActionCrossRepoChatMention, false},

		// None profile allows nothing.
		{"none+search", model.ShareNone, model.ActionCrossRepoSearch, false},
		{"none+symbols", model.ShareNone, model.ActionCrossRepoSymbols, false},
		{"none+metadata", model.ShareNone, model.ActionCrossRepoMetadata, false},
		{"none+file_read", model.ShareNone, model.ActionCrossRepoFileRead, false},
		{"none+graph_view", model.ShareNone, model.ActionCrossRepoGraphView, false},
		{"none+chat_mention", model.ShareNone, model.ActionCrossRepoChatMention, false},

		// Unknown profile allows nothing.
		{"unknown+search", "garbage", model.ActionCrossRepoSearch, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := &model.RepoLink{ShareProfile: tt.profile}
			got := link.AllowsAction(tt.action)
			if got != tt.expected {
				t.Errorf("AllowsAction(%q) with profile=%q: got %v, want %v",
					tt.action, tt.profile, got, tt.expected)
			}
		})
	}
}

func TestRepoLink_IsActive(t *testing.T) {
	tests := []struct {
		profile  string
		expected bool
	}{
		{model.ShareFull, true},
		{model.ShareSymbols, true},
		{model.ShareMetadata, true},
		{model.ShareNone, false},
	}
	for _, tt := range tests {
		link := &model.RepoLink{ShareProfile: tt.profile}
		if got := link.IsActive(); got != tt.expected {
			t.Errorf("IsActive() with profile=%q: got %v, want %v", tt.profile, got, tt.expected)
		}
	}
}

// ── Unit tests for RequiresTargetRepoAccess ─────────────────────────────────

func TestRequiresTargetRepoAccess(t *testing.T) {
	tests := []struct {
		action   string
		expected bool
	}{
		{model.ActionCrossRepoSearch, true},
		{model.ActionCrossRepoFileRead, true},
		{model.ActionCrossRepoSymbols, false},
		{model.ActionCrossRepoMetadata, false},
		{model.ActionCrossRepoGraphView, false},
		{model.ActionCrossRepoChatMention, false},
	}
	for _, tt := range tests {
		got := crossrepo.RequiresTargetRepoAccess(tt.action)
		if got != tt.expected {
			t.Errorf("RequiresTargetRepoAccess(%q): got %v, want %v", tt.action, got, tt.expected)
		}
	}
}

// ── Unit tests for IsOrgCrossRepoEnabled ────────────────────────────────────

func TestIsOrgCrossRepoEnabled(t *testing.T) {
	a := crossrepo.NewAuthorizer(nil, nil, nil, nil)

	tests := []struct {
		name     string
		settings map[string]interface{}
		expected bool
	}{
		{"nil settings", nil, false},
		{"empty settings", map[string]interface{}{}, false},
		{"not set", map[string]interface{}{"other_key": true}, false},
		{"set to false", map[string]interface{}{"cross_repo_enabled": false}, false},
		{"set to true", map[string]interface{}{"cross_repo_enabled": true}, true},
		{"set to string", map[string]interface{}{"cross_repo_enabled": "true"}, false},
		{"set to 1", map[string]interface{}{"cross_repo_enabled": 1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := &model.Organization{Settings: tt.settings}
			got := a.IsOrgCrossRepoEnabled(org)
			if got != tt.expected {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}

// ── Unit tests for GetOrgMaxLinks ───────────────────────────────────────────

func TestGetOrgMaxLinks(t *testing.T) {
	a := crossrepo.NewAuthorizer(nil, nil, nil, nil)

	tests := []struct {
		name     string
		settings map[string]interface{}
		expected int
	}{
		{"nil settings", nil, 0},
		{"empty settings", map[string]interface{}{}, 0},
		{"not set", map[string]interface{}{"other": true}, 0},
		{"float64 (json default)", map[string]interface{}{"max_linked_repos": float64(10)}, 10},
		{"int", map[string]interface{}{"max_linked_repos": 5}, 5},
		{"string (invalid)", map[string]interface{}{"max_linked_repos": "10"}, 0},
		{"zero means unlimited", map[string]interface{}{"max_linked_repos": float64(0)}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := &model.Organization{Settings: tt.settings}
			got := a.GetOrgMaxLinks(org)
			if got != tt.expected {
				t.Errorf("got %d, want %d", got, tt.expected)
			}
		})
	}
}

// ── Validate share profile constants ────────────────────────────────────────

func TestValidShareProfiles(t *testing.T) {
	expected := map[string]bool{
		"full": true, "symbols": true, "metadata": true, "none": true,
	}
	for k, v := range expected {
		if model.ValidShareProfiles[k] != v {
			t.Errorf("ValidShareProfiles[%q]: got %v, want %v", k, model.ValidShareProfiles[k], v)
		}
	}
	// Ensure invalid profiles are rejected.
	if model.ValidShareProfiles["garbage"] {
		t.Error("ValidShareProfiles should reject 'garbage'")
	}
}

// ── Decision helpers ────────────────────────────────────────────────────────

func TestAllow(t *testing.T) {
	d := crossrepo.Allow()
	if !d.Allowed {
		t.Error("Allow() should return allowed=true")
	}
	if d.Reason != "" {
		t.Errorf("Allow() should have empty reason, got %q", d.Reason)
	}
}

func TestDeny(t *testing.T) {
	d := crossrepo.Deny("test reason")
	if d.Allowed {
		t.Error("Deny() should return allowed=false")
	}
	if d.Reason != "test reason" {
		t.Errorf("Deny() reason: got %q, want %q", d.Reason, "test reason")
	}
}

// ── Placeholder for integration tests ───────────────────────────────────────
// These require a running PostgreSQL instance and are skipped in unit test runs.

func TestAuthorize_Integration(t *testing.T) {
	t.Skip("integration test: requires PostgreSQL")

	// This test would:
	// 1. Set up two repos in the same org
	// 2. Create a repo_link with share_profile="symbols"
	// 3. Enable cross_repo_enabled in org settings
	// 4. Verify that Authorize allows ActionCrossRepoSymbols
	// 5. Verify that Authorize denies ActionCrossRepoSearch
	// 6. Change profile to "full" and verify search is allowed
	// 7. Disable cross_repo_enabled and verify all actions are denied
	// 8. Test viewer role restrictions
	// 9. Test target repo access for search/file_read

	_ = context.Background()
	_ = uuid.New()
	_ = time.Now()
}
