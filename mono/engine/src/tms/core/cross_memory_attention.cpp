/**
 * TMS Cross-Memory Attention Module Implementation
 * 
 * Multi-head attention across LTM, STM, and MTM for deep context fusion.
 */

#include "tms/cross_memory_attention.h"
#include <algorithm>
#include <cmath>
#include <numeric>
#include <sstream>
#include <random>
#include <iomanip>

namespace aipr::tms {

// =============================================================================
// Constructor / Destructor
// =============================================================================

CrossMemoryAttention::CrossMemoryAttention(const CrossMemoryAttentionConfig& config)
    : config_(config) {
    
    // Compute head_dim if not specified
    if (config_.head_dim == 0) {
        config_.head_dim = config_.embed_dim / config_.num_heads;
    }
    
    initializeWeights();
    initializeRotaryCache();
}

CrossMemoryAttention::~CrossMemoryAttention() = default;

// =============================================================================
// Main Attention Interface
// =============================================================================

AttentionOutput CrossMemoryAttention::attend(
    const std::vector<float>& query,
    const std::vector<RetrievedChunk>& ltm_items,
    const std::vector<RetrievedChunk>& stm_items,
    const std::vector<PatternEntry>& mtm_patterns,
    const std::vector<StrategyEntry>& mtm_strategies
) {
    auto start = std::chrono::steady_clock::now();
    
    AttentionOutput output;
    
    // Build KV caches
    // For LTM items, we use the embedding from chunk if available, 
    // otherwise synthesize from content
    std::vector<std::vector<float>> ltm_keys, ltm_values;
    std::vector<std::string> ltm_ids;
    
    for (const auto& item : ltm_items) {
        // Use chunk embedding directly as key
        std::vector<float> key(config_.embed_dim, 0.0f);
        
        // Simple encoding: hash content to create pseudo-embedding
        // In production, use actual embeddings stored with chunks
        std::hash<std::string> hasher;
        size_t hash = hasher(item.chunk.content);
        std::mt19937 gen(hash);
        std::normal_distribution<float> dist(0.0f, 1.0f);
        for (int i = 0; i < config_.embed_dim; ++i) {
            key[i] = dist(gen) * 0.1f;
        }
        
        ltm_keys.push_back(key);
        ltm_values.push_back(key);  // In full implementation, values could differ
        ltm_ids.push_back(item.chunk.id);
    }
    
    // Similar for STM
    std::vector<std::vector<float>> stm_keys, stm_values;
    std::vector<std::string> stm_ids;
    
    for (const auto& item : stm_items) {
        std::vector<float> key(config_.embed_dim, 0.0f);
        std::hash<std::string> hasher;
        size_t hash = hasher(item.chunk.content);
        std::mt19937 gen(hash);
        std::normal_distribution<float> dist(0.0f, 1.0f);
        for (int i = 0; i < config_.embed_dim; ++i) {
            key[i] = dist(gen) * 0.1f;
        }
        
        stm_keys.push_back(key);
        stm_values.push_back(key);
        stm_ids.push_back(item.chunk.id);
    }
    
    // MTM patterns and strategies
    std::vector<std::vector<float>> mtm_keys, mtm_values;
    std::vector<std::string> mtm_ids;
    
    for (const auto& pattern : mtm_patterns) {
        if (!pattern.embedding.empty()) {
            mtm_keys.push_back(pattern.embedding);
            mtm_values.push_back(pattern.embedding);
        } else {
            // Create pseudo-embedding
            std::vector<float> key(config_.embed_dim, 0.0f);
            std::hash<std::string> hasher;
            size_t hash = hasher(pattern.name + pattern.description);
            std::mt19937 gen(hash);
            std::normal_distribution<float> dist(0.0f, 1.0f);
            for (int i = 0; i < config_.embed_dim; ++i) {
                key[i] = dist(gen) * 0.1f;
            }
            mtm_keys.push_back(key);
            mtm_values.push_back(key);
        }
        mtm_ids.push_back(pattern.id);
    }
    
    for (const auto& strategy : mtm_strategies) {
        std::vector<float> key(config_.embed_dim, 0.0f);
        std::hash<std::string> hasher;
        size_t hash = hasher(strategy.name + strategy.description);
        std::mt19937 gen(hash);
        std::normal_distribution<float> dist(0.0f, 1.0f);
        for (int i = 0; i < config_.embed_dim; ++i) {
            key[i] = dist(gen) * 0.1f;
        }
        mtm_keys.push_back(key);
        mtm_values.push_back(key);
        mtm_ids.push_back(strategy.id);
    }
    
    // Compute attention weights
    output.scores = computeAttentionWeights(query, ltm_keys, stm_keys, mtm_keys);
    
    // Fuse using attention weights
    std::vector<float> fused(config_.embed_dim, 0.0f);
    
    // Add LTM contributions
    for (size_t i = 0; i < ltm_values.size() && i < output.scores.ltm_aggregated.size(); ++i) {
        float weight = output.scores.ltm_aggregated[i];
        for (int d = 0; d < config_.embed_dim; ++d) {
            fused[d] += weight * ltm_values[i][d];
        }
    }
    
    // Add STM contributions (with bias)
    for (size_t i = 0; i < stm_values.size() && i < output.scores.stm_aggregated.size(); ++i) {
        float weight = output.scores.stm_aggregated[i] * (1.0f + config_.stm_attention_bias);
        for (int d = 0; d < config_.embed_dim; ++d) {
            fused[d] += weight * stm_values[i][d];
        }
    }
    
    // Add MTM contributions
    for (size_t i = 0; i < mtm_values.size() && i < output.scores.mtm_aggregated.size(); ++i) {
        float weight = output.scores.mtm_aggregated[i];
        for (int d = 0; d < config_.embed_dim; ++d) {
            fused[d] += weight * mtm_values[i][d];
        }
    }
    
    // Normalize fused embedding
    float norm = 0.0f;
    for (float v : fused) norm += v * v;
    norm = std::sqrt(norm);
    if (norm > 0) {
        for (float& v : fused) v /= norm;
    }
    
    // Apply FFN + LayerNorm for final transformation
    auto ffn_out = ffnForward(fused);
    output.fused_embedding = layerNorm(addVectors(fused, ffn_out), ln2_gamma_, ln2_beta_);
    
    // Select top items based on attention
    std::vector<std::pair<size_t, float>> ltm_ranked;
    for (size_t i = 0; i < output.scores.ltm_aggregated.size(); ++i) {
        ltm_ranked.emplace_back(i, output.scores.ltm_aggregated[i]);
    }
    std::sort(ltm_ranked.begin(), ltm_ranked.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });
    
