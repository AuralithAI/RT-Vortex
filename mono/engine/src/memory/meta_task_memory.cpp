/**
 * AI PR Reviewer - Meta-Task Memory Implementation
 * 
 * Stores review strategies, patterns, and learned behaviors.
 */

#include "memory_system.h"
#include <algorithm>
#include <cmath>
#include <fstream>
#include <sstream>
#include <nlohmann/json.hpp>

namespace aipr {

using json = nlohmann::json;

class MetaTaskMemoryImpl : public MetaTaskMemory {
public:
    explicit MetaTaskMemoryImpl(const MemoryConfig& config)
        : config_(config) {
    }
    
    ~MetaTaskMemoryImpl() override = default;
    
    void storePattern(const ReviewPattern& pattern) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        patterns_[pattern.id] = pattern;
    }
    
    void storeStrategy(const ReviewStrategy& strategy) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        strategies_[strategy.id] = strategy;
    }
    
    std::vector<ReviewPattern> matchPatterns(
        const std::string& code_context,
        const std::string& file_type,
        int top_k
    ) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        if (top_k < 0) top_k = config_.mtm_top_k;
        
        std::vector<std::pair<std::string, double>> scored;
        
        for (const auto& [id, pattern] : patterns_) {
            double score = 0;
            
            // Language match
            if (!file_type.empty() && 
                std::find(pattern.languages.begin(), pattern.languages.end(), file_type) 
                != pattern.languages.end()) {
                score += 0.3;
            }
            
            // Context keyword matching (simple TF-IDF approximation)
            for (const auto& indicator : pattern.indicators) {
                if (code_context.find(indicator) != std::string::npos) {
                    score += 0.15;
                }
            }
            
            // Effectiveness boost
            score *= (0.5 + 0.5 * pattern.effectiveness_score);
            
            // Usage count boost (log scale)
            if (pattern.usage_count > 0) {
                score *= (1.0 + 0.1 * std::log(pattern.usage_count));
            }
            
            scored.emplace_back(id, score);
        }
        
        // Sort by score
        std::sort(scored.begin(), scored.end(),
                  [](const auto& a, const auto& b) { return a.second > b.second; });
        
        // Build result
        std::vector<ReviewPattern> results;
        for (int i = 0; i < top_k && i < static_cast<int>(scored.size()); ++i) {
            if (scored[i].second > 0.1) {  // Minimum threshold
                results.push_back(patterns_[scored[i].first]);
            }
        }
        
        return results;
    }
    
    std::optional<ReviewStrategy> getStrategy(
        const std::string& review_type,
        const std::string& repository_type
    ) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        ReviewStrategy* best = nullptr;
        double best_score = 0;
        
        for (auto& [id, strategy] : strategies_) {
            // Skip inactive strategies
            if (!strategy.active) continue;
            
            double score = 0;
            
            // Conditions matching
            auto review_it = strategy.conditions.find("review_type");
            if (review_it != strategy.conditions.end() && 
                review_it->second == review_type) {
                score += 0.4;
            }
            
            auto repo_it = strategy.conditions.find("repository_type");
            if (repo_it != strategy.conditions.end() && 
                repo_it->second == repository_type) {
                score += 0.3;
            }
            
            // Effectiveness boost
            score *= (0.5 + 0.5 * strategy.success_rate);
            
            if (score > best_score) {
                best_score = score;
                best = &strategy;
            }
        }
        
        if (best && best_score > 0.2) {
            return *best;
        }
        
        return std::nullopt;
    }
    
    void recordOutcome(
        const std::string& pattern_or_strategy_id,
        bool success,
        double confidence
    ) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        // Update pattern if found
        auto pat_it = patterns_.find(pattern_or_strategy_id);
        if (pat_it != patterns_.end()) {
            auto& pattern = pat_it->second;
            pattern.usage_count++;
            
            // Exponential moving average
            double alpha = 0.1;
            double outcome = success ? confidence : 0;
            pattern.effectiveness_score = 
                alpha * outcome + (1 - alpha) * pattern.effectiveness_score;
            
            pattern.last_used = std::chrono::system_clock::now();
            return;
        }
        
        // Update strategy if found
        auto strat_it = strategies_.find(pattern_or_strategy_id);
        if (strat_it != strategies_.end()) {
            auto& strategy = strat_it->second;
            strategy.usage_count++;
            
            // Update success rate
            double alpha = 0.1;
            strategy.success_rate = 
                alpha * (success ? 1.0 : 0.0) + (1 - alpha) * strategy.success_rate;
            
            strategy.last_used = std::chrono::system_clock::now();
        }
    }
    
    void learnFromFeedback(
        const std::string& feedback_type,
        const std::map<std::string, std::string>& context,
        bool positive
    ) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        FeedbackEntry entry;
        entry.feedback_type = feedback_type;
        entry.context = context;
        entry.positive = positive;
        entry.timestamp = std::chrono::system_clock::now();
        
        feedback_history_.push_back(entry);
        
        // Limit history
        while (feedback_history_.size() > 1000) {
            feedback_history_.erase(feedback_history_.begin());
        }
        
        // Analyze feedback patterns
        analyzeFeeback();
    }
    
    bool save(const std::string& path) const override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        try {
            json j;
            
            // Serialize patterns
            j["patterns"] = json::array();
            for (const auto& [id, p] : patterns_) {
                json jp;
                jp["id"] = p.id;
                jp["name"] = p.name;
                jp["description"] = p.description;
                jp["category"] = p.category;
                jp["languages"] = p.languages;
                jp["indicators"] = p.indicators;
                jp["suggested_checks"] = p.suggested_checks;
                jp["effectiveness_score"] = p.effectiveness_score;
                jp["usage_count"] = p.usage_count;
                jp["last_used"] = std::chrono::system_clock::to_time_t(p.last_used);
                j["patterns"].push_back(jp);
            }
            
            // Serialize strategies
            j["strategies"] = json::array();
            for (const auto& [id, s] : strategies_) {
                json js;
                js["id"] = s.id;
                js["name"] = s.name;
                js["conditions"] = s.conditions;
                js["steps"] = s.steps;
                js["focus_areas"] = s.focus_areas;
                js["success_rate"] = s.success_rate;
                js["usage_count"] = s.usage_count;
                js["active"] = s.active;
                js["last_used"] = std::chrono::system_clock::to_time_t(s.last_used);
                j["strategies"].push_back(js);
            }
            
            // Write file
            std::ofstream out(path);
            out << j.dump(2);
            return true;
        } catch (...) {
            return false;
        }
    }
    
    bool load(const std::string& path) override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        try {
            std::ifstream in(path);
            if (!in.is_open()) return false;
            
            json j;
            in >> j;
            
            // Load patterns
            patterns_.clear();
            for (const auto& jp : j["patterns"]) {
                ReviewPattern p;
                p.id = jp["id"];
                p.name = jp["name"];
                p.description = jp["description"];
                p.category = jp["category"];
                p.languages = jp["languages"].get<std::vector<std::string>>();
                p.indicators = jp["indicators"].get<std::vector<std::string>>();
                p.suggested_checks = jp["suggested_checks"].get<std::vector<std::string>>();
                p.effectiveness_score = jp["effectiveness_score"];
                p.usage_count = jp["usage_count"];
                p.last_used = std::chrono::system_clock::from_time_t(jp["last_used"]);
                patterns_[p.id] = p;
            }
            
            // Load strategies
            strategies_.clear();
            for (const auto& js : j["strategies"]) {
                ReviewStrategy s;
                s.id = js["id"];
                s.name = js["name"];
                s.conditions = js["conditions"].get<std::map<std::string, std::string>>();
                s.steps = js["steps"].get<std::vector<std::string>>();
                s.focus_areas = js["focus_areas"].get<std::vector<std::string>>();
                s.success_rate = js["success_rate"];
                s.usage_count = js["usage_count"];
                s.active = js["active"];
                s.last_used = std::chrono::system_clock::from_time_t(js["last_used"]);
                strategies_[s.id] = s;
            }
            
            return true;
        } catch (...) {
            return false;
        }
    }
    
    std::vector<std::string> suggestImprovements(
        const std::string& review_type
    ) const override {
        std::lock_guard<std::mutex> lock(mutex_);
        
        std::vector<std::string> suggestions;
        
        // Analyze feedback for improvement opportunities
        std::map<std::string, int> negative_counts;
        std::map<std::string, int> total_counts;
        
        for (const auto& entry : feedback_history_) {
            auto type_it = entry.context.find("check_type");
            if (type_it != entry.context.end()) {
                total_counts[type_it->second]++;
                if (!entry.positive) {
                    negative_counts[type_it->second]++;
                }
            }
        }
        
        // Find checks with high negative rates
        for (const auto& [check, total] : total_counts) {
            if (total >= 5) {
                double negative_rate = static_cast<double>(negative_counts[check]) / total;
                if (negative_rate > 0.3) {
                    suggestions.push_back(
                        "Consider adjusting '" + check + "' check - " +
                        std::to_string(static_cast<int>(negative_rate * 100)) + 
                        "% negative feedback"
                    );
                }
            }
        }
        
        // Find underused effective patterns
        for (const auto& [id, pattern] : patterns_) {
            if (pattern.effectiveness_score > 0.8 && pattern.usage_count < 5) {
                suggestions.push_back(
                    "Pattern '" + pattern.name + "' has high effectiveness but low usage"
                );
            }
        }
        
        return suggestions;
    }
    
