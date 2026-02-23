/**
 * AI PR Reviewer - Cross-Memory Attention Implementation
 * 
 * Integrates LTM, STM, and MTM through attention-weighted retrieval.
 */

#include "memory_system.h"
#include <algorithm>
#include <cmath>
#include <numeric>

namespace aipr {

class CrossMemoryAttentionImpl : public CrossMemoryAttention {
public:
    CrossMemoryAttentionImpl(
        std::shared_ptr<LongTermMemory> ltm,
        std::shared_ptr<ShortTermMemory> stm,
        std::shared_ptr<MetaTaskMemory> mtm,
        const MemoryConfig& config
    ) : ltm_(std::move(ltm))
      , stm_(std::move(stm))
      , mtm_(std::move(mtm))
      , config_(config) {
    }
    
    ~CrossMemoryAttentionImpl() override = default;
    
    AttentionResult query(
        const std::vector<float>& query_embedding,
        const std::string& query_text,
        const AttentionContext& context
    ) override {
        AttentionResult result;
        result.query_text = query_text;
        
        // Compute attention weights
        result.attention_weights = computeWeights(context);
        
        // Query each memory system in parallel conceptually
        // (In practice, sequential for now)
        
        // 1. Long-term memory - persistent knowledge
        if (result.attention_weights.ltm_weight > 0.05) {
            auto ltm_results = ltm_->search(
                query_embedding, 
                static_cast<int>(config_.attention_beam_width * result.attention_weights.ltm_weight)
            );
            
            for (const auto& entry : ltm_results) {
                MemoryResult mr;
                mr.source = MemorySource::LTM;
                mr.id = entry.id;
                mr.content = entry.content;
                mr.relevance_score = entry.relevance_score;
                mr.confidence = entry.confidence;
                mr.metadata = entry.metadata;
                result.memories.push_back(mr);
            }
        }
        
        // 2. Short-term memory - session context
        if (result.attention_weights.stm_weight > 0.05 && !context.session_id.empty()) {
            auto stm_results = stm_->retrieve(
                query_embedding,
                context.session_id,
                static_cast<int>(config_.attention_beam_width * result.attention_weights.stm_weight)
            );
            
            for (const auto& session_mem : stm_results) {
                MemoryResult mr;
                mr.source = MemorySource::STM;
                mr.id = session_mem.id;
                mr.content = session_mem.content;
                mr.relevance_score = 0.8;  // Session memory is always relevant
                mr.confidence = 0.9;
                mr.metadata["type"] = session_mem.memory_type;
                result.memories.push_back(mr);
            }
        }
        
        // 3. Meta-task memory - patterns and strategies
        if (result.attention_weights.mtm_weight > 0.05) {
            auto patterns = mtm_->matchPatterns(
                query_text,
                context.file_type,
                static_cast<int>(config_.attention_beam_width * result.attention_weights.mtm_weight)
            );
            
            for (const auto& pattern : patterns) {
                MemoryResult mr;
                mr.source = MemorySource::MTM;
                mr.id = pattern.id;
                mr.content = pattern.description;
                mr.relevance_score = pattern.effectiveness_score;
                mr.confidence = std::min(1.0, 0.5 + pattern.usage_count * 0.01);
                mr.metadata["pattern_name"] = pattern.name;
                mr.metadata["category"] = pattern.category;
                result.memories.push_back(mr);
            }
            
            // Also get relevant strategy
            auto strategy = mtm_->getStrategy(context.review_type, context.repository_type);
            if (strategy) {
                MemoryResult mr;
                mr.source = MemorySource::MTM;
                mr.id = strategy->id;
                mr.content = "Strategy: " + strategy->name;
                mr.relevance_score = strategy->success_rate;
                mr.confidence = 0.9;
                mr.metadata["strategy_steps"] = joinStrings(strategy->steps, " | ");
                result.memories.push_back(mr);
            }
        }
        
        // Apply cross-attention scoring
        applyAttentionScoring(result, query_embedding, context);
        
        // Sort by final score
        std::sort(result.memories.begin(), result.memories.end(),
            [](const MemoryResult& a, const MemoryResult& b) {
                return (a.relevance_score * a.confidence) > (b.relevance_score * b.confidence);
            });
        
        // Trim to max results
        if (result.memories.size() > static_cast<size_t>(config_.attention_beam_width)) {
            result.memories.resize(config_.attention_beam_width);
        }
        
        return result;
    }
    
