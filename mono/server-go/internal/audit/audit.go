// Package audit provides structured security-event logging for the RTVortex server.
//
// Every security-relevant action (login, logout, resource mutation, admin action)
// is recorded asynchronously to the audit_log table in PostgreSQL. The Logger is
// safe for concurrent use and never blocks the request path — if the write fails
// it is logged at WARN level but the request continues.
package audit

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── Well-known audit actions ────────────────────────────────────────────────

const (
	ActionLogin           = "auth.login"
	ActionLoginFailed     = "auth.login_failed"
	ActionLogout          = "auth.logout"
	ActionTokenRefresh    = "auth.token_refresh"
	ActionRepoCreate      = "repo.create"
	ActionRepoUpdate      = "repo.update"
	ActionRepoDelete      = "repo.delete"
	ActionReviewTrigger   = "review.trigger"
	ActionReviewCompleted = "review.completed"
	ActionReviewFailed    = "review.failed"
	ActionOrgCreate       = "org.create"
	ActionOrgUpdate       = "org.update"
	ActionOrgMemberInvite = "org.member_invite"
	ActionOrgMemberRemove = "org.member_remove"
	ActionIndexTrigger    = "index.trigger"
	ActionAdminStats      = "admin.stats"
	ActionAdminHealth     = "admin.detailed_health"
	ActionWebhookReceived = "webhook.received"
)

// ── Logger ──────────────────────────────────────────────────────────────────

// Logger writes audit entries to the database.
type Logger struct {
	repo *store.AuditRepository
}

// NewLogger creates an audit Logger.
func NewLogger(repo *store.AuditRepository) *Logger {
	return &Logger{repo: repo}
}

// Event represents a single auditable action.
type Event struct {
	UserID       *uuid.UUID
	Action       string
	ResourceType string // e.g. "user", "repo", "review", "org", "webhook"
	ResourceID   string
	IPAddress    string
	UserAgent    string
	Metadata     map[string]interface{}
}

// Log writes an audit event. It is fire-and-forget: errors are logged but
// never returned to the caller so that request processing is never blocked.
func (l *Logger) Log(ctx context.Context, evt Event) {
	if l == nil || l.repo == nil {
		return
	}

	entry := &store.AuditEntry{
		UserID:       evt.UserID,
		Action:       evt.Action,
		ResourceType: evt.ResourceType,
		ResourceID:   evt.ResourceID,
		IPAddress:    evt.IPAddress,
		UserAgent:    evt.UserAgent,
		Metadata:     evt.Metadata,
	}

	// Run in a goroutine so we never add latency to the request.
	go func() {
		if err := l.repo.Create(context.Background(), entry); err != nil {
			slog.Warn("audit log write failed",
				"action", evt.Action,
				"error", err,
			)
		}
	}()
}

// LogRequest is a convenience wrapper that extracts the user ID, IP, and
// user-agent from an HTTP request and writes the event.
func (l *Logger) LogRequest(r *http.Request, action, resourceType, resourceID string, meta map[string]interface{}) {
	if l == nil {
		return
	}

	var uid *uuid.UUID
	if id, ok := auth.UserIDFromContext(r.Context()); ok {
		uid = &id
	}

	l.Log(r.Context(), Event{
		UserID:       uid,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     meta,
	})
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// clientIP extracts the best-guess client IP from the request.
func clientIP(r *http.Request) string {
	// Prefer X-Forwarded-For or X-Real-IP set by a reverse proxy.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Strip port from RemoteAddr (e.g. "1.2.3.4:54321" → "1.2.3.4")
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		// Check for IPv6 brackets: "[::1]:port"
		if strings.Contains(addr, "]") {
			if bi := strings.Index(addr, "]"); bi != -1 {
				return strings.Trim(addr[:bi+1], "[]")
			}
		}
		return addr[:idx]
	}
	return addr
}
