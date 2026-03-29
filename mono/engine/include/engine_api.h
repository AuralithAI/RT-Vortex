/**
 * AI PR Reviewer - Engine API
 * 
 * This is the stable public API boundary for the C++ engine.
 * External consumers (Java server via JNI/gRPC, CLI) should only
 * use types and functions declared in this header.
 */

#ifndef AIPR_ENGINE_API_H
#define AIPR_ENGINE_API_H

#include "types.h"
#include <string>
#include <vector>
#include <memory>
#include <functional>
#include <map>

// Platform-specific export macros
#if defined(_WIN32) || defined(_WIN64)
    #ifdef AIPR_ENGINE_EXPORTS
        #define AIPR_API __declspec(dllexport)
    #else
        #define AIPR_API __declspec(dllimport)
    #endif
#else
    #define AIPR_API __attribute__((visibility("default")))
#endif

namespace aipr {

/**
 * Embedding provider types
 */
enum class EmbedProvider {
    HTTP,         // OpenAI-compatible HTTP API
    LOCAL_ONNX,   // Local ONNX Runtime (bge-m3 or minilm)
    CUSTOM        // Custom provider via config
};

/**
 * Engine configuration
 */
struct AIPR_API EngineConfig {
    std::string storage_path = ".rtvortex/index";
    std::string config_profile = "default";
    
    // Indexing settings
    size_t max_file_size_kb = 1024;
    size_t chunk_size = 512;
    size_t chunk_overlap = 64;
    bool enable_ast_chunking = true;
    
    // Retrieval settings
    size_t top_k = 20;
    float lexical_weight = 0.3f;
    float vector_weight = 0.7f;
    size_t graph_expand_depth = 2;
    
    // Embedding settings
    EmbedProvider embed_provider = EmbedProvider::HTTP;
    std::string embed_endpoint = "https://api.openai.com/v1/embeddings";
    std::string embed_api_key_env = "OPENAI_API_KEY";
    std::string embed_model = "text-embedding-3-small";
    size_t embed_dimensions = 1536;
    size_t embed_batch_size = 100;
    size_t embed_timeout_seconds = 60;
    
    // Local ONNX model settings (when embed_provider == LOCAL_ONNX)
    // onnx_model_name selects which bundled model to use:
    //   "bge-m3"  — BAAI/bge-m3, 1024 dimensions (default, highest quality)
    //   "minilm"  — all-MiniLM-L6-v2, 384 dimensions (lightweight, fast)
    std::string onnx_model_name = "bge-m3";
    std::string onnx_model_path = "models/bge-m3/model.onnx";
    std::string onnx_tokenizer_path = "models/bge-m3/tokenizer.json";
    
    // Load from YAML file
    static EngineConfig load(const std::string& config_path);
};

/**
 * Index status and statistics
 */
struct AIPR_API IndexStats {
    std::string repo_id;
    std::string index_version;
    size_t total_files = 0;
    size_t indexed_files = 0;
    size_t total_chunks = 0;
    size_t total_symbols = 0;
    size_t index_size_bytes = 0;
    std::string last_updated;
    bool is_complete = false;
};

/**
 * Review context chunk
 */
struct AIPR_API ContextChunk {
    std::string id;
    std::string file_path;
    size_t start_line = 0;
    size_t end_line = 0;
    std::string content;
    std::string language;
    std::vector<std::string> symbols;
    float relevance_score = 0.0f;
};

/**
 * Review context pack (sent to LLM)
 */
struct AIPR_API ContextPack {
    std::string repo_id;
    std::string pr_title;
    std::string pr_description;
    std::string diff;
    std::vector<ContextChunk> context_chunks;
    std::vector<TouchedSymbol> touched_symbols;
    std::vector<std::string> heuristic_warnings;
};

/**
 * Progress callback for long-running operations
 */
using ProgressCallback = std::function<void(size_t current, size_t total, const std::string& message)>;

/**
 * Main Engine class
 */
class AIPR_API Engine {
public:
    /**
     * Create a new engine instance
     */
    static std::unique_ptr<Engine> create(const EngineConfig& config);
    