    AttentionWeights computeWeights(const AttentionContext& context) override {
        AttentionWeights weights;
        
        // Base weights
        double ltm_base = 0.4;
        double stm_base = 0.35;
        double mtm_base = 0.25;
        
        // Adjust based on task type
        if (context.review_type == "security") {
            // Security reviews need more long-term knowledge
            ltm_base = 0.5;
            mtm_base = 0.3;
            stm_base = 0.2;
        } else if (context.review_type == "incremental") {
            // Incremental reviews focus on session context
            stm_base = 0.5;
            ltm_base = 0.3;
            mtm_base = 0.2;
        } else if (context.review_type == "architecture") {
            // Architecture reviews need patterns
            mtm_base = 0.4;
            ltm_base = 0.4;
            stm_base = 0.2;
        }
        
        // Adjust based on session depth
        if (!context.session_id.empty()) {
            auto history = stm_->getHistory(context.session_id);
            if (history.size() > 10) {
                // Deep session - increase STM weight
                stm_base += 0.1;
                ltm_base -= 0.05;
                mtm_base -= 0.05;
            }
        }
        
        // Normalize
        double total = ltm_base + stm_base + mtm_base;
        weights.ltm_weight = ltm_base / total;
        weights.stm_weight = stm_base / total;
        weights.mtm_weight = mtm_base / total;
        
        return weights;
    }
    
    std::vector<MemoryResult> fuse(
        const std::vector<MemoryResult>& ltm_results,
        const std::vector<MemoryResult>& stm_results,
        const std::vector<MemoryResult>& mtm_results,
        const AttentionWeights& weights
    ) override {
        std::vector<MemoryResult> fused;
        
        // Add weighted results
        for (auto result : ltm_results) {
            result.relevance_score *= weights.ltm_weight;
            fused.push_back(result);
        }
        
        for (auto result : stm_results) {
            result.relevance_score *= weights.stm_weight;
            fused.push_back(result);
        }
        
        for (auto result : mtm_results) {
            result.relevance_score *= weights.mtm_weight;
            fused.push_back(result);
        }
        
        // Deduplicate by content similarity
        deduplicateResults(fused);
        
        // Sort by score
        std::sort(fused.begin(), fused.end(),
            [](const MemoryResult& a, const MemoryResult& b) {
                return a.relevance_score > b.relevance_score;
            });
        
        return fused;
    }
    
    void recordAttention(
        const std::string& query_id,
        const AttentionResult& result,
        bool was_helpful
    ) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        AttentionHistory entry;
        entry.query_id = query_id;
        entry.weights = result.attention_weights;
        entry.memory_sources_used.clear();
        
        for (const auto& mem : result.memories) {
            entry.memory_sources_used.insert(mem.source);
            
            // Record outcome in individual memories
            if (mem.source == MemorySource::LTM && was_helpful) {
                ltm_->updateAccessStats(mem.id, true);
            } else if (mem.source == MemorySource::MTM) {
                mtm_->recordOutcome(mem.id, was_helpful, mem.confidence);
            }
        }
        
        entry.was_helpful = was_helpful;
        entry.timestamp = std::chrono::system_clock::now();
        
        attention_history_.push_back(entry);
        
        // Limit history size
        while (attention_history_.size() > 500) {
            attention_history_.erase(attention_history_.begin());
        }
        
        // Adapt weights based on feedback
        adaptWeights();
    }
    
    double getSourceReliability(MemorySource source) const override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        int helpful = 0, total = 0;
        
        for (const auto& entry : attention_history_) {
            if (entry.memory_sources_used.count(source) > 0) {
                total++;
                if (entry.was_helpful) helpful++;
            }
        }
        
        if (total < 10) {
            // Not enough data, return default
            switch (source) {
                case MemorySource::LTM: return 0.8;
                case MemorySource::STM: return 0.7;
                case MemorySource::MTM: return 0.75;
            }
        }
        
        return static_cast<double>(helpful) / total;
    }
    
