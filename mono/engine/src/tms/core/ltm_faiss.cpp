/**
 * TMS Long-Term Memory (LTM) - FAISS Implementation
 * 
 * Production-grade vector search for massive code repositories.
 */

#include "tms/ltm_faiss.h"
#include "tms/memory_accounts.h"
#include "logging.h"
#include "metrics.h"
#include <fstream>
#include <sstream>
#include <algorithm>
#include <cmath>
#include <filesystem>
#include <numeric>
#include <cstring>

#ifdef AIPR_HAS_MINIZ
#include <miniz.h>
#endif

#ifdef AIPR_HAS_FAISS
#include <faiss/IndexFlat.h>
#include <faiss/IndexIVFFlat.h>
#include <faiss/IndexIVFPQ.h>
#include <faiss/IndexHNSW.h>
#include <faiss/IndexIDMap.h>
#include <faiss/index_io.h>
#include <faiss/MetricType.h>
#endif

namespace aipr::tms {

// =============================================================================
// Lexical Index (BM25-style)
// =============================================================================

class LTMFaiss::LexicalIndex {
public:
    void add(const std::string& id, const std::string& content) {
        // Tokenize content
        auto tokens = tokenize(content);
        
        // Update document frequency
        std::unordered_set<std::string> unique_tokens(tokens.begin(), tokens.end());
        for (const auto& token : unique_tokens) {
            doc_freq_[token]++;
        }
        
        // Store term frequencies
        std::unordered_map<std::string, int> tf;
        for (const auto& token : tokens) {
            tf[token]++;
        }
        doc_terms_[id] = tf;
        doc_lengths_[id] = tokens.size();
        
        total_docs_++;
        avg_doc_length_ = (avg_doc_length_ * (total_docs_ - 1) + tokens.size()) / total_docs_;
    }
    
    void remove(const std::string& id) {
        auto it = doc_terms_.find(id);
        if (it != doc_terms_.end()) {
            // Update document frequency
            for (const auto& [token, _] : it->second) {
                if (--doc_freq_[token] == 0) {
                    doc_freq_.erase(token);
                }
            }
            
            // Update average
            if (total_docs_ > 1) {
                avg_doc_length_ = (avg_doc_length_ * total_docs_ - doc_lengths_[id]) / (total_docs_ - 1);
            } else {
                avg_doc_length_ = 0;
            }
            
            doc_terms_.erase(id);
            doc_lengths_.erase(id);
            total_docs_--;
        }
    }
    
    std::vector<std::pair<std::string, float>> search(const std::string& query, int top_k) {
        auto query_tokens = tokenize(query);
        
        std::unordered_map<std::string, float> scores;
        
        // BM25 scoring
        const float k1 = 1.2f;
        const float b = 0.75f;
        
        for (const auto& [doc_id, terms] : doc_terms_) {
            float score = 0.0f;
            float doc_len = static_cast<float>(doc_lengths_[doc_id]);
            
            for (const auto& token : query_tokens) {
                auto tf_it = terms.find(token);
                if (tf_it == terms.end()) continue;
                
                float tf = static_cast<float>(tf_it->second);
                auto df_it = doc_freq_.find(token);
                float df = df_it != doc_freq_.end() ? static_cast<float>(df_it->second) : 0.0f;
                
                // IDF
                float idf = std::log((total_docs_ - df + 0.5f) / (df + 0.5f) + 1.0f);
                
                // TF normalization
                float tf_norm = (tf * (k1 + 1.0f)) / 
                               (tf + k1 * (1.0f - b + b * doc_len / avg_doc_length_));
                
                score += idf * tf_norm;
            }
            
            if (score > 0) {
                scores[doc_id] = score;
            }
        }
        
        // Sort by score
        std::vector<std::pair<std::string, float>> results(scores.begin(), scores.end());
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
    
private:
    std::unordered_map<std::string, std::unordered_map<std::string, int>> doc_terms_;
    std::unordered_map<std::string, size_t> doc_lengths_;
    std::unordered_map<std::string, int> doc_freq_;
    size_t total_docs_ = 0;
    float avg_doc_length_ = 0.0f;
    
    std::vector<std::string> tokenize(const std::string& text) {
        std::vector<std::string> tokens;
        std::string current;
        
        for (char c : text) {
            if (std::isalnum(c) || c == '_') {
                current += std::tolower(c);
            } else if (!current.empty()) {
                if (current.length() >= 2 && current.length() <= 50) {
                    tokens.push_back(current);
                }
                current.clear();
            }
        }
        
        if (!current.empty() && current.length() >= 2 && current.length() <= 50) {
            tokens.push_back(current);
        }
        
        return tokens;
    }
};

// =============================================================================
// FAISS Index Implementation
// =============================================================================

class LTMFaiss::FAISSIndexImpl {
public:
    explicit FAISSIndexImpl(const LTMConfig& config) : config_(config) {
    }
    
    ~FAISSIndexImpl() {
#ifdef AIPR_HAS_FAISS
        delete index_;
        index_ = nullptr;
#endif
    }
    