    virtual ~Engine() = default;
    
    // Prevent copying
    Engine(const Engine&) = delete;
    Engine& operator=(const Engine&) = delete;
    
    //-------------------------------------------------------------------------
    // Indexing Operations
    //-------------------------------------------------------------------------
    
    /**
     * Start a full index of a repository
     * 
     * @param repo_id Unique repository identifier
     * @param repo_path Path to the repository root
     * @param progress Optional progress callback
     * @return Index statistics
     */
    virtual IndexStats indexRepository(
        const std::string& repo_id,
        const std::string& repo_path,
        ProgressCallback progress = nullptr
    ) = 0;

    /**
     * Index a repository with explicit action control.
     *
     * @param repo_id    Unique repository identifier
     * @param repo_path  Path or URL to the repository
     * @param action     "index" (default), "reindex" (skip git), "reclone" (force fresh clone)
     * @param target_branch  Optional branch to checkout before indexing
     * @param progress   Optional progress callback
     * @return Index statistics
     */
    virtual IndexStats indexRepositoryWithAction(
        const std::string& repo_id,
        const std::string& repo_path,
        const std::string& action,
        const std::string& target_branch,
        ProgressCallback progress = nullptr
    ) { return indexRepository(repo_id, repo_path, progress); }
    
    /**
     * Update index incrementally based on changed files
     * 
     * @param repo_id Repository identifier
     * @param changed_files List of changed file paths
     * @param base_sha Base commit SHA
     * @param head_sha Head commit SHA
     * @return Updated index statistics
     */
    virtual IndexStats updateIndex(
        const std::string& repo_id,
        const std::vector<std::string>& changed_files,
        const std::string& base_sha,
        const std::string& head_sha
    ) = 0;
    
    /**
     * Get index statistics for a repository
     */
    virtual IndexStats getIndexStats(const std::string& repo_id) = 0;
    
    /**
     * Delete index for a repository
     */
    virtual bool deleteIndex(const std::string& repo_id) = 0;
    
    //-------------------------------------------------------------------------
    // Retrieval Operations
    //-------------------------------------------------------------------------
    
    /**
     * Search for relevant context chunks
     * 
     * @param repo_id Repository identifier
     * @param query Search query
     * @param top_k Number of results to return
     * @return Ranked list of context chunks
     */
    virtual std::vector<ContextChunk> search(
        const std::string& repo_id,
        const std::string& query,
        size_t top_k = 20
    ) = 0;

    /**
     * Extended search result including retrieval metadata.
     */
    struct SearchResult {
        std::vector<ContextChunk> chunks;
        float graph_confidence = 0.0f;
        uint32_t graph_expanded_chunks = 0;
        bool requires_llm = true;
        float max_retrieval_score = 0.0f;
    };

    /**
     * Search with full metadata (graph confidence, LLM gate, etc.).
     * Default implementation delegates to search().
     */
    virtual SearchResult searchWithMeta(
        const std::string& repo_id,
        const std::string& query,
        size_t top_k = 20
    ) {
        SearchResult r;
        r.chunks = search(repo_id, query, top_k);
        return r;
    }
    
    //-------------------------------------------------------------------------
    // Review Operations
    //-------------------------------------------------------------------------
    
    /**
     * Build context pack for PR review
     * 
     * @param repo_id Repository identifier
     * @param diff Unified diff of changes
     * @param pr_title PR title
     * @param pr_description PR description
     * @return Context pack ready for LLM
     */
    virtual ContextPack buildReviewContext(
        const std::string& repo_id,
        const std::string& diff,
        const std::string& pr_title = "",
        const std::string& pr_description = ""
    ) = 0;
    
    /**
     * Run heuristic checks on diff (no LLM needed)
     * 
     * @param diff Unified diff
     * @return List of heuristic findings
     */
    virtual std::vector<HeuristicFinding> runHeuristics(
        const std::string& diff
    ) = 0;
    