private:
    void applyAttentionScoring(
        AttentionResult& result,
        const std::vector<float>& query_embedding,
        const AttentionContext& context
    ) {
        // Apply source reliability scores
        for (auto& mem : result.memories) {
            double reliability = getSourceReliability(mem.source);
            mem.confidence *= reliability;
        }
        
        // Boost memories matching current file type
        if (!context.file_type.empty()) {
            for (auto& mem : result.memories) {
                auto lang_it = mem.metadata.find("language");
                if (lang_it != mem.metadata.end() && lang_it->second == context.file_type) {
                    mem.relevance_score *= 1.2;
                }
            }
        }
        
        // Diversity penalty - reduce score of similar results
        for (size_t i = 0; i < result.memories.size(); ++i) {
            for (size_t j = i + 1; j < result.memories.size(); ++j) {
                double similarity = computeContentSimilarity(
                    result.memories[i].content,
                    result.memories[j].content
                );
                
                if (similarity > 0.8) {
                    // Very similar - penalize the lower-scored one
                    result.memories[j].relevance_score *= (1.0 - similarity * 0.5);
                }
            }
        }
    }
    
    void deduplicateResults(std::vector<MemoryResult>& results) {
        std::vector<MemoryResult> unique;
        
        for (const auto& result : results) {
            bool is_duplicate = false;
            
            for (const auto& existing : unique) {
                if (result.id == existing.id) {
                    is_duplicate = true;
                    break;
                }
                
                double sim = computeContentSimilarity(result.content, existing.content);
                if (sim > 0.9) {
                    is_duplicate = true;
                    break;
                }
            }
            
            if (!is_duplicate) {
                unique.push_back(result);
            }
        }
        
        results = std::move(unique);
    }
    
    double computeContentSimilarity(
        const std::string& a, 
        const std::string& b
    ) {
        // Simple Jaccard similarity on words
        std::set<std::string> words_a, words_b;
        
        std::istringstream iss_a(a), iss_b(b);
        std::string word;
        while (iss_a >> word) words_a.insert(word);
        while (iss_b >> word) words_b.insert(word);
        
        if (words_a.empty() || words_b.empty()) return 0;
        
        std::set<std::string> intersection;
        std::set_intersection(
            words_a.begin(), words_a.end(),
            words_b.begin(), words_b.end(),
            std::inserter(intersection, intersection.begin())
        );
        
        std::set<std::string> union_set;
        std::set_union(
            words_a.begin(), words_a.end(),
            words_b.begin(), words_b.end(),
            std::inserter(union_set, union_set.begin())
        );
        
        return static_cast<double>(intersection.size()) / union_set.size();
    }
    
    void adaptWeights() {
        // Analyze recent history to adapt base weights
        if (attention_history_.size() < 20) return;
        
        // Count helpful outcomes by source
        std::map<MemorySource, int> helpful_by_source;
        std::map<MemorySource, int> total_by_source;
        
        auto threshold = std::chrono::system_clock::now() - std::chrono::hours(24);
        
        for (const auto& entry : attention_history_) {
            if (entry.timestamp < threshold) continue;
            
            for (auto source : entry.memory_sources_used) {
                total_by_source[source]++;
                if (entry.was_helpful) {
                    helpful_by_source[source]++;
                }
            }
        }
        
        // Adjust adaptive weights (not implemented in base weights to keep them stable)
        // This information is used in getSourceReliability
    }
    
    std::string joinStrings(
        const std::vector<std::string>& strings, 
        const std::string& delimiter
    ) {
        std::string result;
        for (size_t i = 0; i < strings.size(); ++i) {
            if (i > 0) result += delimiter;
            result += strings[i];
        }
        return result;
    }
    
    std::shared_ptr<LongTermMemory> ltm_;
    std::shared_ptr<ShortTermMemory> stm_;
    std::shared_ptr<MetaTaskMemory> mtm_;
    MemoryConfig config_;
    
    struct AttentionHistory {
        std::string query_id;
        AttentionWeights weights;
        std::set<MemorySource> memory_sources_used;
        bool was_helpful;
        std::chrono::system_clock::time_point timestamp;
    };
    
    mutable std::mutex mutex_;
    std::vector<AttentionHistory> attention_history_;
};

std::unique_ptr<CrossMemoryAttention> createCrossMemoryAttention(
    std::shared_ptr<LongTermMemory> ltm,
    std::shared_ptr<ShortTermMemory> stm,
    std::shared_ptr<MetaTaskMemory> mtm,
    const MemoryConfig& config
) {
    return std::make_unique<CrossMemoryAttentionImpl>(
        std::move(ltm), std::move(stm), std::move(mtm), config
    );
}

} // namespace aipr