    void initialize(FAISSIndexType type) {
#ifdef AIPR_HAS_FAISS
        type_ = type;
        
        switch (type) {
            case FAISSIndexType::FLAT_L2: {
                auto flat = new faiss::IndexFlatL2(config_.dimension);
                index_ = new faiss::IndexIDMap(flat);
                trained_ = true;
                break;
            }
            case FAISSIndexType::FLAT_IP: {
                auto flat = new faiss::IndexFlatIP(config_.dimension);
                index_ = new faiss::IndexIDMap(flat);
                trained_ = true;
                break;
            }
            case FAISSIndexType::IVF_FLAT: {
                auto quantizer = new faiss::IndexFlatL2(config_.dimension);
                auto ivf = new faiss::IndexIVFFlat(quantizer, config_.dimension, 
                                                   config_.nlist, faiss::METRIC_L2);
                ivf->own_fields = true;
                ivf->nprobe = config_.nprobe;
                index_ = new faiss::IndexIDMap(ivf);
                trained_ = false;
                break;
            }
            case FAISSIndexType::IVF_PQ: {
                auto quantizer = new faiss::IndexFlatL2(config_.dimension);
                auto ivf = new faiss::IndexIVFPQ(quantizer, config_.dimension,
                                                  config_.nlist, config_.pq_m, config_.pq_bits);
                ivf->own_fields = true;
                ivf->nprobe = config_.nprobe;
                index_ = new faiss::IndexIDMap(ivf);
                trained_ = false;
                break;
            }
            case FAISSIndexType::HNSW_FLAT: {
                auto hnsw = new faiss::IndexHNSWFlat(config_.dimension, config_.hnsw_m);
                hnsw->hnsw.efConstruction = config_.hnsw_ef_construction;
                hnsw->hnsw.efSearch = config_.hnsw_ef_search;
                index_ = new faiss::IndexIDMap(hnsw);
                trained_ = true;
                break;
            }
            default:
                // AUTO - will be set later based on data size
                if (config_.use_cosine_similarity) {
                    type_ = FAISSIndexType::FLAT_IP;
                    auto flat = new faiss::IndexFlatIP(config_.dimension);
                    index_ = new faiss::IndexIDMap(flat);
                } else {
                    type_ = FAISSIndexType::FLAT_L2;
                    auto flat = new faiss::IndexFlatL2(config_.dimension);
                    index_ = new faiss::IndexIDMap(flat);
                }
                trained_ = true;
                break;
        }
#endif
    }
    
    void add(int64_t id, const std::vector<float>& embedding) {
#ifdef AIPR_HAS_FAISS
        if (!index_ || !trained_) return;
        index_->add_with_ids(1, embedding.data(), &id);
#else
        (void)id; (void)embedding;
#endif
    }
    
    void addBatch(const std::vector<int64_t>& ids, const std::vector<float>& embeddings) {
#ifdef AIPR_HAS_FAISS
        if (!index_ || !trained_) return;
        index_->add_with_ids(ids.size(), embeddings.data(), ids.data());
#else
        (void)ids; (void)embeddings;
#endif
    }
    
    std::pair<std::vector<int64_t>, std::vector<float>> search(
        const std::vector<float>& query,
        int top_k
    ) {
        std::vector<int64_t> ids(top_k, -1);
        std::vector<float> distances(top_k, std::numeric_limits<float>::max());
        
#ifdef AIPR_HAS_FAISS
        if (!index_ || index_->ntotal == 0) {
            return {ids, distances};
        }
        
        index_->search(1, query.data(), top_k, distances.data(), ids.data());
#else
        (void)query;
#endif
        
        return {ids, distances};
    }
    
    void remove(int64_t id) {
#ifdef AIPR_HAS_FAISS
        if (!index_) return;
        faiss::IDSelectorArray selector(1, &id);
        index_->remove_ids(selector);
#else
        (void)id;
#endif
    }
    
    void train(const std::vector<float>& training_data, size_t n_vectors) {
#ifdef AIPR_HAS_FAISS
        if (!index_) return;
        
        // Check if underlying index needs training
        auto id_map = dynamic_cast<faiss::IndexIDMap*>(index_);
        if (id_map) {
            if (!id_map->index->is_trained) {
                id_map->index->train(n_vectors, training_data.data());
            }
        }
        trained_ = true;
#else
        (void)training_data; (void)n_vectors;
#endif
    }
    
    bool isTrained() const { return trained_; }
    
    size_t size() const {
#ifdef AIPR_HAS_FAISS
        return index_ ? index_->ntotal : 0;
#else
        return 0;
#endif
    }
    
    void save(const std::string& path) {
#ifdef AIPR_HAS_FAISS
        if (index_) {
            faiss::write_index(index_, (path + "/faiss.index").c_str());
        }
#else
        (void)path;
#endif
    }
    
    void load(const std::string& path) {
#ifdef AIPR_HAS_FAISS
        std::string index_path = path + "/faiss.index";
        if (std::filesystem::exists(index_path)) {
            if (index_) {
                delete index_;
            }
            index_ = faiss::read_index(index_path.c_str());
            trained_ = true;
        }
#else
        (void)path;
#endif
    }
    
    FAISSIndexType getType() const { return type_; }

private:
    LTMConfig config_;
    FAISSIndexType type_ = FAISSIndexType::FLAT_L2;
    bool trained_ = false;
    
#ifdef AIPR_HAS_FAISS
    faiss::Index* index_ = nullptr;
#endif
};

// =============================================================================
// LTMFaiss Implementation
// =============================================================================

LTMFaiss::LTMFaiss(const LTMConfig& config)
    : config_(config)
    , faiss_impl_(std::make_unique<FAISSIndexImpl>(config))
    , lexical_index_(std::make_unique<LexicalIndex>()) {
    
    // Initialize FAISS index
    FAISSIndexType type = config.index_type;
    if (type == FAISSIndexType::AUTO) {
        type = selectIndexType(0);  // Will adjust when we know data size
    }
    faiss_impl_->initialize(type);
}

LTMFaiss::~LTMFaiss() = default;

// =============================================================================
// Core Operations
// =============================================================================

void LTMFaiss::add(const CodeChunk& chunk, const std::vector<float>& embedding) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Assign FAISS ID
    int64_t faiss_id = next_faiss_id_++;
    
