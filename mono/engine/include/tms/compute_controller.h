/**
 * TMS Compute Controller
 * 
 * Intelligent decision-making module that determines the optimal retrieval
 * strategy based on query complexity, available resources, and learned patterns.
 * 
 * Strategies:
 * - FAST: Quick retrieval for simple queries (< 100ms)
 *   - LTM: 5 items, STM: 5 items, MTM: 3 items
 *   - No cross-memory attention
 *   
 * - BALANCED: Standard retrieval for most queries (< 500ms)
 *   - LTM: 12 items, STM: 8 items, MTM: 5 items
 *   - 4-head cross-memory attention
 *   
 * - THOROUGH: Deep retrieval for complex queries (< 2000ms)
 *   - LTM: 25 items, STM: 15 items, MTM: 10 items
 *   - 8-head cross-memory attention with full fusion
 * 
 * Decision Factors:
 * - Query complexity (length, number of concepts)
 * - Available VRAM
 * - Historical query patterns
 * - Session context richness
 * - Repository size
 */

#pragma once

#include "tms_types.h"
#include <memory>
#include <vector>
#include <chrono>

namespace aipr::tms {

/**
 * Query Complexity Features
 */
struct QueryFeatures {
    size_t query_length;                    // Character count
    size_t estimated_tokens;                // Estimated token count
    int concept_count;                      // Number of distinct concepts
    bool has_code_reference;                // Contains code snippets
    bool is_multi_file;                     // Asks about multiple files
    bool is_architectural;                  // Asks about architecture
    double specificity;                     // How specific (0=vague, 1=very specific)
    std::vector<std::string> detected_intents;  // "explain", "review", "find", etc.
};

/**
 * System Resource State
 */
struct ResourceState {
    float available_vram_gb;
    float available_ram_gb;
    float cpu_load;                         // 0-1 scale
    int active_sessions;
    size_t ltm_size;                        // Number of items in LTM
    double avg_recent_query_time_ms;        // Average of recent queries
};

/**
 * Controller Configuration
 */
struct ControllerConfig {
    // Resource budgets
    float vram_budget_gb = 4.0;
    float ram_budget_gb = 16.0;
    
    // Strategy thresholds
    double fast_threshold = 0.3;            // Below this = FAST
    double balanced_threshold = 0.7;        // Below this = BALANCED, above = THOROUGH
    
    // Default top-k values per strategy
    struct StrategyDefaults {
        int ltm_top_k;
        int stm_top_k;
        int mtm_top_k;
        bool enable_attention;
        int attention_heads;
    };
    
    StrategyDefaults fast_defaults{5, 5, 3, false, 0};
    StrategyDefaults balanced_defaults{12, 8, 5, true, 4};
    StrategyDefaults thorough_defaults{25, 15, 10, true, 8};
    
    // Adaptive learning
    bool enable_adaptive = true;
    double learning_rate = 0.01;
    
    // Heuristic weights
    double weight_query_complexity = 0.3;
    double weight_resource_state = 0.2;
    double weight_historical = 0.2;
    double weight_session_context = 0.15;
    double weight_repo_size = 0.15;
    
    // Time limits (ms)
    int fast_time_limit_ms = 100;
    int balanced_time_limit_ms = 500;
    int thorough_time_limit_ms = 2000;
};

/**
 * Decision Explanation
 */
struct DecisionExplanation {
    ComputeStrategy recommended_strategy;
    double confidence;                      // How confident in the decision
    
    // Feature contributions
    double query_complexity_score;          // 0-1 contribution from query
    double resource_score;                  // 0-1 contribution from resources
    double historical_score;                // 0-1 contribution from history
    double session_score;                   // 0-1 contribution from session
    double repo_score;                      // 0-1 contribution from repo size
    
    // Combined score
    double combined_score;                  // Weighted combination
    
    // Human-readable reasoning
    std::string reasoning;
};

/**
 * Historical Query Record (for learning)
 */
struct QueryRecord {
    std::string query_hash;
    ComputeStrategy used_strategy;
    double actual_time_ms;
    double outcome_score;                   // 0=poor, 1=excellent
    QueryFeatures features;
    std::chrono::system_clock::time_point timestamp;
};

/**
 * ComputeController
 */
class ComputeController {
public:
    explicit ComputeController(const ControllerConfig& config);
    ~ComputeController();
    
    // Non-copyable
    ComputeController(const ComputeController&) = delete;
    ComputeController& operator=(const ComputeController&) = delete;
    
    // =========================================================================
    // Main Decision Interface
    // =========================================================================
    
