/**
 * AI PR Reviewer - Long-Term Memory Implementation
 * 
 * Persistent knowledge base using FAISS for vector search.
 */

#include "memory_system.h"
#include <fstream>
#include <sstream>
#include <algorithm>
#include <cmath>

#ifdef AIPR_HAS_FAISS
#include <faiss/IndexFlat.h>
#include <faiss/IndexIDMap.h>
#include <faiss/index_io.h>
#endif

namespace aipr {

// =============================================================================
// FAISS Index Wrapper
// =============================================================================

class FAISSIndex {
public:
    explicit FAISSIndex(size_t dimension) : dimension_(dimension) {
#ifdef AIPR_HAS_FAISS
        // Use L2 distance with ID mapping
        auto flat_index = new faiss::IndexFlatL2(dimension);
        index_ = new faiss::IndexIDMap(flat_index);
#endif
    }
    
    ~FAISSIndex() {
#ifdef AIPR_HAS_FAISS
        delete index_;
#endif
    }
    
    void add(int64_t id, const std::vector<float>& embedding) {
#ifdef AIPR_HAS_FAISS
        if (embedding.size() != dimension_) {
            throw std::invalid_argument("Embedding dimension mismatch");
        }
        index_->add_with_ids(1, embedding.data(), &id);
#endif
    }
    
    void addBatch(const std::vector<int64_t>& ids, const std::vector<float>& embeddings) {
#ifdef AIPR_HAS_FAISS
        if (embeddings.size() != ids.size() * dimension_) {
            throw std::invalid_argument("Embedding count mismatch");
        }
        index_->add_with_ids(ids.size(), embeddings.data(), ids.data());
#endif
    }
    
    std::vector<std::pair<int64_t, float>> search(
        const std::vector<float>& query, 
        int top_k
    ) {
        std::vector<std::pair<int64_t, float>> results;
        
#ifdef AIPR_HAS_FAISS
        std::vector<float> distances(top_k);
        std::vector<faiss::idx_t> labels(top_k);
        
        index_->search(1, query.data(), top_k, distances.data(), labels.data());
        
        for (int i = 0; i < top_k; ++i) {
            if (labels[i] >= 0) {
                results.emplace_back(labels[i], distances[i]);
            }
        }
#else
        // Brute-force fallback would go here
        (void)query;
        (void)top_k;
#endif
        
        return results;
    }
    
    void remove(int64_t id) {
#ifdef AIPR_HAS_FAISS
        faiss::IDSelectorArray selector(1, &id);
        index_->remove_ids(selector);
#else
        (void)id;
#endif
    }
    
    size_t size() const {
#ifdef AIPR_HAS_FAISS
        return index_->ntotal;
#else
        return 0;
#endif
    }
    
    void save(const std::string& path) {
#ifdef AIPR_HAS_FAISS
        faiss::write_index(index_, path.c_str());
#else
        (void)path;
#endif
    }
    
    void load(const std::string& path) {
#ifdef AIPR_HAS_FAISS
        delete index_;
        index_ = faiss::read_index(path.c_str());
#else
        (void)path;
#endif
    }

private:
    size_t dimension_;
#ifdef AIPR_HAS_FAISS
    faiss::Index* index_ = nullptr;
#endif
};

// =============================================================================
// LongTermMemory Implementation
// =============================================================================

LongTermMemory::LongTermMemory(const MemoryConfig& config)
    : config_(config) {
    faiss_index_ = std::make_unique<FAISSIndex>(config.ltm_dimension);
}

LongTermMemory::~LongTermMemory() = default;

void LongTermMemory::store(const CodeMemory& memory) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Generate numeric ID from string ID
    int64_t numeric_id = std::hash<std::string>{}(memory.id);
    
    // Add to FAISS index
    faiss_index_->add(numeric_id, memory.embedding);
    
    // Store in map
    memories_[memory.id] = memory;
    
    // Update repo index
    repo_index_[memory.repo_id].push_back(memory.id);
}

void LongTermMemory::storeBatch(const std::vector<CodeMemory>& memories) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<int64_t> ids;
    std::vector<float> embeddings;
    
    ids.reserve(memories.size());
    embeddings.reserve(memories.size() * config_.ltm_dimension);
    
    for (const auto& memory : memories) {
        int64_t numeric_id = std::hash<std::string>{}(memory.id);
        ids.push_back(numeric_id);
        
        embeddings.insert(embeddings.end(), 
                          memory.embedding.begin(), 
                          memory.embedding.end());
        
        memories_[memory.id] = memory;
        repo_index_[memory.repo_id].push_back(memory.id);
    }
    
    faiss_index_->addBatch(ids, embeddings);
}

std::vector<CodeMemory> LongTermMemory::retrieve(
    const std::vector<float>& query_embedding,
    int top_k,
    const std::string& repo_filter
) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (top_k < 0) top_k = config_.ltm_top_k;
    
    std::vector<CodeMemory> results;
    
    // Search FAISS
    auto search_results = faiss_index_->search(query_embedding, top_k * 2);
    
    // Build ID to memory map for quick lookup
    std::unordered_map<int64_t, std::string> id_to_key;
    for (const auto& [key, memory] : memories_) {
        int64_t numeric_id = std::hash<std::string>{}(key);
        id_to_key[numeric_id] = key;
    }
    
    // Convert results
    for (const auto& [id, distance] : search_results) {
        auto it = id_to_key.find(id);
        if (it == id_to_key.end()) continue;
        
        const auto& memory = memories_[it->second];
        
        // Apply repo filter
        if (!repo_filter.empty() && memory.repo_id != repo_filter) {
            continue;
        }
        
        // Apply similarity threshold
        float similarity = 1.0f / (1.0f + distance);  // Convert distance to similarity
        if (similarity < config_.ltm_similarity_threshold) {
            continue;
        }
        
        results.push_back(memory);
        
        if (static_cast<int>(results.size()) >= top_k) break;
    }
    
    return results;
}