    // Store in FAISS — normalise for cosine similarity when enabled
    if (config_.use_cosine_similarity && !embedding.empty()) {
        faiss_impl_->add(faiss_id, normalizeQuery(embedding));
    } else {
        faiss_impl_->add(faiss_id, embedding);
    }
    
    // Store metadata
    chunks_[chunk.id] = chunk;
    
    MemoryMetadata meta;
    meta.id = chunk.id;
    meta.created_at = std::chrono::system_clock::now();
    meta.last_accessed = meta.created_at;
    meta.importance_score = chunk.importance_score;
    metadata_[chunk.id] = meta;
    
    // Update indexes
    chunk_id_to_faiss_id_[chunk.id] = faiss_id;
    faiss_id_to_chunk_id_[faiss_id] = chunk.id;
    
    // Extract repo ID from chunk ID (format: "repo_id:chunk_id")
    std::string repo_id;
    size_t colon_pos = chunk.id.find(':');
    if (colon_pos != std::string::npos) {
        repo_id = chunk.id.substr(0, colon_pos);
    }
    
    if (!repo_id.empty()) {
        repo_to_chunks_[repo_id].push_back(chunk.id);
    }
    
    // Add to lexical index
    lexical_index_->add(chunk.id, chunk.content);
    
    // Auto-save check
    additions_since_save_++;
    maybeAutoSave();
}

void LTMFaiss::addBatch(
    const std::vector<CodeChunk>& chunks,
    const std::vector<std::vector<float>>& embeddings
) {
    if (chunks.size() != embeddings.size()) {
        throw std::invalid_argument("Chunks and embeddings count mismatch");
    }
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Flatten embeddings for batch add
    std::vector<float> flat_embeddings;
    flat_embeddings.reserve(embeddings.size() * config_.dimension);
    
    std::vector<int64_t> faiss_ids;
    faiss_ids.reserve(chunks.size());
    
    for (size_t i = 0; i < chunks.size(); ++i) {
        int64_t faiss_id = next_faiss_id_++;
        faiss_ids.push_back(faiss_id);
        
        // If cosine similarity is enabled, L2-normalise the embedding so that
        // Inner Product (IP) distance becomes cosine similarity.
        if (config_.use_cosine_similarity && !embeddings[i].empty()) {
            std::vector<float> normed = embeddings[i];
            float norm = 0.0f;
            for (float v : normed) norm += v * v;
            norm = std::sqrt(norm);
            if (norm > 1e-12f) {
                for (float& v : normed) v /= norm;
            }
            flat_embeddings.insert(flat_embeddings.end(), normed.begin(), normed.end());
        } else {
            flat_embeddings.insert(flat_embeddings.end(),
                                   embeddings[i].begin(), embeddings[i].end());
        }
        
        // Store metadata
        const auto& chunk = chunks[i];
        chunks_[chunk.id] = chunk;
        
        MemoryMetadata meta;
        meta.id = chunk.id;
        meta.created_at = std::chrono::system_clock::now();
        meta.last_accessed = meta.created_at;
        meta.importance_score = chunk.importance_score;
        metadata_[chunk.id] = meta;
        
        // Update indexes
        chunk_id_to_faiss_id_[chunk.id] = faiss_id;
        faiss_id_to_chunk_id_[faiss_id] = chunk.id;
        
        // Extract repo ID
        std::string repo_id;
        size_t colon_pos = chunk.id.find(':');
        if (colon_pos != std::string::npos) {
            repo_id = chunk.id.substr(0, colon_pos);
        }
        
        if (!repo_id.empty()) {
            repo_to_chunks_[repo_id].push_back(chunk.id);
        }
        
        // Lexical index
        lexical_index_->add(chunk.id, chunk.content);
    }
    
    // Batch add to FAISS
    faiss_impl_->addBatch(faiss_ids, flat_embeddings);
    
    additions_since_save_ += chunks.size();

    // record ingestion metrics
    aipr::metrics::Registry::instance().incCounter(
        aipr::metrics::CHUNKS_INGESTED, static_cast<double>(chunks.size()));
    aipr::metrics::Registry::instance().setGauge(
        aipr::metrics::INDEX_SIZE_VECTORS,
        static_cast<double>(faiss_impl_->size()));

    maybeAutoSave();
}

std::vector<RetrievedChunk> LTMFaiss::search(
    const std::vector<float>& query_embedding,
    int top_k,
    const std::string& repo_filter
) {
    if (top_k <= 0) top_k = config_.default_top_k;

    // record search latency
    AIPR_METRICS_SCOPE(aipr::metrics::SEARCH_LATENCY_S);
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Search FAISS — when filtering by repo we MUST search the entire index.
    //
    // FAISS returns the globally nearest vectors regardless of repo ownership.
    // If the target repo is a small fraction of the index (e.g. 5%), even
    // a 10× over-fetch may return zero matching chunks because the nearest
    // neighbors are all from other repos.  The only safe strategy is to
    // search all vectors and let convertResults() filter + cap at top_k.
    //
    // Performance: FlatL2/HNSW brute-force over 70-100k 384-dim vectors
    // takes ~30-60ms which is acceptable for interactive chat.
    int search_k = top_k;
    if (!repo_filter.empty()) {
        size_t total  = chunks_.size();
        size_t repo_n = 0;
        auto it = repo_to_chunks_.find(repo_filter);
        if (it != repo_to_chunks_.end()) repo_n = it->second.size();

        // Always search the full index — post-filter in convertResults
        search_k = static_cast<int>(total);

        LOG_DEBUG("[LTM] search: repo_filter='" + repo_filter +
                  "' repo_chunks=" + std::to_string(repo_n) +
                  " total=" + std::to_string(total) +
                  " search_k=" + std::to_string(search_k));
    }
    
    auto [ids, distances] = faiss_impl_->search(
        config_.use_cosine_similarity ? normalizeQuery(query_embedding) : query_embedding,
        search_k);
    
    return convertResults(ids, distances, repo_filter, top_k);
}

std::vector<RetrievedChunk> LTMFaiss::hybridSearch(
    const std::string& query_text,
    const std::vector<float>& query_embedding,
    int top_k,
    float alpha,
    const std::string& repo_filter
) {
    if (top_k <= 0) top_k = config_.default_top_k;
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Over-fetch when repo_filter is set — same strategy as search():
    // always search the full index so post-filtering can find matches.
    int over_fetch_k;
    if (repo_filter.empty()) {
        over_fetch_k = top_k * 2;
    } else {
        over_fetch_k = static_cast<int>(chunks_.size());
    }
    
    // Vector search (normalize query when using cosine similarity)
    int vector_k = over_fetch_k;
    auto [v_ids, v_distances] = faiss_impl_->search(
        config_.use_cosine_similarity ? normalizeQuery(query_embedding) : query_embedding,
        vector_k);
    
    // Lexical search
    auto lexical_results = lexical_index_->search(query_text, vector_k);
    
    // RRF (Reciprocal Rank Fusion)
    const float k = 60.0f;  // RRF parameter
    std::unordered_map<std::string, float> combined_scores;
    std::unordered_map<std::string, float> vector_scores;
    std::unordered_map<std::string, float> lexical_scores;
    
    // Add vector scores
    for (size_t rank = 0; rank < v_ids.size(); ++rank) {
        if (v_ids[rank] < 0) continue;
        
        auto it = faiss_id_to_chunk_id_.find(v_ids[rank]);
        if (it != faiss_id_to_chunk_id_.end()) {
            // Apply repo filter: skip chunks that don't belong to the target repo
            if (!repo_filter.empty()) {
                const std::string& chunk_id = it->second;
                if (chunk_id.size() <= repo_filter.size() || 
                    chunk_id.substr(0, repo_filter.size() + 1) != repo_filter + ":") {
                    continue;
                }
            }
            float rrf_score = alpha / (k + rank + 1);
            combined_scores[it->second] += rrf_score;
            vector_scores[it->second] = rrf_score;
        }
    }
    
    // Add lexical scores
    for (size_t rank = 0; rank < lexical_results.size(); ++rank) {
        // Apply repo filter to lexical results too
        if (!repo_filter.empty()) {
            const std::string& chunk_id = lexical_results[rank].first;
            if (chunk_id.size() <= repo_filter.size() || 
                chunk_id.substr(0, repo_filter.size() + 1) != repo_filter + ":") {
                continue;
            }
        }
        float rrf_score = (1.0f - alpha) / (k + rank + 1);
        combined_scores[lexical_results[rank].first] += rrf_score;
        lexical_scores[lexical_results[rank].first] = rrf_score;
    }
    
    // Sort by combined score
    std::vector<std::pair<std::string, float>> ranked(combined_scores.begin(), combined_scores.end());
    std::partial_sort(
        ranked.begin(),
        ranked.begin() + std::min(static_cast<size_t>(top_k), ranked.size()),
        ranked.end(),
        [](const auto& a, const auto& b) { return a.second > b.second; }
    );
    
    // Build results
    std::vector<RetrievedChunk> results;
    for (size_t i = 0; i < std::min(static_cast<size_t>(top_k), ranked.size()); ++i) {
        const auto& [chunk_id, score] = ranked[i];
        
        auto chunk_it = chunks_.find(chunk_id);
        if (chunk_it == chunks_.end()) continue;
        
        RetrievedChunk result;
        result.chunk = chunk_it->second;
        result.combined_score = score;
        auto vs_it = vector_scores.find(chunk_id);
        result.similarity_score = vs_it != vector_scores.end() ? vs_it->second : 0.0f;
        auto ls_it = lexical_scores.find(chunk_id);
        result.lexical_score = ls_it != lexical_scores.end() ? ls_it->second : 0.0f;
        result.memory_source = "LTM";
        
        if (metadata_.count(chunk_id)) {
            result.metadata = metadata_[chunk_id];
        }
        
        results.push_back(result);
    }
    
    if (!repo_filter.empty()) {
        LOG_DEBUG("[LTM] hybridSearch: repo_filter='" + repo_filter +
                  "' returning " + std::to_string(results.size()) + " chunks after filtering");
    }
    
    return results;
}

std::vector<RetrievedChunk> LTMFaiss::hybridSearchByAccount(
    const std::string& query_text,
    const std::vector<float>& query_embedding,
    const std::string& account_tag,
    int top_k,
    const std::string& repo_filter
) {
    if (top_k <= 0) top_k = config_.default_top_k;

    // Over-fetch 4× then filter by account tag
    auto candidates = hybridSearch(query_text, query_embedding,
                                   top_k * 4, 0.7f, repo_filter);

    std::vector<RetrievedChunk> filtered;
    filtered.reserve(static_cast<size_t>(top_k));

    for (auto& rc : candidates) {
        bool has_tag = false;
        for (const auto& tag : rc.chunk.tags) {
            if (tag == account_tag) { has_tag = true; break; }
        }
        if (has_tag) {
            filtered.push_back(std::move(rc));
            if (static_cast<int>(filtered.size()) >= top_k) break;
        }
    }

    LOG_DEBUG("[LTM] hybridSearchByAccount: account=" + account_tag +
              " candidates=" + std::to_string(candidates.size()) +
              " filtered=" + std::to_string(filtered.size()));

    return filtered;
}

// =============================================================================
// CRUD Operations
// =============================================================================

std::optional<CodeChunk> LTMFaiss::get(const std::string& chunk_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = chunks_.find(chunk_id);
    if (it != chunks_.end()) {
        // Update access time
        if (metadata_.count(chunk_id)) {
            metadata_[chunk_id].last_accessed = std::chrono::system_clock::now();
            metadata_[chunk_id].access_count++;
        }
        return it->second;
    }
    
    return std::nullopt;
}

bool LTMFaiss::contains(const std::string& chunk_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    return chunks_.count(chunk_id) > 0;
}

bool LTMFaiss::remove(const std::string& chunk_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto faiss_id_it = chunk_id_to_faiss_id_.find(chunk_id);
    if (faiss_id_it == chunk_id_to_faiss_id_.end()) {
        return false;
    }
    
    // Remove from FAISS
    faiss_impl_->remove(faiss_id_it->second);
    
    // Remove from lexical index
    lexical_index_->remove(chunk_id);
    
    // Remove from indexes
    faiss_id_to_chunk_id_.erase(faiss_id_it->second);
    chunk_id_to_faiss_id_.erase(chunk_id);
    
    // Remove from repo index
    size_t colon_pos = chunk_id.find(':');
    if (colon_pos != std::string::npos) {
        std::string repo_id = chunk_id.substr(0, colon_pos);
        auto& repo_chunks = repo_to_chunks_[repo_id];
        repo_chunks.erase(std::remove(repo_chunks.begin(), repo_chunks.end(), chunk_id), 
                          repo_chunks.end());
    }
    
    // Remove metadata
    chunks_.erase(chunk_id);
    metadata_.erase(chunk_id);
    
    return true;
}

size_t LTMFaiss::removeByRepo(const std::string& repo_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = repo_to_chunks_.find(repo_id);
    if (it == repo_to_chunks_.end()) {
        return 0;
    }
    
    size_t count = it->second.size();
    
    for (const auto& chunk_id : it->second) {
        auto faiss_id_it = chunk_id_to_faiss_id_.find(chunk_id);
        if (faiss_id_it != chunk_id_to_faiss_id_.end()) {
            faiss_impl_->remove(faiss_id_it->second);
            faiss_id_to_chunk_id_.erase(faiss_id_it->second);
            chunk_id_to_faiss_id_.erase(chunk_id);
        }
        
        lexical_index_->remove(chunk_id);
        chunks_.erase(chunk_id);
        metadata_.erase(chunk_id);
    }
    
    repo_to_chunks_.erase(repo_id);
    
    return count;
}

void LTMFaiss::updateImportance(const std::string& chunk_id, double delta) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = metadata_.find(chunk_id);
    if (it != metadata_.end()) {
        it->second.importance_score = std::clamp(it->second.importance_score + delta, 0.0, 2.0);
    }
}

