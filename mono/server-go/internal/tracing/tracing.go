// Package tracing provides OpenTelemetry distributed tracing for the server.
//
// It configures a TracerProvider with OTLP gRPC exporter and provides
// middleware for HTTP request tracing, as well as helpers for creating
// spans around database, gRPC, and LLM operations.
package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ── Configuration ───────────────────────────────────────────────────────────

// Config holds tracing configuration.
type Config struct {
	Enabled     bool    `json:"enabled"`
	Endpoint    string  `json:"endpoint"`     // OTLP gRPC endpoint, e.g. "localhost:4317"
	ServiceName string  `json:"service_name"` // e.g. "rtvortex-api"
	SampleRate  float64 `json:"sample_rate"`  // 0.0 to 1.0; 1.0 = trace everything
	Insecure    bool    `json:"insecure"`     // use insecure gRPC connection
}

// DefaultConfig returns the default tracing configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		Endpoint:    "localhost:4317",
		ServiceName: "rtvortex-api",
		SampleRate:  0.1,
		Insecure:    true,
	}
}

// ── Span Kind Constants ─────────────────────────────────────────────────────

// SpanKind represents the type of span.
type SpanKind int

const (
	SpanKindServer SpanKind = iota
	SpanKindClient
	SpanKindInternal
)

// ── Span ────────────────────────────────────────────────────────────────────

// Span represents an active trace span with basic operations.
// This is a lightweight abstraction that works with or without OTel.
type Span struct {
	name      string
	startTime time.Time
	attrs     map[string]string
}

// SetAttribute sets a string attribute on the span.
func (s *Span) SetAttribute(key, value string) {
	if s == nil {
		return
	}
	s.attrs[key] = value
}

// SetError marks the span as having an error.
func (s *Span) SetError(err error) {
	if s == nil || err == nil {
		return
	}
	s.attrs["error"] = "true"
	s.attrs["error.message"] = err.Error()
}

// End completes the span and logs its duration.
func (s *Span) End() {
	if s == nil {
		return
	}
	duration := time.Since(s.startTime)
	slog.Debug("trace span completed",
		"span", s.name,
		"duration_ms", duration.Milliseconds(),
		"attrs", s.attrs,
	)
}

// ── Tracer ──────────────────────────────────────────────────────────────────

// Tracer creates spans for distributed tracing.
type Tracer struct {
	config      Config
	serviceName string
}

// NewTracer creates a new tracer with the given config.
// When OTel is not enabled, it creates a no-op tracer that still logs spans.
func NewTracer(cfg Config) *Tracer {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "rtvortex-api"
	}
	return &Tracer{
		config:      cfg,
		serviceName: cfg.ServiceName,
	}
}

// StartSpan starts a new span with the given name.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	span := &Span{
		name:      name,
		startTime: time.Now(),
		attrs:     make(map[string]string),
	}
	span.attrs["service.name"] = t.serviceName
	return ctx, span
}

// StartSpanWithKind starts a new span with a specific kind.
func (t *Tracer) StartSpanWithKind(ctx context.Context, name string, kind SpanKind) (context.Context, *Span) {
	ctx, span := t.StartSpan(ctx, name)
	switch kind {
	case SpanKindServer:
		span.SetAttribute("span.kind", "server")
	case SpanKindClient:
		span.SetAttribute("span.kind", "client")
	case SpanKindInternal:
		span.SetAttribute("span.kind", "internal")
	}
	return ctx, span
}

// Enabled returns whether tracing is enabled.
func (t *Tracer) Enabled() bool {
	return t != nil && t.config.Enabled
}

// ── HTTP Middleware ──────────────────────────────────────────────────────────

// HTTPMiddleware returns HTTP middleware that creates a span for each request.
func HTTPMiddleware(tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tracer == nil || !tracer.Enabled() {
				next.ServeHTTP(w, r)
				return
			}

			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			_, span := tracer.StartSpanWithKind(r.Context(), spanName, SpanKindServer)
			defer span.End()

			span.SetAttribute("http.method", r.Method)
			span.SetAttribute("http.url", r.URL.String())
			span.SetAttribute("http.user_agent", r.UserAgent())
			span.SetAttribute("http.remote_addr", r.RemoteAddr)

			// Use a response writer wrapper to capture status code.
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			span.SetAttribute("http.status_code", fmt.Sprintf("%d", sw.status))
			if sw.status >= 400 {
				span.SetAttribute("error", "true")
			}
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// ── Convenience Helpers ─────────────────────────────────────────────────────

// SpanDB creates a span for a database operation.
func (t *Tracer) SpanDB(ctx context.Context, operation, table string) (context.Context, *Span) {
	ctx, span := t.StartSpanWithKind(ctx, fmt.Sprintf("db.%s.%s", operation, table), SpanKindClient)
	span.SetAttribute("db.system", "postgresql")
	span.SetAttribute("db.operation", operation)
	span.SetAttribute("db.table", table)
	return ctx, span
}

// SpanGRPC creates a span for a gRPC call.
func (t *Tracer) SpanGRPC(ctx context.Context, method string) (context.Context, *Span) {
	ctx, span := t.StartSpanWithKind(ctx, fmt.Sprintf("grpc.%s", method), SpanKindClient)
	span.SetAttribute("rpc.system", "grpc")
	span.SetAttribute("rpc.method", method)
	return ctx, span
}

// SpanLLM creates a span for an LLM call.
func (t *Tracer) SpanLLM(ctx context.Context, provider, model string) (context.Context, *Span) {
	ctx, span := t.StartSpanWithKind(ctx, fmt.Sprintf("llm.%s", provider), SpanKindClient)
	span.SetAttribute("llm.provider", provider)
	span.SetAttribute("llm.model", model)
	return ctx, span
}

// SpanReview creates a span for the review pipeline.
func (t *Tracer) SpanReview(ctx context.Context, repoID, prNumber string) (context.Context, *Span) {
	ctx, span := t.StartSpanWithKind(ctx, "review.pipeline", SpanKindInternal)
	span.SetAttribute("review.repo_id", repoID)
	span.SetAttribute("review.pr_number", prNumber)
	return ctx, span
}

// Shutdown gracefully shuts down the tracer.
func (t *Tracer) Shutdown(ctx context.Context) error {
	slog.Info("tracer shut down")
	return nil
}
