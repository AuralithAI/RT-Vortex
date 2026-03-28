/**
 * AI PR Reviewer - Brain-Inspired Memory Architecture
 * 
 * Implements a human-brain-inspired memory system for advanced RAG:
 * 
 * 1. Long-Term Memory (LTM) - Persistent knowledge base with FAISS vector search
 *    - Repository code embeddings
 *    - Historical review patterns
 *    - Semantic code understanding
 * 
 * 2. Short-Term Memory (STM) - Active working memory for current session
 *    - Current PR context
 *    - Recent retrievals
 *    - Conversation history
 * 
 * 3. Meta-Task Memory (MTM) - Patterns, strategies & meta-learning
 *    - Learned review strategies
 *    - Common issue patterns
 *    - Project-specific rules
 * 
 * 4. Cross-Memory Attention - Deep fusion across memory types
 *    - Attention-weighted retrieval
 *    - Memory consolidation
 *    - Context-aware blending
 */

#pragma once

#include "types.h"
#include "retriever.h"
#include <string>
#include <vector>
#include <memory>
#include <map>
#include <unordered_map>
#include <chrono>
#include <functional>
#include <optional>
#include <mutex>
#include <thread>
#include <queue>
#include <atomic>

namespace aipr {

// =============================================================================
// Memory Item Types
// =============================================================================

/**
 * Base memory item with common fields
 */
struct MemoryItem {
    std::string id;
    std::string content;
    std::vector<float> embedding;
    std::chrono::system_clock::time_point created_at;
    std::chrono::system_clock::time_point last_accessed;
    double importance_score = 1.0;
    int access_count = 0;
    std::map<std::string, std::string> metadata;
    
    // Memory type flags
    bool is_code = false;
    bool is_pattern = false;
    bool is_strategy = false;
};

/**
 * Code memory item (for LTM)
 */
struct CodeMemory : public MemoryItem {
    std::string repo_id;
    std::string file_path;
    std::string language;
    std::vector<std::string> symbols;
    std::string chunk_id;
    size_t start_line = 0;
    size_t end_line = 0;
};

/**
 * Pattern memory item (for MTM)
 */
struct PatternMemory : public MemoryItem {
    std::string pattern_type;  // "bug", "security", "performance", "style"
    std::string rule_name;
    double confidence = 0.0;
    int occurrence_count = 0;
    std::vector<std::string> example_ids;  // References to code memories
};

/**
 * Strategy memory item (for MTM)
 */
struct StrategyMemory : public MemoryItem {
    std::string strategy_type;  // "review", "analysis", "suggestion"
    std::string context_type;   // "security", "performance", "refactoring"
    double effectiveness_score = 0.0;
    int use_count = 0;
    std::vector<std::string> applicable_patterns;
};

/**
 * Session memory item (for STM)
 */
struct SessionMemory : public MemoryItem {
    std::string session_id;
    std::string query;
    std::vector<std::string> retrieved_ids;
    std::string response;
    double relevance_score = 0.0;
};

// =============================================================================
// Memory Retrieval Results
// =============================================================================

/**
 * Cross-memory retrieval result with attention scores
 */
struct MemoryRetrievalResult {
    std::vector<MemoryItem> ltm_items;      // Long-term memories
    std::vector<MemoryItem> stm_items;      // Short-term memories
    std::vector<PatternMemory> mtm_patterns;// Meta-task patterns
    std::vector<StrategyMemory> mtm_strategies; // Meta-task strategies
    
    // Attention weights for cross-memory fusion
    std::vector<float> ltm_attention;
    std::vector<float> stm_attention;
    std::vector<float> mtm_attention;
    
    // Fused context (after cross-memory attention)
    std::string fused_context;
    std::vector<float> fused_embedding;
    