// =============================================================================
// Memory Management
// =============================================================================

size_t LTMFaiss::consolidate(double threshold) {
    if (threshold < 0) threshold = config_.similarity_threshold;
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<std::string> to_remove;
    
    for (const auto& [chunk_id, meta] : metadata_) {
        if (meta.importance_score < threshold) {
            to_remove.push_back(chunk_id);
        }
    }
    
    // Note: We don't actually remove here to avoid modifying during iteration
    // In production, queue removals for batch processing
    
    return to_remove.size();
}

void LTMFaiss::train(const std::vector<std::vector<float>>& training_embeddings) {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Flatten embeddings
    std::vector<float> flat;
    flat.reserve(training_embeddings.size() * config_.dimension);
    
    for (const auto& emb : training_embeddings) {
        flat.insert(flat.end(), emb.begin(), emb.end());
    }
    
    faiss_impl_->train(flat, training_embeddings.size());
}

bool LTMFaiss::isTrained() const {
    return faiss_impl_->isTrained();
}

// =============================================================================
// Persistence
// =============================================================================

void LTMFaiss::save() {
    save(config_.storage_path);
}

void LTMFaiss::save(const std::string& path) {
    try {
        std::filesystem::create_directories(path);
    } catch (...) {
        return;  // Cannot create directory, skip save
    }
    
    // Save FAISS index
    try {
        faiss_impl_->save(path);
    } catch (...) {
        // FAISS write failed — continue saving metadata/chunks
    }
    
    // Save metadata (JSON format)
    std::ofstream meta_file(path + "/metadata.json");
    if (meta_file.is_open()) {
        meta_file << "{\n";
        meta_file << "  \"chunk_count\": " << chunks_.size() << ",\n";
        meta_file << "  \"next_faiss_id\": " << next_faiss_id_ << ",\n";
        meta_file << "  \"repos\": [";
        
        bool first = true;
        for (const auto& [repo_id, _] : repo_to_chunks_) {
            if (!first) meta_file << ", ";
            meta_file << "\"" << repo_id << "\"";
            first = false;
        }
        meta_file << "]\n";
        meta_file << "}\n";
        meta_file.close();
    }
    
    // Save chunks (binary format for efficiency)
    {
        // Serialize to an in-memory buffer first, then optionally gzip
        std::ostringstream buf(std::ios::binary);

        const uint32_t magic = 0x4C544D43; // "LTMC"
        const uint32_t version = 2;        // v2: gzip-compressed payload
        buf.write(reinterpret_cast<const char*>(&magic), sizeof(magic));
        buf.write(reinterpret_cast<const char*>(&version), sizeof(version));

        uint64_t count = chunks_.size();
        buf.write(reinterpret_cast<const char*>(&count), sizeof(count));
        buf.write(reinterpret_cast<const char*>(&next_faiss_id_), sizeof(next_faiss_id_));

        for (const auto& [chunk_id, chunk] : chunks_) {
            auto write_str = [&](const std::string& s) {
                uint32_t len = static_cast<uint32_t>(s.size());
                buf.write(reinterpret_cast<const char*>(&len), sizeof(len));
                buf.write(s.data(), len);
            };

            write_str(chunk_id);
            write_str(chunk.id);
            std::string repo_id;
            for (const auto& tag : chunk.tags) {
                if (tag.rfind("repo:", 0) == 0) {
                    repo_id = tag.substr(5);
                    break;
                }
            }
            write_str(repo_id);
            write_str(chunk.file_path);
            write_str(chunk.language);
            write_str(chunk.name);
            write_str(chunk.type);
            write_str(chunk.content);

            int64_t faiss_id = -1;
            auto fid_it = chunk_id_to_faiss_id_.find(chunk_id);
            if (fid_it != chunk_id_to_faiss_id_.end()) {
                faiss_id = fid_it->second;
            }
            buf.write(reinterpret_cast<const char*>(&faiss_id), sizeof(faiss_id));

            bool has_meta = metadata_.count(chunk_id) > 0;
            buf.write(reinterpret_cast<const char*>(&has_meta), sizeof(has_meta));
            if (has_meta) {
                const auto& meta = metadata_.at(chunk_id);
                buf.write(reinterpret_cast<const char*>(&meta.importance_score), sizeof(meta.importance_score));
                buf.write(reinterpret_cast<const char*>(&meta.access_count), sizeof(meta.access_count));
                buf.write(reinterpret_cast<const char*>(&meta.decay_factor), sizeof(meta.decay_factor));
            }
        }

        std::string raw = buf.str();

#ifdef AIPR_HAS_MINIZ
        // gzip compress the payload
        mz_ulong compressed_len = mz_compressBound(static_cast<mz_ulong>(raw.size()));
        std::vector<uint8_t> compressed(compressed_len);
        int rc = mz_compress2(compressed.data(), &compressed_len,
                              reinterpret_cast<const uint8_t*>(raw.data()),
                              static_cast<mz_ulong>(raw.size()),
                              MZ_BEST_SPEED);
        if (rc == MZ_OK) {
            std::ofstream chunk_file(path + "/chunks.bin", std::ios::binary);
            if (chunk_file.is_open()) {
                // Write a tiny envelope: magic(4) + version(4) + uncompressed_size(8) + compressed payload
                const uint32_t gz_magic = 0x4C544D43;
                const uint32_t gz_version = 2;
                uint64_t uncompressed_size = raw.size();
                chunk_file.write(reinterpret_cast<const char*>(&gz_magic), sizeof(gz_magic));
                chunk_file.write(reinterpret_cast<const char*>(&gz_version), sizeof(gz_version));
                chunk_file.write(reinterpret_cast<const char*>(&uncompressed_size), sizeof(uncompressed_size));
                chunk_file.write(reinterpret_cast<const char*>(compressed.data()),
                                 static_cast<std::streamsize>(compressed_len));
                chunk_file.close();
            }
        } else {
            // Compression failed — fall back to raw write
            LOG_WARN("[LTM] miniz compression failed (rc=" + std::to_string(rc) + "), writing uncompressed");
            std::ofstream chunk_file(path + "/chunks.bin", std::ios::binary);
            if (chunk_file.is_open()) {
                chunk_file.write(raw.data(), static_cast<std::streamsize>(raw.size()));
                chunk_file.close();
            }
        }
#else
        // No compression available — write raw
        std::ofstream chunk_file(path + "/chunks.bin", std::ios::binary);
        if (chunk_file.is_open()) {
            chunk_file.write(raw.data(), static_cast<std::streamsize>(raw.size()));
            chunk_file.close();
        }
#endif
    }
    
    additions_since_save_ = 0;
}

