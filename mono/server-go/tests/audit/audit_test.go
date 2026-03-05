package audit_test

import (
	"context"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/google/uuid"
)

// ── Logger nil-safety ───────────────────────────────────────────────────────

func TestLogger_NilLogger_LogDoesNotPanic(t *testing.T) {
	var l *audit.Logger
	// Calling Log on a nil logger should not panic.
	l.Log(context.Background(), audit.Event{
		Action:       audit.ActionLogin,
		ResourceType: "user",
		ResourceID:   "123",
	})
}

func TestNewLogger_NilRepo(t *testing.T) {
	// Creating a logger with nil repo is valid (fire-and-forget).
	l := audit.NewLogger(nil)
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	// Log should not panic even with nil repo.
	l.Log(context.Background(), audit.Event{
		Action:       audit.ActionLogin,
		ResourceType: "user",
	})
}

// ── Event struct ────────────────────────────────────────────────────────────

func TestEvent_Fields(t *testing.T) {
	uid := uuid.New()
	evt := audit.Event{
		UserID:       &uid,
		Action:       audit.ActionRepoCreate,
		ResourceType: "repo",
		ResourceID:   "abc-123",
		IPAddress:    "192.168.1.1",
		UserAgent:    "Mozilla/5.0",
		Metadata:     map[string]interface{}{"key": "value"},
	}

	if evt.Action != "repo.create" {
		t.Errorf("expected repo.create, got %s", evt.Action)
	}
	if evt.IPAddress != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", evt.IPAddress)
	}
	if evt.Metadata["key"] != "value" {
		t.Error("expected metadata key=value")
	}
}

// ── Action Constants ────────────────────────────────────────────────────────

func TestActionConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"Login", audit.ActionLogin, "auth.login"},
		{"LoginFailed", audit.ActionLoginFailed, "auth.login_failed"},
		{"Logout", audit.ActionLogout, "auth.logout"},
		{"TokenRefresh", audit.ActionTokenRefresh, "auth.token_refresh"},
		{"RepoCreate", audit.ActionRepoCreate, "repo.create"},
		{"RepoUpdate", audit.ActionRepoUpdate, "repo.update"},
		{"RepoDelete", audit.ActionRepoDelete, "repo.delete"},
		{"ReviewTrigger", audit.ActionReviewTrigger, "review.trigger"},
		{"ReviewCompleted", audit.ActionReviewCompleted, "review.completed"},
		{"ReviewFailed", audit.ActionReviewFailed, "review.failed"},
		{"OrgCreate", audit.ActionOrgCreate, "org.create"},
		{"OrgUpdate", audit.ActionOrgUpdate, "org.update"},
		{"OrgMemberInvite", audit.ActionOrgMemberInvite, "org.member_invite"},
		{"OrgMemberRemove", audit.ActionOrgMemberRemove, "org.member_remove"},
		{"IndexTrigger", audit.ActionIndexTrigger, "index.trigger"},
		{"AdminStats", audit.ActionAdminStats, "admin.stats"},
		{"AdminHealth", audit.ActionAdminHealth, "admin.detailed_health"},
		{"WebhookReceived", audit.ActionWebhookReceived, "webhook.received"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.value)
			}
		})
	}
}