    // Retrieval metadata
    double retrieval_confidence = 0.0;
    std::chrono::milliseconds retrieval_time{0};
};

// =============================================================================
// Memory Configuration
// =============================================================================

struct MemoryConfig {
    // LTM Configuration
    size_t ltm_capacity = 1000000;      // Max items in long-term memory
    size_t ltm_dimension = 1536;         // Embedding dimension
    int ltm_top_k = 20;                  // Default retrieval count
    double ltm_similarity_threshold = 0.5;
    
    // STM Configuration
    size_t stm_capacity = 100;           // Max items in short-term memory
    std::chrono::minutes stm_ttl{30};    // Time-to-live for STM items
    int stm_top_k = 10;
    
    // MTM Configuration
    size_t mtm_pattern_capacity = 10000;
    size_t mtm_strategy_capacity = 1000;
    double mtm_confidence_threshold = 0.7;
    
    // Cross-Memory Attention
    bool enable_cross_attention = true;
    int attention_heads = 8;
    double attention_dropout = 0.1;
    
    // Memory consolidation
    bool enable_consolidation = true;
    std::chrono::hours consolidation_interval{24};
    double consolidation_threshold = 0.3;  // Min importance to keep
    
    // Persistence
    std::string storage_path;
    bool persist_stm = false;  // Usually STM is ephemeral
    bool persist_mtm = true;
};

// =============================================================================
// Long-Term Memory (LTM)
// =============================================================================

/**
 * Long-Term Memory - Persistent knowledge base with FAISS
 * 
 * Stores:
 * - Code embeddings from indexed repositories
 * - Historical review contexts
 * - Semantic code understanding
 */
class LongTermMemory {
public:
    explicit LongTermMemory(const MemoryConfig& config);
    ~LongTermMemory();
    
    /**
     * Add code memory to LTM
     */
    void store(const CodeMemory& memory);
    
    /**
     * Batch store for efficiency
     */
    void storeBatch(const std::vector<CodeMemory>& memories);
    
    /**
     * Retrieve by embedding similarity
     */
    std::vector<CodeMemory> retrieve(
        const std::vector<float>& query_embedding,
        int top_k = -1,  // Use config default if -1
        const std::string& repo_filter = ""
    );
    
    /**
     * Retrieve by text query (computes embedding internally)
     */
    std::vector<CodeMemory> retrieveByQuery(
        const std::string& query,
        int top_k = -1,
        const std::string& repo_filter = ""
    );
    
    /**
     * Hybrid retrieval: vector + lexical
     */
    std::vector<CodeMemory> hybridRetrieve(
        const std::string& query,
        const std::vector<float>& query_embedding,
        int top_k = -1,
        double alpha = 0.7  // Weight for vector vs lexical
    );
    
    /**
     * Get memory by ID
     */
    std::optional<CodeMemory> get(const std::string& id);
    
    /**
     * Remove memory by ID
     */
    bool remove(const std::string& id);
    
    /**
     * Remove all memories for a repository
     */
    size_t removeByRepo(const std::string& repo_id);
    
    /**
     * Update memory importance based on access patterns
     */
    void updateImportance(const std::string& id, double delta);
    
    /**
     * Run memory consolidation (remove low-importance items)
     */
    size_t consolidate(double threshold = -1);  // Use config default if -1
    
    /**
     * Persist to storage
     */
    void persist();
    
    /**
     * Load from storage
     */
    void load();
    
    /**
     * Get statistics
     */
    struct Stats {
        size_t total_items = 0;
        size_t total_repos = 0;
        double avg_importance = 0.0;
        size_t memory_bytes = 0;
    };
    Stats getStats() const;

private:
    MemoryConfig config_;
    std::unique_ptr<class FAISSIndex> faiss_index_;
    std::unordered_map<std::string, CodeMemory> memories_;
    std::unordered_map<std::string, std::vector<std::string>> repo_index_;
    mutable std::mutex mutex_;
    
