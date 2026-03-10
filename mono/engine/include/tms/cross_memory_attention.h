/**
 * TMS Cross-Memory Attention Module
 * 
 * The core innovation of the brain-inspired RAG system.
 * Implements multi-head attention across LTM, STM, and MTM to create
 * deeply fused context that captures relationships between memories.
 * 
 * Key Difference from Simple Concatenation:
 * - Standard RAG: Concatenate retrieved chunks → LLM sees them as independent
 * - Cross-Memory Attention: Attention-weighted fusion → LLM sees unified context
 * 
 * Architecture:
 * ```
 *                   ┌────────────────────────────────────────────┐
 *                   │         Cross-Memory Attention             │
 *                   │                                            │
 *     Query ───────►│  ┌─────────┐  ┌─────────┐  ┌─────────┐     │
 *                   │  │ LTM KV  │  │ STM KV  │  │ MTM KV  │     │
 *                   │  │ Cache   │  │ Cache   │  │ Cache   │     │
 *                   │  └────┬────┘  └────┬────┘  └────┬────┘     │
 *                   │       │            │            │          │
 *                   │       └──────┬─────┴────────────┘          │
 *                   │              │                             │
 *                   │       ┌──────▼──────┐                      │
 *                   │       │ Multi-Head  │                      │
 *                   │       │  Attention  │                      │
 *                   │       └──────┬──────┘                      │
 *                   │              │                             │
 *                   │       ┌──────▼──────┐                      │
 *                   │       │   FFN +     │                      │
 *                   │       │ LayerNorm   │                      │
 *                   │       └──────┬──────┘                      │
 *                   │              │                             │
 *                   └──────────────┼─────────────────────────────┘
 *                                  ▼
 *                           Fused Output
 * ```
 * 
 * Features:
 * - Multi-head attention (8 heads default)
 * - Rotary position embeddings (RoPE)
 * - Memory-type-aware attention bias
 * - Attention weight output for explainability
 * - Efficient batched computation
 */

#pragma once

#include "tms_types.h"
#include <memory>
#include <vector>
#include <mutex>

namespace aipr::tms {

/**
 * Attention Configuration
 */
struct CrossMemoryAttentionConfig {
    // Architecture
    int num_heads = 8;
    int embed_dim = 1536;
    int head_dim = 64;                      // embed_dim / num_heads (if 0, computed)
    int ffn_dim = 4096;                     // Feed-forward hidden dim
    
    // Regularization
    double dropout = 0.1;
    double attention_dropout = 0.1;
    
    // Position encoding
    bool use_rotary_embedding = true;
    int max_sequence_length = 8192;
    double rope_theta = 10000.0;
    
    // Memory-specific biases
    double ltm_attention_bias = 0.0;        // Bias towards LTM items
    double stm_attention_bias = 0.1;        // Slight bias towards recent STM
    double mtm_attention_bias = 0.05;       // Slight bias towards patterns
    
    // Output control
    int max_output_tokens = 4096;           // Max tokens in fused context
    bool include_attention_weights = true;  // Include weights in output
    
    // Computation
    bool use_flash_attention = true;        // Use flash attention if available
    int batch_size = 1;                     // Batch size for attention
};

/**
 * Key-Value Cache for a Memory Type
 */
struct MemoryKVCache {
    std::vector<std::vector<float>> keys;   // [num_items, embed_dim]
    std::vector<std::vector<float>> values; // [num_items, embed_dim]
    std::vector<std::string> item_ids;      // Corresponding item IDs
    std::string memory_type;                // "LTM", "STM", "MTM"
};

/**
 * Attention Scores (for explainability)
 */
struct AttentionScores {
    // Per-head attention weights
    std::vector<std::vector<float>> ltm_per_head;   // [num_heads, ltm_items]
    std::vector<std::vector<float>> stm_per_head;   // [num_heads, stm_items]
    std::vector<std::vector<float>> mtm_per_head;   // [num_heads, mtm_items]
    