std::vector<CodeMemory> LongTermMemory::retrieveByQuery(
    const std::string& query,
    int top_k,
    const std::string& repo_filter
) {
    // This would normally compute embedding - for now return empty
    // In production, call embedding service
    (void)query;
    (void)top_k;
    (void)repo_filter;
    return {};
}

std::vector<CodeMemory> LongTermMemory::hybridRetrieve(
    const std::string& query,
    const std::vector<float>& query_embedding,
    int top_k,
    double alpha
) {
    // Vector search
    auto vector_results = retrieve(query_embedding, top_k * 2, "");
    
    // Lexical search (simplified - would use BM25 in production)
    // For now, just filter by keyword presence
    std::vector<CodeMemory> lexical_results;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        for (const auto& [key, memory] : memories_) {
            if (memory.content.find(query) != std::string::npos) {
                lexical_results.push_back(memory);
                if (static_cast<int>(lexical_results.size()) >= top_k * 2) break;
            }
        }
    }
    
    // Merge with RRF (Reciprocal Rank Fusion)
    std::unordered_map<std::string, double> scores;
    const double k = 60.0;  // RRF parameter
    
    for (size_t i = 0; i < vector_results.size(); ++i) {
        scores[vector_results[i].id] += alpha * (1.0 / (k + i + 1));
    }
    
    for (size_t i = 0; i < lexical_results.size(); ++i) {
        scores[lexical_results[i].id] += (1.0 - alpha) * (1.0 / (k + i + 1));
    }
    
    // Sort by combined score
    std::vector<std::pair<std::string, double>> sorted_scores(scores.begin(), scores.end());
    std::sort(sorted_scores.begin(), sorted_scores.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });
    
    // Build result
    std::vector<CodeMemory> results;
    {
        std::lock_guard<std::mutex> lock(mutex_);
        for (const auto& [id, score] : sorted_scores) {
            if (static_cast<int>(results.size()) >= top_k) break;
            
            auto it = memories_.find(id);
            if (it != memories_.end()) {
                results.push_back(it->second);
            }
        }
    }
    
    return results;
}

std::optional<CodeMemory> LongTermMemory::get(const std::string& id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = memories_.find(id);
    if (it != memories_.end()) {
        return it->second;
    }
    return std::nullopt;
}

bool LongTermMemory::remove(const std::string& id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = memories_.find(id);
    if (it == memories_.end()) {
        return false;
    }
    
    // Remove from FAISS
    int64_t numeric_id = std::hash<std::string>{}(id);
    faiss_index_->remove(numeric_id);
    
    // Remove from repo index
    auto& repo_ids = repo_index_[it->second.repo_id];
    repo_ids.erase(std::remove(repo_ids.begin(), repo_ids.end(), id), repo_ids.end());
    
    // Remove from map
    memories_.erase(it);
    
    return true;
}

size_t LongTermMemory::removeByRepo(const std::string& repo_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = repo_index_.find(repo_id);
    if (it == repo_index_.end()) {
        return 0;
    }
    
    size_t count = 0;
    for (const auto& id : it->second) {
        int64_t numeric_id = std::hash<std::string>{}(id);
        faiss_index_->remove(numeric_id);
        memories_.erase(id);
        count++;
    }
    
    repo_index_.erase(it);
    
    return count;
}

void LongTermMemory::updateImportance(const std::string& id, double delta) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = memories_.find(id);
    if (it != memories_.end()) {
        it->second.importance_score += delta;
        it->second.access_count++;
        it->second.last_accessed = std::chrono::system_clock::now();
    }
}

size_t LongTermMemory::consolidate(double threshold) {
    if (threshold < 0) threshold = config_.consolidation_threshold;
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<std::string> to_remove;
    
    for (const auto& [id, memory] : memories_) {
        if (memory.importance_score < threshold) {
            to_remove.push_back(id);
        }
    }
    
    for (const auto& id : to_remove) {
        int64_t numeric_id = std::hash<std::string>{}(id);
        faiss_index_->remove(numeric_id);
        
        auto& repo_ids = repo_index_[memories_[id].repo_id];
        repo_ids.erase(std::remove(repo_ids.begin(), repo_ids.end(), id), repo_ids.end());
        
        memories_.erase(id);
    }
    
    return to_remove.size();
}

void LongTermMemory::persist() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (config_.storage_path.empty()) return;
    
    // Save FAISS index
    faiss_index_->save(config_.storage_path + "/ltm_index.faiss");
    
    // Save memories as JSON
    // (In production, use a proper serialization format)
    std::ofstream file(config_.storage_path + "/ltm_memories.json");
    // Serialization would go here
}

void LongTermMemory::load() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (config_.storage_path.empty()) return;
    
    // Load FAISS index
    faiss_index_->load(config_.storage_path + "/ltm_index.faiss");
    
    // Load memories from JSON
    // Deserialization would go here
}

LongTermMemory::Stats LongTermMemory::getStats() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    Stats stats;
    stats.total_items = memories_.size();
    stats.total_repos = repo_index_.size();
    
    double total_importance = 0;
    for (const auto& [id, memory] : memories_) {
        total_importance += memory.importance_score;
    }
    
    if (!memories_.empty()) {
        stats.avg_importance = total_importance / memories_.size();
    }
    
    // Rough memory estimate
    stats.memory_bytes = memories_.size() * (sizeof(CodeMemory) + config_.ltm_dimension * sizeof(float));
    
    return stats;
}

void LongTermMemory::rebuildIndex() {
    // Would rebuild FAISS index from memories
}

} // namespace aipr
