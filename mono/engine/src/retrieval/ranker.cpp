/**
 * AI PR Reviewer - Result Ranker
 * 
 * Merges and ranks results from multiple retrieval sources.
 */

#include "retriever.h"
#include "types.h"
#include <string>
#include <vector>
#include <unordered_map>
#include <unordered_set>
#include <algorithm>
#include <cmath>

namespace aipr {

/**
 * Ranker configuration
 */
struct RankerConfig {
    float lexical_weight = 0.3f;
    float vector_weight = 0.7f;
    float graph_boost = 0.1f;       // Boost for chunks containing related symbols
    float recency_boost = 0.05f;    // Boost for recently modified files
    float path_boost = 0.1f;        // Boost for files in similar paths
    bool enable_mmr = true;         // Maximum Marginal Relevance for diversity
    float mmr_lambda = 0.5f;        // MMR lambda (0 = diversity, 1 = relevance)
};

/**
 * Intermediate result for ranking
 */
struct RankingCandidate {
    std::string chunk_id;
    Chunk chunk;
    float lexical_score = 0.0f;
    float vector_score = 0.0f;
    float graph_score = 0.0f;
    float final_score = 0.0f;
    std::vector<std::string> matching_symbols;
};

/**
 * Result ranker
 */
class Ranker {
public:
    Ranker(const RankerConfig& config = {}) : config_(config) {}
    
    /**
     * Merge and rank results from multiple sources
     */
    std::vector<SearchResult> rank(
        const std::vector<std::pair<std::string, float>>& lexical_results,
        const std::vector<std::pair<std::string, float>>& vector_results,
        const std::unordered_map<std::string, Chunk>& chunks,
        const std::unordered_set<std::string>& related_symbols = {},
        size_t top_k = 20
    ) {
        // Collect all candidates
        std::unordered_map<std::string, RankingCandidate> candidates;
        
        // Add lexical results
        for (const auto& [chunk_id, score] : lexical_results) {
            auto& candidate = candidates[chunk_id];
            candidate.chunk_id = chunk_id;
            candidate.lexical_score = score;
        }
        
        // Add vector results
        for (const auto& [chunk_id, score] : vector_results) {
            auto& candidate = candidates[chunk_id];
            candidate.chunk_id = chunk_id;
            candidate.vector_score = score;
        }
        
        // Populate chunks and calculate graph boost
        for (auto& [chunk_id, candidate] : candidates) {
            auto chunk_it = chunks.find(chunk_id);
            if (chunk_it != chunks.end()) {
                candidate.chunk = chunk_it->second;
                
                // Check for related symbols
                for (const auto& symbol : candidate.chunk.symbols) {
                    if (related_symbols.find(symbol) != related_symbols.end()) {
                        candidate.matching_symbols.push_back(symbol);
                        candidate.graph_score += config_.graph_boost;
                    }
                }
            }
        }
        
        // Calculate final scores
        for (auto& [chunk_id, candidate] : candidates) {
            candidate.final_score = 
                config_.lexical_weight * candidate.lexical_score +
                config_.vector_weight * candidate.vector_score +
                candidate.graph_score;
        }
        
        // Convert to vector for sorting
        std::vector<RankingCandidate> sorted_candidates;
        sorted_candidates.reserve(candidates.size());
        for (auto& [_, candidate] : candidates) {
            sorted_candidates.push_back(std::move(candidate));
        }
        
        // Apply MMR for diversity if enabled
        std::vector<SearchResult> results;
        if (config_.enable_mmr && sorted_candidates.size() > 1) {
            results = applyMMR(sorted_candidates, top_k);
        } else {
            // Simple sort by score
            std::sort(sorted_candidates.begin(), sorted_candidates.end(),
                [](const RankingCandidate& a, const RankingCandidate& b) {
                    return a.final_score > b.final_score;
                });
            
            for (size_t i = 0; i < std::min(top_k, sorted_candidates.size()); ++i) {
                SearchResult result;
                result.chunk = sorted_candidates[i].chunk;
                result.score = sorted_candidates[i].final_score;
                result.lexical_score = sorted_candidates[i].lexical_score;
                result.vector_score = sorted_candidates[i].vector_score;
                result.graph_score = sorted_candidates[i].graph_score;
                results.push_back(result);
            }
        }
        
        return results;
    }
    
private:
    RankerConfig config_;
    
    /**
     * Apply Maximum Marginal Relevance for result diversity
     */
    std::vector<SearchResult> applyMMR(
        std::vector<RankingCandidate>& candidates,
        size_t top_k
    ) {
        std::vector<SearchResult> results;
        std::vector<bool> selected(candidates.size(), false);
        
        // Precompute file paths for diversity
        std::vector<std::string> file_paths;
        for (const auto& c : candidates) {
            file_paths.push_back(c.chunk.file_path);
        }
        
        while (results.size() < top_k && results.size() < candidates.size()) {
            float best_mmr = -1.0f;
            size_t best_idx = 0;
            
            for (size_t i = 0; i < candidates.size(); ++i) {
                if (selected[i]) continue;
                
                float relevance = candidates[i].final_score;
                
                // Calculate max similarity to already selected results
                float max_similarity = 0.0f;
                for (size_t j = 0; j < candidates.size(); ++j) {
                    if (selected[j]) {
                        float similarity = computeSimilarity(candidates[i], candidates[j]);
                        max_similarity = std::max(max_similarity, similarity);
                    }
                }
                
                // MMR score
                float mmr = config_.mmr_lambda * relevance - 
                           (1.0f - config_.mmr_lambda) * max_similarity;
                
                if (mmr > best_mmr) {
                    best_mmr = mmr;
                    best_idx = i;
                }
            }
            
            selected[best_idx] = true;
            
            SearchResult result;
            result.chunk = candidates[best_idx].chunk;
            result.score = candidates[best_idx].final_score;
            result.lexical_score = candidates[best_idx].lexical_score;
            result.vector_score = candidates[best_idx].vector_score;
            result.graph_score = candidates[best_idx].graph_score;
            results.push_back(result);
        }
        
        return results;
    }
    
    /**
     * Compute similarity between two candidates for MMR
     */
    float computeSimilarity(
        const RankingCandidate& a,
        const RankingCandidate& b
    ) {
        float similarity = 0.0f;
        
        // Same file penalty
        if (a.chunk.file_path == b.chunk.file_path) {
            similarity += 0.5f;
            
            // Overlapping line ranges
            bool overlaps = !(a.chunk.end_line < b.chunk.start_line || 
                            b.chunk.end_line < a.chunk.start_line);
            if (overlaps) {
                similarity += 0.3f;
            }
        }
        
        // Same language
        if (a.chunk.language == b.chunk.language) {
            similarity += 0.1f;
        }
        
        // Shared symbols
        std::unordered_set<std::string> a_symbols(
            a.chunk.symbols.begin(), a.chunk.symbols.end()
        );
        for (const auto& sym : b.chunk.symbols) {
            if (a_symbols.find(sym) != a_symbols.end()) {
                similarity += 0.1f;
            }
        }
        
        return std::min(similarity, 1.0f);
    }
};

} // namespace aipr