void LTMFaiss::load() {
    load(config_.storage_path);
}

void LTMFaiss::load(const std::string& path) {
    if (!std::filesystem::exists(path)) {
        return;
    }
    
    // Load FAISS index
    faiss_impl_->load(path);
    
    // Load chunks from binary
    std::string chunk_path = path + "/chunks.bin";
    if (!std::filesystem::exists(chunk_path)) return;

    std::ifstream chunk_file(chunk_path, std::ios::binary);
    if (!chunk_file.is_open()) return;

    uint32_t magic = 0, version = 0;
    chunk_file.read(reinterpret_cast<char*>(&magic), sizeof(magic));
    chunk_file.read(reinterpret_cast<char*>(&version), sizeof(version));

    if (magic != 0x4C544D43) return;

    // Prepare the byte stream to parse from.
    // v1 → read directly from chunk_file
    // v2 → read uncompressed_size, decompress, then parse from std::istringstream
    std::unique_ptr<std::istream> stream_owner;
    std::istream* in = &chunk_file;

    if (version == 2) {
#ifdef AIPR_HAS_MINIZ
        uint64_t uncompressed_size = 0;
        chunk_file.read(reinterpret_cast<char*>(&uncompressed_size), sizeof(uncompressed_size));

        // Read remaining bytes as compressed payload
        std::string compressed_data((std::istreambuf_iterator<char>(chunk_file)),
                                    std::istreambuf_iterator<char>());
        chunk_file.close();

        std::string decompressed(uncompressed_size, '\0');
        mz_ulong dest_len = static_cast<mz_ulong>(uncompressed_size);
        int rc = mz_uncompress(reinterpret_cast<uint8_t*>(decompressed.data()), &dest_len,
                               reinterpret_cast<const uint8_t*>(compressed_data.data()),
                               static_cast<mz_ulong>(compressed_data.size()));
        if (rc != MZ_OK) {
            LOG_WARN("[LTM] failed to decompress chunks.bin (rc=" + std::to_string(rc) + ")");
            return;
        }

        // Re-parse magic + version from decompressed buffer
        auto iss = std::make_unique<std::istringstream>(decompressed, std::ios::binary);
        uint32_t inner_magic = 0, inner_version = 0;
        iss->read(reinterpret_cast<char*>(&inner_magic), sizeof(inner_magic));
        iss->read(reinterpret_cast<char*>(&inner_version), sizeof(inner_version));
        in = iss.get();
        stream_owner = std::move(iss);
#else
        LOG_WARN("[LTM] chunks.bin is v2 (compressed) but AIPR_HAS_MINIZ is not defined");
        return;
#endif
    } else if (version != 1) {
        LOG_WARN("[LTM] unknown chunks.bin version " + std::to_string(version));
        return;
    }

    // From here, 'in' points to the byte stream at the position right after
    // magic + version (same for both v1 and v2).
    uint64_t count = 0;
    in->read(reinterpret_cast<char*>(&count), sizeof(count));
    in->read(reinterpret_cast<char*>(&next_faiss_id_), sizeof(next_faiss_id_));

    auto read_str = [&]() -> std::string {
        uint32_t len = 0;
        in->read(reinterpret_cast<char*>(&len), sizeof(len));
        if (len > 100'000'000) return "";
        std::string s(len, '\0');
        in->read(s.data(), len);
        return s;
    };

    chunks_.clear();
    chunk_id_to_faiss_id_.clear();
    faiss_id_to_chunk_id_.clear();
    repo_to_chunks_.clear();
    metadata_.clear();

    for (uint64_t i = 0; i < count && in->good(); ++i) {
        std::string chunk_id = read_str();
        CodeChunk chunk;
        chunk.id = read_str();
        std::string repo_id = read_str();

        // Fallback: if the persisted repo_id is empty (older data where
        // the repo: tag was never added during ingestion), extract the
        // repo UUID from the chunk_id which has the format "repo_uuid:rest".
        if (repo_id.empty() && !chunk_id.empty()) {
            // UUID format: 8-4-4-4-12 = 36 chars, followed by ':'
            if (chunk_id.size() > 36 && chunk_id[36] == ':') {
                repo_id = chunk_id.substr(0, 36);
            }
        }

        if (!repo_id.empty()) {
            chunk.tags.push_back("repo:" + repo_id);
        }
        chunk.file_path = read_str();
        chunk.language = read_str();
        chunk.name = read_str();
        chunk.type = read_str();
        chunk.content = read_str();

        int64_t faiss_id = -1;
        in->read(reinterpret_cast<char*>(&faiss_id), sizeof(faiss_id));

        if (faiss_id >= 0) {
            chunk_id_to_faiss_id_[chunk_id] = faiss_id;
            faiss_id_to_chunk_id_[faiss_id] = chunk_id;
        }

        if (!repo_id.empty()) {
            repo_to_chunks_[repo_id].push_back(chunk_id);
        }

        bool has_meta = false;
        in->read(reinterpret_cast<char*>(&has_meta), sizeof(has_meta));
        if (has_meta) {
            MemoryMetadata meta;
            in->read(reinterpret_cast<char*>(&meta.importance_score), sizeof(meta.importance_score));
            in->read(reinterpret_cast<char*>(&meta.access_count), sizeof(meta.access_count));
            in->read(reinterpret_cast<char*>(&meta.decay_factor), sizeof(meta.decay_factor));
            meta.last_accessed = std::chrono::system_clock::now();
            metadata_[chunk_id] = meta;
        }

        chunks_[chunk_id] = std::move(chunk);
    }

    // Re-classify chunks into memory accounts.
    // The v1/v2 binary format only persists the "repo:" tag — account tags
    // (account:dev, account:ops, account:security, account:history) are
    // lost across restarts.  Re-classify here so that account-aware search
    // works correctly after a cold start.
    {
        MemoryAccountClassifier classifier;
        size_t reclassified = 0;
        for (auto& [cid, c] : chunks_) {
            // Skip if the chunk already carries an account tag (future-proof)
            bool has_account = false;
            for (const auto& tag : c.tags) {
                if (tag.rfind("account:", 0) == 0) { has_account = true; break; }
            }
            if (has_account) continue;

            auto account = classifier.classify(c);
            c.tags.push_back(MemoryAccountClassifier::accountTag(account));
            ++reclassified;
        }
        if (reclassified > 0) {
            LOG_INFO("[LTM] re-classified " + std::to_string(reclassified) +
                     " chunks into memory accounts after load");
        }

        // Log per-account distribution for diagnostics
        std::unordered_map<std::string, size_t> account_counts;
        for (const auto& [cid2, c2] : chunks_) {
            for (const auto& tag : c2.tags) {
                if (tag.rfind("account:", 0) == 0) {
                    account_counts[tag]++;
                    break;
                }
            }
        }
        std::string dist;
        for (const auto& [tag, cnt] : account_counts) {
            if (!dist.empty()) dist += ", ";
            dist += tag + "=" + std::to_string(cnt);
        }
        if (!dist.empty()) {
            LOG_INFO("[LTM] account distribution: " + dist);
        }
    }

    // Rebuild the in-memory BM25 lexical index from loaded chunks.
    // The lexical index is not persisted to disk, so it must be
    // reconstructed on every load.
    for (const auto& [cid, c] : chunks_) {
        lexical_index_->add(cid, c.content);
    }

    LOG_INFO("[LTM] loaded " + std::to_string(chunks_.size()) +
             " chunks, rebuilt lexical index (" +
             std::to_string(chunks_.size()) + " docs)");
}

