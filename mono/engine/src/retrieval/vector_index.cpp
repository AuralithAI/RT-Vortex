/**
 * AI PR Reviewer - Vector Index
 * 
 * Vector similarity search (FAISS or fallback).
 */

#include "retriever.h"
#include "types.h"
#include <string>
#include <vector>
#include <unordered_map>
#include <algorithm>
#include <cmath>

namespace aipr {

/**
 * Compute cosine similarity between two vectors
 */
float cosineSimilarity(const std::vector<float>& a, const std::vector<float>& b) {
    if (a.size() != b.size() || a.empty()) return 0.0f;
    
    float dot = 0.0f, norm_a = 0.0f, norm_b = 0.0f;
    for (size_t i = 0; i < a.size(); ++i) {
        dot += a[i] * b[i];
        norm_a += a[i] * a[i];
        norm_b += b[i] * b[i];
    }
    
    float denom = std::sqrt(norm_a) * std::sqrt(norm_b);
    if (denom < 1e-8f) return 0.0f;
    
    return dot / denom;
}

/**
 * Normalize a vector to unit length
 */
void normalizeVector(std::vector<float>& vec) {
    float norm = 0.0f;
    for (float v : vec) {
        norm += v * v;
    }
    norm = std::sqrt(norm);
    
    if (norm > 1e-8f) {
        for (float& v : vec) {
            v /= norm;
        }
    }
}

/**
 * Simple brute-force vector index (fallback when FAISS is not available)
 */
class VectorIndex {
public:
    VectorIndex(size_t dimensions = 1536) : dimensions_(dimensions) {}
    
    /**
     * Add a chunk with its embedding
     */
    void addChunk(
        const std::string& repo_id,
        const std::string& chunk_id,
        std::vector<float> embedding
    ) {
        if (embedding.size() != dimensions_) {
            return;  // Dimension mismatch
        }
        
        normalizeVector(embedding);
        indices_[repo_id][chunk_id] = std::move(embedding);
    }
    
    /**
     * Remove a chunk
     */
    void removeChunk(const std::string& repo_id, const std::string& chunk_id) {
        auto it = indices_.find(repo_id);
        if (it != indices_.end()) {
            it->second.erase(chunk_id);
        }
    }
    
    /**
     * Search for similar chunks
     */
    std::vector<std::pair<std::string, float>> search(
        const std::string& repo_id,
        const std::vector<float>& query_embedding,
        size_t top_k = 20
    ) {
        std::vector<std::pair<std::string, float>> results;
        
        auto it = indices_.find(repo_id);
        if (it == indices_.end()) return results;
        
        // Normalize query
        std::vector<float> query = query_embedding;
        normalizeVector(query);
        
        // Brute-force search
        for (const auto& [chunk_id, embedding] : it->second) {
            float similarity = cosineSimilarity(query, embedding);
            results.push_back({chunk_id, similarity});
        }
        
        // Sort by similarity
        std::sort(results.begin(), results.end(),
            [](const auto& a, const auto& b) { return a.second > b.second; });
        
        // Limit to top_k
        if (results.size() > top_k) {
            results.resize(top_k);
        }
        
        return results;
    }
    
    /**
     * Clear index for a repository
     */
    void clear(const std::string& repo_id) {
        indices_.erase(repo_id);
    }
    
    /**
     * Get index statistics
     */
    struct Stats {
        size_t vector_count = 0;
        size_t dimensions = 0;
    };
    
    Stats getStats(const std::string& repo_id) const {
        Stats stats;
        stats.dimensions = dimensions_;
        
        auto it = indices_.find(repo_id);
        if (it != indices_.end()) {
            stats.vector_count = it->second.size();
        }
        
        return stats;
    }
    
    /**
     * Save index to disk
     */
    bool save(const std::string& repo_id, const std::string& path) {
        // TODO: Implement persistence
        return false;
    }
    
    /**
     * Load index from disk
     */
    bool load(const std::string& repo_id, const std::string& path) {
        // TODO: Implement persistence
        return false;
    }
    
private:
    size_t dimensions_;
    std::unordered_map<std::string, std::unordered_map<std::string, std::vector<float>>> indices_;
};

#ifdef AIPR_HAS_FAISS
// FAISS-based implementation for production use
// TODO: Implement FAISS wrapper
#endif

} // namespace aipr