    void rebuildIndex();
};

// =============================================================================
// Short-Term Memory (STM)
// =============================================================================

/**
 * Short-Term Memory - Active working memory
 * 
 * Stores:
 * - Current PR context
 * - Recent retrievals
 * - Conversation history
 * - Ephemeral computation results
 */
class ShortTermMemory {
public:
    explicit ShortTermMemory(const MemoryConfig& config);
    ~ShortTermMemory();
    
    /**
     * Store session memory
     */
    void store(const SessionMemory& memory);
    
    /**
     * Add to conversation history
     */
    void addToHistory(const std::string& session_id, const std::string& role, const std::string& content);
    
    /**
     * Get conversation history
     */
    std::vector<std::pair<std::string, std::string>> getHistory(const std::string& session_id);
    
    /**
     * Store recent retrieval for caching
     */
    void cacheRetrieval(const std::string& query_hash, const std::vector<std::string>& result_ids);
    
    /**
     * Check retrieval cache
     */
    std::optional<std::vector<std::string>> getCachedRetrieval(const std::string& query_hash);
    
    /**
     * Retrieve recent session memories
     */
    std::vector<SessionMemory> getRecent(const std::string& session_id, int limit = 10);
    
    /**
     * Retrieve by recency and relevance
     */
    std::vector<SessionMemory> retrieve(
        const std::vector<float>& query_embedding,
        const std::string& session_id,
        int top_k = -1
    );
    
    /**
     * Clear session
     */
    void clearSession(const std::string& session_id);
    
    /**
     * Cleanup expired items
     */
    size_t cleanup();
    
    /**
     * Get active session IDs
     */
    std::vector<std::string> getActiveSessions() const;

private:
    MemoryConfig config_;
    std::unordered_map<std::string, std::vector<SessionMemory>> session_memories_;
    std::unordered_map<std::string, std::vector<std::pair<std::string, std::string>>> histories_;
    std::unordered_map<std::string, std::vector<std::string>> retrieval_cache_;
    std::unordered_map<std::string, std::chrono::system_clock::time_point> cache_timestamps_;
    mutable std::mutex mutex_;
};

// =============================================================================
// Meta-Task Memory (MTM)
// =============================================================================

/**
 * Meta-Task Memory - Patterns, strategies & meta-learning
 * 
 * Stores:
 * - Learned review patterns (e.g., "null pointer check missing")
 * - Effective review strategies
 * - Project-specific rules
 * - Common issue fingerprints
 */
class MetaTaskMemory {
public:
    explicit MetaTaskMemory(const MemoryConfig& config);
    ~MetaTaskMemory();
    
    // Pattern Management
    
    /**
     * Store a learned pattern
     */
    void storePattern(const PatternMemory& pattern);
    
    /**
     * Update pattern confidence based on feedback
     */
    void updatePatternConfidence(const std::string& pattern_id, bool positive_feedback);
    
    /**
     * Retrieve patterns matching a code context
     */
    std::vector<PatternMemory> matchPatterns(
        const std::vector<float>& code_embedding,
        const std::string& language = "",
        int top_k = 10
    );
    
    /**
     * Get patterns by type
     */
    std::vector<PatternMemory> getPatternsByType(const std::string& pattern_type);
    
    // Strategy Management
    
    /**
     * Store a review strategy
     */
    void storeStrategy(const StrategyMemory& strategy);
    
    /**
     * Update strategy effectiveness
     */
    void updateStrategyEffectiveness(const std::string& strategy_id, double effectiveness);
    
    /**
     * Retrieve applicable strategies for a context
     */
    std::vector<StrategyMemory> matchStrategies(
        const std::string& context_type,
        const std::vector<std::string>& detected_patterns,
        int top_k = 5
    );
    
    /**
     * Get most effective strategies
     */
    std::vector<StrategyMemory> getTopStrategies(int limit = 10);
    
    // Meta-learning
    
    /**
     * Learn from a review outcome
     */
    void learnFromOutcome(
        const std::string& session_id,
        const std::vector<std::string>& used_pattern_ids,
        const std::vector<std::string>& used_strategy_ids,
        double outcome_score  // 0.0 = bad review, 1.0 = great review
    );
    