    //-------------------------------------------------------------------------
    // Utility
    //-------------------------------------------------------------------------
    
    /**
     * Get engine version
     */
    virtual std::string getVersion() const = 0;

    /**
     * Get the storage path used by this engine instance.
     * Repo clones live at  <storage_path>/repos/<repo_id>.
     */
    virtual std::string getStoragePath() const { return ""; }
    
    /**
     * Run self-diagnostics
     */
    virtual DiagnosticResult runDiagnostics() = 0;

    /**
     * Get embedding statistics for a repository (or global if repo_id is empty).
     */
    struct EmbedStats {
        std::string active_model;
        size_t embedding_dimension = 0;
        std::string backend_type;
        size_t total_chunks = 0;
        size_t total_vectors = 0;
        size_t index_size_bytes = 0;
        size_t kg_nodes = 0;
        size_t kg_edges = 0;
        bool kg_enabled = false;
        size_t merkle_cached_files = 0;
        double merkle_cache_hit_rate = 0.0;
        double avg_embed_latency_ms = 0.0;
        double avg_search_latency_ms = 0.0;
        size_t total_queries = 0;
        size_t embed_cache_size = 0;
        double embed_cache_hit_rate = 0.0;
        double llm_avoided_rate = 0.0;
        double avg_confidence_score = 0.0;
        size_t llm_avoided_count = 0;
        size_t llm_used_count = 0;
        double avg_graph_expansion_ms = 0.0;
        double avg_graph_expanded_chunks = 0.0;
        size_t model_swaps_total = 0;
        bool multi_vector_enabled = false;
        size_t coarse_dimension = 0;
        size_t fine_dimension = 0;
        size_t coarse_index_vectors = 0;
        size_t fine_index_vectors = 0;
    };
    virtual EmbedStats getEmbedStats(const std::string& repo_id) {
        return EmbedStats{};
    }
    
    /**
     * Configure embedding provider at runtime (per-request override).
     *
     * Called by the gRPC layer before indexing to push the user's
     * provider choice from the Go server. The API key is passed
     * transiently — it is NOT persisted in the C++ engine.
     *
     * @param provider   "http", "onnx", or "mock"
     * @param endpoint   API URL (for HTTP providers)
     * @param model      Model name (e.g. "text-embedding-3-small")
     * @param api_key    Transient API key — used for the current operation only
     * @param dimensions Embedding vector dimensionality
     */
    virtual void configureEmbedding(
        const std::string& provider,
        const std::string& endpoint,
        const std::string& model,
        const std::string& api_key,
        size_t dimensions
    ) {
        // Default no-op — overridden by EngineImpl.
        (void)provider; (void)endpoint; (void)model; (void)api_key; (void)dimensions;
    }

    /**
     * Set a transient VCS clone token for the next indexRepository call.
     *
     * The token is consumed once and cleared after use. It is injected
     * into HTTPS clone URLs to authenticate git operations for private
     * repositories. The token is NEVER persisted in the engine or in
     * .git/config — the remote URL is reset to the clean URL after clone.
     *
     * @param token  VCS personal access token or OAuth token
     */
    virtual void setCloneToken(const std::string& token) {
        // Default no-op — overridden by EngineImpl.
        (void)token;
    }

    /**
     * Embed and store an asset chunk (document, PDF, URL content).
     *
     * @param repo_id    Repository to associate the chunk with
     * @param content    Pre-chunked text content (with metadata prefix)
     * @param source     Source URL or identifier
     * @param asset_type "document", "pdf", "url"
     * @param metadata   Additional key-value metadata tags
     * @return true if successfully embedded and stored
     */
    virtual bool embedAndStoreAssetChunk(
        const std::string& repo_id,
        const std::string& content,
        const std::string& source,
        const std::string& asset_type,
        const std::map<std::string, std::string>& metadata)
    {
        (void)repo_id; (void)content; (void)source;
        (void)asset_type; (void)metadata;
        return false;
    }

protected:
    Engine() = default;
};

}

#endif
