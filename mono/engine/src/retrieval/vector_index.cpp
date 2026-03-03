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
#include <fstream>

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
     * 
     * Binary format:
     *   [uint32_t] magic = 0x56494458 ("VIDX")
     *   [uint32_t] version = 1
     *   [uint64_t] dimensions
     *   [uint64_t] chunk_count
     *   For each chunk:
     *     [uint32_t] chunk_id_length
     *     [char...] chunk_id
     *     [float...] embedding (dimensions_ floats)
     */
    bool save(const std::string& repo_id, const std::string& path) {
        auto it = indices_.find(repo_id);
        if (it == indices_.end()) return false;

        std::ofstream out(path, std::ios::binary);
        if (!out) return false;

        const uint32_t magic = 0x56494458;
        const uint32_t version = 1;
        out.write(reinterpret_cast<const char*>(&magic), sizeof(magic));
        out.write(reinterpret_cast<const char*>(&version), sizeof(version));

        uint64_t dims = dimensions_;
        out.write(reinterpret_cast<const char*>(&dims), sizeof(dims));

        uint64_t count = it->second.size();
        out.write(reinterpret_cast<const char*>(&count), sizeof(count));

        for (const auto& [chunk_id, embedding] : it->second) {
            uint32_t id_len = static_cast<uint32_t>(chunk_id.size());
            out.write(reinterpret_cast<const char*>(&id_len), sizeof(id_len));
            out.write(chunk_id.data(), id_len);
            out.write(reinterpret_cast<const char*>(embedding.data()),
                       static_cast<std::streamsize>(dimensions_ * sizeof(float)));
        }

        return out.good();
    }
    
    /**
     * Load index from disk
     */
    bool load(const std::string& repo_id, const std::string& path) {
        std::ifstream in(path, std::ios::binary);
        if (!in) return false;

        uint32_t magic = 0, version = 0;
        in.read(reinterpret_cast<char*>(&magic), sizeof(magic));
        in.read(reinterpret_cast<char*>(&version), sizeof(version));
        if (magic != 0x56494458 || version != 1) return false;

        uint64_t dims = 0;
        in.read(reinterpret_cast<char*>(&dims), sizeof(dims));
        if (dims != dimensions_) return false; // dimension mismatch

        uint64_t count = 0;
        in.read(reinterpret_cast<char*>(&count), sizeof(count));
        if (count > 100'000'000) return false; // sanity

        auto& repo_index = indices_[repo_id];
        repo_index.clear();

        for (uint64_t i = 0; i < count && in.good(); ++i) {
            uint32_t id_len = 0;
            in.read(reinterpret_cast<char*>(&id_len), sizeof(id_len));
            if (id_len > 10'000'000) return false;
            std::string chunk_id(id_len, '\0');
            in.read(chunk_id.data(), id_len);

            std::vector<float> embedding(dimensions_);
            in.read(reinterpret_cast<char*>(embedding.data()),
                     static_cast<std::streamsize>(dimensions_ * sizeof(float)));
            repo_index[std::move(chunk_id)] = std::move(embedding);
        }

        return in.good() || in.eof();
    }
    
private:
    size_t dimensions_;
    std::unordered_map<std::string, std::unordered_map<std::string, std::vector<float>>> indices_;
};

} // namespace aipr

#ifdef AIPR_HAS_FAISS
#include "faiss_index.h"

namespace aipr {

/**
 * FAISS-backed vector index for production use.
 * Uses FAISSIndex wrapper around faiss::IndexIDMap<IndexFlatL2>.
 * Falls back to VectorIndex (brute-force) if FAISS is not compiled in.
 */
class FAISSVectorIndex {
public:
    FAISSVectorIndex(size_t dimensions = 1536)
        : dimensions_(dimensions)
    {}

    void addChunk(
        const std::string& repo_id,
        const std::string& chunk_id,
        std::vector<float> embedding
    ) {
        if (embedding.size() != dimensions_) return;
        normalizeVector(embedding);

        auto& repo = repos_[repo_id];
        if (!repo.index) {
            repo.index = std::make_unique<FAISSIndex>(dimensions_);
        }

        int64_t id = static_cast<int64_t>(repo.id_to_chunk.size());
        repo.chunk_to_id[chunk_id] = id;
        repo.id_to_chunk[id] = chunk_id;
        repo.index->add(id, embedding);
    }

    void removeChunk(const std::string& repo_id, const std::string& chunk_id) {
        auto rit = repos_.find(repo_id);
        if (rit == repos_.end()) return;
        auto cit = rit->second.chunk_to_id.find(chunk_id);
        if (cit == rit->second.chunk_to_id.end()) return;
        rit->second.index->remove(cit->second);
        rit->second.id_to_chunk.erase(cit->second);
        rit->second.chunk_to_id.erase(cit);
    }

    std::vector<std::pair<std::string, float>> search(
        const std::string& repo_id,
        const std::vector<float>& query_embedding,
        size_t top_k = 20
    ) {
        std::vector<std::pair<std::string, float>> results;
        auto rit = repos_.find(repo_id);
        if (rit == repos_.end()) return results;

        std::vector<float> query = query_embedding;
        normalizeVector(query);

        auto raw = rit->second.index->search(query, static_cast<int>(top_k));
        for (const auto& [id, distance] : raw) {
            auto it = rit->second.id_to_chunk.find(id);
            if (it != rit->second.id_to_chunk.end()) {
                // FAISS returns L2 distance; convert to similarity (1 / (1 + d))
                float similarity = 1.0f / (1.0f + distance);
                results.push_back({it->second, similarity});
            }
        }
        return results;
    }

    void clear(const std::string& repo_id) {
        repos_.erase(repo_id);
    }

    bool save(const std::string& repo_id, const std::string& path) {
        auto rit = repos_.find(repo_id);
        if (rit == repos_.end() || !rit->second.index) return false;
        rit->second.index->save(path);
        return true;
    }

    bool load(const std::string& repo_id, const std::string& path) {
        auto& repo = repos_[repo_id];
        repo.index = std::make_unique<FAISSIndex>(dimensions_);
        repo.index->load(path);
        return true;
    }

private:
    struct RepoData {
        std::unique_ptr<FAISSIndex> index;
        std::unordered_map<std::string, int64_t> chunk_to_id;
        std::unordered_map<int64_t, std::string> id_to_chunk;
    };

    size_t dimensions_;
    std::unordered_map<std::string, RepoData> repos_;
};

} // namespace aipr
#endif // AIPR_HAS_FAISS
