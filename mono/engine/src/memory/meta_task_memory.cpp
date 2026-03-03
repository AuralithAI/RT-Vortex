/**
 * AI PR Reviewer - Meta-Task Memory Implementation
 * 
 * Stores review strategies, patterns, and learned behaviors.
 */

#include "memory_system.h"
#include "faiss_index.h"
#include <algorithm>
#include <cmath>
#include <fstream>
#include <sstream>

namespace aipr {

// =============================================================================
// Constructor / Destructor
// =============================================================================

MetaTaskMemory::MetaTaskMemory(const MemoryConfig& config)
    : config_(config) {
}

MetaTaskMemory::~MetaTaskMemory() = default;

// =============================================================================
// Pattern Management
// =============================================================================

void MetaTaskMemory::storePattern(const PatternMemory& pattern) {
    std::lock_guard<std::mutex> lock(mutex_);
    patterns_[pattern.id] = pattern;
}

void MetaTaskMemory::updatePatternConfidence(const std::string& pattern_id, bool positive_feedback) {
    std::lock_guard<std::mutex> lock(mutex_);
    auto it = patterns_.find(pattern_id);
    if (it == patterns_.end()) return;

    auto& pattern = it->second;
    double alpha = 0.1;
    double outcome = positive_feedback ? 1.0 : 0.0;
    pattern.confidence = alpha * outcome + (1 - alpha) * pattern.confidence;
    pattern.occurrence_count++;
}

std::vector<PatternMemory> MetaTaskMemory::matchPatterns(
    const std::vector<float>& code_embedding,
    const std::string& language,
    int top_k
) {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<std::pair<std::string, double>> scored;

    for (const auto& [id, pattern] : patterns_) {
        double score = 0;

        // Language match (check metadata)
        if (!language.empty()) {
            auto lang_it = pattern.metadata.find("language");
            if (lang_it != pattern.metadata.end() && lang_it->second == language) {
                score += 0.3;
            }
        }

        // Embedding similarity (simple dot product if embeddings available)
        if (!code_embedding.empty() && !pattern.embedding.empty() &&
            code_embedding.size() == pattern.embedding.size()) {
            float dot = 0.0f;
            for (size_t i = 0; i < code_embedding.size(); ++i) {
                dot += code_embedding[i] * pattern.embedding[i];
            }
            score += 0.5 * std::max(0.0f, dot);
        }

        // Confidence boost
        score *= (0.5 + 0.5 * pattern.confidence);

        // Occurrence boost (log scale)
        if (pattern.occurrence_count > 0) {
            score *= (1.0 + 0.1 * std::log(pattern.occurrence_count));
        }

        scored.emplace_back(id, score);
    }

    // Sort by score descending
    std::sort(scored.begin(), scored.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });

    std::vector<PatternMemory> results;
    for (int i = 0; i < top_k && i < static_cast<int>(scored.size()); ++i) {
        if (scored[i].second > 0.1) {
            results.push_back(patterns_[scored[i].first]);
        }
    }
    return results;
}

std::vector<PatternMemory> MetaTaskMemory::getPatternsByType(const std::string& pattern_type) {
    std::lock_guard<std::mutex> lock(mutex_);
    std::vector<PatternMemory> results;
    for (const auto& [id, pattern] : patterns_) {
        if (pattern.pattern_type == pattern_type) {
            results.push_back(pattern);
        }
    }
    return results;
}

// =============================================================================
// Strategy Management
// =============================================================================

void MetaTaskMemory::storeStrategy(const StrategyMemory& strategy) {
    std::lock_guard<std::mutex> lock(mutex_);
    strategies_[strategy.id] = strategy;
}

void MetaTaskMemory::updateStrategyEffectiveness(const std::string& strategy_id, double effectiveness) {
    std::lock_guard<std::mutex> lock(mutex_);
    auto it = strategies_.find(strategy_id);
    if (it == strategies_.end()) return;

    auto& strategy = it->second;
    double alpha = 0.1;
    strategy.effectiveness_score = alpha * effectiveness + (1 - alpha) * strategy.effectiveness_score;
    strategy.use_count++;
}

