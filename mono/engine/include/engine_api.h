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
 * Engine configuration
 */
struct AIPR_API EngineConfig {
    std::string storage_path = ".aipr/index";
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
    std::string embed_provider = "http";
    std::string embed_endpoint;
    size_t embed_dimensions = 1536;
    
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
     * Run self-diagnostics
     */
    virtual DiagnosticResult runDiagnostics() = 0;

protected:
    Engine() = default;
};

}

#endif
