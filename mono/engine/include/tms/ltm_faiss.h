/**
 * TMS Long-Term Memory (LTM) - FAISS Implementation
 * 
 * Persistent knowledge base using FAISS for efficient vector search.
 * Optimized for 500k–5M+ code chunks from massive monorepos.
 * 
 * Index Types Supported:
 * - IndexFlatL2: Exact search (small repos < 100k chunks)
 * - IndexIVFFlat: Inverted file index (medium repos 100k-1M chunks)
 * - IndexIVFPQ: Product quantization (large repos 1M+ chunks)
 * - IndexHNSWFlat: HNSW graph (fast approximate search)
 * 
 * Memory Usage Estimates (1536-dim embeddings):
 * - 100k chunks: ~600 MB (Flat) / ~100 MB (PQ)
 * - 1M chunks: ~6 GB (Flat) / ~1 GB (PQ)
 * - 5M chunks: ~30 GB (Flat) / ~5 GB (PQ)
 */

#pragma once

#include "tms_types.h"
#include <memory>
#include <mutex>
#include <unordered_map>
#include <functional>

namespace aipr::tms {

/**
 * FAISS Index Types
 */
enum class FAISSIndexType {
    FLAT_L2,        // Exact L2 distance (brute force)
    FLAT_IP,        // Exact Inner Product
    IVF_FLAT,       // Inverted File with Flat quantizer
    IVF_PQ,         // Inverted File with Product Quantization
    HNSW_FLAT,      // Hierarchical Navigable Small World graph
    AUTO            // Automatically choose based on data size
};

/**
 * LTM Configuration
 */
struct LTMConfig {
    size_t dimension = 1536;
    FAISSIndexType index_type = FAISSIndexType::AUTO;
    
    // Capacity
    size_t max_capacity = 10000000;     // 10M chunks max
    
    // IVF parameters (for IVF_FLAT and IVF_PQ)
    int nlist = 4096;                   // Number of clusters
    int nprobe = 64;                    // Clusters to search
    
    // PQ parameters (for IVF_PQ)
    int pq_m = 32;                      // Number of subquantizers
    int pq_bits = 8;                    // Bits per subquantizer
    
    // HNSW parameters
    int hnsw_m = 32;                    // Number of connections per layer
    int hnsw_ef_construction = 200;     // Construction-time ef
    int hnsw_ef_search = 128;           // Search-time ef
    
    // GPU acceleration
    bool use_gpu = false;
    int gpu_device_id = 0;
    
    // Search defaults
    int default_top_k = 12;
    float similarity_threshold = 0.0f;  // Minimum similarity (0 = no threshold)
    
    // Persistence
    std::string storage_path;
    bool auto_save = true;
    size_t auto_save_interval = 1000;   // Save every N additions
};

/**
 * LTMFaiss - Long-Term Memory with FAISS
 */
class LTMFaiss {
public:
    explicit LTMFaiss(const LTMConfig& config);
    ~LTMFaiss();
    
    // Non-copyable
    LTMFaiss(const LTMFaiss&) = delete;
    LTMFaiss& operator=(const LTMFaiss&) = delete;
    
    // =========================================================================
    // Core Operations
    // =========================================================================
    
    /**
     * Add a chunk to LTM
     * 
     * @param chunk The code chunk with all metadata
     * @param embedding Pre-computed embedding vector
     */
    void add(const CodeChunk& chunk, const std::vector<float>& embedding);
    
    /**
     * Batch add for efficiency
     * 
     * Much more efficient than individual adds for large ingestion.
     * @param chunks Vector of code chunks
     * @param embeddings Corresponding embeddings (same order)
     */
    void addBatch(
        const std::vector<CodeChunk>& chunks,
        const std::vector<std::vector<float>>& embeddings
    );
    
    /**
     * Search by embedding similarity
     * 
     * @param query_embedding Query vector
     * @param top_k Number of results to return
     * @param repo_filter Optional: limit to specific repository
     * @return Vector of retrieved chunks with scores
     */
    std::vector<RetrievedChunk> search(
        const std::vector<float>& query_embedding,
        int top_k = -1,
        const std::string& repo_filter = ""
    );
    
    /**
     * Search with multiple queries (batched)
     */
    std::vector<std::vector<RetrievedChunk>> searchBatch(
        const std::vector<std::vector<float>>& query_embeddings,
        int top_k = -1
    );
    
