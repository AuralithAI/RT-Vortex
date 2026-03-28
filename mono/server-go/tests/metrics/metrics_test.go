package metrics_test

import (
	"testing"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/metrics"
)

// ── Metric Existence ────────────────────────────────────────────────────────
// These tests verify that all Prometheus metrics are registered and can be
// called without panicking. We don't test actual values because the global
// Prometheus registry accumulates across tests.

func TestHTTPRequestsTotal_Exists(t *testing.T) {
	// Should not panic
	metrics.HTTPRequestsTotal.WithLabelValues("GET", "/health", "200").Add(0)
}

func TestHTTPRequestDuration_Exists(t *testing.T) {
	metrics.HTTPRequestDuration.WithLabelValues("GET", "/health", "200").Observe(0.001)
}

func TestHTTPRequestsInFlight_Exists(t *testing.T) {
	metrics.HTTPRequestsInFlight.Inc()
	metrics.HTTPRequestsInFlight.Dec()
}

func TestReviewPipelineTotal_Exists(t *testing.T) {
	metrics.ReviewPipelineTotal.WithLabelValues("completed").Add(0)
}

func TestReviewPipelineDuration_Exists(t *testing.T) {
	metrics.ReviewPipelineDuration.Observe(1.0)
}

func TestReviewCommentsTotal_Exists(t *testing.T) {
	metrics.ReviewCommentsTotal.Add(0)
}

func TestReviewFilesAnalyzed_Exists(t *testing.T) {
	metrics.ReviewFilesAnalyzed.Add(0)
}

func TestEngineGRPCRequestsTotal_Exists(t *testing.T) {
	metrics.EngineGRPCRequestsTotal.WithLabelValues("Index", "ok").Add(0)
}

func TestEngineGRPCDuration_Exists(t *testing.T) {
	metrics.EngineGRPCDuration.WithLabelValues("Index").Observe(0.1)
}

func TestEnginePoolHealthy_Exists(t *testing.T) {
	metrics.EnginePoolHealthy.Set(1)
}

func TestEngineReconnectsTotal_Exists(t *testing.T) {
	metrics.EngineReconnectsTotal.Add(0)
}

func TestLLMRequestsTotal_Exists(t *testing.T) {
	metrics.LLMRequestsTotal.WithLabelValues("openai", "gpt-4", "ok").Add(0)
}

func TestLLMRequestDuration_Exists(t *testing.T) {
	metrics.LLMRequestDuration.WithLabelValues("openai", "gpt-4").Observe(2.0)
}

func TestLLMTokensUsed_Exists(t *testing.T) {
	metrics.LLMTokensUsed.WithLabelValues("openai", "prompt").Add(0)
	metrics.LLMTokensUsed.WithLabelValues("openai", "completion").Add(0)
}

func TestWSConnectionsActive_Exists(t *testing.T) {
	metrics.WSConnectionsActive.Inc()
	metrics.WSConnectionsActive.Dec()
}

func TestWSMessagesTotal_Exists(t *testing.T) {
	metrics.WSMessagesTotal.Add(0)
}

func TestRateLimitRejectionsTotal_Exists(t *testing.T) {
	metrics.RateLimitRejectionsTotal.WithLabelValues("api").Add(0)
}

func TestAuthAttemptsTotal_Exists(t *testing.T) {
	metrics.AuthAttemptsTotal.WithLabelValues("github", "success").Add(0)
}

// ── Recording Helpers ───────────────────────────────────────────────────────

func TestRecordHTTPRequest(t *testing.T) {
	// Should not panic
	metrics.RecordHTTPRequest("GET", "/api/v1/health", 200, 50*time.Millisecond)
	metrics.RecordHTTPRequest("POST", "/api/v1/reviews", 201, 2*time.Second)
	metrics.RecordHTTPRequest("GET", "/not-found", 404, 10*time.Millisecond)
}

func TestRecordEngineCall(t *testing.T) {
	metrics.RecordEngineCall("IndexRepository", "ok", 5*time.Second)
	metrics.RecordEngineCall("Search", "error", 100*time.Millisecond)
}

func TestRecordLLMRequest(t *testing.T) {
	metrics.RecordLLMRequest("openai", "gpt-4", "ok", 3*time.Second, 500, 200)
	metrics.RecordLLMRequest("anthropic", "claude-3", "error", 1*time.Second, 0, 0)
}

func TestRecordPipelineComplete(t *testing.T) {
	metrics.RecordPipelineComplete("completed", 30*time.Second, 15, 8)
	metrics.RecordPipelineComplete("failed", 5*time.Second, 0, 3)
}