private:
    struct FeedbackEntry {
        std::string feedback_type;
        std::map<std::string, std::string> context;
        bool positive;
        std::chrono::system_clock::time_point timestamp;
    };
    
    void analyzeFeeback() {
        // Count recent positive/negative by pattern
        auto now = std::chrono::system_clock::now();
        auto threshold = now - std::chrono::hours(24);
        
        std::map<std::string, int> positive_counts;
        std::map<std::string, int> negative_counts;
        
        for (const auto& entry : feedback_history_) {
            if (entry.timestamp < threshold) continue;
            
            auto pattern_it = entry.context.find("pattern_id");
            if (pattern_it != entry.context.end()) {
                if (entry.positive) {
                    positive_counts[pattern_it->second]++;
                } else {
                    negative_counts[pattern_it->second]++;
                }
            }
        }
        
        // Update pattern effectiveness based on recent feedback
        for (const auto& [pattern_id, pos_count] : positive_counts) {
            auto it = patterns_.find(pattern_id);
            if (it == patterns_.end()) continue;
            
            int neg_count = negative_counts[pattern_id];
            int total = pos_count + neg_count;
            
            if (total >= 3) {
                double recent_rate = static_cast<double>(pos_count) / total;
                
                // Blend recent rate with historical
                auto& pattern = it->second;
                pattern.effectiveness_score = 
                    0.3 * recent_rate + 0.7 * pattern.effectiveness_score;
            }
        }
        
        // Deactivate consistently failing strategies
        for (auto& [id, strategy] : strategies_) {
            if (strategy.usage_count >= 10 && strategy.success_rate < 0.2) {
                strategy.active = false;
            }
        }
    }
    
    MemoryConfig config_;
    mutable std::mutex mutex_;
    
    std::unordered_map<std::string, ReviewPattern> patterns_;
    std::unordered_map<std::string, ReviewStrategy> strategies_;
    std::vector<FeedbackEntry> feedback_history_;
};

std::unique_ptr<MetaTaskMemory> createMetaTaskMemory(const MemoryConfig& config) {
    return std::make_unique<MetaTaskMemoryImpl>(config);
}

} // namespace aipr
