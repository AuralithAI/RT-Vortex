/**
 * TMS Meta-Task Memory (MTM) - Graph Implementation
 * 
 * Stores learned patterns, strategies, and meta-knowledge.
 */

#include "tms/mtm_graph.h"
#include <algorithm>
#include <cmath>
#include <fstream>
#include <sstream>
#include <filesystem>
#include <queue>
#include <numeric>

namespace aipr::tms {

// =============================================================================
// Pattern Index (FAISS-backed for fast matching)
// =============================================================================

class MTMGraph::PatternIndex {
public:
    explicit PatternIndex(size_t dimension) : dimension_(dimension) {}
    
    void add(const std::string& pattern_id, const std::vector<float>& embedding) {
        if (embedding.size() != dimension_) return;
        
        embeddings_[pattern_id] = embedding;
        pattern_ids_.push_back(pattern_id);
    }
    
    void remove(const std::string& pattern_id) {
        embeddings_.erase(pattern_id);
        pattern_ids_.erase(
            std::remove(pattern_ids_.begin(), pattern_ids_.end(), pattern_id),
            pattern_ids_.end()
        );
    }
    
    std::vector<std::pair<std::string, float>> search(
        const std::vector<float>& query,
        int top_k
    ) {
        std::vector<std::pair<std::string, float>> results;
        
        for (const auto& [id, emb] : embeddings_) {
            float sim = cosineSimilarity(query, emb);
            results.emplace_back(id, sim);
        }
        
        std::partial_sort(
            results.begin(),
            results.begin() + std::min(static_cast<size_t>(top_k), results.size()),
            results.end(),
            [](const auto& a, const auto& b) { return a.second > b.second; }
        );
        
        if (results.size() > static_cast<size_t>(top_k)) {
            results.resize(top_k);
        }
        
        return results;
    }
    
    void clear() {
        embeddings_.clear();
        pattern_ids_.clear();
    }
    
private:
    size_t dimension_;
    std::unordered_map<std::string, std::vector<float>> embeddings_;
    std::vector<std::string> pattern_ids_;
    
    float cosineSimilarity(const std::vector<float>& a, const std::vector<float>& b) {
        if (a.size() != b.size() || a.empty()) return 0.0f;
        
        float dot = 0.0f, norm_a = 0.0f, norm_b = 0.0f;
        for (size_t i = 0; i < a.size(); ++i) {
            dot += a[i] * b[i];
            norm_a += a[i] * a[i];
            norm_b += b[i] * b[i];
        }
        
        float denom = std::sqrt(norm_a) * std::sqrt(norm_b);
        return denom > 0 ? dot / denom : 0.0f;
    }
};

// =============================================================================
// Constructor / Destructor
// =============================================================================

MTMGraph::MTMGraph(const MTMConfig& config)
    : config_(config)
    , pattern_index_(std::make_unique<PatternIndex>(config.embedding_dimension)) {
}

MTMGraph::~MTMGraph() = default;

// =============================================================================
// Pattern Management
// =============================================================================

void MTMGraph::storePattern(const PatternEntry& pattern) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Check capacity
    if (patterns_.size() >= config_.max_patterns && !patterns_.count(pattern.id)) {
        // Remove lowest confidence pattern
        auto lowest = patterns_.begin();
        for (auto it = patterns_.begin(); it != patterns_.end(); ++it) {
            if (it->second.confidence < lowest->second.confidence) {
                lowest = it;
            }
        }
        
        pattern_index_->remove(lowest->first);
        type_to_patterns_[lowest->second.pattern_type].erase(lowest->first);
        for (const auto& lang : lowest->second.applicable_languages) {
            language_to_patterns_[lang].erase(lowest->first);
        }
        patterns_.erase(lowest);
    }
    
    // Store pattern
    patterns_[pattern.id] = pattern;
    
    // Update indexes
    type_to_patterns_[pattern.pattern_type].insert(pattern.id);
    for (const auto& lang : pattern.applicable_languages) {
        language_to_patterns_[lang].insert(pattern.id);
    }
    
