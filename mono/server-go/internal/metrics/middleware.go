package metrics

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Middleware is a chi-compatible middleware that records Prometheus HTTP metrics
// for every request: request count, latency histogram, and in-flight gauge.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()

		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		duration := time.Since(start)

		// Use chi's route pattern as the path label so that path parameters
		// (e.g. /api/v1/reviews/123) are collapsed into their template
		// (e.g. /api/v1/reviews/{reviewID}).
		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		if routePattern == "" {
			routePattern = "unmatched"
		}

		RecordHTTPRequest(r.Method, routePattern, ww.Status(), duration)
	})
}