    for (size_t i = 0; i < std::min(ltm_ranked.size(), size_t(10)); ++i) {
        size_t idx = ltm_ranked[i].first;
        if (idx < ltm_items.size()) {
            RetrievedChunk chunk = ltm_items[idx];
            chunk.attention_weight = ltm_ranked[i].second;
            output.attended_ltm.push_back(chunk);
        }
    }
    
    // Same for STM
    std::vector<std::pair<size_t, float>> stm_ranked;
    for (size_t i = 0; i < output.scores.stm_aggregated.size(); ++i) {
        stm_ranked.emplace_back(i, output.scores.stm_aggregated[i]);
    }
    std::sort(stm_ranked.begin(), stm_ranked.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });
    
    for (size_t i = 0; i < std::min(stm_ranked.size(), size_t(5)); ++i) {
        size_t idx = stm_ranked[i].first;
        if (idx < stm_items.size()) {
            RetrievedChunk chunk = stm_items[idx];
            chunk.attention_weight = stm_ranked[i].second;
            output.attended_stm.push_back(chunk);
        }
    }
    
    // Patterns
    std::vector<std::pair<size_t, float>> mtm_ranked;
    for (size_t i = 0; i < output.scores.mtm_aggregated.size(); ++i) {
        mtm_ranked.emplace_back(i, output.scores.mtm_aggregated[i]);
    }
    std::sort(mtm_ranked.begin(), mtm_ranked.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });
    
    for (size_t i = 0; i < std::min(mtm_ranked.size(), mtm_patterns.size()); ++i) {
        size_t idx = mtm_ranked[i].first;
        if (idx < mtm_patterns.size()) {
            output.attended_patterns.push_back(mtm_patterns[idx]);
        }
    }
    
    // Strategies
    for (size_t i = mtm_patterns.size(); i < std::min(mtm_ranked.size(), mtm_patterns.size() + mtm_strategies.size()); ++i) {
        size_t idx = mtm_ranked[i].first - mtm_patterns.size();
        if (idx < mtm_strategies.size()) {
            output.attended_strategies.push_back(mtm_strategies[idx]);
        }
    }
    
    // Build fused context string
    output.fused_context = buildFusedContext(
        output.attended_ltm,
        output.attended_stm,
        output.attended_patterns,
        output.attended_strategies,
        output.scores
    );
    
    // Top attended for explainability
    for (size_t i = 0; i < std::min(ltm_ranked.size(), size_t(5)); ++i) {
        output.scores.top_ltm.emplace_back(ltm_ids[ltm_ranked[i].first], ltm_ranked[i].second);
    }
    for (size_t i = 0; i < std::min(stm_ranked.size(), size_t(3)); ++i) {
        output.scores.top_stm.emplace_back(stm_ids[stm_ranked[i].first], stm_ranked[i].second);
    }
    for (size_t i = 0; i < std::min(mtm_ranked.size(), size_t(3)); ++i) {
        output.scores.top_mtm.emplace_back(mtm_ids[mtm_ranked[i].first], mtm_ranked[i].second);
    }
    
    // Compute confidence
    float max_ltm = ltm_ranked.empty() ? 0.0f : ltm_ranked[0].second;
    float max_stm = stm_ranked.empty() ? 0.0f : stm_ranked[0].second;
    output.confidence = std::max(max_ltm, max_stm);
    
    // Timing
    auto end = std::chrono::steady_clock::now();
    output.computation_time = std::chrono::duration_cast<std::chrono::microseconds>(end - start);
    output.total_items_attended = ltm_items.size() + stm_items.size() + 
                                  mtm_patterns.size() + mtm_strategies.size();
    
    // Token estimation
    output.output_tokens = output.fused_context.length() / 4;
    
    // Update stats
    updateStats(output);
    
    return output;
}