// =============================================================================
// Statistics
// =============================================================================

LTMFaiss::Stats LTMFaiss::getStats() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    Stats stats;
    stats.total_chunks = chunks_.size();
    stats.total_repos = repo_to_chunks_.size();
    stats.index_vectors = faiss_impl_->size();
    stats.is_trained = faiss_impl_->isTrained();
    stats.index_type = faiss_impl_->getType();
    
    // Estimate memory usage
    stats.memory_bytes = chunks_.size() * (config_.dimension * sizeof(float) + 500);  // Rough estimate
    
    for (const auto& [repo_id, chunk_ids] : repo_to_chunks_) {
        stats.chunks_per_repo[repo_id] = chunk_ids.size();
    }
    
    return stats;
}

std::vector<std::string> LTMFaiss::getRepositories() const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    std::vector<std::string> repos;
    repos.reserve(repo_to_chunks_.size());
    
    for (const auto& [repo_id, _] : repo_to_chunks_) {
        repos.push_back(repo_id);
    }
    
    return repos;
}

size_t LTMFaiss::getRepoChunkCount(const std::string& repo_id) const {
    std::lock_guard<std::mutex> lock(mutex_);
    
    auto it = repo_to_chunks_.find(repo_id);
    return it != repo_to_chunks_.end() ? it->second.size() : 0;
}