    // Add to embedding index
    if (!pattern.embedding.empty()) {
        pattern_index_->add(pattern.id, pattern.embedding);
    }
}

std::optional<PatternEntry> MTMGraph::getPattern(const std::string& pattern_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = patterns_.find(pattern_id);
    if (it != patterns_.end()) {
        return it->second;
    }
    return std::nullopt;
}

std::vector<PatternEntry> MTMGraph::matchPatterns(
    const std::vector<float>& code_embedding,
    int top_k,
    double min_confidence
) {
    if (top_k <= 0) top_k = config_.pattern_search_k;
    if (min_confidence < 0) min_confidence = config_.confidence_threshold;
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Search by embedding similarity
    auto matches = pattern_index_->search(code_embedding, top_k * 2);  // Over-fetch for filtering
    
    std::vector<PatternEntry> results;
    for (const auto& [id, score] : matches) {
        auto it = patterns_.find(id);
        if (it != patterns_.end() && it->second.confidence >= min_confidence) {
            results.push_back(it->second);
            if (results.size() >= static_cast<size_t>(top_k)) break;
        }
    }
    
    return results;
}

std::vector<PatternEntry> MTMGraph::matchPatternsForLanguage(
    const std::vector<float>& code_embedding,
    const std::string& language,
    int top_k
) {
    if (top_k <= 0) top_k = config_.pattern_search_k;
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Get patterns for this language
    auto lang_it = language_to_patterns_.find(language);
    if (lang_it == language_to_patterns_.end()) {
        return {};
    }
    
    // Search and filter by language
    auto matches = pattern_index_->search(code_embedding, top_k * 3);
    
    std::vector<PatternEntry> results;
    for (const auto& [id, score] : matches) {
        if (!lang_it->second.count(id)) continue;
        
        auto it = patterns_.find(id);
        if (it != patterns_.end()) {
            results.push_back(it->second);
            if (results.size() >= static_cast<size_t>(top_k)) break;
        }
    }
    
    return results;
}

std::vector<PatternEntry> MTMGraph::getPatternsByCategory(PatternCategory category) {
    return getPatternsByType(categoryToString(category));
}

std::vector<PatternEntry> MTMGraph::getPatternsByType(const std::string& pattern_type) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<PatternEntry> results;
    
    auto it = type_to_patterns_.find(pattern_type);
    if (it != type_to_patterns_.end()) {
        for (const auto& id : it->second) {
            if (patterns_.count(id)) {
                results.push_back(patterns_[id]);
            }
        }
    }
    
    // Sort by confidence
    std::sort(results.begin(), results.end(),
              [](const auto& a, const auto& b) { return a.confidence > b.confidence; });
    
    return results;
}

void MTMGraph::updatePatternConfidence(const std::string& pattern_id, bool was_helpful) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = patterns_.find(pattern_id);
    if (it == patterns_.end()) return;
    
    // Bayesian-style update
    if (was_helpful) {
        it->second.true_positive_count++;
        it->second.confidence = std::min(1.0, 
            it->second.confidence + config_.learning_rate * (1.0 - it->second.confidence));
    } else {
        it->second.false_positive_count++;
        it->second.confidence = std::max(0.0,
            it->second.confidence - config_.learning_rate * it->second.confidence);
    }
    
    it->second.metadata.last_accessed = std::chrono::system_clock::now();
}

void MTMGraph::recordPatternOccurrence(
    const std::string& pattern_id,
    const std::string& chunk_id
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = patterns_.find(pattern_id);
    if (it != patterns_.end()) {
        it->second.occurrence_count++;
        
        // Add example if not too many
        if (it->second.example_chunk_ids.size() < 10) {
            it->second.example_chunk_ids.push_back(chunk_id);
        }
    }
}

