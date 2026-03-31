/**
 * TMS Memory System - Main Orchestrator
 * 
 * The unified interface for the brain-inspired cognitive memory system.
 * Coordinates LTM, STM, MTM, Cross-Memory Attention, and Compute Controller.
 * 
 * Usage Example:
 * ```cpp
 * TMSConfig config;
 * config.storage_path = "/data/tms";
 * config.embedding_dimension = 1536;
 * config.vram_budget_gb = 8.0;
 * 
 * TMSMemorySystem tms(config);
 * tms.initialize();
 * 
 * // Ingest a repository
 * tms.ingestRepository("/path/to/monorepo");
 * 
 * // Query with human-like understanding
 * TMSQuery query;
 * query.query_text = "How does authentication flow through the entire monolith?";
 * query.session_id = "review_session_123";
 * 
 * TMSResponse response = tms.forward(query);
 * // response.attention_output.fused_context contains the relevant context
 * ```
 */

#pragma once

#include "tms_types.h"
#include "ltm_faiss.h"
#include "stm.h"
#include "mtm_graph.h"
#include "cross_memory_attention.h"
#include "compute_controller.h"
#include "multi_vector_index.h"

#include <memory>
#include <thread>
#include <atomic>
#include <mutex>
#include <functional>

namespace aipr::tms {

// Forward declarations
class RepoParser;
class EmbeddingEngine;
class EmbeddingIngestor;

/**
 * TMSMemorySystem - The Main Orchestrator
 * 
 * This is the primary interface for the TMS cognitive memory system.
 * It coordinates all memory subsystems and provides a unified API.
 */
class TMSMemorySystem {
public:
    explicit TMSMemorySystem(const TMSConfig& config);
    ~TMSMemorySystem();
    
    // Non-copyable, non-movable
    TMSMemorySystem(const TMSMemorySystem&) = delete;
    TMSMemorySystem& operator=(const TMSMemorySystem&) = delete;
    
    // =========================================================================
    // Initialization & Lifecycle
    // =========================================================================
    
    /**
     * Initialize the memory system
     * - Creates FAISS index for LTM
     * - Initializes STM ring buffer
     * - Builds MTM graph
     * - Loads persisted data
     * - Starts background consolidation thread
     */
    void initialize();
    
    /**
     * Shutdown the memory system
     * - Stops background threads
     * - Persists all data
     * - Releases resources
     */
    void shutdown();
    
    /**
     * Check if initialized
     */
    bool isInitialized() const { return initialized_.load(); }
    
    // =========================================================================
    // Repository Ingestion
    // =========================================================================
    
    /**
     * Ingest an entire repository into LTM
     * 
     * This is the main entry point for indexing a large monorepo.
     * Process:
     * 1. Walk the repository tree
     * 2. Parse each file with tree-sitter
     * 3. Extract semantic chunks with rich metadata
     * 4. Compute embeddings using EmbeddingEngine
     * 5. Store in LTM (FAISS)
     * 6. Update MTM patterns/strategies
     * 
     * @param repo_path Path to the repository root
     * @param repo_id Unique identifier for this repository
     * @param progress_callback Optional callback for progress updates
     */
    void ingestRepository(
        const std::string& repo_path,
        const std::string& repo_id,
        std::function<void(float progress, const std::string& status)> progress_callback = nullptr
    );
    
    /**
     * Ingest pre-parsed chunks (for external parsing pipelines)
     */
    void ingestChunks(
        const std::string& repo_id,
        const std::vector<CodeChunk>& chunks
    );
    
    /**
     * Ingest chunks with pre-computed embeddings
     */
    void ingestChunksWithEmbeddings(
        const std::string& repo_id,
        const std::vector<CodeChunk>& chunks,
        const std::vector<std::vector<float>>& embeddings
    );
    
    /**
     * Remove a repository from memory
     */
    void removeRepository(const std::string& repo_id);
    
    /**
     * Update changed files only (incremental indexing)
     */
    void updateRepository(
        const std::string& repo_id,
        const std::vector<std::string>& changed_files,
        const std::vector<std::string>& deleted_files
    );
    
    // =========================================================================
    // Forward Pass (Main Query Interface)
    // =========================================================================
    
    /**
     * Execute a forward pass through the TMS
     * 
     * This is the main query method that implements the full TMS pipeline:
     * 
     * 1. Compute Controller decides strategy (FAST/BALANCED/THOROUGH)
     * 2. LTM retrieval (FAISS search)
     * 3. STM retrieval (recent context)
     * 4. MTM pattern/strategy matching
     * 5. Cross-Memory Attention fusion
     * 6. Context augmentation
     * 
     * @param query The input query
     * @return TMSResponse with fused context and metadata
     */
    TMSResponse forward(const TMSQuery& query);
    
    /**
     * Simplified query (computes embedding internally)
     */
    TMSResponse query(const std::string& query_text, const std::string& session_id = "");
    
    // =========================================================================
    // Session Management (STM)
    // =========================================================================
    
    /**
     * Start a new session (creates STM context)
     */
    void startSession(const std::string& session_id);
    
    /**
     * Add context to current session
     */
    void addSessionContext(
        const std::string& session_id,
        const std::string& context_type,
        const std::string& content,
        const std::vector<float>& embedding = {}
    );
    
    /**
     * End a session (clears STM)
     */
    void endSession(const std::string& session_id);
    
    // =========================================================================
    // Learning & Feedback
    // =========================================================================
    
