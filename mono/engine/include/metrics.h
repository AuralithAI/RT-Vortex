/**
 * TMS Metrics Registry — Phase 0 Observability
 *
 * Thread-safe metrics registry that collects counters, gauges, and histograms
 * from the C++ engine. The registry is exposed via a server-streaming gRPC
 * RPC (StreamEngineMetrics) so the Go API server can relay them to the React
 * dashboard over WebSocket.
 *
 * Design principles:
 *   - Singleton (like perf::Profiler) — accessible from any compilation unit.
 *   - Lock-free fast path where possible (std::atomic for counters/gauges).
 *   - Histogram uses a pre-allocated ring of samples + lazy percentile calc.
 *   - snapshot() is O(metrics) — safe to call at 1 Hz from the gRPC stream.
 *   - syncFromProfiler() bridges existing perf::Profiler data.
 *
 * Feature-flagged: instrumenting call-sites is always cheap (atomic inc),
 * but the gRPC stream only runs when a subscriber connects.
 */

#pragma once

#include <atomic>
#include <chrono>
#include <cstdint>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

namespace aipr {
namespace metrics {

// ── Well-known metric names ────────────────────────────────────────────────

// Embedding pipeline
constexpr const char* EMBED_LATENCY_S       = "embed_latency_s";
constexpr const char* EMBED_BATCH_SIZE      = "embed_batch_size";
constexpr const char* EMBED_CACHE_HIT_RATE  = "embed_cache_hit_rate";
constexpr const char* EMBED_TOTAL_CALLS     = "embed_total_calls";

// Embedding provider observability
constexpr const char* EMBED_ACTIVE_BACKEND  = "embed_active_backend";   // 0=mock,1=onnx,2=http
constexpr const char* EMBED_HTTP_REQUESTS   = "embed_http_requests";    // total API calls
constexpr const char* EMBED_HTTP_ERRORS     = "embed_http_errors";      // failed API calls
constexpr const char* EMBED_HTTP_TOKENS     = "embed_http_tokens_used"; // total tokens consumed
constexpr const char* EMBED_HTTP_RATE_LIMITS = "embed_http_rate_limits"; // 429 responses
constexpr const char* EMBED_HTTP_LATENCY_S  = "embed_http_latency_s";  // HTTP round-trip time

// Search / retrieval
constexpr const char* SEARCH_LATENCY_S      = "search_latency_s";
constexpr const char* SEARCH_CHUNKS_RETURNED = "search_chunks_returned";

// FAISS index
constexpr const char* FAISS_RECALL          = "faiss_recall";
constexpr const char* CHUNKS_INGESTED       = "chunks_ingested";
constexpr const char* INDEX_SIZE_VECTORS    = "index_size_vectors";

// TMS / Cross-Memory Attention
constexpr const char* CMA_SCORE             = "cma_score";
constexpr const char* TMS_FORWARD_LATENCY_S = "tms_forward_latency_s";

// Component health
constexpr const char* MINILM_READY          = "minilm_ready";
constexpr const char* FAISS_LOADED          = "faiss_loaded";

// LLM avoided rate (heuristic-only reviews / total reviews)
constexpr const char* LLM_AVOIDED_RATE      = "llm_avoided_rate";

// Hierarchical chunking
constexpr const char* AVG_PREFIX_LENGTH_CHARS   = "avg_prefix_length_chars";
constexpr const char* HIERARCHY_CHUNKS_TOTAL    = "hierarchy_chunks_total";
constexpr const char* EMBED_CACHE_HITS_TOTAL    = "embed_cache_hits_total";
constexpr const char* EMBED_CACHE_MISSES_TOTAL  = "embed_cache_misses_total";

// Knowledge Graph
constexpr const char* KG_ENABLED                = "aipr_kg_enabled";
constexpr const char* KG_NODES_TOTAL            = "aipr_kg_nodes_total";
constexpr const char* KG_EDGES_TOTAL            = "aipr_kg_edges_total";

// Memory Accounts (query routing)
constexpr const char* ACCOUNT_QUERIES_DEV_TOTAL      = "aipr_account_queries_dev_total";
constexpr const char* ACCOUNT_QUERIES_OPS_TOTAL      = "aipr_account_queries_ops_total";
constexpr const char* ACCOUNT_QUERIES_SECURITY_TOTAL = "aipr_account_queries_security_total";
constexpr const char* ACCOUNT_QUERIES_HISTORY_TOTAL  = "aipr_account_queries_history_total";

// ── Histogram snapshot ─────────────────────────────────────────────────────

struct HistogramSnapshot {
    uint64_t    count   = 0;
    double      sum     = 0.0;
    double      min_val = 0.0;
    double      max_val = 0.0;
    double      avg     = 0.0;
    double      p50     = 0.0;
    double      p90     = 0.0;
    double      p95     = 0.0;
    double      p99     = 0.0;
};

// ── Metric types ───────────────────────────────────────────────────────────

enum class MetricType { COUNTER, GAUGE, HISTOGRAM };

struct MetricValue {
    MetricType type = MetricType::GAUGE;
    // For COUNTER / GAUGE:
    double scalar = 0.0;
    // For HISTOGRAM:
    HistogramSnapshot histogram;
};

// Full snapshot returned by Registry::snapshot()
struct Snapshot {
    uint64_t                                    timestamp_ms = 0;
    std::unordered_map<std::string, MetricValue> metrics;
    uint64_t                                    uptime_s = 0;
};

// ── Registry (singleton) ───────────────────────────────────────────────────

class Registry {
public:
    static Registry& instance();