    /**
     * Consolidate patterns (merge similar, remove ineffective)
     */
    size_t consolidatePatterns();
    
    /**
     * Persist to storage
     */
    void persist();
    
    /**
     * Load from storage
     */
    void load();

private:
    MemoryConfig config_;
    std::unordered_map<std::string, PatternMemory> patterns_;
    std::unordered_map<std::string, StrategyMemory> strategies_;
    std::unique_ptr<class FAISSIndex> pattern_index_;
    mutable std::mutex mutex_;
    
    void rebuildPatternIndex();
};

// =============================================================================
// Cross-Memory Attention
// =============================================================================

/**
 * Cross-Memory Attention Module
 * 
 * Implements multi-head attention across memory types:
 * - Query comes from current context (STM)
 * - Keys/Values from LTM, STM, MTM
 * - Produces fused context with attention-weighted information
 * 
 * This is NOT simple prompt concatenation - it's deep fusion where:
 * - Attention weights determine which memories are most relevant
 * - Cross-attention allows memories to "see" each other
 * - Output is a unified, contextually-aware representation
 */
class CrossMemoryAttention {
public:
    struct Config {
        int num_heads = 8;
        int embed_dim = 1536;
        int ffn_dim = 4096;
        double dropout = 0.1;
        bool use_rotary_embedding = true;
        int max_sequence_length = 8192;
    };
    
    explicit CrossMemoryAttention(const Config& config);
    ~CrossMemoryAttention();
    
    /**
     * Compute cross-memory attention
     * 
     * @param query Current context embedding (from STM)
     * @param ltm_items Long-term memory items
     * @param stm_items Short-term memory items (recent context)
     * @param mtm_patterns Meta-task patterns
     * @param mtm_strategies Meta-task strategies
     * @return Attention-weighted fused result
     */
    MemoryRetrievalResult attend(
        const std::vector<float>& query,
        const std::vector<CodeMemory>& ltm_items,
        const std::vector<SessionMemory>& stm_items,
        const std::vector<PatternMemory>& mtm_patterns,
        const std::vector<StrategyMemory>& mtm_strategies
    );
    
    /**
     * Compute attention weights (for explainability)
     */
    struct AttentionWeights {
        std::vector<std::vector<float>> ltm_weights;  // [heads][items]
        std::vector<std::vector<float>> stm_weights;
        std::vector<std::vector<float>> mtm_weights;
        std::vector<float> aggregated_ltm;  // Averaged across heads
        std::vector<float> aggregated_stm;
        std::vector<float> aggregated_mtm;
    };
    
    AttentionWeights computeAttentionWeights(
        const std::vector<float>& query,
        const std::vector<MemoryItem>& ltm_items,
        const std::vector<MemoryItem>& stm_items,
        const std::vector<MemoryItem>& mtm_items
    );

private:
    Config config_;
    
    // Projection matrices (simplified - in production use ONNX/TensorRT)
    std::vector<std::vector<float>> wq_;  // Query projection
    std::vector<std::vector<float>> wk_;  // Key projection
    std::vector<std::vector<float>> wv_;  // Value projection
    std::vector<std::vector<float>> wo_;  // Output projection
    
    std::vector<float> multiHeadAttention(
        const std::vector<float>& query,
        const std::vector<std::vector<float>>& keys,
        const std::vector<std::vector<float>>& values
    );
    
    std::vector<float> softmax(const std::vector<float>& x);
    float dotProduct(const std::vector<float>& a, const std::vector<float>& b);
};

// =============================================================================
// Memory System (Unified Interface)
// =============================================================================

/**
 * Unified Memory System
 * 
 * Coordinates LTM, STM, MTM and cross-memory attention.
 * Provides high-level API for memory operations.
 */
class MemorySystem {
public:
    explicit MemorySystem(const MemoryConfig& config);
    ~MemorySystem();
    