AttentionOutput CrossMemoryAttention::attend(
    const std::vector<float>& query,
    const MemoryKVCache& ltm_cache,
    const MemoryKVCache& stm_cache,
    const MemoryKVCache& mtm_cache
) {
    // Convert caches to vectors and call main method
    std::vector<RetrievedChunk> ltm_items, stm_items;
    std::vector<PatternEntry> patterns;
    std::vector<StrategyEntry> strategies;
    
    // This is a simplified version - in production, fully utilize caches
    return attend(query, ltm_items, stm_items, patterns, strategies);
}

AttentionScores CrossMemoryAttention::computeAttentionWeights(
    const std::vector<float>& query,
    const std::vector<std::vector<float>>& ltm_embeddings,
    const std::vector<std::vector<float>>& stm_embeddings,
    const std::vector<std::vector<float>>& mtm_embeddings
) {
    AttentionScores scores;
    
    // Initialize per-head weights
    scores.ltm_per_head.resize(config_.num_heads);
    scores.stm_per_head.resize(config_.num_heads);
    scores.mtm_per_head.resize(config_.num_heads);
    
    // Project query
    auto q_projected = linear(query, w_q_);
    
    // Split into heads (must match config_.head_dim used for weight init)
    int head_dim = config_.head_dim;
    
    for (int h = 0; h < config_.num_heads; ++h) {
        // Extract this head's query slice
        std::vector<float> q_head(q_projected.begin() + h * head_dim,
                                   q_projected.begin() + (h + 1) * head_dim);
        
        // Apply rotary embedding
        if (config_.use_rotary_embedding) {
            q_head = applyRotaryEmbedding(q_head, 0);
        }
        
        // Compute attention for LTM
        std::vector<float> ltm_scores;
        for (const auto& emb : ltm_embeddings) {
            auto k_projected = linear(emb, w_k_);
            std::vector<float> k_head(k_projected.begin() + h * head_dim,
                                       k_projected.begin() + (h + 1) * head_dim);
            
            float score = dotProduct(q_head, k_head) / std::sqrt(static_cast<float>(head_dim));
            score += config_.ltm_attention_bias;
            ltm_scores.push_back(score);
        }
        scores.ltm_per_head[h] = softmax(ltm_scores);
        
        // Compute attention for STM
        std::vector<float> stm_scores;
        for (const auto& emb : stm_embeddings) {
            auto k_projected = linear(emb, w_k_);
            std::vector<float> k_head(k_projected.begin() + h * head_dim,
                                       k_projected.begin() + (h + 1) * head_dim);
            
            float score = dotProduct(q_head, k_head) / std::sqrt(static_cast<float>(head_dim));
            score += config_.stm_attention_bias;
            stm_scores.push_back(score);
        }
        scores.stm_per_head[h] = softmax(stm_scores);
        
        // Compute attention for MTM
        std::vector<float> mtm_scores;
        for (const auto& emb : mtm_embeddings) {
            auto k_projected = linear(emb, w_k_);
            std::vector<float> k_head(k_projected.begin() + h * head_dim,
                                       k_projected.begin() + (h + 1) * head_dim);
            
            float score = dotProduct(q_head, k_head) / std::sqrt(static_cast<float>(head_dim));
            score += config_.mtm_attention_bias;
            mtm_scores.push_back(score);
        }
        scores.mtm_per_head[h] = softmax(mtm_scores);
    }
    
    // Aggregate across heads
    if (!ltm_embeddings.empty()) {
        scores.ltm_aggregated.resize(ltm_embeddings.size(), 0.0f);
        for (int h = 0; h < config_.num_heads; ++h) {
            for (size_t i = 0; i < ltm_embeddings.size(); ++i) {
                scores.ltm_aggregated[i] += scores.ltm_per_head[h][i] / config_.num_heads;
            }
        }
    }
    
    if (!stm_embeddings.empty()) {
        scores.stm_aggregated.resize(stm_embeddings.size(), 0.0f);
        for (int h = 0; h < config_.num_heads; ++h) {
            for (size_t i = 0; i < stm_embeddings.size(); ++i) {
                scores.stm_aggregated[i] += scores.stm_per_head[h][i] / config_.num_heads;
            }
        }
    }
    
    if (!mtm_embeddings.empty()) {
        scores.mtm_aggregated.resize(mtm_embeddings.size(), 0.0f);
        for (int h = 0; h < config_.num_heads; ++h) {
            for (size_t i = 0; i < mtm_embeddings.size(); ++i) {
                scores.mtm_aggregated[i] += scores.mtm_per_head[h][i] / config_.num_heads;
            }
        }
    }
    
    return scores;
}

