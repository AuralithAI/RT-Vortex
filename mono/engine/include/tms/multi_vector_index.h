/**
 * Multi-Vector Index — Matryoshka Dual-Resolution Search
 *
 * Leverages the Matryoshka Representation Learning property of BGE-M3:
 *   a single embedding can be truncated to any prefix length while
 *   retaining proportionally useful semantics.
 *
 * Architecture:
 * ┌──────────────────────────────────────────────────────────┐
 * │  EmbeddingEngine  →  1024-dim full vector                │
 * │                       ├── [:384]  → coarse_index_ (fast) │
 * │                       └── [:1024] → fine_index_ (precise)│
 * └──────────────────────────────────────────────────────────┘
 *
 * Search path:
 *   1. Truncate query → 384 dims, search coarse_index_ for 3× top_k candidates
 *   2. Re-rank candidates in fine_index_ using full 1024-dim vectors
 *   3. Return top_k results with fine-grained scores
 *
 * When the engine is running at 384-dim (e.g. MiniLM), this module is a
 * no-op passthrough — the caller just uses the primary LTM index directly.
 */

#pragma once

#include "tms_types.h"
#include "ltm_faiss.h"
#include <memory>
#include <mutex>

namespace aipr::tms {

/**
 * Multi-Vector Configuration
 */
struct MultiVectorConfig {
    /// Full (fine-grained) embedding dimension — must match the model output.
    size_t fine_dimension = 1024;

    /// Coarse (Matryoshka-truncated) dimension for fast screening.
    size_t coarse_dimension = 384;

    /// Oversampling factor: search coarse index for this many × top_k candidates.
    int oversampling_factor = 3;

    /// Whether to enable the dual-index path.  When false, all operations
    /// delegate directly to the fine index (single-resolution mode).
    bool enabled = false;

    /// Storage sub-directory for FAISS indexes.
    std::string storage_path;
};

/**
 * MultiVectorIndex — Dual-Resolution FAISS Search
 */
class MultiVectorIndex {
public:
    /**
     * Construct the dual-index wrapper.
     *
     * @param config   Multi-vector configuration
     * @param ltm_config  Base LTM config (cloned & adjusted for each sub-index)
     */
    MultiVectorIndex(const MultiVectorConfig& config, const LTMConfig& ltm_config);
    ~MultiVectorIndex();

    MultiVectorIndex(const MultiVectorIndex&) = delete;
    MultiVectorIndex& operator=(const MultiVectorIndex&) = delete;

    // ─────────────────────────────────────────────────────────────────────
    //  Indexing
    // ─────────────────────────────────────────────────────────────────────

    /**
     * Add a chunk with its full-dimension embedding.
     * Internally stores both the truncated (coarse) and full (fine) vectors.
     *
     * @param chunk     Code chunk metadata
     * @param embedding Full-dimension embedding (fine_dimension)
     */
    void add(const CodeChunk& chunk, const std::vector<float>& embedding);

    /**
     * Batch add for ingestion throughput.
     */
    void addBatch(
        const std::vector<CodeChunk>& chunks,
        const std::vector<std::vector<float>>& embeddings
    );

    // ─────────────────────────────────────────────────────────────────────
    //  Retrieval
    // ─────────────────────────────────────────────────────────────────────

    /**
     * Dual-resolution search.
     *
     * 1. Truncate query to coarse_dimension, search coarse index for
     *    oversampling_factor × top_k candidates.
     * 2. Re-rank using fine-dimension inner-product on the fine index.
     * 3. Return top_k results.
     *
     * @param query_embedding   Full-dimension query vector
     * @param top_k             Desired result count (-1 = default from LTM config)
     * @param repo_filter       Optional repo filter
     */
    std::vector<RetrievedChunk> search(
        const std::vector<float>& query_embedding,
        int top_k = -1,
        const std::string& repo_filter = ""
    );

    /**
     * Dual-resolution hybrid (vector + BM25) search.
     */
    std::vector<RetrievedChunk> hybridSearch(
        const std::string& query_text,
        const std::vector<float>& query_embedding,
        int top_k = -1,
        float alpha = 0.7f,
        const std::string& repo_filter = ""
    );

    // ─────────────────────────────────────────────────────────────────────
    //  CRUD pass-through
    // ─────────────────────────────────────────────────────────────────────

    bool remove(const std::string& chunk_id);
    size_t removeByRepo(const std::string& repo_id);

    // ─────────────────────────────────────────────────────────────────────
    //  Persistence
    // ─────────────────────────────────────────────────────────────────────

    void save();
    void load();

    // ─────────────────────────────────────────────────────────────────────
    //  Diagnostics
    // ─────────────────────────────────────────────────────────────────────

    struct Stats {
        size_t total_chunks = 0;
        size_t coarse_index_vectors = 0;
        size_t fine_index_vectors = 0;
        size_t coarse_dimension = 0;
        size_t fine_dimension = 0;
        bool dual_index_active = false;
    };

    Stats getStats() const;

    /**
     * Access the fine (primary) LTM index directly.
     * Used by subsystems that need raw FAISS access (e.g. GraphRAG seed lookup).
     */
    LTMFaiss& fineIndex() { return *fine_index_; }
    const LTMFaiss& fineIndex() const { return *fine_index_; }

    /**
     * Whether dual-resolution mode is active.
     */
    bool isDualActive() const { return config_.enabled && coarse_index_ != nullptr; }

private:
    MultiVectorConfig config_;

    /// Fine (full-dimension) index — always present.
    std::unique_ptr<LTMFaiss> fine_index_;

    /// Coarse (truncated-dimension) index — only when dual mode is enabled.
    std::unique_ptr<LTMFaiss> coarse_index_;

    mutable std::mutex mutex_;

    /// Truncate a vector to the coarse dimension (first N floats).
    static std::vector<float> truncate(
        const std::vector<float>& full_vec, size_t target_dim);

    /// Re-rank coarse candidates using fine-dimension scores.
    std::vector<RetrievedChunk> rerank(
        const std::vector<RetrievedChunk>& coarse_candidates,
        const std::vector<float>& query_embedding,
        int top_k
    );
};

} // namespace aipr::tms