bool MTMGraph::deletePattern(const std::string& pattern_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = patterns_.find(pattern_id);
    if (it == patterns_.end()) return false;
    
    // Remove from indexes
    pattern_index_->remove(pattern_id);
    type_to_patterns_[it->second.pattern_type].erase(pattern_id);
    for (const auto& lang : it->second.applicable_languages) {
        language_to_patterns_[lang].erase(pattern_id);
    }
    
    // Remove edges
    out_edges_.erase(pattern_id);
    in_edges_.erase(pattern_id);
    
    // Remove from other nodes' edges
    for (auto& [_, edges] : out_edges_) {
        edges.erase(
            std::remove_if(edges.begin(), edges.end(),
                          [&](const MTMEdge& e) { return e.target_id == pattern_id; }),
            edges.end()
        );
    }
    for (auto& [_, edges] : in_edges_) {
        edges.erase(
            std::remove_if(edges.begin(), edges.end(),
                          [&](const MTMEdge& e) { return e.source_id == pattern_id; }),
            edges.end()
        );
    }
    
    patterns_.erase(it);
    return true;
}

// =============================================================================
// Strategy Management
// =============================================================================

void MTMGraph::storeStrategy(const StrategyEntry& strategy) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Check capacity
    if (strategies_.size() >= config_.max_strategies && !strategies_.count(strategy.id)) {
        // Remove lowest effectiveness strategy
        auto lowest = strategies_.begin();
        for (auto it = strategies_.begin(); it != strategies_.end(); ++it) {
            if (it->second.effectiveness_score < lowest->second.effectiveness_score) {
                lowest = it;
            }
        }
        
        type_to_strategies_[lowest->second.strategy_type].erase(lowest->first);
        strategies_.erase(lowest);
    }
    
    // Store strategy
    strategies_[strategy.id] = strategy;
    
    // Update indexes
    type_to_strategies_[strategy.strategy_type].insert(strategy.id);
}

std::optional<StrategyEntry> MTMGraph::getStrategy(const std::string& strategy_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = strategies_.find(strategy_id);
    if (it != strategies_.end()) {
        return it->second;
    }
    return std::nullopt;
}

std::vector<StrategyEntry> MTMGraph::matchStrategies(
    const std::string& context_type,
    const std::vector<std::string>& detected_pattern_ids,
    int top_k
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<std::pair<StrategyEntry*, double>> scored;
    
    for (auto& [id, strategy] : strategies_) {
        // Score based on context match and pattern applicability
        double score = 0.0;
        
        if (strategy.context_type == context_type) {
            score += 0.5;
        }
        
        // Check pattern overlap
        for (const auto& pattern_id : detected_pattern_ids) {
            if (std::find(strategy.applicable_pattern_ids.begin(),
                         strategy.applicable_pattern_ids.end(),
                         pattern_id) != strategy.applicable_pattern_ids.end()) {
                score += 0.3;
            }
        }
        
        // Weight by effectiveness
        score *= strategy.effectiveness_score;
        
        if (score > 0) {
            scored.emplace_back(&strategy, score);
        }
    }
    
    // Sort by score
    std::partial_sort(
        scored.begin(),
        scored.begin() + std::min(static_cast<size_t>(top_k), scored.size()),
        scored.end(),
        [](const auto& a, const auto& b) { return a.second > b.second; }
    );
    
    std::vector<StrategyEntry> results;
    for (size_t i = 0; i < std::min(static_cast<size_t>(top_k), scored.size()); ++i) {
        results.push_back(*scored[i].first);
    }
    
    return results;
}

std::vector<StrategyEntry> MTMGraph::getStrategiesByCategory(StrategyCategory category) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::string type = categoryToString(category);
    std::vector<StrategyEntry> results;
    
    auto it = type_to_strategies_.find(type);
    if (it != type_to_strategies_.end()) {
        for (const auto& id : it->second) {
            if (strategies_.count(id)) {
                results.push_back(strategies_[id]);
            }
        }
    }
    
    return results;
}

