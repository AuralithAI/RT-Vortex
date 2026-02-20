/**
 * AI PR Reviewer - Lexical Index
 * 
 * Trigram-based text search for fast filtering.
 */

#include "retriever.h"
#include "types.h"
#include <string>
#include <vector>
#include <unordered_map>
#include <unordered_set>
#include <algorithm>
#include <cctype>

namespace aipr {

/**
 * Extract trigrams from text
 */
std::vector<std::string> extractTrigrams(const std::string& text) {
    std::vector<std::string> trigrams;
    
    // Normalize text
    std::string normalized;
    normalized.reserve(text.size());
    for (char c : text) {
        if (std::isalnum(static_cast<unsigned char>(c))) {
            normalized += std::tolower(static_cast<unsigned char>(c));
        } else if (!normalized.empty() && normalized.back() != ' ') {
            normalized += ' ';
        }
    }
    
    // Extract trigrams
    if (normalized.size() >= 3) {
        for (size_t i = 0; i <= normalized.size() - 3; ++i) {
            trigrams.push_back(normalized.substr(i, 3));
        }
    }
    
    return trigrams;
}

/**
 * Lexical index using trigrams
 */
class LexicalIndex {
public:
    /**
     * Add a chunk to the index
     */
    void addChunk(const std::string& repo_id, const Chunk& chunk) {
        auto& repo_index = indices_[repo_id];
        
        auto trigrams = extractTrigrams(chunk.content);
        for (const auto& trigram : trigrams) {
            repo_index.trigram_to_chunks[trigram].insert(chunk.id);
        }
        
        repo_index.chunks[chunk.id] = chunk;
    }
    
    /**
     * Remove a chunk from the index
     */
    void removeChunk(const std::string& repo_id, const std::string& chunk_id) {
        auto it = indices_.find(repo_id);
        if (it == indices_.end()) return;
        
        auto& repo_index = it->second;
        
        // Find chunk and remove its trigrams
        auto chunk_it = repo_index.chunks.find(chunk_id);
        if (chunk_it != repo_index.chunks.end()) {
            auto trigrams = extractTrigrams(chunk_it->second.content);
            for (const auto& trigram : trigrams) {
                repo_index.trigram_to_chunks[trigram].erase(chunk_id);
            }
            repo_index.chunks.erase(chunk_it);
        }
    }
    
    /**
     * Search for chunks matching query
     */
    std::vector<std::pair<std::string, float>> search(
        const std::string& repo_id,
        const std::string& query,
        size_t top_k = 100
    ) {
        std::vector<std::pair<std::string, float>> results;
        
        auto it = indices_.find(repo_id);
        if (it == indices_.end()) return results;
        
        const auto& repo_index = it->second;
        auto query_trigrams = extractTrigrams(query);
        
        if (query_trigrams.empty()) return results;
        
        // Count matching trigrams per chunk
        std::unordered_map<std::string, size_t> chunk_matches;
        
        for (const auto& trigram : query_trigrams) {
            auto trig_it = repo_index.trigram_to_chunks.find(trigram);
            if (trig_it != repo_index.trigram_to_chunks.end()) {
                for (const auto& chunk_id : trig_it->second) {
                    chunk_matches[chunk_id]++;
                }
            }
        }
        
        // Calculate scores (Jaccard-like similarity)
        for (const auto& [chunk_id, matches] : chunk_matches) {
            float score = static_cast<float>(matches) / 
                         static_cast<float>(query_trigrams.size());
            results.push_back({chunk_id, score});
        }
        
        // Sort by score
        std::sort(results.begin(), results.end(),
            [](const auto& a, const auto& b) { return a.second > b.second; });
        
        // Limit to top_k
        if (results.size() > top_k) {
            results.resize(top_k);
        }
        
        return results;
    }
    
    /**
     * Get chunk by ID
     */
    const Chunk* getChunk(const std::string& repo_id, const std::string& chunk_id) const {
        auto it = indices_.find(repo_id);
        if (it == indices_.end()) return nullptr;
        
        auto chunk_it = it->second.chunks.find(chunk_id);
        if (chunk_it == it->second.chunks.end()) return nullptr;
        
        return &chunk_it->second;
    }
    
    /**
     * Get all chunks for a file
     */
    std::vector<Chunk> getChunksByFile(
        const std::string& repo_id,
        const std::string& file_path
    ) const {
        std::vector<Chunk> result;
        
        auto it = indices_.find(repo_id);
        if (it == indices_.end()) return result;
        
        for (const auto& [id, chunk] : it->second.chunks) {
            if (chunk.file_path == file_path) {
                result.push_back(chunk);
            }
        }
        
        // Sort by line number
        std::sort(result.begin(), result.end(),
            [](const Chunk& a, const Chunk& b) { return a.start_line < b.start_line; });
        
        return result;
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
        size_t chunk_count = 0;
        size_t trigram_count = 0;
    };
    
    Stats getStats(const std::string& repo_id) const {
        Stats stats;
        
        auto it = indices_.find(repo_id);
        if (it != indices_.end()) {
            stats.chunk_count = it->second.chunks.size();
            stats.trigram_count = it->second.trigram_to_chunks.size();
        }
        
        return stats;
    }
    
private:
    struct RepoIndex {
        std::unordered_map<std::string, Chunk> chunks;
        std::unordered_map<std::string, std::unordered_set<std::string>> trigram_to_chunks;
    };
    
    std::unordered_map<std::string, RepoIndex> indices_;
};

} // namespace aipr