// =============================================================================
// KV Cache Management
// =============================================================================

MemoryKVCache CrossMemoryAttention::buildKVCache(
    const std::vector<RetrievedChunk>& items,
    const std::string& memory_type
) {
    MemoryKVCache cache;
    cache.memory_type = memory_type;
    
    for (const auto& item : items) {
        // Create embedding from content
        std::vector<float> key(config_.embed_dim, 0.0f);
        std::hash<std::string> hasher;
        size_t hash = hasher(item.chunk.content);
        std::mt19937 gen(hash);
        std::normal_distribution<float> dist(0.0f, 1.0f);
        for (int i = 0; i < config_.embed_dim; ++i) {
            key[i] = dist(gen) * 0.1f;
        }
        
        cache.keys.push_back(linear(key, w_k_));
        cache.values.push_back(linear(key, w_v_));
        cache.item_ids.push_back(item.chunk.id);
    }
    
    return cache;
}

MemoryKVCache CrossMemoryAttention::buildKVCache(
    const std::vector<PatternEntry>& patterns,
    const std::string& memory_type
) {
    MemoryKVCache cache;
    cache.memory_type = memory_type;
    
    for (const auto& pattern : patterns) {
        std::vector<float> key = pattern.embedding;
        if (key.empty() || key.size() != static_cast<size_t>(config_.embed_dim)) {
            key.resize(config_.embed_dim, 0.0f);
        }
        
        cache.keys.push_back(linear(key, w_k_));
        cache.values.push_back(linear(key, w_v_));
        cache.item_ids.push_back(pattern.id);
    }
    
    return cache;
}