std::vector<StrategyEntry> MTMGraph::getTopStrategies(int limit) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<StrategyEntry> all;
    all.reserve(strategies_.size());
    
    for (const auto& [_, strategy] : strategies_) {
        all.push_back(strategy);
    }
    
    std::partial_sort(
        all.begin(),
        all.begin() + std::min(static_cast<size_t>(limit), all.size()),
        all.end(),
        [](const auto& a, const auto& b) {
            return a.effectiveness_score > b.effectiveness_score;
        }
    );
    
    if (all.size() > static_cast<size_t>(limit)) {
        all.resize(limit);
    }
    
    return all;
}

void MTMGraph::updateStrategyEffectiveness(
    const std::string& strategy_id,
    double outcome_score
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = strategies_.find(strategy_id);
    if (it == strategies_.end()) return;
    
    // Running average update
    it->second.use_count++;
    if (outcome_score > 0.7) it->second.success_count++;
    
    double alpha = 1.0 / it->second.use_count;  // Decaying learning rate
    it->second.effectiveness_score = 
        (1.0 - alpha) * it->second.effectiveness_score + alpha * outcome_score;
    
    it->second.metadata.last_accessed = std::chrono::system_clock::now();
}

bool MTMGraph::deleteStrategy(const std::string& strategy_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = strategies_.find(strategy_id);
    if (it == strategies_.end()) return false;
    
    type_to_strategies_[it->second.strategy_type].erase(strategy_id);
    
    // Remove edges
    out_edges_.erase(strategy_id);
    in_edges_.erase(strategy_id);
    
    strategies_.erase(it);
    return true;
}

// =============================================================================
// Graph Operations
// =============================================================================

void MTMGraph::addEdge(const MTMEdge& edge) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Check edge limit
    size_t total_edges = 0;
    for (const auto& [_, edges] : out_edges_) {
        total_edges += edges.size();
    }
    
    if (total_edges >= config_.max_edges) {
        return;  // Could implement LRU eviction
    }
    
    out_edges_[edge.source_id].push_back(edge);
    in_edges_[edge.target_id].push_back(edge);
}

bool MTMGraph::removeEdge(const std::string& source_id, const std::string& target_id, EdgeType type) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    bool removed = false;
    
    auto out_it = out_edges_.find(source_id);
    if (out_it != out_edges_.end()) {
        auto& edges = out_it->second;
        auto new_end = std::remove_if(edges.begin(), edges.end(),
            [&](const MTMEdge& e) {
                return e.target_id == target_id && e.type == type;
            });
        removed = (new_end != edges.end());
        edges.erase(new_end, edges.end());
    }
    
    auto in_it = in_edges_.find(target_id);
    if (in_it != in_edges_.end()) {
        auto& edges = in_it->second;
        edges.erase(
            std::remove_if(edges.begin(), edges.end(),
                [&](const MTMEdge& e) {
                    return e.source_id == source_id && e.type == type;
                }),
            edges.end()
        );
    }
    
    return removed;
}

std::vector<MTMEdge> MTMGraph::getOutEdges(const std::string& node_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = out_edges_.find(node_id);
    if (it != out_edges_.end()) {
        return it->second;
    }
    return {};
}

std::vector<MTMEdge> MTMGraph::getInEdges(const std::string& node_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = in_edges_.find(node_id);
    if (it != in_edges_.end()) {
        return it->second;
    }
    return {};
}

std::vector<PatternEntry> MTMGraph::getRelatedPatterns(
    const std::string& pattern_id,
    int max_depth
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::unordered_set<std::string> visited;
    std::queue<std::pair<std::string, int>> queue;
    std::vector<PatternEntry> results;
    
    queue.push({pattern_id, 0});
    visited.insert(pattern_id);
    
    while (!queue.empty()) {
        auto [current_id, depth] = queue.front();
        queue.pop();
        
        if (depth > 0) {  // Don't include the start pattern
            auto it = patterns_.find(current_id);
            if (it != patterns_.end()) {
                results.push_back(it->second);
            }
        }
        
        if (depth >= max_depth) continue;
        
        // Follow edges
        auto out_it = out_edges_.find(current_id);
        if (out_it != out_edges_.end()) {
            for (const auto& edge : out_it->second) {
                if (!visited.count(edge.target_id) && patterns_.count(edge.target_id)) {
                    visited.insert(edge.target_id);
                    queue.push({edge.target_id, depth + 1});
                }
            }
        }
        
        auto in_it = in_edges_.find(current_id);
        if (in_it != in_edges_.end()) {
            for (const auto& edge : in_it->second) {
                if (!visited.count(edge.source_id) && patterns_.count(edge.source_id)) {
                    visited.insert(edge.source_id);
                    queue.push({edge.source_id, depth + 1});
                }
            }
        }
    }
    
    return results;
}

