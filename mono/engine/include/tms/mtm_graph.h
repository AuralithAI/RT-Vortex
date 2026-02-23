/**
 * TMS Meta-Task Memory (MTM) - Graph Implementation
 * 
 * Stores learned patterns, strategies, and meta-knowledge about code.
 * Uses a graph structure to represent relationships between concepts.
 * 
 * Graph Structure:
 * - Nodes: Patterns (e.g., "authentication_flow"), Strategies (e.g., "security_review")
 * - Edges: Relationships (e.g., "applies_to", "suggests", "conflicts_with")
 * 
 * Features:
 * - Pattern matching against code embeddings
 * - Strategy recommendation based on detected patterns
 * - Confidence tracking and updates from feedback
 * - Pattern merging for similar concepts
 * - Graph traversal for related patterns
 */

#pragma once

#include "tms_types.h"
#include <memory>
#include <mutex>
#include <unordered_map>
#include <unordered_set>
#include <vector>
#include <functional>

namespace aipr::tms {

/**
 * Edge type in the MTM graph
 */
enum class EdgeType {
    APPLIES_TO,         // Pattern applies to strategy
    SUGGESTS,           // One pattern suggests another
    CONFLICTS_WITH,     // Conflicting patterns/strategies
    PARENT_OF,          // Hierarchical relationship
    RELATED_TO,         // General relationship
    TRIGGERED_BY,       // Pattern triggered by code pattern
    RESOLVES            // Strategy resolves a pattern
};

/**
 * Graph Edge
 */
struct MTMEdge {
    std::string source_id;
    std::string target_id;
    EdgeType type;
    double weight = 1.0;
    std::string label;
    std::map<std::string, std::string> attributes;
};

/**
 * Pattern Category
 */
enum class PatternCategory {
    BUG,                // Bug patterns (null pointer, race condition)
    SECURITY,           // Security issues (injection, auth bypass)
    PERFORMANCE,        // Performance anti-patterns
    ARCHITECTURE,       // Architectural concerns
    STYLE,              // Code style issues
    TESTING,            // Testing patterns
    DOCUMENTATION,      // Documentation patterns
    CUSTOM              // User-defined patterns
};

/**
 * Strategy Category
 */
enum class StrategyCategory {
    REVIEW,             // Code review strategies
    ANALYSIS,           // Static analysis strategies
    REFACTOR,           // Refactoring strategies
    EXPLAIN,            // Code explanation strategies
    SECURITY_AUDIT,     // Security audit strategies
    PERFORMANCE_AUDIT,  // Performance audit strategies
    CUSTOM              // User-defined strategies
};

/**
 * MTM Configuration
 */
struct MTMConfig {
    size_t max_patterns = 10000;
    size_t max_strategies = 1000;
    size_t max_edges = 100000;
    
    double confidence_threshold = 0.5;      // Min confidence to use pattern
    double similarity_threshold = 0.85;     // For pattern merging
    double learning_rate = 0.1;             // How fast confidence updates
    
    bool enable_auto_merge = true;          // Auto-merge similar patterns
    bool enable_decay = true;               // Time-based confidence decay
    double decay_rate = 0.01;               // Per-day decay rate
    
    std::string storage_path;
    
    // FAISS for pattern embedding search
    size_t embedding_dimension = 1536;
    int pattern_search_k = 10;
};

/**
 * MTMGraph - Meta-Task Memory Graph
 */
class MTMGraph {
public:
    explicit MTMGraph(const MTMConfig& config);
    ~MTMGraph();
    
    // Non-copyable
    MTMGraph(const MTMGraph&) = delete;
    MTMGraph& operator=(const MTMGraph&) = delete;
    
    // =========================================================================
    // Pattern Management
    // =========================================================================
    
    /**
     * Store a pattern
     */
    void storePattern(const PatternEntry& pattern);
    
    /**
     * Get pattern by ID
     */
    std::optional<PatternEntry> getPattern(const std::string& pattern_id);
    
    /**
     * Search patterns by embedding
     */
    std::vector<PatternEntry> matchPatterns(
        const std::vector<float>& code_embedding,
        int top_k = -1,
        double min_confidence = -1.0
    );
    
    /**
     * Search patterns by embedding with language filter
     */
    std::vector<PatternEntry> matchPatternsForLanguage(
        const std::vector<float>& code_embedding,
        const std::string& language,
        int top_k = -1
    );
    
    /**
     * Get patterns by category
     */
    std::vector<PatternEntry> getPatternsByCategory(PatternCategory category);
    
    /**
     * Get patterns by type string
     */
    std::vector<PatternEntry> getPatternsByType(const std::string& pattern_type);
    
    /**
     * Update pattern confidence based on feedback
     */
    void updatePatternConfidence(
        const std::string& pattern_id,
        bool was_helpful
    );
    
    /**
     * Record pattern occurrence
     */
    void recordPatternOccurrence(
        const std::string& pattern_id,
        const std::string& chunk_id
    );
    
    /**
     * Delete pattern
     */
    bool deletePattern(const std::string& pattern_id);
    