    // Averaged across heads
    std::vector<float> ltm_aggregated;      // [ltm_items]
    std::vector<float> stm_aggregated;      // [stm_items]
    std::vector<float> mtm_aggregated;      // [mtm_items]
    
    // Top attended items (for quick inspection)
    std::vector<std::pair<std::string, float>> top_ltm;  // id, score
    std::vector<std::pair<std::string, float>> top_stm;
    std::vector<std::pair<std::string, float>> top_mtm;
};

/**
 * Attention Output
 */
struct AttentionOutput {
    // Fused representation
    std::vector<float> fused_embedding;     // [embed_dim]
    std::string fused_context;              // Concatenated context string
    
    // Individual contributions
    std::vector<RetrievedChunk> attended_ltm;
    std::vector<RetrievedChunk> attended_stm;
    std::vector<PatternEntry> attended_patterns;
    std::vector<StrategyEntry> attended_strategies;
    
    // Attention scores
    AttentionScores scores;
    
    // Metrics
    double confidence;
    std::chrono::microseconds computation_time;
    size_t total_items_attended;
    size_t output_tokens;
};

/**
 * CrossMemoryAttention
 */
class CrossMemoryAttention {
public:
    explicit CrossMemoryAttention(const CrossMemoryAttentionConfig& config);
    ~CrossMemoryAttention();
    
    // Non-copyable
    CrossMemoryAttention(const CrossMemoryAttention&) = delete;
    CrossMemoryAttention& operator=(const CrossMemoryAttention&) = delete;
    
    // =========================================================================
    // Main Attention Interface
    // =========================================================================
    
    /**
     * Compute cross-memory attention
     * 
     * @param query Query embedding [embed_dim]
     * @param ltm_items Items from Long-Term Memory
     * @param stm_items Items from Short-Term Memory
     * @param mtm_patterns Patterns from Meta-Task Memory
     * @param mtm_strategies Strategies from Meta-Task Memory
     * @return AttentionOutput with fused context and scores
     */
    AttentionOutput attend(
        const std::vector<float>& query,
        const std::vector<RetrievedChunk>& ltm_items,
        const std::vector<RetrievedChunk>& stm_items,
        const std::vector<PatternEntry>& mtm_patterns,
        const std::vector<StrategyEntry>& mtm_strategies
    );
    
    /**
     * Attend with pre-built KV caches (more efficient for repeated queries)
     */
    AttentionOutput attend(
        const std::vector<float>& query,
        const MemoryKVCache& ltm_cache,
        const MemoryKVCache& stm_cache,
        const MemoryKVCache& mtm_cache
    );
    
    /**
     * Compute attention weights only (for analysis/debugging)
     */
    AttentionScores computeAttentionWeights(
        const std::vector<float>& query,
        const std::vector<std::vector<float>>& ltm_embeddings,
        const std::vector<std::vector<float>>& stm_embeddings,
        const std::vector<std::vector<float>>& mtm_embeddings
    );
    
    // =========================================================================
    // KV Cache Management
    // =========================================================================
    
    /**
     * Build KV cache from retrieved chunks
     */
    MemoryKVCache buildKVCache(
        const std::vector<RetrievedChunk>& items,
        const std::string& memory_type
    );
    
    /**
     * Build KV cache from patterns
     */
    MemoryKVCache buildKVCache(
        const std::vector<PatternEntry>& patterns,
        const std::string& memory_type
    );
    
    /**
     * Build KV cache from strategies
     */
    MemoryKVCache buildKVCache(
        const std::vector<StrategyEntry>& strategies,
        const std::string& memory_type
    );
    
    /**
     * Clear all caches
     */
    void clearCaches();
    
    // =========================================================================
    // Configuration
    // =========================================================================
    
    /**
     * Get configuration
     */
    const CrossMemoryAttentionConfig& getConfig() const { return config_; }
    
    /**
     * Update number of heads dynamically
     */
    void setNumHeads(int num_heads);
    