std::vector<StrategyEntry> MTMGraph::getStrategiesForPattern(const std::string& pattern_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<StrategyEntry> results;
    
    // Find strategies that list this pattern as applicable
    for (const auto& [_, strategy] : strategies_) {
        if (std::find(strategy.applicable_pattern_ids.begin(),
                     strategy.applicable_pattern_ids.end(),
                     pattern_id) != strategy.applicable_pattern_ids.end()) {
            results.push_back(strategy);
        }
    }
    
    // Also check edges
    auto out_it = out_edges_.find(pattern_id);
    if (out_it != out_edges_.end()) {
        for (const auto& edge : out_it->second) {
            if (edge.type == EdgeType::RESOLVES || edge.type == EdgeType::APPLIES_TO) {
                auto strat_it = strategies_.find(edge.target_id);
                if (strat_it != strategies_.end()) {
                    results.push_back(strat_it->second);
                }
            }
        }
    }
    
    return results;
}

// =============================================================================
// Learning & Optimization
// =============================================================================

void MTMGraph::learnFromOutcome(
    const std::vector<std::string>& used_pattern_ids,
    const std::vector<std::string>& used_strategy_ids,
    double outcome_score
) {
    bool positive = outcome_score > 0.6;
    
    for (const auto& id : used_pattern_ids) {
        updatePatternConfidence(id, positive);
    }
    
    for (const auto& id : used_strategy_ids) {
        updateStrategyEffectiveness(id, outcome_score);
    }
    
    // Strengthen edges between co-used patterns/strategies
    if (positive) {
        std::lock_guard<std::mutex> lock(mutex_);
        
        for (size_t i = 0; i < used_pattern_ids.size(); ++i) {
            for (size_t j = i + 1; j < used_pattern_ids.size(); ++j) {
                // Check if edge exists, strengthen or create
                bool found = false;
                for (auto& edge : out_edges_[used_pattern_ids[i]]) {
                    if (edge.target_id == used_pattern_ids[j] && 
                        edge.type == EdgeType::RELATED_TO) {
                        edge.weight *= 1.1;
                        found = true;
                        break;
                    }
                }
                
                if (!found && out_edges_[used_pattern_ids[i]].size() < 50) {
                    MTMEdge edge;
                    edge.source_id = used_pattern_ids[i];
                    edge.target_id = used_pattern_ids[j];
                    edge.type = EdgeType::RELATED_TO;
                    edge.weight = 1.0;
                    addEdge(edge);
                }
            }
        }
    }
}

size_t MTMGraph::consolidatePatterns() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    size_t affected = 0;
    
    // Remove low-confidence patterns
    std::vector<std::string> to_remove;
    for (const auto& [id, pattern] : patterns_) {
        if (pattern.confidence < config_.confidence_threshold * 0.5 && 
            pattern.occurrence_count < 3) {
            to_remove.push_back(id);
        }
    }
    
    for (const auto& id : to_remove) {
        deletePattern(id);
        affected++;
    }
    
    // Merge similar patterns (if enabled)
    if (config_.enable_auto_merge) {
        auto similar = findSimilarPatterns(PatternEntry{});  // Would need implementation
        // TODO: Implement merging logic
    }
    
    return affected;
}