MemoryKVCache CrossMemoryAttention::buildKVCache(
    const std::vector<StrategyEntry>& strategies,
    const std::string& memory_type
) {
    MemoryKVCache cache;
    cache.memory_type = memory_type;
    
    for (const auto& strategy : strategies) {
        std::vector<float> key(config_.embed_dim, 0.0f);
        std::hash<std::string> hasher;
        size_t hash = hasher(strategy.name + strategy.description);
        std::mt19937 gen(hash);
        std::normal_distribution<float> dist(0.0f, 1.0f);
        for (int i = 0; i < config_.embed_dim; ++i) {
            key[i] = dist(gen) * 0.1f;
        }
        
        cache.keys.push_back(linear(key, w_k_));
        cache.values.push_back(linear(key, w_v_));
        cache.item_ids.push_back(strategy.id);
    }
    
    return cache;
}

void CrossMemoryAttention::clearCaches() {
    // Clear any persistent caches if implemented
}

// =============================================================================
// Configuration
// =============================================================================

void CrossMemoryAttention::setNumHeads(int num_heads) {
    if (num_heads != config_.num_heads) {
        config_.num_heads = num_heads;
        config_.head_dim = config_.embed_dim / num_heads;
        // Note: In production, would need to reinitialize weights
    }
}

void CrossMemoryAttention::setMemoryBiases(double ltm_bias, double stm_bias, double mtm_bias) {
    config_.ltm_attention_bias = ltm_bias;
    config_.stm_attention_bias = stm_bias;
    config_.mtm_attention_bias = mtm_bias;
}

// =============================================================================
// Statistics
// =============================================================================

CrossMemoryAttention::Stats CrossMemoryAttention::getStats() const {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    return stats_;
}

void CrossMemoryAttention::resetStats() {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    stats_ = Stats{};
}

// =============================================================================
// Internal Operations
// =============================================================================

void CrossMemoryAttention::initializeWeights() {
    std::random_device rd;
    std::mt19937 gen(rd());
    float scale = 1.0f / std::sqrt(static_cast<float>(config_.embed_dim));
    std::normal_distribution<float> dist(0.0f, scale);
    
    int total_head_dim = config_.num_heads * config_.head_dim;
    
    // Q, K, V projections
    w_q_.resize(config_.embed_dim, std::vector<float>(total_head_dim));
    w_k_.resize(config_.embed_dim, std::vector<float>(total_head_dim));
    w_v_.resize(config_.embed_dim, std::vector<float>(total_head_dim));
    w_o_.resize(total_head_dim, std::vector<float>(config_.embed_dim));
    
    for (int i = 0; i < config_.embed_dim; ++i) {
        for (int j = 0; j < total_head_dim; ++j) {
            w_q_[i][j] = dist(gen);
            w_k_[i][j] = dist(gen);
            w_v_[i][j] = dist(gen);
        }
    }
    
    for (int i = 0; i < total_head_dim; ++i) {
        for (int j = 0; j < config_.embed_dim; ++j) {
            w_o_[i][j] = dist(gen);
        }
    }
    
    // FFN weights
    ffn_w1_.resize(config_.embed_dim, std::vector<float>(config_.ffn_dim));
    ffn_b1_.resize(config_.ffn_dim, 0.0f);
    ffn_w2_.resize(config_.ffn_dim, std::vector<float>(config_.embed_dim));
    ffn_b2_.resize(config_.embed_dim, 0.0f);
    
    float ffn_scale = 1.0f / std::sqrt(static_cast<float>(config_.ffn_dim));
    std::normal_distribution<float> ffn_dist(0.0f, ffn_scale);
    
    for (int i = 0; i < config_.embed_dim; ++i) {
        for (int j = 0; j < config_.ffn_dim; ++j) {
            ffn_w1_[i][j] = ffn_dist(gen);
        }
    }
    
    for (int i = 0; i < config_.ffn_dim; ++i) {
        for (int j = 0; j < config_.embed_dim; ++j) {
            ffn_w2_[i][j] = ffn_dist(gen);
        }
    }
    
    // Layer norm parameters
    ln1_gamma_.resize(config_.embed_dim, 1.0f);
    ln1_beta_.resize(config_.embed_dim, 0.0f);
    ln2_gamma_.resize(config_.embed_dim, 1.0f);
    ln2_beta_.resize(config_.embed_dim, 0.0f);
}