    /**
     * Initialize the memory system
     */
    void initialize();
    
    /**
     * Index a repository into LTM
     */
    void indexRepository(
        const std::string& repo_id,
        const std::vector<Chunk>& chunks,
        const std::vector<std::vector<float>>& embeddings
    );
    
    /**
     * Start a review session (initializes STM)
     */
    void startSession(const std::string& session_id);
    
    /**
     * Add current context to STM
     */
    void addContext(
        const std::string& session_id,
        const std::string& context_type,  // "diff", "file", "query"
        const std::string& content,
        const std::vector<float>& embedding
    );
    
    /**
     * Unified retrieval with cross-memory attention
     * 
     * This is the main entry point for retrieval:
     * 1. Queries LTM for relevant code
     * 2. Checks STM for recent context
     * 3. Matches MTM patterns and strategies
     * 4. Applies cross-memory attention for fusion
     */
    MemoryRetrievalResult retrieve(
        const std::string& session_id,
        const std::string& query,
        const std::vector<float>& query_embedding,
        int top_k = 20
    );
    
    /**
     * Learn from review feedback
     */
    void learnFromFeedback(
        const std::string& session_id,
        double review_quality,  // 0.0 = bad, 1.0 = great
        const std::vector<std::string>& helpful_items,
        const std::vector<std::string>& unhelpful_items
    );
    
    /**
     * End session (cleanup STM)
     */
    void endSession(const std::string& session_id);
    
    /**
     * Get memory statistics
     */
    struct SystemStats {
        LongTermMemory::Stats ltm_stats;
        size_t stm_sessions = 0;
        size_t mtm_patterns = 0;
        size_t mtm_strategies = 0;
        size_t total_memory_mb = 0;
    };
    SystemStats getStats() const;
    
    /**
     * Run maintenance (consolidation, cleanup)
     */
    void runMaintenance();
    
    /**
     * Persist all memories
     */
    void persist();
    
    /**
     * Load all memories
     */
    void load();
    
    // Direct access to memory subsystems
    LongTermMemory& ltm() { return *ltm_; }
    ShortTermMemory& stm() { return *stm_; }
    MetaTaskMemory& mtm() { return *mtm_; }

private:
    MemoryConfig config_;
    std::unique_ptr<LongTermMemory> ltm_;
    std::unique_ptr<ShortTermMemory> stm_;
    std::unique_ptr<MetaTaskMemory> mtm_;
    std::unique_ptr<CrossMemoryAttention> attention_;
    
    std::atomic<bool> initialized_{false};
    std::mutex mutex_;
    
    // Background consolidation
    std::thread consolidation_thread_;
    std::atomic<bool> stop_consolidation_{false};
    void consolidationLoop();
};

// =============================================================================
// Context Pack Builder (Enhanced with Memory System)
// =============================================================================

/**
 * Build context pack using brain-inspired memory system
 */
class MemoryAwareContextBuilder {
public:
    explicit MemoryAwareContextBuilder(MemorySystem& memory);
    
    struct ContextPack {
        std::string combined_context;  // Ready for LLM prompt
        std::vector<CodeMemory> code_chunks;
        std::vector<PatternMemory> applicable_patterns;
        std::vector<StrategyMemory> suggested_strategies;
        MemoryRetrievalResult retrieval_result;
        
        // For explainability
        std::map<std::string, double> attention_scores;
        std::vector<std::string> reasoning_trace;
    };
    
    /**
     * Build context for a review
     */
    ContextPack buildReviewContext(
        const std::string& session_id,
        const std::string& diff,
        const std::vector<float>& diff_embedding,
        const std::vector<std::string>& changed_files,
        int max_tokens = 8000
    );

private:
    MemorySystem& memory_;
    
    std::string formatContext(const MemoryRetrievalResult& result, int max_tokens);
};

} // namespace aipr
