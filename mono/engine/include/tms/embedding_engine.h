/**
 * TMS Embedding Engine
 * 
 * Wrapper around embedding computation.
 * Supports multiple backends:
 * - HTTP API (OpenAI, local server)
 * - ONNX Runtime (local models)
 * - Sentence Transformers (via Python bridge)
 * 
 * Optimized for code embeddings with:
 * - Batched processing
 * - Caching
 * - Rate limiting
 * - Retry logic
 */

#pragma once

#include "tms_types.h"
#include <string>
#include <vector>
#include <memory>
#include <functional>
#include <chrono>

namespace aipr::tms {

/**
 * Embedding Backend Type
 */
enum class EmbeddingBackend {
    HTTP_API,           // OpenAI-compatible HTTP API
    ONNX_RUNTIME,       // Local ONNX model
    SENTENCE_TRANSFORMERS,  // Python Sentence Transformers
    MOCK                // For testing
};

/**
 * Embedding Configuration
 */
struct EmbeddingConfig {
    EmbeddingBackend backend = EmbeddingBackend::HTTP_API;
    
    // Model settings
    std::string model_name = "text-embedding-3-small";
    size_t embedding_dimension = 1536;
    
    // HTTP API settings (for HTTP_API backend)
    std::string api_endpoint = "https://api.openai.com/v1/embeddings";
    std::string api_key;
    int timeout_ms = 30000;
    int max_retries = 3;
    int retry_delay_ms = 1000;
    
    // Rate limiting
    int max_requests_per_minute = 3000;
    int max_tokens_per_minute = 1000000;
    
    // ONNX settings (for ONNX_RUNTIME backend)
    std::string onnx_model_path;
    std::string tokenizer_path;
    bool use_gpu = false;
    int gpu_device_id = 0;
    
    // Batching
    int batch_size = 100;           // Embeddings per API call
    int max_tokens_per_batch = 8000;
    
    // Caching
    bool enable_cache = true;
    size_t cache_size = 100000;     // Max cached embeddings
    std::string cache_path;         // For persistent cache
    
    // Code-specific settings
    bool normalize_code = true;     // Normalize whitespace, etc.
    int max_input_tokens = 8000;    // Truncate if longer
};

/**
 * Embedding Result
 */
struct EmbeddingResult {
    std::vector<float> embedding;
    int tokens_used = 0;
    std::chrono::microseconds computation_time{0};
    bool from_cache = false;
    std::string error;              // Empty if success
    bool success = true;
};

/**
 * Batch Embedding Result
 */
struct BatchEmbeddingResult {
    std::vector<std::vector<float>> embeddings;
    std::vector<int> tokens_used;
    std::vector<bool> from_cache;
    std::vector<std::string> errors;
    int successful_count = 0;
    int failed_count = 0;
    std::chrono::milliseconds total_time{0};
};

/**
 * Progress callback for batch operations
 */
using EmbeddingProgressCallback = std::function<void(
    int completed,
    int total,
    const std::string& status
)>;

/**
 * EmbeddingEngine
 */
class EmbeddingEngine {
public:
    explicit EmbeddingEngine(const EmbeddingConfig& config);
    ~EmbeddingEngine();
    
    // Non-copyable
    EmbeddingEngine(const EmbeddingEngine&) = delete;
    EmbeddingEngine& operator=(const EmbeddingEngine&) = delete;
    
    // =========================================================================
    // Main Embedding Interface
    // =========================================================================
    
    /**
     * Embed a single text
     */
    EmbeddingResult embed(const std::string& text);
    
    /**
     * Embed code with metadata (optimized for code)
     */
    EmbeddingResult embedCode(const CodeChunk& chunk);
    
    /**
     * Batch embed texts
     */
    BatchEmbeddingResult embedBatch(
        const std::vector<std::string>& texts,
        EmbeddingProgressCallback progress = nullptr
    );
    
    /**
     * Batch embed code chunks
     */
    BatchEmbeddingResult embedChunks(
        const std::vector<CodeChunk>& chunks,
        EmbeddingProgressCallback progress = nullptr
    );
    
    // =========================================================================
    // Cache Management
    // =========================================================================
    
    /**
     * Get cached embedding if available
     */
    std::optional<std::vector<float>> getCached(const std::string& content_hash);
    
