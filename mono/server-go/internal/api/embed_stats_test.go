package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
)

// ── EmbedStats JSON serialisation ──────────────────────────────────────────

func TestEmbedStatsJSONRoundTrip(t *testing.T) {
	original := &engine.EmbedStats{
		ActiveModel:          "bge-m3",
		EmbeddingDimension:   1024,
		BackendType:          "onnx_runtime",
		TotalChunks:          50000,
		TotalVectors:         50000,
		IndexSizeBytes:       120000000,
		KGNodes:              3200,
		KGEdges:              8900,
		KGEnabled:            true,
		MerkleCachedFiles:    1500,
		MerkleCacheHitRate:   0.87,
		AvgEmbedLatencyMs:    2.3,
		AvgSearchLatencyMs:   1.1,
		TotalQueries:         7000,
		EmbedCacheSize:       4000,
		EmbedCacheHitRate:    0.62,
		LLMAvoidsRate:        0.35,
		AvgConfidenceScore:   0.78,
		LLMAvoidsCount:       2450,
		LLMUsedCount:         4550,
		AvgGraphExpansionMs:  0.5,
		AvgGraphExpandChunks: 6.2,
		ModelSwapsTotal:      1,
		MultiVectorEnabled:   true,
		CoarseDimension:      384,
		FineDimension:        1024,
		CoarseIndexVectors:   50000,
		FineIndexVectors:     50000,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded engine.EmbedStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Spot-check critical fields
	if decoded.ActiveModel != "bge-m3" {
		t.Errorf("ActiveModel: got %q, want %q", decoded.ActiveModel, "bge-m3")
	}
	if decoded.EmbeddingDimension != 1024 {
		t.Errorf("EmbeddingDimension: got %d, want %d", decoded.EmbeddingDimension, 1024)
	}
	if decoded.TotalChunks != 50000 {
		t.Errorf("TotalChunks: got %d, want %d", decoded.TotalChunks, 50000)
	}
	if decoded.KGEnabled != true {
		t.Error("KGEnabled: got false, want true")
	}
	if decoded.MultiVectorEnabled != true {
		t.Error("MultiVectorEnabled: got false, want true")
	}
	if decoded.CoarseDimension != 384 {
		t.Errorf("CoarseDimension: got %d, want %d", decoded.CoarseDimension, 384)
	}
	if decoded.FineDimension != 1024 {
		t.Errorf("FineDimension: got %d, want %d", decoded.FineDimension, 1024)
	}
	if decoded.LLMAvoidsCount != 2450 {
		t.Errorf("LLMAvoidsCount: got %d, want %d", decoded.LLMAvoidsCount, 2450)
	}
	if decoded.ModelSwapsTotal != 1 {
		t.Errorf("ModelSwapsTotal: got %d, want %d", decoded.ModelSwapsTotal, 1)
	}
}

func TestEmbedStatsJSONFieldNames(t *testing.T) {
	stats := &engine.EmbedStats{
		ActiveModel:        "test-model",
		MultiVectorEnabled: true,
		MerkleCacheHitRate: 0.5,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify snake_case JSON field names
	expectedFields := []string{
		"active_model",
		"embedding_dimension",
		"backend_type",
		"total_chunks",
		"total_vectors",
		"index_size_bytes",
		"kg_nodes",
		"kg_edges",
		"kg_enabled",
		"merkle_cached_files",
		"merkle_cache_hit_rate",
		"avg_embed_latency_ms",
		"avg_search_latency_ms",
		"total_queries",
		"embed_cache_size",
		"embed_cache_hit_rate",
		"llm_avoided_rate",
		"avg_confidence_score",
		"llm_avoided_count",
		"llm_used_count",
		"avg_graph_expansion_ms",
		"avg_graph_expanded_chunks",
		"model_swaps_total",
		"multi_vector_enabled",
		"coarse_dimension",
		"fine_dimension",
		"coarse_index_vectors",
		"fine_index_vectors",
	}

	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field: %q", field)
		}
	}
}

// ── GetEmbedStats handler behaviour ────────────────────────────────────────

// newTestRouter builds a chi router with just the embed-stats route.
func newTestRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v1/repos/{repoID}/embed-stats", h.GetEmbedStats)
	return r
}

func TestGetEmbedStatsInvalidUUID(t *testing.T) {
	h := &Handler{} // nil EngineClient is fine — UUID check comes first
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos/not-a-uuid/embed-stats", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestGetEmbedStatsNilEngineClient(t *testing.T) {
	h := &Handler{EngineClient: nil}
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/repos/00000000-0000-0000-0000-000000000001/embed-stats", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestEmbedStatsZeroValueDefaults(t *testing.T) {
	var stats engine.EmbedStats

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded engine.EmbedStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ActiveModel != "" {
		t.Errorf("zero ActiveModel should be empty, got %q", decoded.ActiveModel)
	}
	if decoded.EmbeddingDimension != 0 {
		t.Errorf("zero EmbeddingDimension should be 0, got %d", decoded.EmbeddingDimension)
	}
	if decoded.KGEnabled {
		t.Error("zero KGEnabled should be false")
	}
	if decoded.MultiVectorEnabled {
		t.Error("zero MultiVectorEnabled should be false")
	}
}