void MTMGraph::applyDecay() {
    if (!config_.enable_decay) return;
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto now = std::chrono::system_clock::now();
    
    for (auto& [_, pattern] : patterns_) {
        auto age = std::chrono::duration_cast<std::chrono::hours>(
            now - pattern.metadata.last_accessed
        );
        
        double days = age.count() / 24.0;
        double decay = std::exp(-config_.decay_rate * days);
        
        pattern.confidence *= decay;
        pattern.metadata.decay_factor = decay;
    }
    
    for (auto& [_, strategy] : strategies_) {
        auto age = std::chrono::duration_cast<std::chrono::hours>(
            now - strategy.metadata.last_accessed
        );
        
        double days = age.count() / 24.0;
        double decay = std::exp(-config_.decay_rate * days);
        
        strategy.effectiveness_score *= decay;
        strategy.metadata.decay_factor = decay;
    }
}

// =============================================================================
// Persistence
// =============================================================================

void MTMGraph::save() {
    save(config_.storage_path);
}

void MTMGraph::save(const std::string& path) {
    std::filesystem::create_directories(path);
    
    // Save patterns (JSON)
    std::ofstream patterns_file(path + "/patterns.json");
    if (patterns_file.is_open()) {
        patterns_file << "{\n  \"count\": " << patterns_.size() << ",\n";
        patterns_file << "  \"patterns\": [\n";
        
        bool first = true;
        for (const auto& [id, pattern] : patterns_) {
            if (!first) patterns_file << ",\n";
            patterns_file << "    {\"id\": \"" << id << "\", ";
            patterns_file << "\"name\": \"" << pattern.name << "\", ";
            patterns_file << "\"confidence\": " << pattern.confidence << "}";
            first = false;
        }
        
        patterns_file << "\n  ]\n}\n";
        patterns_file.close();
    }
    
    // Save strategies (JSON)
    std::ofstream strategies_file(path + "/strategies.json");
    if (strategies_file.is_open()) {
        strategies_file << "{\n  \"count\": " << strategies_.size() << ",\n";
        strategies_file << "  \"strategies\": [\n";
        
        bool first = true;
        for (const auto& [id, strategy] : strategies_) {
            if (!first) strategies_file << ",\n";
            strategies_file << "    {\"id\": \"" << id << "\", ";
            strategies_file << "\"name\": \"" << strategy.name << "\", ";
            strategies_file << "\"effectiveness\": " << strategy.effectiveness_score << "}";
            first = false;
        }
        
        strategies_file << "\n  ]\n}\n";
        strategies_file.close();
    }
}

void MTMGraph::load() {
    load(config_.storage_path);
}

void MTMGraph::load(const std::string& path) {
    // TODO: Implement JSON loading
}

// =============================================================================
// Statistics
// =============================================================================

MTMGraph::Stats MTMGraph::getStats() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    Stats stats;
    stats.total_patterns = patterns_.size();
    stats.total_strategies = strategies_.size();
    
    // Count edges
    for (const auto& [_, edges] : out_edges_) {
        stats.total_edges += edges.size();
    }
    
    // Calculate averages
    if (!patterns_.empty()) {
        double sum = 0;
        for (const auto& [_, p] : patterns_) {
            sum += p.confidence;
        }
        stats.avg_pattern_confidence = sum / patterns_.size();
    }
    
    if (!strategies_.empty()) {
        double sum = 0;
        for (const auto& [_, s] : strategies_) {
            sum += s.effectiveness_score;
        }
        stats.avg_strategy_effectiveness = sum / strategies_.size();
    }
    
    // Count by type
    for (const auto& [type, ids] : type_to_patterns_) {
        stats.patterns_by_type[type] = ids.size();
    }
    
    for (const auto& [type, ids] : type_to_strategies_) {
        stats.strategies_by_type[type] = ids.size();
    }
    
    return stats;
}

// =============================================================================
// Helpers
// =============================================================================

void MTMGraph::rebuildPatternIndex() {
    pattern_index_->clear();
    
    for (const auto& [id, pattern] : patterns_) {
        if (!pattern.embedding.empty()) {
            pattern_index_->add(id, pattern.embedding);
        }
    }
}

