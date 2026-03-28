package swarmauth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// ── Context Keys ────────────────────────────────────────────────────────────

type contextKey string

const ctxAgentClaims contextKey = "swarm_agent_claims"

// AgentClaimsFromContext extracts the validated agent claims from the request context.
func AgentClaimsFromContext(ctx context.Context) (*AgentClaims, bool) {
	c, ok := ctx.Value(ctxAgentClaims).(*AgentClaims)
	return c, ok
}

// ── RequireAgentToken Middleware ─────────────────────────────────────────────

// RequireAgentToken returns HTTP middleware that validates agent JWTs on
// /internal/swarm/* routes. It checks the JWT signature, expiry, type="agent",
// and Redis hash match.
func RequireAgentToken(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				slog.Debug("swarm auth: no bearer token",
					"path", r.URL.Path,
					"remote", r.RemoteAddr,
				)
				http.Error(w, `{"error":"missing agent authorization"}`, http.StatusUnauthorized)
				return
			}

			claims, err := svc.ValidateToken(r.Context(), token)
			if err != nil {
				slog.Debug("swarm auth: token validation failed",
					"error", err,
					"remote", r.RemoteAddr,
				)
				http.Error(w, `{"error":"invalid or expired agent token"}`, http.StatusUnauthorized)
				return
			}

			// Inject claims into context.
			ctx := context.WithValue(r.Context(), ctxAgentClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── RequireServiceSecret Middleware ──────────────────────────────────────────

// RequireServiceSecret returns middleware that validates the X-Service-Secret
// header. Used only on the /internal/swarm/auth/register endpoint.
func RequireServiceSecret(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret := r.Header.Get("X-Service-Secret")
			if !svc.ValidateServiceSecret(secret) {
				slog.Warn("swarm auth: invalid service secret",
					"remote", r.RemoteAddr,
					"path", r.URL.Path,
				)
				http.Error(w, `{"error":"invalid service secret"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractBearerToken pulls "Bearer <token>" from the Authorization header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}