    /**
     * Decide the optimal strategy for a query
     * 
     * @param query_embedding Query embedding vector
     * @param resource_state Current system resources
     * @return ComputeDecision with strategy and parameters
     */
    ComputeDecision decide(
        const std::vector<float>& query_embedding,
        const ResourceState& resource_state
    );
    
    /**
     * Decide with full query features
     */
    ComputeDecision decide(
        const QueryFeatures& features,
        const ResourceState& resource_state
    );
    
    /**
     * Quick decision based on query text only
     */
    ComputeDecision decideSimple(
        const std::string& query_text,
        float available_vram_gb
    );
    
    /**
     * Force a specific strategy
     */
    ComputeDecision forceStrategy(ComputeStrategy strategy);
    
    /**
     * Get explanation for a decision
     */
    DecisionExplanation explain(
        const QueryFeatures& features,
        const ResourceState& resource_state
    );
    
    // =========================================================================
    // Feature Extraction
    // =========================================================================
    
    /**
     * Extract features from query text
     */
    QueryFeatures extractFeatures(const std::string& query_text);
    
    /**
     * Extract features from query embedding
     */
    QueryFeatures extractFeatures(const std::vector<float>& query_embedding);
    
    /**
     * Get current resource state
     */
    ResourceState getResourceState();
    
    // =========================================================================
    // Learning & Adaptation
    // =========================================================================
    
    /**
     * Record query outcome for learning
     */
    void recordOutcome(
        const std::string& query_text,
        ComputeStrategy used_strategy,
        double actual_time_ms,
        double outcome_score
    );
    
    /**
     * Update decision model based on recent outcomes
     */
    void updateModel();
    
    /**
     * Get learned adjustments
     */
    struct LearnedAdjustments {
        double fast_threshold_adjustment;
        double balanced_threshold_adjustment;
        std::map<std::string, double> intent_to_strategy_bias;
    };
    
    LearnedAdjustments getLearnedAdjustments() const;
    
    // =========================================================================
    // Session Context
    // =========================================================================
    
    /**
     * Set session context (affects decisions)
     */
    void setSessionContext(
        const std::string& session_id,
        size_t context_richness,            // How much context is available
        size_t recent_query_count
    );
    
    /**
     * Set repository context
     */
    void setRepoContext(
        const std::string& repo_id,
        size_t chunk_count
    );
    
    // =========================================================================
    // Configuration
    // =========================================================================
    
    /**
     * Get current config
     */
    const ControllerConfig& getConfig() const { return config_; }
    
    /**
     * Update config
     */
    void updateConfig(const ControllerConfig& config);
    
    /**
     * Set VRAM budget
     */
    void setVRAMBudget(float gb);
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    struct Stats {
        size_t total_decisions = 0;
        size_t fast_decisions = 0;
        size_t balanced_decisions = 0;
        size_t thorough_decisions = 0;
        double avg_decision_time_us = 0.0;
        double avg_outcome_score = 0.0;
    };
    
    Stats getStats() const;
    
    /**
     * Reset statistics
     */
    void resetStats();

private:
    ControllerConfig config_;
    
    // Historical records for learning
    std::vector<QueryRecord> history_;
    static constexpr size_t MAX_HISTORY = 10000;
    
    // Learned parameters
    double learned_fast_threshold_ = 0.0;
    double learned_balanced_threshold_ = 0.0;
    std::unordered_map<std::string, double> intent_bias_;
    
    // Session/repo context
    struct SessionContext {
        size_t context_richness = 0;
        size_t recent_query_count = 0;
    };
    std::unordered_map<std::string, SessionContext> session_contexts_;
    
    struct RepoContext {
        size_t chunk_count = 0;
    };
    std::unordered_map<std::string, RepoContext> repo_contexts_;
    
    // Statistics
    Stats stats_;
    mutable std::mutex stats_mutex_;
    
    // MLP weights (simplified - use Eigen for production)
    // Input: [query_features (8), resource_features (6)] = 14 dims
    // Hidden: 32 dims
    // Output: 3 dims (FAST, BALANCED, THOROUGH scores)
    std::vector<std::vector<float>> mlp_w1_;  // 14 x 32
    std::vector<float> mlp_b1_;               // 32
    std::vector<std::vector<float>> mlp_w2_;  // 32 x 3
    std::vector<float> mlp_b2_;               // 3
    bool mlp_initialized_ = false;
    
    // Helpers
    void initializeMLP();
    std::vector<float> mlpForward(const std::vector<float>& input);
    double computeQueryComplexity(const QueryFeatures& features);
    double computeResourceScore(const ResourceState& state);
    double computeHistoricalScore(const QueryFeatures& features);
    std::vector<std::string> detectIntents(const std::string& query_text);
    ComputeDecision buildDecision(ComputeStrategy strategy, const std::string& reasoning);
};

} // namespace aipr::tms