    /**
     * Clear cache
     */
    void clearCache();
    
    /**
     * Save cache to disk
     */
    void saveCache();
    
    /**
     * Load cache from disk
     */
    void loadCache();
    
    /**
     * Get cache statistics
     */
    struct CacheStats {
        size_t size = 0;
        size_t hits = 0;
        size_t misses = 0;
        double hit_rate = 0.0;
    };
    CacheStats getCacheStats() const;
    
    // =========================================================================
    // Configuration
    // =========================================================================
    
    /**
     * Get configuration
     */
    const EmbeddingConfig& getConfig() const { return config_; }
    
    /**
     * Update API key
     */
    void setApiKey(const std::string& key);
    
    /**
     * Update endpoint
     */
    void setEndpoint(const std::string& endpoint);
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    struct Stats {
        size_t total_embeddings = 0;
        size_t total_tokens = 0;
        double avg_embedding_time_ms = 0.0;
        size_t api_calls = 0;
        size_t api_errors = 0;
        size_t rate_limit_waits = 0;
    };
    
    Stats getStats() const;
    void resetStats();

private:
    EmbeddingConfig config_;
    
    // Backend implementation (pimpl)
    class BackendImpl;
    std::unique_ptr<BackendImpl> backend_;
    
    // Cache
    class EmbeddingCache;
    std::unique_ptr<EmbeddingCache> cache_;
    
    // Rate limiter
    class RateLimiter;
    std::unique_ptr<RateLimiter> rate_limiter_;
    
    // Statistics
    mutable Stats stats_;
    mutable std::mutex stats_mutex_;
    
    // Helpers
    std::string prepareCodeInput(const CodeChunk& chunk);
    std::string normalizeCode(const std::string& code);
    std::string computeHash(const std::string& content);
    void updateStats(const EmbeddingResult& result);
};

// =============================================================================
// Embedding Ingestor
// =============================================================================

/**
 * EmbeddingIngestor - Orchestrates the full ingestion pipeline
 * 
 * Combines RepoParser, EmbeddingEngine, and LTM storage:
 * 1. Parse repository into chunks
 * 2. Compute embeddings for all chunks
 * 3. Store in LTM
 * 4. Update MTM patterns
 */
class EmbeddingIngestor {
public:
    // Forward declare to avoid circular includes
    class LTMFaiss;
    class MTMGraph;
    
    struct Config {
        // Batch processing
        int chunk_batch_size = 500;
        int embedding_batch_size = 100;
        
        // Progress reporting
        int progress_interval = 100;    // Report every N chunks
        
        // Error handling
        bool continue_on_error = true;
        int max_errors = 100;           // Abort after this many errors
        
        // MTM updates
        bool update_mtm = true;
        bool detect_patterns = true;
    };
    
    EmbeddingIngestor(
        EmbeddingEngine& embedding_engine,
        LTMFaiss& ltm,
        MTMGraph* mtm = nullptr,
        const Config& config = Config{}
    );
    ~EmbeddingIngestor();
    
    /**
     * Ingest a full repository
     */
    struct IngestResult {
        size_t chunks_processed = 0;
        size_t chunks_stored = 0;
        size_t chunks_failed = 0;
        size_t patterns_detected = 0;
        std::chrono::milliseconds total_time{0};
        std::vector<std::string> errors;
    };
    
    IngestResult ingestRepository(
        const std::string& repo_path,
        const std::string& repo_id,
        EmbeddingProgressCallback progress = nullptr
    );
    
    /**
     * Ingest pre-parsed chunks
     */
    IngestResult ingestChunks(
        const std::string& repo_id,
        const std::vector<CodeChunk>& chunks,
        EmbeddingProgressCallback progress = nullptr
    );
    
    /**
     * Ingest chunks with pre-computed embeddings
     */
    IngestResult ingestChunksWithEmbeddings(
        const std::string& repo_id,
        const std::vector<CodeChunk>& chunks,
        const std::vector<std::vector<float>>& embeddings
    );

private:
    EmbeddingEngine& embedding_engine_;
    LTMFaiss& ltm_;
    MTMGraph* mtm_;
    Config config_;
    
    void detectAndStorePatterns(const std::vector<CodeChunk>& chunks);
};

} // namespace aipr::tms
