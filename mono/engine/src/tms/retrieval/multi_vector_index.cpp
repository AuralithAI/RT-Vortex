/**
 * Multi-Vector Index — Implementation
 *
 * Dual-resolution FAISS search using Matryoshka embedding truncation.
 * See multi_vector_index.h for architecture documentation.
 */

#include "tms/multi_vector_index.h"
#include "metrics.h"
#include "logging.h"

#include <algorithm>
#include <numeric>
#include <cmath>
#include <chrono>
#include <unordered_map>

namespace aipr::tms {

namespace {
    void recordLatency(double seconds) {
        metrics::Registry::instance().observeHistogram(
            metrics::MULTIVEC_SEARCH_LATENCY_S, seconds);
    }
} // anonymous namespace

// ─────────────────────────────────────────────────────────────────────────────
// Construction
// ─────────────────────────────────────────────────────────────────────────────

MultiVectorIndex::MultiVectorIndex(
    const MultiVectorConfig& config,
    const LTMConfig& ltm_config)
    : config_(config)
{
    // Always create the fine (full-dimension) index.
    LTMConfig fine_cfg = ltm_config;
    fine_cfg.dimension = config_.fine_dimension;
    if (!config_.storage_path.empty()) {
        fine_cfg.storage_path = config_.storage_path + "/fine";
    }
    fine_index_ = std::make_unique<LTMFaiss>(fine_cfg);

    if (config_.enabled && config_.coarse_dimension < config_.fine_dimension) {
        LTMConfig coarse_cfg = ltm_config;
        coarse_cfg.dimension = config_.coarse_dimension;
        coarse_cfg.index_type = FAISSIndexType::HNSW_FLAT;
        if (!config_.storage_path.empty()) {
            coarse_cfg.storage_path = config_.storage_path + "/coarse";
        }
        coarse_index_ = std::make_unique<LTMFaiss>(coarse_cfg);

        LOG_INFO("[MultiVec] dual-resolution enabled: coarse=" +
                 std::to_string(config_.coarse_dimension) + "d HNSW + fine=" +
                 std::to_string(config_.fine_dimension) + "d");
    } else {
        LOG_INFO("[MultiVec] single-resolution mode: " +
                 std::to_string(config_.fine_dimension) + "d");
    }
}

MultiVectorIndex::~MultiVectorIndex() = default;

// ─────────────────────────────────────────────────────────────────────────────
// Indexing
// ─────────────────────────────────────────────────────────────────────────────

void MultiVectorIndex::add(
    const CodeChunk& chunk,
    const std::vector<float>& embedding)
{
    std::lock_guard<std::mutex> lock(mutex_);
    fine_index_->add(chunk, embedding);
    if (isDualActive()) {
        coarse_index_->add(chunk, truncate(embedding, config_.coarse_dimension));
    }
}

void MultiVectorIndex::addBatch(
    const std::vector<CodeChunk>& chunks,
    const std::vector<std::vector<float>>& embeddings)
{
    if (chunks.empty()) return;
    std::lock_guard<std::mutex> lock(mutex_);

    fine_index_->addBatch(chunks, embeddings);

    if (isDualActive()) {
        std::vector<std::vector<float>> coarse_vecs;
        coarse_vecs.reserve(embeddings.size());
        for (const auto& emb : embeddings) {
            coarse_vecs.push_back(truncate(emb, config_.coarse_dimension));
        }
        coarse_index_->addBatch(chunks, coarse_vecs);
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Retrieval
// ─────────────────────────────────────────────────────────────────────────────

std::vector<RetrievedChunk> MultiVectorIndex::search(
    const std::vector<float>& query_embedding,
    int top_k,
    const std::string& repo_filter)
{
    auto t0 = std::chrono::steady_clock::now();

    if (!isDualActive()) {
        auto results = fine_index_->search(query_embedding, top_k, repo_filter);
        recordLatency(std::chrono::duration<double>(
            std::chrono::steady_clock::now() - t0).count());
        return results;
    }

    int effective_k = (top_k > 0 ? top_k : 12);
    int coarse_k = std::min<int>(
        effective_k * config_.oversampling_factor,
        static_cast<int>(coarse_index_->getStats().index_vectors));
    if (coarse_k < 1) coarse_k = effective_k;

    auto coarse_query = truncate(query_embedding, config_.coarse_dimension);
    auto coarse_results = coarse_index_->search(coarse_query, coarse_k, repo_filter);

    if (coarse_results.empty()) {
        recordLatency(std::chrono::duration<double>(
            std::chrono::steady_clock::now() - t0).count());
        return {};
    }

    auto results = rerank(coarse_results, query_embedding, effective_k);
    recordLatency(std::chrono::duration<double>(
        std::chrono::steady_clock::now() - t0).count());
    return results;
}

std::vector<RetrievedChunk> MultiVectorIndex::hybridSearch(
    const std::string& query_text,
    const std::vector<float>& query_embedding,
    int top_k,
    float alpha,
    const std::string& repo_filter)
{
    auto t0 = std::chrono::steady_clock::now();

    if (!isDualActive()) {
        auto results = fine_index_->hybridSearch(
            query_text, query_embedding, top_k, alpha, repo_filter);
        recordLatency(std::chrono::duration<double>(
            std::chrono::steady_clock::now() - t0).count());
        return results;
    }

    int effective_k = (top_k > 0 ? top_k : 12);
    int coarse_k = std::min<int>(
        effective_k * config_.oversampling_factor,
        static_cast<int>(coarse_index_->getStats().index_vectors));
    if (coarse_k < 1) coarse_k = effective_k;

    auto coarse_query = truncate(query_embedding, config_.coarse_dimension);
    auto coarse_results = coarse_index_->hybridSearch(
        query_text, coarse_query, coarse_k, alpha, repo_filter);

    if (coarse_results.empty()) {
        recordLatency(std::chrono::duration<double>(
            std::chrono::steady_clock::now() - t0).count());
        return {};
    }

    auto results = rerank(coarse_results, query_embedding, effective_k);
    recordLatency(std::chrono::duration<double>(
        std::chrono::steady_clock::now() - t0).count());
    return results;
}

// ─────────────────────────────────────────────────────────────────────────────
// CRUD pass-through
// ─────────────────────────────────────────────────────────────────────────────

bool MultiVectorIndex::remove(const std::string& chunk_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    bool removed = fine_index_->remove(chunk_id);
    if (isDualActive()) coarse_index_->remove(chunk_id);
    return removed;
}

size_t MultiVectorIndex::removeByRepo(const std::string& repo_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    size_t removed = fine_index_->removeByRepo(repo_id);
    if (isDualActive()) coarse_index_->removeByRepo(repo_id);
    return removed;
}

// ─────────────────────────────────────────────────────────────────────────────
// Persistence
// ─────────────────────────────────────────────────────────────────────────────

void MultiVectorIndex::save() {
    std::lock_guard<std::mutex> lock(mutex_);
    fine_index_->save();
    if (isDualActive()) coarse_index_->save();
}

void MultiVectorIndex::load() {
    std::lock_guard<std::mutex> lock(mutex_);
    fine_index_->load();
    if (isDualActive()) coarse_index_->load();
}

// ─────────────────────────────────────────────────────────────────────────────
// Diagnostics
// ─────────────────────────────────────────────────────────────────────────────

MultiVectorIndex::Stats MultiVectorIndex::getStats() const {
    std::lock_guard<std::mutex> lock(mutex_);
    auto fine_stats = fine_index_->getStats();

    Stats s;
    s.total_chunks = fine_stats.total_chunks;
    s.fine_index_vectors = fine_stats.index_vectors;
    s.fine_dimension = config_.fine_dimension;
    s.dual_index_active = isDualActive();

    if (isDualActive()) {
        auto coarse_stats = coarse_index_->getStats();
        s.coarse_index_vectors = coarse_stats.index_vectors;
        s.coarse_dimension = config_.coarse_dimension;
    }
    return s;
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

std::vector<float> MultiVectorIndex::truncate(
    const std::vector<float>& full_vec, size_t target_dim)
{
    if (full_vec.size() <= target_dim) return full_vec;
    return std::vector<float>(full_vec.begin(), full_vec.begin() + target_dim);
}

std::vector<RetrievedChunk> MultiVectorIndex::rerank(
    const std::vector<RetrievedChunk>& coarse_candidates,
    const std::vector<float>& query_embedding,
    int top_k)
{
    // Strategy: do a single fine-index search for all coarse candidates'
    // worth of results, then intersect to re-score.
    //
    // The fine-index search with the full-dim query will produce scores
    // that are more accurate than the coarse scores. We intersect the
    // candidate set from coarse with fine scores.

    int fine_k = std::min<int>(
        static_cast<int>(coarse_candidates.size()),
        static_cast<int>(fine_index_->getStats().index_vectors));
    if (fine_k < 1) fine_k = 1;

    auto fine_results = fine_index_->search(query_embedding, fine_k, "");

    // Build chunk_id → fine_score map
    std::unordered_map<std::string, float> fine_score_map;
    fine_score_map.reserve(fine_results.size());
    for (const auto& fr : fine_results) {
        fine_score_map[fr.chunk.id] = fr.similarity_score;
    }

    // Re-score each coarse candidate
    struct Scored {
        RetrievedChunk chunk;
        float final_score;
    };
    std::vector<Scored> scored;
    scored.reserve(coarse_candidates.size());

    for (const auto& cand : coarse_candidates) {
        float score = cand.similarity_score; // default: coarse score
        auto it = fine_score_map.find(cand.chunk.id);
        if (it != fine_score_map.end()) {
            // Weighted combination: fine dominates
            score = 0.3f * cand.similarity_score + 0.7f * it->second;
        }
        scored.push_back({cand, score});
    }

    // Sort descending by fine score
    std::sort(scored.begin(), scored.end(),
        [](const Scored& a, const Scored& b) {
            return a.final_score > b.final_score;
        });

    // Take top_k
    std::vector<RetrievedChunk> results;
    int n = std::min<int>(top_k, static_cast<int>(scored.size()));
    results.reserve(n);
    for (int i = 0; i < n; ++i) {
        auto rc = scored[i].chunk;
        rc.similarity_score = scored[i].final_score;
        results.push_back(std::move(rc));
    }

    return results;
}

} // namespace aipr::tms