    // ── Counters (monotonically increasing) ─────────────────────────────
    void incCounter(const std::string& name, double delta = 1.0);
    double getCounter(const std::string& name) const;

    // ── Gauges (point-in-time value) ────────────────────────────────────
    void setGauge(const std::string& name, double value);
    double getGauge(const std::string& name) const;

    // ── Histograms (stream of samples) ──────────────────────────────────
    void observeHistogram(const std::string& name, double value);
    HistogramSnapshot getHistogram(const std::string& name) const;

    // ── Snapshot everything ─────────────────────────────────────────────
    Snapshot snapshot() const;

    // ── Bridge from existing perf::Profiler ─────────────────────────────
    // Copies all perf::Profiler timings into matching histograms.
    void syncFromProfiler();

    // ── Reset (for testing) ─────────────────────────────────────────────
    void reset();

private:
    Registry();

    // Internal histogram ring buffer
    static constexpr size_t kMaxSamples = 4096;

    struct HistogramData {
        std::vector<double> samples;  // ring buffer
        size_t              head = 0;
        size_t              count = 0;
        double              sum = 0.0;
        double              min_val = std::numeric_limits<double>::max();
        double              max_val = std::numeric_limits<double>::lowest();

        void observe(double v);
        HistogramSnapshot snap() const;
    };

    mutable std::mutex mu_;

    std::unordered_map<std::string, double>        counters_;
    std::unordered_map<std::string, double>        gauges_;
    std::unordered_map<std::string, HistogramData> histograms_;

    std::chrono::steady_clock::time_point start_time_;
};

// ── RAII scope timer that records into a histogram ─────────────────────────

class ScopeHistogramTimer {
public:
    explicit ScopeHistogramTimer(const std::string& metric_name)
        : name_(metric_name)
        , start_(std::chrono::steady_clock::now()) {}

    ~ScopeHistogramTimer() {
        auto elapsed = std::chrono::steady_clock::now() - start_;
        double secs = std::chrono::duration<double>(elapsed).count();
        Registry::instance().observeHistogram(name_, secs);
    }

    ScopeHistogramTimer(const ScopeHistogramTimer&) = delete;
    ScopeHistogramTimer& operator=(const ScopeHistogramTimer&) = delete;

private:
    std::string name_;
    std::chrono::steady_clock::time_point start_;
};

// Convenience macro
#define AIPR_METRICS_SCOPE(metric) \
    aipr::metrics::ScopeHistogramTimer __mtimer_##__LINE__(metric)

} // namespace metrics
} // namespace aipr