std::vector<PatternEntry> MTMGraph::findSimilarPatterns(const PatternEntry& pattern) {
    // TODO: Implement similarity search for pattern merging
    return {};
}

void MTMGraph::mergePatterns(const std::string& keep_id, const std::string& merge_id) {
    // TODO: Implement pattern merging
}

std::string MTMGraph::categoryToString(PatternCategory cat) {
    switch (cat) {
        case PatternCategory::BUG: return "bug";
        case PatternCategory::SECURITY: return "security";
        case PatternCategory::PERFORMANCE: return "performance";
        case PatternCategory::ARCHITECTURE: return "architecture";
        case PatternCategory::STYLE: return "style";
        case PatternCategory::TESTING: return "testing";
        case PatternCategory::DOCUMENTATION: return "documentation";
        case PatternCategory::CUSTOM: return "custom";
        default: return "unknown";
    }
}

std::string MTMGraph::categoryToString(StrategyCategory cat) {
    switch (cat) {
        case StrategyCategory::REVIEW: return "review";
        case StrategyCategory::ANALYSIS: return "analysis";
        case StrategyCategory::REFACTOR: return "refactor";
        case StrategyCategory::EXPLAIN: return "explain";
        case StrategyCategory::SECURITY_AUDIT: return "security_audit";
        case StrategyCategory::PERFORMANCE_AUDIT: return "performance_audit";
        case StrategyCategory::CUSTOM: return "custom";
        default: return "unknown";
    }
}

// =============================================================================
// Default Patterns & Strategies
// =============================================================================

std::vector<PatternEntry> getDefaultPatterns(const std::string& language) {
    std::vector<PatternEntry> patterns;
    
    // Common patterns for all languages
    PatternEntry null_check;
    null_check.id = "default_null_check";
    null_check.name = "Missing Null Check";
    null_check.description = "Potential null pointer dereference";
    null_check.pattern_type = "bug";
    null_check.confidence = 0.8;
    null_check.applicable_languages = {"cpp", "java", "c", "csharp"};
    patterns.push_back(null_check);
    
    PatternEntry sql_injection;
    sql_injection.id = "default_sql_injection";
    sql_injection.name = "SQL Injection Risk";
    sql_injection.description = "User input used directly in SQL query";
    sql_injection.pattern_type = "security";
    sql_injection.confidence = 0.9;
    sql_injection.applicable_languages = {"java", "python", "javascript", "php"};
    patterns.push_back(sql_injection);
    
    PatternEntry resource_leak;
    resource_leak.id = "default_resource_leak";
    resource_leak.name = "Resource Leak";
    resource_leak.description = "Resource opened but not properly closed";
    resource_leak.pattern_type = "bug";
    resource_leak.confidence = 0.7;
    resource_leak.applicable_languages = {"cpp", "java", "c", "python"};
    patterns.push_back(resource_leak);
    
    return patterns;
}

std::vector<StrategyEntry> getDefaultStrategies(const std::string& review_type) {
    std::vector<StrategyEntry> strategies;
    
    StrategyEntry security_review;
    security_review.id = "default_security_review";
    security_review.name = "Security-Focused Review";
    security_review.description = "Focus on security vulnerabilities";
    security_review.strategy_type = "review";
    security_review.context_type = "security";
    security_review.effectiveness_score = 0.8;
    security_review.focus_areas = {"input_validation", "authentication", "authorization", "encryption"};
    security_review.applicable_pattern_ids = {"default_sql_injection"};
    strategies.push_back(security_review);
    
    StrategyEntry performance_review;
    performance_review.id = "default_performance_review";
    performance_review.name = "Performance Analysis";
    performance_review.description = "Focus on performance issues";
    performance_review.strategy_type = "analysis";
    performance_review.context_type = "performance";
    performance_review.effectiveness_score = 0.75;
    performance_review.focus_areas = {"complexity", "memory", "io", "caching"};
    strategies.push_back(performance_review);
    
    return strategies;
}

} // namespace aipr::tms