    // =========================================================================
    // Strategy Management
    // =========================================================================
    
    /**
     * Store a strategy
     */
    void storeStrategy(const StrategyEntry& strategy);
    
    /**
     * Get strategy by ID
     */
    std::optional<StrategyEntry> getStrategy(const std::string& strategy_id);
    
    /**
     * Match strategies for a context and detected patterns
     */
    std::vector<StrategyEntry> matchStrategies(
        const std::string& context_type,
        const std::vector<std::string>& detected_pattern_ids,
        int top_k = -1
    );
    
    /**
     * Get strategies by category
     */
    std::vector<StrategyEntry> getStrategiesByCategory(StrategyCategory category);
    
    /**
     * Get most effective strategies
     */
    std::vector<StrategyEntry> getTopStrategies(int limit = 10);
    
    /**
     * Update strategy effectiveness
     */
    void updateStrategyEffectiveness(
        const std::string& strategy_id,
        double outcome_score
    );
    
    /**
     * Delete strategy
     */
    bool deleteStrategy(const std::string& strategy_id);
    
    // =========================================================================
    // Graph Operations
    // =========================================================================
    
    /**
     * Add edge between nodes
     */
    void addEdge(const MTMEdge& edge);
    
    /**
     * Remove edge
     */
    bool removeEdge(const std::string& source_id, const std::string& target_id, EdgeType type);
    
    /**
     * Get edges from a node
     */
    std::vector<MTMEdge> getOutEdges(const std::string& node_id);
    
    /**
     * Get edges to a node
     */
    std::vector<MTMEdge> getInEdges(const std::string& node_id);
    
    /**
     * Get related patterns (traverse graph)
     */
    std::vector<PatternEntry> getRelatedPatterns(
        const std::string& pattern_id,
        int max_depth = 2
    );
    
    /**
     * Get strategies that resolve a pattern
     */
    std::vector<StrategyEntry> getStrategiesForPattern(const std::string& pattern_id);
    
    // =========================================================================
    // Learning & Optimization
    // =========================================================================
    
    /**
     * Learn from review outcome
     * 
     * Updates pattern confidence and strategy effectiveness based on
     * the outcome of a review that used those patterns/strategies.
     */
    void learnFromOutcome(
        const std::vector<std::string>& used_pattern_ids,
        const std::vector<std::string>& used_strategy_ids,
        double outcome_score
    );
    
    /**
     * Consolidate patterns
     * - Merge similar patterns
     * - Remove low-confidence patterns
     * - Update edges
     * @return Number of patterns affected
     */
    size_t consolidatePatterns();
    
    /**
     * Apply time decay to confidence scores
     */
    void applyDecay();
    
    // =========================================================================
    // Persistence
    // =========================================================================
    
    /**
     * Save to storage
     */
    void save();
    
    /**
     * Save to specific path
     */
    void save(const std::string& path);
    
    /**
     * Load from storage
     */
    void load();
    
    /**
     * Load from specific path
     */
    void load(const std::string& path);
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    struct Stats {
        size_t total_patterns = 0;
        size_t total_strategies = 0;
        size_t total_edges = 0;
        double avg_pattern_confidence = 0.0;
        double avg_strategy_effectiveness = 0.0;
        std::map<std::string, size_t> patterns_by_type;
        std::map<std::string, size_t> strategies_by_type;
    };
    
    Stats getStats() const;

private:
    MTMConfig config_;
    
    // Pattern storage
    std::unordered_map<std::string, PatternEntry> patterns_;
    
    // Strategy storage
    std::unordered_map<std::string, StrategyEntry> strategies_;
    
    // Graph edges (adjacency list)
    std::unordered_map<std::string, std::vector<MTMEdge>> out_edges_;
    std::unordered_map<std::string, std::vector<MTMEdge>> in_edges_;
    
    // Indexes
    std::unordered_map<std::string, std::unordered_set<std::string>> type_to_patterns_;
    std::unordered_map<std::string, std::unordered_set<std::string>> type_to_strategies_;
    std::unordered_map<std::string, std::unordered_set<std::string>> language_to_patterns_;
    
    // Pattern embedding index (for fast matching)
    class PatternIndex;
    std::unique_ptr<PatternIndex> pattern_index_;
    
    // Thread safety
    mutable std::mutex mutex_;
    
    // Helpers
    void rebuildPatternIndex();
    std::vector<PatternEntry> findSimilarPatterns(const PatternEntry& pattern);
    void mergePatterns(const std::string& keep_id, const std::string& merge_id);
    std::string categoryToString(PatternCategory cat);
    std::string categoryToString(StrategyCategory cat);
};

// =============================================================================
// Built-in Patterns (Default patterns for common issues)
// =============================================================================

/**
 * Get default patterns for a language
 */
std::vector<PatternEntry> getDefaultPatterns(const std::string& language);

/**
 * Get default strategies for a review type
 */
std::vector<StrategyEntry> getDefaultStrategies(const std::string& review_type);

} // namespace aipr::tms