    /**
     * Set memory type biases
     */
    void setMemoryBiases(double ltm_bias, double stm_bias, double mtm_bias);
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    struct Stats {
        size_t total_attention_calls = 0;
        double avg_computation_time_us = 0.0;
        size_t avg_items_attended = 0;
        double avg_ltm_attention = 0.0;
        double avg_stm_attention = 0.0;
        double avg_mtm_attention = 0.0;
    };
    
    Stats getStats() const;
    void resetStats();

private:
    CrossMemoryAttentionConfig config_;
    
    // Projection matrices
    // Q, K, V projections for each head
    std::vector<std::vector<float>> w_q_;   // [embed_dim, num_heads * head_dim]
    std::vector<std::vector<float>> w_k_;   // [embed_dim, num_heads * head_dim]
    std::vector<std::vector<float>> w_v_;   // [embed_dim, num_heads * head_dim]
    std::vector<std::vector<float>> w_o_;   // [num_heads * head_dim, embed_dim]
    
    // FFN weights
    std::vector<std::vector<float>> ffn_w1_;  // [embed_dim, ffn_dim]
    std::vector<float> ffn_b1_;               // [ffn_dim]
    std::vector<std::vector<float>> ffn_w2_;  // [ffn_dim, embed_dim]
    std::vector<float> ffn_b2_;               // [embed_dim]
    
    // Layer norm parameters
    std::vector<float> ln1_gamma_;            // [embed_dim]
    std::vector<float> ln1_beta_;             // [embed_dim]
    std::vector<float> ln2_gamma_;            // [embed_dim]
    std::vector<float> ln2_beta_;             // [embed_dim]
    
    // Rotary embeddings cache
    std::vector<std::vector<float>> cos_cache_;  // [max_seq_len, head_dim/2]
    std::vector<std::vector<float>> sin_cache_;  // [max_seq_len, head_dim/2]
    
    // Statistics
    mutable Stats stats_;
    mutable std::mutex stats_mutex_;
    
    // =========================================================================
    // Internal Operations
    // =========================================================================
    
    void initializeWeights();
    void initializeRotaryCache();
    
    // Core attention computation
    std::vector<float> multiHeadAttention(
        const std::vector<float>& query,
        const std::vector<std::vector<float>>& keys,
        const std::vector<std::vector<float>>& values,
        const std::vector<float>& attention_bias,
        std::vector<std::vector<float>>* per_head_weights = nullptr
    );
    
    // Single head attention
    std::pair<std::vector<float>, std::vector<float>> singleHeadAttention(
        const std::vector<float>& q,        // [head_dim]
        const std::vector<std::vector<float>>& k,  // [num_items, head_dim]
        const std::vector<std::vector<float>>& v,  // [num_items, head_dim]
        const std::vector<float>& bias      // [num_items]
    );
    
    // Apply rotary embeddings
    std::vector<float> applyRotaryEmbedding(
        const std::vector<float>& x,
        int position
    );
    
    // FFN forward
    std::vector<float> ffnForward(const std::vector<float>& x);
    
    // Layer norm
    std::vector<float> layerNorm(
        const std::vector<float>& x,
        const std::vector<float>& gamma,
        const std::vector<float>& beta
    );
    
    // Softmax
    std::vector<float> softmax(const std::vector<float>& x);
    
    // Linear projection
    std::vector<float> linear(
        const std::vector<float>& x,
        const std::vector<std::vector<float>>& w,
        const std::vector<float>& b = {}
    );
    
    // Matrix operations
    float dotProduct(const std::vector<float>& a, const std::vector<float>& b);
    std::vector<float> addVectors(const std::vector<float>& a, const std::vector<float>& b);
    std::vector<float> scaleVector(const std::vector<float>& x, float scale);
    
    // Context building
    std::string buildFusedContext(
        const std::vector<RetrievedChunk>& ltm_items,
        const std::vector<RetrievedChunk>& stm_items,
        const std::vector<PatternEntry>& patterns,
        const std::vector<StrategyEntry>& strategies,
        const AttentionScores& scores
    );
    
    // Update statistics
    void updateStats(const AttentionOutput& output);
};

} // namespace aipr::tms
