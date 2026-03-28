/**
 * AI PR Reviewer - Cross-Memory Attention Implementation
 *
 * Multi-head attention across LTM, STM, MTM memory types.
 * Produces fused context with attention-weighted information.
 */

#include "memory_system.h"
#include <algorithm>
#include <cmath>
#include <numeric>
#include <random>
#include <sstream>
#include <set>

namespace aipr {

// =============================================================================
// Constructor / Destructor
// =============================================================================

CrossMemoryAttention::CrossMemoryAttention(const Config& config)
    : config_(config) {
    // Initialize projection matrices with random values (Xavier init)
    int head_dim = config_.embed_dim / config_.num_heads;
    if (head_dim <= 0) head_dim = 1;

    auto initMatrix = [&](std::vector<std::vector<float>>& mat, int rows, int cols) {
        std::mt19937 gen(42);
        float scale = std::sqrt(2.0f / (rows + cols));
        std::normal_distribution<float> dist(0.0f, scale);
        mat.resize(rows, std::vector<float>(cols, 0.0f));
        for (auto& row : mat) {
            for (auto& v : row) v = dist(gen);
        }
    };

    initMatrix(wq_, config_.embed_dim, config_.embed_dim);
    initMatrix(wk_, config_.embed_dim, config_.embed_dim);
    initMatrix(wv_, config_.embed_dim, config_.embed_dim);
    initMatrix(wo_, config_.embed_dim, config_.embed_dim);
}

CrossMemoryAttention::~CrossMemoryAttention() = default;

// =============================================================================
// Main Interface
// =============================================================================

MemoryRetrievalResult CrossMemoryAttention::attend(
    const std::vector<float>& query,
    const std::vector<CodeMemory>& ltm_items,
    const std::vector<SessionMemory>& stm_items,
    const std::vector<PatternMemory>& mtm_patterns,
    const std::vector<StrategyMemory>& mtm_strategies
) {
    MemoryRetrievalResult result;
    auto start = std::chrono::steady_clock::now();

    // Build key/value matrices from all memory items
    std::vector<std::vector<float>> keys;
    std::vector<std::vector<float>> values;

    // Track which items came from which source
    size_t ltm_count = 0, stm_count = 0, mtm_count = 0;

    // Add LTM embeddings
    for (const auto& item : ltm_items) {
        if (!item.embedding.empty()) {
            keys.push_back(item.embedding);
            values.push_back(item.embedding);
            ltm_count++;
        }
    }

    // Add STM embeddings
    for (const auto& item : stm_items) {
        if (!item.embedding.empty()) {
            keys.push_back(item.embedding);
            values.push_back(item.embedding);
            stm_count++;
        }
    }

    // Add MTM pattern embeddings
    for (const auto& pattern : mtm_patterns) {
        if (!pattern.embedding.empty()) {
            keys.push_back(pattern.embedding);
            values.push_back(pattern.embedding);
            mtm_count++;
        }
    }

    // If we have embeddings, compute multi-head attention
    if (!keys.empty() && !query.empty()) {
        auto fused = multiHeadAttention(query, keys, values);
        result.fused_embedding = fused;

        // Compute per-source attention scores
        // We use dot product between query and each key as a proxy
        std::vector<float> all_scores;
        for (const auto& key : keys) {
            all_scores.push_back(dotProduct(query, key));
        }
        auto attn_weights = softmax(all_scores);

        // Split attention weights by source
        size_t idx = 0;
        result.ltm_attention.resize(ltm_count, 0.0f);
        for (size_t i = 0; i < ltm_count && idx < attn_weights.size(); ++i, ++idx) {
            result.ltm_attention[i] = attn_weights[idx];
        }

        result.stm_attention.resize(stm_count, 0.0f);
        for (size_t i = 0; i < stm_count && idx < attn_weights.size(); ++i, ++idx) {
            result.stm_attention[i] = attn_weights[idx];
        }

        result.mtm_attention.resize(mtm_count, 0.0f);
        for (size_t i = 0; i < mtm_count && idx < attn_weights.size(); ++i, ++idx) {
            result.mtm_attention[i] = attn_weights[idx];
        }
    }

    // Copy items into result, sorted by attention weight
    // LTM items
    for (size_t i = 0; i < ltm_items.size(); ++i) {
        MemoryItem item;
        item.id = ltm_items[i].id;
        item.content = ltm_items[i].content;
        item.embedding = ltm_items[i].embedding;
        item.importance_score = ltm_items[i].importance_score;
        item.created_at = ltm_items[i].created_at;
        item.last_accessed = ltm_items[i].last_accessed;
        item.access_count = ltm_items[i].access_count;
        item.is_code = true;
        result.ltm_items.push_back(std::move(item));
    }

    // STM items
    for (size_t i = 0; i < stm_items.size(); ++i) {
        MemoryItem item;
        item.id = stm_items[i].id;
        item.content = stm_items[i].content;
        item.embedding = stm_items[i].embedding;
        item.importance_score = stm_items[i].importance_score;
        result.stm_items.push_back(std::move(item));
    }

    // MTM patterns
    result.mtm_patterns.insert(result.mtm_patterns.end(),
                                mtm_patterns.begin(), mtm_patterns.end());

    // MTM strategies
    result.mtm_strategies.insert(result.mtm_strategies.end(),
                                  mtm_strategies.begin(), mtm_strategies.end());

    // Build fused context string
    std::ostringstream context;
    if (!mtm_patterns.empty()) {
        context << "## Patterns\n";
        for (const auto& p : mtm_patterns) {
            context << "- " << p.rule_name << " (" << p.pattern_type << ")\n";
        }
        context << "\n";
    }

    if (!mtm_strategies.empty()) {
        context << "## Strategies\n";
        for (const auto& s : mtm_strategies) {
            context << "- " << s.strategy_type << " / " << s.context_type
                    << " (effectiveness: " << s.effectiveness_score << ")\n";
        }
        context << "\n";
    }

    if (!ltm_items.empty()) {
        context << "## Code Context\n";
        for (const auto& c : ltm_items) {
            context << "### " << c.file_path << " (lines "
                    << c.start_line << "-" << c.end_line << ")\n";
            context << "```" << c.language << "\n"
                    << c.content << "\n```\n\n";
        }
    }

    result.fused_context = context.str();

    // Compute confidence
    float total_attn = 0.0f;
    for (auto w : result.ltm_attention) total_attn += w;
    for (auto w : result.stm_attention) total_attn += w;
    for (auto w : result.mtm_attention) total_attn += w;
    result.retrieval_confidence = std::min(1.0, static_cast<double>(total_attn));

    auto end = std::chrono::steady_clock::now();
    result.retrieval_time = std::chrono::duration_cast<std::chrono::milliseconds>(end - start);

    return result;
}

CrossMemoryAttention::AttentionWeights CrossMemoryAttention::computeAttentionWeights(
    const std::vector<float>& query,
    const std::vector<MemoryItem>& ltm_items,
    const std::vector<MemoryItem>& stm_items,
    const std::vector<MemoryItem>& mtm_items
) {
    AttentionWeights weights;
    int num_heads = config_.num_heads;

    // For each head, compute attention scores
    auto computeHeadScores = [&](const std::vector<MemoryItem>& items) {
        std::vector<std::vector<float>> head_weights(num_heads);
        for (int h = 0; h < num_heads; ++h) {
            std::vector<float> scores;
            for (const auto& item : items) {
                if (!item.embedding.empty() && !query.empty()) {
                    scores.push_back(dotProduct(query, item.embedding));
                } else {
                    scores.push_back(0.0f);
                }
            }
            head_weights[h] = softmax(scores);
        }
        return head_weights;
    };

    weights.ltm_weights = computeHeadScores(ltm_items);
    weights.stm_weights = computeHeadScores(stm_items);
    weights.mtm_weights = computeHeadScores(mtm_items);

    // Aggregate across heads (average)
    auto aggregate = [&](const std::vector<std::vector<float>>& per_head,
                         size_t item_count) {
        std::vector<float> agg(item_count, 0.0f);
        if (per_head.empty()) return agg;
        for (const auto& head : per_head) {
            for (size_t i = 0; i < item_count && i < head.size(); ++i) {
                agg[i] += head[i];
            }
        }
        float scale = 1.0f / per_head.size();
        for (auto& v : agg) v *= scale;
        return agg;
    };

    weights.aggregated_ltm = aggregate(weights.ltm_weights, ltm_items.size());
    weights.aggregated_stm = aggregate(weights.stm_weights, stm_items.size());
    weights.aggregated_mtm = aggregate(weights.mtm_weights, mtm_items.size());

    return weights;
}

// =============================================================================
// Private Helpers
// =============================================================================

std::vector<float> CrossMemoryAttention::multiHeadAttention(
    const std::vector<float>& query,
    const std::vector<std::vector<float>>& keys,
    const std::vector<std::vector<float>>& values
) {
    if (keys.empty() || values.empty() || query.empty()) {
        return query;
    }

    int dim = static_cast<int>(query.size());
    int num_heads = config_.num_heads;
    int head_dim = dim / num_heads;
    if (head_dim <= 0) head_dim = 1;

    // Simplified multi-head attention:
    // For each head, compute attention scores and weighted sum
    std::vector<float> output(dim, 0.0f);

    for (int h = 0; h < num_heads; ++h) {
        int start = h * head_dim;
        int end = std::min(start + head_dim, dim);

        // Compute attention scores for this head
        std::vector<float> scores;
        scores.reserve(keys.size());
        for (const auto& key : keys) {
            float score = 0.0f;
            for (int d = start; d < end && d < static_cast<int>(key.size()); ++d) {
                score += query[d] * key[d];
            }
            score /= std::sqrt(static_cast<float>(head_dim));
            scores.push_back(score);
        }

        // Softmax
        auto attn = softmax(scores);

        // Weighted sum of values
        for (size_t i = 0; i < values.size(); ++i) {
            for (int d = start; d < end && d < static_cast<int>(values[i].size()); ++d) {
                output[d] += attn[i] * values[i][d];
            }
        }
    }

    return output;
}

std::vector<float> CrossMemoryAttention::softmax(const std::vector<float>& x) {
    if (x.empty()) return {};

    float max_val = *std::max_element(x.begin(), x.end());
    std::vector<float> result(x.size());
    float sum = 0.0f;

    for (size_t i = 0; i < x.size(); ++i) {
        result[i] = std::exp(x[i] - max_val);
        sum += result[i];
    }

    if (sum > 0.0f) {
        for (auto& v : result) v /= sum;
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

} // namespace aipr
