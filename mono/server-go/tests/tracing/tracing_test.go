package tracing_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/tracing"
)

// ── Config Tests ────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := tracing.DefaultConfig()
	if cfg.Enabled {
		t.Error("default config should have tracing disabled")
	}
	if cfg.Endpoint != "localhost:4317" {
		t.Errorf("default endpoint = %q, want localhost:4317", cfg.Endpoint)
	}
	if cfg.ServiceName != "rtvortex-api" {
		t.Errorf("default service name = %q, want rtvortex-api", cfg.ServiceName)
	}
	if cfg.SampleRate != 0.1 {
		t.Errorf("default sample rate = %f, want 0.1", cfg.SampleRate)
	}
	if !cfg.Insecure {
		t.Error("default should be insecure")
	}
}

// ── Tracer Tests ────────────────────────────────────────────────────────────

func TestNewTracer(t *testing.T) {
	cfg := tracing.Config{
		Enabled:     true,
		ServiceName: "test-service",
		SampleRate:  1.0,
	}
	tracer := tracing.NewTracer(cfg)
	if tracer == nil {
		t.Fatal("NewTracer returned nil")
	}
	if !tracer.Enabled() {
		t.Error("tracer should be enabled")
	}
}

func TestTracer_Disabled(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: false})
	if tracer.Enabled() {
		t.Error("tracer should be disabled")
	}
}

func TestTracer_DefaultServiceName(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true})
	if tracer == nil {
		t.Fatal("NewTracer returned nil")
	}
	// Should use default service name "rtvortex-api".
	if !tracer.Enabled() {
		t.Error("tracer should be enabled")
	}
}

func TestTracer_Nil(t *testing.T) {
	var tracer *tracing.Tracer
	if tracer.Enabled() {
		t.Error("nil tracer should not be enabled")
	}
}

// ── Span Tests ──────────────────────────────────────────────────────────────

func TestStartSpan(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	ctx, span := tracer.StartSpan(context.Background(), "test.operation")
	if ctx == nil {
		t.Error("context should not be nil")
	}
	if span == nil {
		t.Fatal("span should not be nil")
	}
	// Should not panic.
	span.End()
}

func TestStartSpanWithKind(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})

	kinds := []tracing.SpanKind{
		tracing.SpanKindServer,
		tracing.SpanKindClient,
		tracing.SpanKindInternal,
	}
	for _, kind := range kinds {
		t.Run(fmt.Sprintf("kind-%d", kind), func(t *testing.T) {
			_, span := tracer.StartSpanWithKind(context.Background(), "op", kind)
			if span == nil {
				t.Fatal("span should not be nil")
			}
			span.End()
		})
	}
}

func TestSpan_SetAttribute(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	_, span := tracer.StartSpan(context.Background(), "test")
	// Should not panic.
	span.SetAttribute("key", "value")
	span.SetAttribute("another", "val2")
	span.End()
}

func TestSpan_SetError(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	_, span := tracer.StartSpan(context.Background(), "test")
	// Should not panic with nil.
	span.SetError(nil)
	// Should not panic with real error.
	span.SetError(errors.New("something went wrong"))
	span.End()
}

func TestSpan_NilSafety(t *testing.T) {
	var span *tracing.Span
	// All methods should be nil-safe.
	span.SetAttribute("key", "value")
	span.SetError(errors.New("err"))
	span.End()
}

// ── Convenience Span Tests ──────────────────────────────────────────────────

func TestSpanDB(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	_, span := tracer.SpanDB(context.Background(), "SELECT", "users")
	if span == nil {
		t.Fatal("span should not be nil")
	}
	span.End()
}

func TestSpanGRPC(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	_, span := tracer.SpanGRPC(context.Background(), "engine.HealthCheck")
	if span == nil {
		t.Fatal("span should not be nil")
	}
	span.End()
}

func TestSpanLLM(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	_, span := tracer.SpanLLM(context.Background(), "openai", "gpt-4o")
	if span == nil {
		t.Fatal("span should not be nil")
	}
	span.End()
}

func TestSpanReview(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	_, span := tracer.SpanReview(context.Background(), "repo-123", "42")
	if span == nil {
		t.Fatal("span should not be nil")
	}
	span.End()
}

// ── HTTP Middleware Tests ────────────────────────────────────────────────────

func TestHTTPMiddleware_Enabled(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	middleware := tracing.HTTPMiddleware(tracer)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rr.Body.String())
	}
}

func TestHTTPMiddleware_Disabled(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: false})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := tracing.HTTPMiddleware(tracer)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHTTPMiddleware_NilTracer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := tracing.HTTPMiddleware(nil)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestHTTPMiddleware_CapturesErrorStatus(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	middleware := tracing.HTTPMiddleware(tracer)
	wrapped := middleware(handler)

	req := httptest.NewRequest("POST", "/api/v1/orgs", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHTTPMiddleware_CapturesInternalError(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := tracing.HTTPMiddleware(tracer)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/api/v1/error", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

// ── Shutdown Tests ──────────────────────────────────────────────────────────

func TestTracer_Shutdown(t *testing.T) {
	tracer := tracing.NewTracer(tracing.Config{Enabled: true, ServiceName: "test"})
	err := tracer.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}

// ── SpanKind Constants ──────────────────────────────────────────────────────

func TestSpanKindConstants(t *testing.T) {
	if tracing.SpanKindServer != 0 {
		t.Errorf("SpanKindServer = %d, want 0", tracing.SpanKindServer)
	}
	if tracing.SpanKindClient != 1 {
		t.Errorf("SpanKindClient = %d, want 1", tracing.SpanKindClient)
	}
	if tracing.SpanKindInternal != 2 {
		t.Errorf("SpanKindInternal = %d, want 2", tracing.SpanKindInternal)
	}
}