// =============================================================================
// Helpers
// =============================================================================

FAISSIndexType LTMFaiss::selectIndexType(size_t expected_size) {
    // When cosine similarity is enabled, embeddings are L2-normalised before
    // insertion.  Inner Product on normalised vectors == cosine similarity,
    // so we must use IP-based indices to get meaningful distance values.
    if (config_.use_cosine_similarity) {
        if (expected_size < 10000) {
            return FAISSIndexType::FLAT_IP;
        }
        // HNSW uses L2 internally — fall through to FLAT_IP for safety.
        return FAISSIndexType::FLAT_IP;
    }

    if (expected_size < 10000) {
        return FAISSIndexType::FLAT_L2;
    } else if (expected_size < 1000000) {
        return FAISSIndexType::HNSW_FLAT;
    } else if (expected_size < 10000000) {
        return FAISSIndexType::IVF_FLAT;
    } else {
        return FAISSIndexType::IVF_PQ;
    }
}

std::vector<RetrievedChunk> LTMFaiss::convertResults(
    const std::vector<int64_t>& ids,
    const std::vector<float>& distances,
    const std::string& repo_filter,
    int top_k
) {
    std::vector<RetrievedChunk> results;

    // Precompute the repo prefix for filtering (format: "repo_id:")
    std::string repo_prefix;
    if (!repo_filter.empty()) {
        repo_prefix = repo_filter + ":";
    }
    
    for (size_t i = 0; i < ids.size(); ++i) {
        if (ids[i] < 0) continue;
        
        auto chunk_id_it = faiss_id_to_chunk_id_.find(ids[i]);
        if (chunk_id_it == faiss_id_to_chunk_id_.end()) continue;
        
        const std::string& chunk_id = chunk_id_it->second;

        // ── Repo filter: skip chunks that don't belong to the requested repo ──
        if (!repo_prefix.empty()) {
            if (chunk_id.compare(0, repo_prefix.size(), repo_prefix) != 0) {
                continue;  // chunk belongs to a different repository
            }
        }
        
        auto chunk_it = chunks_.find(chunk_id);
        if (chunk_it == chunks_.end()) continue;
        
        RetrievedChunk result;
        result.chunk = chunk_it->second;
        
        // Convert distance to similarity score (0-1).
        // When using cosine similarity (normalised embeddings + IP), the
        // distance IS the cosine similarity already in [0,1].  Otherwise
        // convert L2 distance: similarity = 1 / (1 + sqrt(d)).
        if (config_.use_cosine_similarity) {
            result.similarity_score = std::clamp(distances[i], 0.0f, 1.0f);
        } else {
            result.similarity_score = 1.0f / (1.0f + std::sqrt(distances[i]));
        }
        result.combined_score = result.similarity_score;
        result.memory_source = "LTM";
        
        if (metadata_.count(chunk_id)) {
            result.metadata = metadata_[chunk_id];
            
            // Update access time
            metadata_[chunk_id].last_accessed = std::chrono::system_clock::now();
            metadata_[chunk_id].access_count++;
        }
        
        results.push_back(result);

        // Stop once we have enough results (respects top_k after filtering)
        if (top_k > 0 && static_cast<int>(results.size()) >= top_k) {
            break;
        }
    }
    
    return results;
}

void LTMFaiss::maybeAutoSave() {
    if (config_.auto_save && additions_since_save_ >= config_.auto_save_interval) {
        // Perform synchronous save (async would require thread management)
        try {
            save();
        } catch (const std::exception&) {
            // Log but don't throw — auto-save is best-effort
        }
    }
}

std::vector<float> LTMFaiss::normalizeQuery(const std::vector<float>& v) {
    std::vector<float> normed = v;
    float norm = 0.0f;
    for (float x : normed) norm += x * x;
    norm = std::sqrt(norm);
    if (norm > 1e-12f) {
        for (float& x : normed) x /= norm;
    }
    return normed;
}

} // namespace aipr::tms
