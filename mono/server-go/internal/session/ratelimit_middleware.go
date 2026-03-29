// Package session — rate limiting middleware for chi router.
package session

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/metrics"
)

// RateLimitMiddleware returns a chi middleware that enforces rate limits using
// the named limiter configuration. The key is derived from the authenticated
// user ID (if present) or the client's IP address.
//
// When a user belongs to an organization, a second org-level limiter is also
// checked (limiterName + ":org"). Both limits must pass.
func RateLimitMiddleware(rl *RateLimiter, limiterName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Derive a per-client key: prefer user ID, fall back to IP.
			key := r.RemoteAddr
			if uid, ok := auth.UserIDFromContext(r.Context()); ok {
				key = uid.String()
			}

			result, err := rl.Allow(r.Context(), limiterName, key)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			// Per-org limit check (if user has an org in context).
			if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims.OrgID.String() != "00000000-0000-0000-0000-000000000000" {
				orgLimiter := limiterName + ":org"
				orgResult, orgErr := rl.Allow(r.Context(), orgLimiter, claims.OrgID.String())
				if orgErr == nil && !orgResult.Allowed {
					metrics.RateLimitRejectionsTotal.WithLabelValues(orgLimiter).Inc()
					retryAfter := time.Until(orgResult.ResetAt).Seconds()
					if retryAfter < 1 {
						retryAfter = 1
					}
					w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter)))
					w.Header().Set("X-RateLimit-Scope", "organization")
					w.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprintf(w, `{"error":"organization rate limit exceeded","retry_after_seconds":%d}`, int(retryAfter))
					return
				}
			}

			// Set standard rate-limit response headers.
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Remaining+1))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))

			if !result.Allowed {
				metrics.RateLimitRejectionsTotal.WithLabelValues(limiterName).Inc()
				retryAfter := time.Until(result.ResetAt).Seconds()
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter)))
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":"rate limit exceeded","retry_after_seconds":%d}`, int(retryAfter))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