std::vector<StrategyMemory> MetaTaskMemory::matchStrategies(
    const std::string& context_type,
    const std::vector<std::string>& detected_patterns,
    int top_k
) {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<std::pair<std::string, double>> scored;

    for (const auto& [id, strategy] : strategies_) {
        double score = 0;

        // Context type match
        if (!context_type.empty() && strategy.context_type == context_type) {
            score += 0.4;
        }

        // Pattern overlap
        for (const auto& pat_id : detected_patterns) {
            for (const auto& applicable : strategy.applicable_patterns) {
                if (applicable == pat_id) {
                    score += 0.15;
                    break;
                }
            }
        }

        // Effectiveness boost
        score *= (0.5 + 0.5 * strategy.effectiveness_score);

        scored.emplace_back(id, score);
    }

    std::sort(scored.begin(), scored.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });

    std::vector<StrategyMemory> results;
    for (int i = 0; i < top_k && i < static_cast<int>(scored.size()); ++i) {
        if (scored[i].second > 0.1) {
            results.push_back(strategies_[scored[i].first]);
        }
    }
    return results;
}

std::vector<StrategyMemory> MetaTaskMemory::getTopStrategies(int limit) {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<StrategyMemory> all;
    all.reserve(strategies_.size());
    for (const auto& [id, strategy] : strategies_) {
        all.push_back(strategy);
    }

    std::sort(all.begin(), all.end(),
              [](const auto& a, const auto& b) {
                  return a.effectiveness_score > b.effectiveness_score;
              });

    if (static_cast<int>(all.size()) > limit) {
        all.resize(limit);
    }
    return all;
}

// =============================================================================
// Meta-learning
// =============================================================================

void MetaTaskMemory::learnFromOutcome(
    const std::string& /*session_id*/,
    const std::vector<std::string>& used_pattern_ids,
    const std::vector<std::string>& used_strategy_ids,
    double outcome_score
) {
    std::lock_guard<std::mutex> lock(mutex_);

    for (const auto& pid : used_pattern_ids) {
        auto it = patterns_.find(pid);
        if (it != patterns_.end()) {
            auto& pattern = it->second;
            pattern.occurrence_count++;
            double alpha = 0.1;
            pattern.confidence = alpha * outcome_score + (1 - alpha) * pattern.confidence;
        }
    }

    for (const auto& sid : used_strategy_ids) {
        auto it = strategies_.find(sid);
        if (it != strategies_.end()) {
            auto& strategy = it->second;
            strategy.use_count++;
            double alpha = 0.1;
            strategy.effectiveness_score = alpha * outcome_score + (1 - alpha) * strategy.effectiveness_score;
        }
    }
}

size_t MetaTaskMemory::consolidatePatterns() {
    std::lock_guard<std::mutex> lock(mutex_);

    size_t removed = 0;
    auto it = patterns_.begin();
    while (it != patterns_.end()) {
        // Remove patterns with very low confidence and enough data
        if (it->second.occurrence_count >= 10 && it->second.confidence < 0.1) {
            it = patterns_.erase(it);
            removed++;
        } else {
            ++it;
        }
    }
    return removed;
}

// =============================================================================
// Persistence
// =============================================================================

void MetaTaskMemory::persist() {
    std::lock_guard<std::mutex> lock(mutex_);

    if (config_.storage_path.empty()) return;

    try {
        std::string path = config_.storage_path + "/mtm_data.bin";
        std::ofstream out(path, std::ios::binary);
        if (!out.is_open()) return;

        // Simple serialization: pattern count + strategies count
        size_t pattern_count = patterns_.size();
        size_t strategy_count = strategies_.size();
        out.write(reinterpret_cast<const char*>(&pattern_count), sizeof(pattern_count));
        out.write(reinterpret_cast<const char*>(&strategy_count), sizeof(strategy_count));

        for (const auto& [id, p] : patterns_) {
            size_t len = id.size();
            out.write(reinterpret_cast<const char*>(&len), sizeof(len));
            out.write(id.data(), len);
            out.write(reinterpret_cast<const char*>(&p.confidence), sizeof(p.confidence));
            out.write(reinterpret_cast<const char*>(&p.occurrence_count), sizeof(p.occurrence_count));
        }

        for (const auto& [id, s] : strategies_) {
            size_t len = id.size();
            out.write(reinterpret_cast<const char*>(&len), sizeof(len));
            out.write(id.data(), len);
            out.write(reinterpret_cast<const char*>(&s.effectiveness_score), sizeof(s.effectiveness_score));
            out.write(reinterpret_cast<const char*>(&s.use_count), sizeof(s.use_count));
        }
    } catch (...) {
        // Ignore persistence errors
    }
}

void MetaTaskMemory::load() {
    std::lock_guard<std::mutex> lock(mutex_);

    if (config_.storage_path.empty()) return;

    // TODO: implement deserialization matching persist()
}

// =============================================================================
// Private Helpers
// =============================================================================

void MetaTaskMemory::rebuildPatternIndex() {
    // TODO: rebuild FAISS index for pattern embeddings
}

} // namespace aipr