    /**
     * Learn from review outcome
     * 
     * Updates pattern confidence and strategy effectiveness based on feedback.
     * This is how the system improves over time.
     * 
     * @param session_id Session where the review happened
     * @param outcome_score 0.0 = bad review, 1.0 = great review
     * @param helpful_chunk_ids Chunks that were helpful
     * @param unhelpful_chunk_ids Chunks that were not helpful
     */
    void learnFromOutcome(
        const std::string& session_id,
        double outcome_score,
        const std::vector<std::string>& helpful_chunk_ids = {},
        const std::vector<std::string>& unhelpful_chunk_ids = {}
    );
    
    /**
     * Register a new pattern (e.g., from user feedback)
     */
    void registerPattern(const PatternEntry& pattern);
    
    /**
     * Register a new strategy
     */
    void registerStrategy(const StrategyEntry& strategy);
    
    // =========================================================================
    // Memory Consolidation
    // =========================================================================
    
    /**
     * Run memory consolidation
     * 
     * - Promotes important STM items to LTM
     * - Updates MTM patterns based on usage
     * - Prunes low-importance items
     * - Merges similar patterns
     */
    void consolidate();
    
    /**
     * Set consolidation interval
     */
    void setConsolidationInterval(std::chrono::hours interval);
    
    // =========================================================================
    // Persistence
    // =========================================================================
    
    /**
     * Save all memory to storage
     */
    void save();
    
    /**
     * Load memory from storage
     */
    void load();
    
    // =========================================================================
    // Statistics & Introspection
    // =========================================================================
    
    struct SystemStats {
        // LTM stats
        size_t ltm_total_chunks = 0;
        size_t ltm_total_repos = 0;
        size_t ltm_index_size_mb = 0;
        
        // STM stats
        size_t stm_active_sessions = 0;
        size_t stm_total_items = 0;
        
        // MTM stats
        size_t mtm_patterns = 0;
        size_t mtm_strategies = 0;
        
        // Performance stats
        double avg_query_time_ms = 0.0;
        double avg_embedding_time_ms = 0.0;
        size_t total_queries = 0;
        
        // Memory usage
        size_t total_memory_mb = 0;
        size_t vram_usage_mb = 0;
    };
    
    SystemStats getStats() const;
    
    /**
     * Get current compute budget
     */
    float getCurrentBudget() const;
    
    // =========================================================================
    // Direct Access to Subsystems (Advanced Use)
    // =========================================================================
    
    LTMFaiss& ltm() { return *ltm_; }
    STM& stm() { return *stm_; }
    MTMGraph& mtm() { return *mtm_; }
    CrossMemoryAttention& attention() { return *attention_; }
    ComputeController& controller() { return *controller_; }
    EmbeddingEngine& embeddingEngine() { return *embedding_engine_; }
    MultiVectorIndex* multiVector() { return multi_vector_.get(); }
    const TMSConfig& tmsConfig() const { return config_; }
    
    /**
     * Reconfigure the embedding engine at runtime.
     *
     * Used when the gRPC layer receives a per-request embedding config
     * from the Go server (e.g. user switched from MiniLM to OpenAI).
     * The API key is held only in memory and is NOT persisted.
     *
     * @param backend  "onnx", "http", or "mock"
     * @param endpoint API URL (for HTTP backends)
     * @param model    Model name
     * @param api_key  Transient API key
     * @param dims     Embedding dimension
     */
    void reconfigureEmbedding(
        const std::string& backend,
        const std::string& endpoint,
        const std::string& model,
        const std::string& api_key,
        size_t dims
    );

    /**
     * Store a pre-embedded asset chunk (image, audio) in the FAISS index.
     *
     * Converts the AssetChunk into a CodeChunk-compatible format and inserts
     * the pre-computed embedding vector into the LTM FAISS index.
     *
     * @param chunk  Asset chunk with pre-computed embedding vector
     * @return true if successfully stored in the index
     */
    bool storeAssetChunk(const AssetChunk& chunk);
    
private:
    TMSConfig config_;
    
    // Memory subsystems
    std::unique_ptr<LTMFaiss> ltm_;
    std::unique_ptr<MultiVectorIndex> multi_vector_;
    std::unique_ptr<STM> stm_;
    std::unique_ptr<MTMGraph> mtm_;
    std::unique_ptr<CrossMemoryAttention> attention_;
    std::unique_ptr<ComputeController> controller_;
    
    // Ingestion components
    std::unique_ptr<RepoParser> repo_parser_;
    std::unique_ptr<EmbeddingEngine> embedding_engine_;
    std::unique_ptr<EmbeddingIngestor> ingestor_;

    // Knowledge Graph (optional, gated by knowledge_graph_enabled)
    class KnowledgeGraphHandle;
    std::unique_ptr<KnowledgeGraphHandle> kg_handle_;

    // Merkle cache (optional, for incremental reindex)
    class MerkleCacheHandle;
    std::unique_ptr<MerkleCacheHandle> merkle_handle_;
    
    // State
    std::atomic<bool> initialized_{false};
    std::atomic<bool> shutdown_requested_{false};
    mutable std::mutex mutex_;
    
    // Background consolidation
    std::thread consolidation_thread_;
    void consolidationLoop();
    
    // Internal helpers
    std::vector<float> computeEmbedding(const std::string& text);
    void updateImportanceScores(const std::vector<std::string>& accessed_ids);
    CrossMemoryOutput runCrossMemoryAttention(
        const std::vector<float>& query_embedding,
        const std::vector<RetrievedChunk>& ltm_results,
        const std::vector<RetrievedChunk>& stm_results,
        const std::vector<PatternEntry>& patterns,
        const std::vector<StrategyEntry>& strategies,
        const ComputeDecision& decision
    );
};

} // namespace aipr::tms
