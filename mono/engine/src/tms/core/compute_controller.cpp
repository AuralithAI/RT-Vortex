/**
 * TMS Compute Controller Implementation
 * 
 * Intelligent strategy selection for optimal retrieval/attention.
 */

#include "tms/compute_controller.h"
#include <algorithm>
#include <cmath>
#include <numeric>
#include <random>
#include <sstream>
#include <regex>

namespace aipr::tms {

// =============================================================================
// Constructor / Destructor
// =============================================================================

ComputeController::ComputeController(const ControllerConfig& config)
    : config_(config) {
    initializeMLP();
}

ComputeController::~ComputeController() = default;

// =============================================================================
// Main Decision Interface
// =============================================================================

ComputeDecision ComputeController::decide(
    const std::vector<float>& query_embedding,
    const ResourceState& resource_state
) {
    // Extract features from embedding (simplified)
    QueryFeatures features;
    features.estimated_tokens = 50;  // Default estimate
    features.concept_count = 1;
    
    return decide(features, resource_state);
}

ComputeDecision ComputeController::decide(
    const QueryFeatures& features,
    const ResourceState& resource_state
) {
    auto start = std::chrono::steady_clock::now();
    
    // Compute scores for each factor
    double query_score = computeQueryComplexity(features);
    double resource_score = computeResourceScore(resource_state);
    double historical_score = computeHistoricalScore(features);
    
    // Session context score
    double session_score = 0.5;  // Default
    // TODO: Look up actual session context
    
    // Repo size score
    double repo_score = 0.5;
    if (resource_state.ltm_size > 1000000) {
        repo_score = 0.8;  // Large repo = more thorough
    } else if (resource_state.ltm_size > 100000) {
        repo_score = 0.6;
    } else if (resource_state.ltm_size < 10000) {
        repo_score = 0.3;
    }
    
    // Weighted combination
    double combined = 
        config_.weight_query_complexity * query_score +
        config_.weight_resource_state * resource_score +
        config_.weight_historical * historical_score +
        config_.weight_session_context * session_score +
        config_.weight_repo_size * repo_score;
    
    // Apply learned adjustments
    double fast_threshold = config_.fast_threshold + learned_fast_threshold_;
    double balanced_threshold = config_.balanced_threshold + learned_balanced_threshold_;
    
    // Determine strategy
    ComputeStrategy strategy;
    std::string reasoning;
    
    if (combined < fast_threshold) {
        strategy = ComputeStrategy::FAST;
        reasoning = "Simple query, low complexity (" + std::to_string(combined) + ")";
    } else if (combined < balanced_threshold) {
        strategy = ComputeStrategy::BALANCED;
        reasoning = "Moderate query complexity (" + std::to_string(combined) + ")";
    } else {
        strategy = ComputeStrategy::THOROUGH;
        reasoning = "Complex query requiring deep analysis (" + std::to_string(combined) + ")";
    }
    
    // Resource constraints can force downgrade
    if (resource_state.available_vram_gb < 2.0 && strategy == ComputeStrategy::THOROUGH) {
        strategy = ComputeStrategy::BALANCED;
        reasoning += " [downgraded: low VRAM]";
    }
    if (resource_state.available_vram_gb < 1.0 && strategy != ComputeStrategy::FAST) {
        strategy = ComputeStrategy::FAST;
        reasoning += " [downgraded: very low VRAM]";
    }
    
    // Build decision
    auto decision = buildDecision(strategy, reasoning);
    
    // Update stats
    {
        std::lock_guard<std::mutex> lock(stats_mutex_);
        stats_.total_decisions++;
        switch (strategy) {
            case ComputeStrategy::FAST: stats_.fast_decisions++; break;
            case ComputeStrategy::BALANCED: stats_.balanced_decisions++; break;
            case ComputeStrategy::THOROUGH: stats_.thorough_decisions++; break;
        }
        
        auto end = std::chrono::steady_clock::now();
        auto duration = std::chrono::duration_cast<std::chrono::microseconds>(end - start);
        double new_avg = (stats_.avg_decision_time_us * (stats_.total_decisions - 1) + duration.count()) 
                         / stats_.total_decisions;
        stats_.avg_decision_time_us = new_avg;
    }
    
    return decision;
}

ComputeDecision ComputeController::decideSimple(
    const std::string& query_text,
    float available_vram_gb
) {
    QueryFeatures features = extractFeatures(query_text);
    
    ResourceState state;
    state.available_vram_gb = available_vram_gb;
    state.available_ram_gb = 16.0f;  // Default
    state.cpu_load = 0.5f;
    state.ltm_size = 100000;  // Default
    
    return decide(features, state);
}

ComputeDecision ComputeController::forceStrategy(ComputeStrategy strategy) {
    return buildDecision(strategy, "Forced by user");
}

DecisionExplanation ComputeController::explain(
    const QueryFeatures& features,
    const ResourceState& resource_state
) {
    DecisionExplanation exp;
    
    exp.query_complexity_score = computeQueryComplexity(features);
    exp.resource_score = computeResourceScore(resource_state);
    exp.historical_score = computeHistoricalScore(features);
    
    // Session and repo scores (simplified)
    exp.session_score = 0.5;
    exp.repo_score = resource_state.ltm_size > 100000 ? 0.7 : 0.4;
    
    exp.combined_score = 
        config_.weight_query_complexity * exp.query_complexity_score +
        config_.weight_resource_state * exp.resource_score +
        config_.weight_historical * exp.historical_score +
        config_.weight_session_context * exp.session_score +
        config_.weight_repo_size * exp.repo_score;
    
    // Determine strategy
    if (exp.combined_score < config_.fast_threshold) {
        exp.recommended_strategy = ComputeStrategy::FAST;
    } else if (exp.combined_score < config_.balanced_threshold) {
        exp.recommended_strategy = ComputeStrategy::BALANCED;
    } else {
        exp.recommended_strategy = ComputeStrategy::THOROUGH;
    }
    
    exp.confidence = 1.0 - std::abs(exp.combined_score - 0.5);  // More confident at extremes
    
    // Build reasoning
    std::ostringstream oss;
    oss << "Query complexity: " << (exp.query_complexity_score < 0.3 ? "low" : 
                                    exp.query_complexity_score < 0.7 ? "medium" : "high");
    oss << ", Resources: " << (exp.resource_score < 0.3 ? "constrained" : "available");
    oss << ", Combined score: " << exp.combined_score;
    exp.reasoning = oss.str();
    
    return exp;
}

// =============================================================================
// Feature Extraction
// =============================================================================

QueryFeatures ComputeController::extractFeatures(const std::string& query_text) {
    QueryFeatures features;
    
    features.query_length = query_text.length();
    features.estimated_tokens = query_text.length() / 4;  // Rough estimate
    
    // Count concepts (simple heuristic: count distinct important words)
    std::unordered_set<std::string> words;
    std::string current;
    for (char c : query_text) {
        if (std::isalnum(c)) {
            current += std::tolower(c);
        } else if (!current.empty()) {
            if (current.length() > 3) {
                words.insert(current);
            }
            current.clear();
        }
    }
    if (!current.empty() && current.length() > 3) {
        words.insert(current);
    }
    features.concept_count = words.size();
    
    // Check for code references
    features.has_code_reference = 
        query_text.find("```") != std::string::npos ||
        query_text.find("function") != std::string::npos ||
        query_text.find("class") != std::string::npos ||
        query_text.find("method") != std::string::npos;
    
    // Check for multi-file queries
    std::vector<std::string> multi_file_indicators = {
        "across", "throughout", "entire", "all files", "whole", "flow", "architecture"
    };
    features.is_multi_file = false;
    for (const auto& indicator : multi_file_indicators) {
        if (query_text.find(indicator) != std::string::npos) {
            features.is_multi_file = true;
            break;
        }
    }
    
    // Check for architectural queries
    std::vector<std::string> arch_indicators = {
        "architecture", "design", "structure", "pattern", "flow", "system", "component"
    };
    features.is_architectural = false;
    for (const auto& indicator : arch_indicators) {
        if (query_text.find(indicator) != std::string::npos) {
            features.is_architectural = true;
            break;
        }
    }
    
    // Specificity (inverse of vagueness)
    // Questions with specific file names, function names, etc. are more specific
    bool has_quotes = query_text.find('"') != std::string::npos || 
                      query_text.find('\'') != std::string::npos;
    bool has_path = query_text.find('/') != std::string::npos ||
                    query_text.find('.cpp') != std::string::npos ||
                    query_text.find('.java') != std::string::npos;
    
    features.specificity = 0.5;
    if (has_quotes) features.specificity += 0.2;
    if (has_path) features.specificity += 0.2;
    if (features.concept_count > 5) features.specificity += 0.1;
    features.specificity = std::min(1.0, features.specificity);
    
    // Detect intents
    features.detected_intents = detectIntents(query_text);
    
    return features;
}

QueryFeatures ComputeController::extractFeatures(const std::vector<float>& query_embedding) {
    // Without the text, we can only make rough estimates
    QueryFeatures features;
    features.estimated_tokens = 50;
    features.concept_count = 2;
    features.specificity = 0.5;
    return features;
}

ResourceState ComputeController::getResourceState() {
    ResourceState state;
    
    // In production, query actual system resources
    state.available_vram_gb = config_.vram_budget_gb;
    state.available_ram_gb = 16.0f;  // Default
    state.cpu_load = 0.5f;
    
    return state;
}

// =============================================================================
// Learning & Adaptation
// =============================================================================

void ComputeController::recordOutcome(
    const std::string& query_text,
    ComputeStrategy used_strategy,
    double actual_time_ms,
    double outcome_score
) {
    QueryRecord record;
    record.query_hash = std::to_string(std::hash<std::string>{}(query_text));
    record.used_strategy = used_strategy;
    record.actual_time_ms = actual_time_ms;
    record.outcome_score = outcome_score;
    record.features = extractFeatures(query_text);
    record.timestamp = std::chrono::system_clock::now();
    
    // Add to history
    history_.push_back(record);
    if (history_.size() > MAX_HISTORY) {
        history_.erase(history_.begin());
    }
    
    // Update stats
    {
        std::lock_guard<std::mutex> lock(stats_mutex_);
        stats_.avg_outcome_score = 
            (stats_.avg_outcome_score * (stats_.total_decisions - 1) + outcome_score) 
            / stats_.total_decisions;
    }
}

void ComputeController::updateModel() {
    if (history_.size() < 100) return;  // Need enough data
    
    // Analyze recent outcomes to adjust thresholds
    double fast_good = 0, fast_bad = 0;
    double balanced_good = 0, balanced_bad = 0;
    double thorough_good = 0, thorough_bad = 0;
    
    for (const auto& record : history_) {
        switch (record.used_strategy) {
            case ComputeStrategy::FAST:
                if (record.outcome_score > 0.7) fast_good++;
                else if (record.outcome_score < 0.4) fast_bad++;
                break;
            case ComputeStrategy::BALANCED:
                if (record.outcome_score > 0.7) balanced_good++;
                else if (record.outcome_score < 0.4) balanced_bad++;
                break;
            case ComputeStrategy::THOROUGH:
                if (record.outcome_score > 0.7) thorough_good++;
                else if (record.outcome_score < 0.4) thorough_bad++;
                break;
        }
    }
    
    // Adjust thresholds based on success rates
    double fast_rate = fast_good / (fast_good + fast_bad + 1);
    double balanced_rate = balanced_good / (balanced_good + balanced_bad + 1);
    
    // If FAST is doing well, expand its range
    if (fast_rate > 0.8) {
        learned_fast_threshold_ += config_.learning_rate * 0.05;
    } else if (fast_rate < 0.5) {
        learned_fast_threshold_ -= config_.learning_rate * 0.05;
    }
    
    // Similar for BALANCED
    if (balanced_rate > 0.8) {
        learned_balanced_threshold_ += config_.learning_rate * 0.05;
    } else if (balanced_rate < 0.5) {
        learned_balanced_threshold_ -= config_.learning_rate * 0.05;
    }
    
    // Clamp adjustments
    learned_fast_threshold_ = std::clamp(learned_fast_threshold_, -0.1, 0.1);
    learned_balanced_threshold_ = std::clamp(learned_balanced_threshold_, -0.1, 0.1);
}

ComputeController::LearnedAdjustments ComputeController::getLearnedAdjustments() const {
    LearnedAdjustments adj;
    adj.fast_threshold_adjustment = learned_fast_threshold_;
    adj.balanced_threshold_adjustment = learned_balanced_threshold_;
    adj.intent_to_strategy_bias = intent_bias_;
    return adj;
}

// =============================================================================
// Session Context
// =============================================================================

void ComputeController::setSessionContext(
    const std::string& session_id,
    size_t context_richness,
    size_t recent_query_count
) {
    SessionContext ctx;
    ctx.context_richness = context_richness;
    ctx.recent_query_count = recent_query_count;
    session_contexts_[session_id] = ctx;
}

void ComputeController::setRepoContext(
    const std::string& repo_id,
    size_t chunk_count
) {
    RepoContext ctx;
    ctx.chunk_count = chunk_count;
    repo_contexts_[repo_id] = ctx;
}

// =============================================================================
// Configuration
// =============================================================================

void ComputeController::updateConfig(const ControllerConfig& config) {
    config_ = config;
}

void ComputeController::setVRAMBudget(float gb) {
    config_.vram_budget_gb = gb;
}

// =============================================================================
// Statistics
// =============================================================================

ComputeController::Stats ComputeController::getStats() const {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    return stats_;
}

void ComputeController::resetStats() {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    stats_ = Stats{};
}

// =============================================================================
// Internal Helpers
// =============================================================================

void ComputeController::initializeMLP() {
    if (mlp_initialized_) return;
    
    // Initialize MLP weights with small random values
    std::random_device rd;
    std::mt19937 gen(rd());
    std::normal_distribution<float> dist(0.0f, 0.1f);
    
    // Layer 1: 14 inputs -> 32 hidden
    mlp_w1_.resize(14, std::vector<float>(32));
    for (auto& row : mlp_w1_) {
        for (auto& val : row) {
            val = dist(gen);
        }
    }
    mlp_b1_.resize(32, 0.0f);
    
    // Layer 2: 32 hidden -> 3 outputs
    mlp_w2_.resize(32, std::vector<float>(3));
    for (auto& row : mlp_w2_) {
        for (auto& val : row) {
            val = dist(gen);
        }
    }
    mlp_b2_.resize(3, 0.0f);
    
    mlp_initialized_ = true;
}

std::vector<float> ComputeController::mlpForward(const std::vector<float>& input) {
    if (!mlp_initialized_ || input.size() != 14) {
        return {0.33f, 0.34f, 0.33f};  // Uniform distribution
    }
    
    // Layer 1: Linear + ReLU
    std::vector<float> hidden(32);
    for (size_t j = 0; j < 32; ++j) {
        float sum = mlp_b1_[j];
        for (size_t i = 0; i < 14; ++i) {
            sum += input[i] * mlp_w1_[i][j];
        }
        hidden[j] = std::max(0.0f, sum);  // ReLU
    }
    
    // Layer 2: Linear + Softmax
    std::vector<float> logits(3);
    for (size_t j = 0; j < 3; ++j) {
        float sum = mlp_b2_[j];
        for (size_t i = 0; i < 32; ++i) {
            sum += hidden[i] * mlp_w2_[i][j];
        }
        logits[j] = sum;
    }
    
    // Softmax
    float max_logit = *std::max_element(logits.begin(), logits.end());
    float sum_exp = 0.0f;
    for (auto& l : logits) {
        l = std::exp(l - max_logit);
        sum_exp += l;
    }
    for (auto& l : logits) {
        l /= sum_exp;
    }
    
    return logits;
}

double ComputeController::computeQueryComplexity(const QueryFeatures& features) {
    double score = 0.0;
    
    // Length factor
    if (features.query_length > 500) score += 0.3;
    else if (features.query_length > 200) score += 0.2;
    else if (features.query_length > 100) score += 0.1;
    
    // Concept count
    if (features.concept_count > 10) score += 0.3;
    else if (features.concept_count > 5) score += 0.2;
    else if (features.concept_count > 2) score += 0.1;
    
    // Code reference adds complexity
    if (features.has_code_reference) score += 0.1;
    
    // Multi-file queries are complex
    if (features.is_multi_file) score += 0.2;
    
    // Architectural queries are most complex
    if (features.is_architectural) score += 0.2;
    
    // Specificity reduces apparent complexity (we know what to look for)
    score -= features.specificity * 0.1;
    
    return std::clamp(score, 0.0, 1.0);
}

double ComputeController::computeResourceScore(const ResourceState& state) {
    // Higher score = more resources available = can handle more complex queries
    double score = 0.0;
    
    // VRAM is most important
    if (state.available_vram_gb >= 8.0) score += 0.4;
    else if (state.available_vram_gb >= 4.0) score += 0.3;
    else if (state.available_vram_gb >= 2.0) score += 0.2;
    else score += 0.1;
    
    // CPU load (lower is better)
    score += (1.0 - state.cpu_load) * 0.3;
    
    // RAM
    if (state.available_ram_gb >= 16.0) score += 0.15;
    else if (state.available_ram_gb >= 8.0) score += 0.1;
    
    // Active sessions (fewer is better)
    if (state.active_sessions < 10) score += 0.15;
    else if (state.active_sessions < 50) score += 0.1;
    
    return std::clamp(score, 0.0, 1.0);
}

double ComputeController::computeHistoricalScore(const QueryFeatures& features) {
    // Look for similar queries in history
    if (history_.empty()) return 0.5;
    
    // Simple heuristic: if similar queries needed THOROUGH, suggest it
    double thorough_count = 0, total = 0;
    
    for (const auto& record : history_) {
        // Check if similar (same intents)
        bool similar = false;
        for (const auto& intent : features.detected_intents) {
            for (const auto& hist_intent : record.features.detected_intents) {
                if (intent == hist_intent) {
                    similar = true;
                    break;
                }
            }
        }
        
        if (similar) {
            total++;
            if (record.used_strategy == ComputeStrategy::THOROUGH && 
                record.outcome_score > 0.7) {
                thorough_count++;
            }
        }
    }
    
    if (total < 5) return 0.5;  // Not enough data
    
    return thorough_count / total;
}

std::vector<std::string> ComputeController::detectIntents(const std::string& query_text) {
    std::vector<std::string> intents;
    std::string lower = query_text;
    std::transform(lower.begin(), lower.end(), lower.begin(), ::tolower);
    
    // Review intents
    if (lower.find("review") != std::string::npos ||
        lower.find("check") != std::string::npos ||
        lower.find("issue") != std::string::npos ||
        lower.find("bug") != std::string::npos) {
        intents.push_back("review");
    }
    
    // Explanation intents
    if (lower.find("explain") != std::string::npos ||
        lower.find("what does") != std::string::npos ||
        lower.find("how does") != std::string::npos ||
        lower.find("understand") != std::string::npos) {
        intents.push_back("explain");
    }
    
    // Search intents
    if (lower.find("find") != std::string::npos ||
        lower.find("where") != std::string::npos ||
        lower.find("search") != std::string::npos ||
        lower.find("locate") != std::string::npos) {
        intents.push_back("find");
    }
    
    // Architecture intents
    if (lower.find("architecture") != std::string::npos ||
        lower.find("design") != std::string::npos ||
        lower.find("structure") != std::string::npos ||
        lower.find("flow") != std::string::npos) {
        intents.push_back("architecture");
    }
    
    // Security intents
    if (lower.find("security") != std::string::npos ||
        lower.find("vulnerability") != std::string::npos ||
        lower.find("injection") != std::string::npos ||
        lower.find("auth") != std::string::npos) {
        intents.push_back("security");
    }
    
    // Performance intents
    if (lower.find("performance") != std::string::npos ||
        lower.find("slow") != std::string::npos ||
        lower.find("optimize") != std::string::npos ||
        lower.find("efficient") != std::string::npos) {
        intents.push_back("performance");
    }
    
    if (intents.empty()) {
        intents.push_back("general");
    }
    
    return intents;
}

ComputeDecision ComputeController::buildDecision(ComputeStrategy strategy, const std::string& reasoning) {
    ComputeDecision decision;
    decision.strategy = strategy;
    decision.reasoning = reasoning;
    
    switch (strategy) {
        case ComputeStrategy::FAST:
            decision.ltm_top_k = config_.fast_defaults.ltm_top_k;
            decision.stm_top_k = config_.fast_defaults.stm_top_k;
            decision.mtm_top_k = config_.fast_defaults.mtm_top_k;
            decision.enable_cross_attention = config_.fast_defaults.enable_attention;
            decision.attention_heads = config_.fast_defaults.attention_heads;
            decision.memory_budget_mb = 512;
            break;
            
        case ComputeStrategy::BALANCED:
            decision.ltm_top_k = config_.balanced_defaults.ltm_top_k;
            decision.stm_top_k = config_.balanced_defaults.stm_top_k;
            decision.mtm_top_k = config_.balanced_defaults.mtm_top_k;
            decision.enable_cross_attention = config_.balanced_defaults.enable_attention;
            decision.attention_heads = config_.balanced_defaults.attention_heads;
            decision.memory_budget_mb = 1024;
            break;
            
        case ComputeStrategy::THOROUGH:
            decision.ltm_top_k = config_.thorough_defaults.ltm_top_k;
            decision.stm_top_k = config_.thorough_defaults.stm_top_k;
            decision.mtm_top_k = config_.thorough_defaults.mtm_top_k;
            decision.enable_cross_attention = config_.thorough_defaults.enable_attention;
            decision.attention_heads = config_.thorough_defaults.attention_heads;
            decision.memory_budget_mb = 2048;
            break;
    }
    
    return decision;
}

} // namespace aipr::tms