void CrossMemoryAttention::initializeRotaryCache() {
    if (!config_.use_rotary_embedding) return;
    
    int half_dim = config_.head_dim / 2;
    cos_cache_.resize(config_.max_sequence_length, std::vector<float>(half_dim));
    sin_cache_.resize(config_.max_sequence_length, std::vector<float>(half_dim));
    
    for (int pos = 0; pos < config_.max_sequence_length; ++pos) {
        for (int i = 0; i < half_dim; ++i) {
            float freq = 1.0f / std::pow(config_.rope_theta, 2.0f * i / config_.head_dim);
            float angle = pos * freq;
            cos_cache_[pos][i] = std::cos(angle);
            sin_cache_[pos][i] = std::sin(angle);
        }
    }
}

std::vector<float> CrossMemoryAttention::applyRotaryEmbedding(
    const std::vector<float>& x,
    int position
) {
    if (!config_.use_rotary_embedding || position >= config_.max_sequence_length) {
        return x;
    }
    
    std::vector<float> result(x.size());
    int half_dim = x.size() / 2;
    
    for (int i = 0; i < half_dim; ++i) {
        float cos_val = cos_cache_[position][i];
        float sin_val = sin_cache_[position][i];
        
        result[i] = x[i] * cos_val - x[i + half_dim] * sin_val;
        result[i + half_dim] = x[i] * sin_val + x[i + half_dim] * cos_val;
    }
    
    return result;
}

std::vector<float> CrossMemoryAttention::ffnForward(const std::vector<float>& x) {
    // Linear 1
    auto h = linear(x, ffn_w1_, ffn_b1_);
    
    // GELU activation
    for (float& v : h) {
        v = 0.5f * v * (1.0f + std::tanh(std::sqrt(2.0f / M_PI) * (v + 0.044715f * v * v * v)));
    }
    
    // Linear 2
    return linear(h, ffn_w2_, ffn_b2_);
}

std::vector<float> CrossMemoryAttention::layerNorm(
    const std::vector<float>& x,
    const std::vector<float>& gamma,
    const std::vector<float>& beta
) {
    // Compute mean
    float mean = 0.0f;
    for (float v : x) mean += v;
    mean /= x.size();
    
    // Compute variance
    float var = 0.0f;
    for (float v : x) var += (v - mean) * (v - mean);
    var /= x.size();
    
    // Normalize
    std::vector<float> result(x.size());
    float eps = 1e-5f;
    float denom = std::sqrt(var + eps);
    if (!std::isfinite(denom) || denom < eps) denom = 1.0f;
    for (size_t i = 0; i < x.size(); ++i) {
        float val = gamma[i] * (x[i] - mean) / denom + beta[i];
        result[i] = std::isfinite(val) ? val : 0.0f;
    }
    
    return result;
}

std::vector<float> CrossMemoryAttention::softmax(const std::vector<float>& x) {
    if (x.empty()) return {};
    
    std::vector<float> result(x.size());
    
    float max_val = *std::max_element(x.begin(), x.end());
    float sum = 0.0f;
    
    for (size_t i = 0; i < x.size(); ++i) {
        result[i] = std::exp(x[i] - max_val);
        sum += result[i];
    }
    
    if (sum < 1e-12f) {
        float uniform = 1.0f / static_cast<float>(x.size());
        std::fill(result.begin(), result.end(), uniform);
    } else {
        for (float& v : result) {
            v /= sum;
        }
    }
    
    return result;
}

std::vector<float> CrossMemoryAttention::linear(
    const std::vector<float>& x,
    const std::vector<std::vector<float>>& w,
    const std::vector<float>& b
) {
    if (w.empty() || x.size() != w.size()) {
        return std::vector<float>(w.empty() ? 0 : w[0].size(), 0.0f);
    }
    
    size_t out_dim = w[0].size();
    std::vector<float> result(out_dim, 0.0f);
    
    for (size_t j = 0; j < out_dim; ++j) {
        for (size_t i = 0; i < x.size(); ++i) {
            result[j] += x[i] * w[i][j];
        }
        if (!b.empty() && j < b.size()) {
            result[j] += b[j];
        }
    }
    
    return result;
}

float CrossMemoryAttention::dotProduct(const std::vector<float>& a, const std::vector<float>& b) {
    float result = 0.0f;
    size_t n = std::min(a.size(), b.size());
    for (size_t i = 0; i < n; ++i) {
        result += a[i] * b[i];
    }
    return result;
}