    /**
     * Hybrid search: vector + lexical (BM25)
     * 
     * Combines FAISS vector search with keyword matching using RRF fusion.
     * @param query_text Original query text (for lexical matching)
     * @param query_embedding Query embedding (for vector search)
     * @param top_k Number of results
     * @param alpha Weight for vector vs lexical (0.7 = 70% vector)
     */
    std::vector<RetrievedChunk> hybridSearch(
        const std::string& query_text,
        const std::vector<float>& query_embedding,
        int top_k = -1,
        float alpha = 0.7f
    );
    
    // =========================================================================
    // CRUD Operations
    // =========================================================================
    
    /**
     * Get chunk by ID
     */
    std::optional<CodeChunk> get(const std::string& chunk_id);
    
    /**
     * Check if chunk exists
     */
    bool contains(const std::string& chunk_id);
    
    /**
     * Remove chunk by ID
     */
    bool remove(const std::string& chunk_id);
    
    /**
     * Remove all chunks for a repository
     * @return Number of chunks removed
     */
    size_t removeByRepo(const std::string& repo_id);
    
    /**
     * Update chunk metadata (without re-indexing embedding)
     */
    bool updateMetadata(const std::string& chunk_id, const MemoryMetadata& metadata);
    
    /**
     * Update importance score
     */
    void updateImportance(const std::string& chunk_id, double delta);
    
    // =========================================================================
    // Memory Management
    // =========================================================================
    
    /**
     * Consolidate memory (remove low-importance items)
     * @param threshold Minimum importance to keep (use config default if < 0)
     * @return Number of items removed
     */
    size_t consolidate(double threshold = -1.0);
    
    /**
     * Rebuild the FAISS index
     * 
     * Call after many deletions to reclaim space and improve search quality.
     */
    void rebuildIndex();
    
    /**
     * Train the index (required for IVF/PQ before adding data)
     * 
     * @param training_embeddings Sample embeddings for training
     */
    void train(const std::vector<std::vector<float>>& training_embeddings);
    
    /**
     * Check if index is trained
     */
    bool isTrained() const;
    
    // =========================================================================
    // Persistence
    // =========================================================================
    
    /**
     * Save to storage
     */
    void save();
    
    /**
     * Save to specific path
     */
    void save(const std::string& path);
    
    /**
     * Load from storage
     */
    void load();
    
    /**
     * Load from specific path
     */
    void load(const std::string& path);
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    struct Stats {
        size_t total_chunks = 0;
        size_t total_repos = 0;
        size_t index_vectors = 0;       // Vectors in FAISS index
        size_t memory_bytes = 0;        // Estimated memory usage
        bool is_trained = false;
        FAISSIndexType index_type;
        std::map<std::string, size_t> chunks_per_repo;
        std::map<std::string, size_t> chunks_per_language;
    };
    
    Stats getStats() const;
    
    /**
     * Get all repository IDs
     */
    std::vector<std::string> getRepositories() const;
    
    /**
     * Get chunk count for a repository
     */
    size_t getRepoChunkCount(const std::string& repo_id) const;

private:
    LTMConfig config_;
    
    // FAISS index (pimpl for FAISS types)
    class FAISSIndexImpl;
    std::unique_ptr<FAISSIndexImpl> faiss_impl_;
    
    // Metadata storage (chunk_id -> metadata)
    std::unordered_map<std::string, CodeChunk> chunks_;
    std::unordered_map<std::string, MemoryMetadata> metadata_;
    
    // Indexes for fast lookup
    std::unordered_map<std::string, std::vector<std::string>> repo_to_chunks_;  // repo_id -> chunk_ids
    std::unordered_map<std::string, int64_t> chunk_id_to_faiss_id_;             // chunk_id -> FAISS internal ID
    std::unordered_map<int64_t, std::string> faiss_id_to_chunk_id_;             // FAISS ID -> chunk_id
    
    // For lexical search (BM25)
    class LexicalIndex;
    std::unique_ptr<LexicalIndex> lexical_index_;
    
    // Thread safety
    mutable std::mutex mutex_;
    
    // Internal ID counter
    int64_t next_faiss_id_ = 0;
    size_t additions_since_save_ = 0;
    
    // Helpers
    FAISSIndexType selectIndexType(size_t expected_size);
    void initializeFAISSIndex(FAISSIndexType type);
    std::vector<RetrievedChunk> convertResults(
        const std::vector<int64_t>& ids,
        const std::vector<float>& distances
    );
    void maybeAutoSave();
};

} // namespace aipr::tms
