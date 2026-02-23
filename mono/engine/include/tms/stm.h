/**
 * TMS Short-Term Memory (STM) - Ring Buffer Implementation
 * 
 * Active working memory for the current session.
 * Implements a ring buffer for recent queries, retrievals, and context.
 * 
 * Features:
 * - Fast O(1) insert/retrieve for recent items
 * - Session-based isolation
 * - TTL-based expiration
 * - Query/retrieval caching
 * - Conversation history tracking
 */

#pragma once

#include "tms_types.h"
#include <memory>
#include <mutex>
#include <unordered_map>
#include <deque>
#include <chrono>
#include <optional>

namespace aipr::tms {

/**
 * STM Entry - An item in short-term memory
 */
struct STMEntry {
    std::string id;
    std::string session_id;
    std::string entry_type;                 // "query", "retrieval", "context", "response"
    std::string content;
    std::vector<float> embedding;
    
    // For queries
    std::string query_text;
    std::vector<std::string> retrieved_chunk_ids;
    
    // For context
    std::string context_type;               // "diff", "file", "conversation"
    
    // Timestamps
    std::chrono::system_clock::time_point created_at;
    std::chrono::system_clock::time_point last_accessed;
    
    // Relevance tracking
    double relevance_score = 0.0;
    int access_count = 0;
    bool promoted_to_ltm = false;           // Marked for LTM promotion
};

/**
 * Conversation Turn
 */
struct ConversationTurn {
    std::string role;                       // "user", "assistant", "system"
    std::string content;
    std::chrono::system_clock::time_point timestamp;
    std::optional<std::vector<float>> embedding;
};

/**
 * Retrieval Cache Entry
 */
struct RetrievalCacheEntry {
    std::string query_hash;
    std::vector<std::string> chunk_ids;
    std::chrono::system_clock::time_point cached_at;
    int hit_count = 0;
};

/**
 * STM Configuration
 */
struct STMConfig {
    size_t capacity = 100;                  // Max entries per session
    std::chrono::minutes ttl{30};           // Time-to-live
    size_t max_sessions = 1000;             // Max concurrent sessions
    size_t conversation_history_limit = 50; // Max conversation turns per session
    size_t retrieval_cache_size = 500;      // Max cached retrievals
    std::chrono::minutes cache_ttl{5};      // Cache TTL
    bool enable_embedding_cache = true;
    int default_top_k = 10;
};

/**
 * STM - Short-Term Memory
 */
class STM {
public:
    explicit STM(const STMConfig& config);
    ~STM();
    
    // Non-copyable
    STM(const STM&) = delete;
    STM& operator=(const STM&) = delete;
    
    // =========================================================================
    // Session Management
    // =========================================================================
    
    /**
     * Start a new session
     * Creates an empty STM context for the session.
     */
    void startSession(const std::string& session_id);
    
    /**
     * End a session
     * Clears all STM data for the session.
     */
    void endSession(const std::string& session_id);
    
    /**
     * Check if session exists
     */
    bool hasSession(const std::string& session_id) const;
    
    /**
     * Get all active session IDs
     */
    std::vector<std::string> getActiveSessions() const;
    
    /**
     * Touch session (update last access time)
     */
    void touchSession(const std::string& session_id);
    
    // =========================================================================
    // Entry Management
    // =========================================================================
    
    /**
     * Store an entry in STM
     */
    void store(const STMEntry& entry);
    
    /**
     * Store a query and its retrieval results
     */
    void storeQuery(
        const std::string& session_id,
        const std::string& query_text,
        const std::vector<float>& query_embedding,
        const std::vector<std::string>& retrieved_chunk_ids
    );
    
    /**
     * Store context (diff, file, etc.)
     */
    void storeContext(
        const std::string& session_id,
        const std::string& context_type,
        const std::string& content,
        const std::vector<float>& embedding = {}
    );
    
    /**
     * Get entry by ID
     */
    std::optional<STMEntry> get(const std::string& entry_id);
    
    /**
     * Get recent entries for a session
     */
    std::vector<STMEntry> getRecent(
        const std::string& session_id,
        int limit = -1,
        const std::string& entry_type = ""
    );
    
    /**
     * Search STM by embedding similarity
     */
    std::vector<RetrievedChunk> search(
        const std::string& session_id,
        const std::vector<float>& query_embedding,
        int top_k = -1
    );
    
    /**
     * Mark entry for LTM promotion
     */
    void markForPromotion(const std::string& entry_id);
    
    /**
     * Get entries marked for promotion
     */
    std::vector<STMEntry> getPromotionCandidates(const std::string& session_id);
    
    // =========================================================================
    // Conversation History
    // =========================================================================
    
    /**
     * Add to conversation history
     */
    void addConversationTurn(
        const std::string& session_id,
        const std::string& role,
        const std::string& content,
        const std::vector<float>& embedding = {}
    );
    
    /**
     * Get conversation history
     */
    std::vector<ConversationTurn> getConversationHistory(
        const std::string& session_id,
        int limit = -1
    );
    
    /**
     * Clear conversation history
     */
    void clearConversationHistory(const std::string& session_id);
    
    // =========================================================================
    // Retrieval Cache
    // =========================================================================
    
    /**
     * Cache a retrieval result
     */
    void cacheRetrieval(
        const std::string& query_hash,
        const std::vector<std::string>& chunk_ids
    );
    
    /**
     * Get cached retrieval
     */
    std::optional<std::vector<std::string>> getCachedRetrieval(
        const std::string& query_hash
    );
    
    /**
     * Compute query hash for caching
     */
    static std::string computeQueryHash(
        const std::string& query_text,
        const std::string& repo_filter = ""
    );
    
    /**
     * Clear retrieval cache
     */
    void clearCache();
    
    // =========================================================================
    // Maintenance
    // =========================================================================
    
    /**
     * Cleanup expired entries
     * @return Number of entries removed
     */
    size_t cleanup();
    
    /**
     * Cleanup expired sessions
     * @return Number of sessions removed
     */
    size_t cleanupSessions();
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    struct Stats {
        size_t active_sessions = 0;
        size_t total_entries = 0;
        size_t conversation_turns = 0;
        size_t cache_size = 0;
        size_t cache_hits = 0;
        size_t cache_misses = 0;
        double hit_rate = 0.0;
    };
    
    Stats getStats() const;
    
    struct SessionStats {
        std::string session_id;
        size_t entry_count = 0;
        size_t conversation_turns = 0;
        std::chrono::system_clock::time_point created_at;
        std::chrono::system_clock::time_point last_accessed;
    };
    
    SessionStats getSessionStats(const std::string& session_id) const;

private:
    STMConfig config_;
    
    // Per-session data
    struct SessionData {
        std::string id;
        std::deque<STMEntry> entries;                   // Ring buffer
        std::vector<ConversationTurn> conversation;
        std::chrono::system_clock::time_point created_at;
        std::chrono::system_clock::time_point last_accessed;
        std::unordered_map<std::string, size_t> entry_index;  // entry_id -> position
    };
    
    std::unordered_map<std::string, SessionData> sessions_;
    
    // Retrieval cache
    std::unordered_map<std::string, RetrievalCacheEntry> retrieval_cache_;
    size_t cache_hits_ = 0;
    size_t cache_misses_ = 0;
    
    // Thread safety
    mutable std::mutex mutex_;
    
    // Helpers
    void ensureSession(const std::string& session_id);
    void evictOldestEntry(SessionData& session);
    bool isExpired(const std::chrono::system_clock::time_point& time) const;
    float cosineSimilarity(const std::vector<float>& a, const std::vector<float>& b) const;
};

} // namespace aipr::tms