std::vector<float> CrossMemoryAttention::addVectors(const std::vector<float>& a, const std::vector<float>& b) {
    std::vector<float> result(std::max(a.size(), b.size()), 0.0f);
    for (size_t i = 0; i < a.size(); ++i) result[i] += a[i];
    for (size_t i = 0; i < b.size(); ++i) result[i] += b[i];
    return result;
}

std::vector<float> CrossMemoryAttention::scaleVector(const std::vector<float>& x, float scale) {
    std::vector<float> result(x.size());
    for (size_t i = 0; i < x.size(); ++i) {
        result[i] = x[i] * scale;
    }
    return result;
}

std::string CrossMemoryAttention::buildFusedContext(
    const std::vector<RetrievedChunk>& ltm_items,
    const std::vector<RetrievedChunk>& stm_items,
    const std::vector<PatternEntry>& patterns,
    const std::vector<StrategyEntry>& strategies,
    const AttentionScores& scores
) {
    std::ostringstream context;
    
    // Add relevant code from LTM
    if (!ltm_items.empty()) {
        context << "## Relevant Code Context\n\n";
        for (size_t i = 0; i < ltm_items.size(); ++i) {
            const auto& item = ltm_items[i];
            float weight = i < scores.ltm_aggregated.size() ? scores.ltm_aggregated[i] : 0.0f;
            
            context << "### " << item.chunk.file_path;
            if (!item.chunk.name.empty()) {
                context << " - " << item.chunk.name;
            }
            context << " (relevance: " << std::fixed << std::setprecision(2) << weight << ")\n";
            context << "```" << item.chunk.language << "\n";
            context << item.chunk.content << "\n";
            context << "```\n\n";
        }
    }
    
    // Add recent context from STM
    if (!stm_items.empty()) {
        context << "## Recent Session Context\n\n";
        for (const auto& item : stm_items) {
            context << "- " << item.chunk.content.substr(0, 200);
            if (item.chunk.content.length() > 200) context << "...";
            context << "\n";
        }
        context << "\n";
    }
    
    // Add applicable patterns
    if (!patterns.empty()) {
        context << "## Detected Patterns\n\n";
        for (const auto& pattern : patterns) {
            context << "- **" << pattern.name << "** (confidence: " 
                    << std::fixed << std::setprecision(2) << pattern.confidence << "): "
                    << pattern.description << "\n";
        }
        context << "\n";
    }
    
    // Add suggested strategies
    if (!strategies.empty()) {
        context << "## Suggested Review Approach\n\n";
        for (const auto& strategy : strategies) {
            context << "- **" << strategy.name << "**: " << strategy.description << "\n";
            if (!strategy.focus_areas.empty()) {
                context << "  Focus on: ";
                for (size_t i = 0; i < strategy.focus_areas.size(); ++i) {
                    if (i > 0) context << ", ";
                    context << strategy.focus_areas[i];
                }
                context << "\n";
            }
        }
    }
    
    return context.str();
}

void CrossMemoryAttention::updateStats(const AttentionOutput& output) {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    
    stats_.total_attention_calls++;
    
    double n = static_cast<double>(stats_.total_attention_calls);
    stats_.avg_computation_time_us = 
        (stats_.avg_computation_time_us * (n - 1) + output.computation_time.count()) / n;
    stats_.avg_items_attended = 
        static_cast<size_t>((stats_.avg_items_attended * (n - 1) + output.total_items_attended) / n);
    
    // Track attention distribution
    if (!output.scores.ltm_aggregated.empty()) {
        float sum = 0;
        for (float w : output.scores.ltm_aggregated) sum += w;
        stats_.avg_ltm_attention = (stats_.avg_ltm_attention * (n - 1) + sum) / n;
    }
    
    if (!output.scores.stm_aggregated.empty()) {
        float sum = 0;
        for (float w : output.scores.stm_aggregated) sum += w;
        stats_.avg_stm_attention = (stats_.avg_stm_attention * (n - 1) + sum) / n;
    }
    
    if (!output.scores.mtm_aggregated.empty()) {
        float sum = 0;
        for (float w : output.scores.mtm_aggregated) sum += w;
        stats_.avg_mtm_attention = (stats_.avg_mtm_attention * (n - 1) + sum) / n;
    }
}

} // namespace aipr::tms
