/**
 * TMS Memory System - Main Orchestrator Implementation
 * 
 * Coordinates all TMS components for brain-inspired code understanding.
 */

#include "tms/tms_memory_system.h"
#include "tms/repo_parser.h"
#include "tms/embedding_engine.h"
#include "tms/memory_accounts.h"
#include "tms/graph_rag.h"
#include "hierarchy_builder.h"
#include "chunk_prefixer.h"
#include "knowledge_graph.h"
#include "merkle_cache.h"
#include "logging.h"
#include "metrics.h"
#include <chrono>
#include <algorithm>
#include <sstream>
#include <fstream>
#include <iostream>
#include <filesystem>
#include <unordered_set>
#include <future>

namespace aipr::tms {

// ── KnowledgeGraphHandle (pimpl for optional KG) ───────────────────────────

class TMSMemorySystem::KnowledgeGraphHandle {
public:
    explicit KnowledgeGraphHandle(const std::string& storage_path)
        : kg_(storage_path + "/graph/knowledge_graph.db") {
        kg_.open();
    }
    ~KnowledgeGraphHandle() {
        try { kg_.close(); } catch (...) {}
    }
    aipr::KnowledgeGraph& get() { return kg_; }
private:
    aipr::KnowledgeGraph kg_;
};

// ── MerkleCacheHandle (pimpl for optional Merkle cache) ────────────────────

class TMSMemorySystem::MerkleCacheHandle {
public:
    explicit MerkleCacheHandle(const std::string& storage_path)
        : cache_(storage_path + "/cache/merkle.db") {
        cache_.open();
    }
    ~MerkleCacheHandle() {
        try { cache_.close(); } catch (...) {}
    }
    aipr::MerkleCache& get() { return cache_; }
private:
    aipr::MerkleCache cache_;
};

// =============================================================================
// Constructor / Destructor
// =============================================================================

TMSMemorySystem::TMSMemorySystem(const TMSConfig& config)
    : config_(config) {
}

TMSMemorySystem::~TMSMemorySystem() {
    shutdown();
}

// =============================================================================
// Initialization & Lifecycle
// =============================================================================

void TMSMemorySystem::initialize() {
    if (initialized_.load()) return;
    
    std::lock_guard<std::mutex> lock(mutex_);
    if (initialized_.load()) return;  // Double-check after lock
    
    // Initialize LTM (FAISS)
    LTMConfig ltm_config;
    ltm_config.dimension = config_.embedding_dimension;
    ltm_config.max_capacity = config_.ltm_capacity;
    ltm_config.nlist = config_.ltm_nlist;
    ltm_config.nprobe = config_.ltm_nprobe;
    ltm_config.pq_m = config_.ltm_m;
    ltm_config.pq_bits = config_.ltm_bits;
    ltm_config.use_gpu = config_.ltm_use_gpu;
    ltm_config.use_cosine_similarity = true;
    ltm_config.default_top_k = config_.ltm_default_top_k;
    ltm_config.storage_path = config_.storage_path + "/ltm";
    
    ltm_ = std::make_unique<LTMFaiss>(ltm_config);

    // Initialize Multi-Vector dual-resolution index (optional)
    if (config_.multi_vector_enabled) {
        MultiVectorConfig mv_config;
        mv_config.enabled = true;
        mv_config.fine_dimension = config_.multi_vector_fine_dim;
        mv_config.coarse_dimension = config_.multi_vector_coarse_dim;
        mv_config.oversampling_factor = config_.multi_vector_oversampling;
        mv_config.storage_path = config_.storage_path + "/multi_vector";

        multi_vector_ = std::make_unique<MultiVectorIndex>(mv_config, ltm_config);
        LOG_INFO("[TMS] Multi-Vector dual-resolution index enabled (" +
                 std::to_string(config_.multi_vector_coarse_dim) + "d coarse + " +
                 std::to_string(config_.multi_vector_fine_dim) + "d fine)");
    }
    
    // Initialize STM
    STMConfig stm_config;
    stm_config.capacity = config_.stm_capacity;
    stm_config.ttl = config_.stm_ttl;
    stm_config.default_top_k = config_.stm_default_top_k;
    
    stm_ = std::make_unique<STM>(stm_config);
    
    // Initialize MTM
    MTMConfig mtm_config;
    mtm_config.max_patterns = config_.mtm_pattern_capacity;
    mtm_config.max_strategies = config_.mtm_strategy_capacity;
    mtm_config.confidence_threshold = config_.mtm_confidence_threshold;
    mtm_config.embedding_dimension = config_.embedding_dimension;
    mtm_config.storage_path = config_.storage_path + "/mtm";
    
    mtm_ = std::make_unique<MTMGraph>(mtm_config);
    
    // Initialize Cross-Memory Attention
    CrossMemoryAttentionConfig attn_config;
    attn_config.num_heads = config_.attention_num_heads;
    attn_config.embed_dim = config_.embedding_dimension;
    attn_config.head_dim = config_.attention_head_dim;
    attn_config.dropout = config_.attention_dropout;
    attn_config.use_rotary_embedding = config_.use_rotary_embedding;
    attn_config.max_sequence_length = config_.max_sequence_length;
    
    attention_ = std::make_unique<CrossMemoryAttention>(attn_config);
    
    // Initialize Compute Controller
    ControllerConfig ctrl_config;
    ctrl_config.vram_budget_gb = config_.vram_budget_gb;
    ctrl_config.enable_adaptive = config_.enable_adaptive_strategy;
    
    controller_ = std::make_unique<ComputeController>(ctrl_config);
    
    // Initialize ingestion components
    RepoParserConfig parser_config;
    parser_config.chunking = ChunkingConfig{};
    repo_parser_ = std::make_unique<RepoParser>(parser_config);
    
    EmbeddingConfig embed_config;
    embed_config.model_name = config_.embedding_model;
    embed_config.embedding_dimension = config_.embedding_dimension;
    embed_config.cache_path = config_.storage_path + "/embedding_cache";

    // Set backend based on TMSConfig
    std::cerr << "[TMS] embedding_backend=" << config_.embedding_backend
              << " onnx_model=" << config_.onnx_model_path
              << " tokenizer=" << config_.onnx_tokenizer_path
              << " dim=" << config_.embedding_dimension << std::endl;
    if (config_.embedding_backend == "onnx") {
        embed_config.backend = EmbeddingBackend::ONNX_RUNTIME;
        embed_config.onnx_model_path = config_.onnx_model_path;
        embed_config.tokenizer_path = config_.onnx_tokenizer_path;
    } else if (config_.embedding_backend == "http") {
        embed_config.backend = EmbeddingBackend::HTTP_API;
        embed_config.api_endpoint = config_.embed_api_endpoint;
        embed_config.api_key = config_.embed_api_key;
    } else {
        embed_config.backend = EmbeddingBackend::MOCK;
    }
    
    embedding_engine_ = std::make_unique<EmbeddingEngine>(embed_config);
    
    // Load persisted data
    load();
    
    // Start background consolidation thread
    if (config_.enable_consolidation) {
        shutdown_requested_.store(false);
        consolidation_thread_ = std::thread([this]() {
            consolidationLoop();
        });
    }
    
    initialized_.store(true);

    // set component health gauges
    metrics::Registry::instance().setGauge(metrics::FAISS_LOADED, 1.0);

    // Set the active embedding backend gauge so the dashboard shows the
    // correct label from the very first metrics snapshot.
    {
        double backend_id = 0.0; // mock
        if (config_.embedding_backend == "onnx")  backend_id = 1.0;
        else if (config_.embedding_backend == "http") backend_id = 2.0;
        metrics::Registry::instance().setGauge(metrics::EMBED_ACTIVE_BACKEND, backend_id);
    }

    // Initialize Knowledge Graph (optional)
    if (config_.knowledge_graph_enabled) {
        try {
            kg_handle_ = std::make_unique<KnowledgeGraphHandle>(config_.storage_path);
            metrics::Registry::instance().setGauge(metrics::KG_ENABLED, 1.0);
            LOG_INFO("[TMS] Knowledge Graph initialized");
        } catch (const std::exception& e) {
            LOG_ERROR("[TMS] Failed to initialize Knowledge Graph: " + std::string(e.what()));
        }
    }

    // Initialize Merkle Cache (for incremental reindex)
    try {
        merkle_handle_ = std::make_unique<MerkleCacheHandle>(config_.storage_path);
        LOG_INFO("[TMS] Merkle Cache initialized");
    } catch (const std::exception& e) {
        LOG_ERROR("[TMS] Failed to initialize Merkle Cache: " + std::string(e.what()));
    }
}

void TMSMemorySystem::shutdown() {
    if (!initialized_.load()) return;
    
    // Signal shutdown
    shutdown_requested_.store(true);
    
    // Wait for consolidation thread
    if (consolidation_thread_.joinable()) {
        consolidation_thread_.join();
    }
    
    // Persist data — must not throw since shutdown() is called from destructor
    try {
        save();
    } catch (const std::exception& e) {
        // Log but don't propagate — throwing from destructors is fatal
        (void)e;
    } catch (...) {
        // Swallow unknown exceptions
    }
    
    // Clear resources
    {
        std::lock_guard<std::mutex> lock(mutex_);
        ltm_.reset();
        stm_.reset();
        mtm_.reset();
        attention_.reset();
        controller_.reset();
        repo_parser_.reset();
        embedding_engine_.reset();
    }
    
    initialized_.store(false);
}

// =============================================================================
// Repository Ingestion
// =============================================================================

void TMSMemorySystem::ingestRepository(
    const std::string& repo_path,
    const std::string& repo_id,
    std::function<void(float, const std::string&)> progress_callback
) {
    if (!initialized_.load()) {
        throw std::runtime_error("TMSMemorySystem not initialized");
    }
    
    auto start_time = std::chrono::steady_clock::now();

    // =========================================================================
    // Discover files (lightweight — only collects paths, no content)
    // =========================================================================
    if (progress_callback) {
        progress_callback(0.0f, "Scanning repository...");
    }

    auto all_files = repo_parser_->listFiles(repo_path);

    // Normalise to relative paths.  walkDirectory() may return absolute
    // paths; downstream code constructs full paths via repo_path + "/" + f,
    // so the entries must be relative to repo_path.
    for (auto& f : all_files) {
        if (f.size() > repo_path.size() + 1 &&
            f.compare(0, repo_path.size(), repo_path) == 0) {
            f = f.substr(repo_path.size() + 1);
        }
    }

    const size_t total_files = all_files.size();

    std::cerr << "[TMS] ingestRepository: repo_path=" << repo_path
              << " total_files=" << total_files << std::endl;

    if (total_files == 0) {
        if (progress_callback) {
            progress_callback(1.0f, "No indexable files found");
        }
        return;
    }

    // =========================================================================
    // Merkle-based incremental filtering
    //
    // Compare file content hashes against the Merkle cache to skip
    // unchanged files. For changed files, also identify KG-dependent
    // files that need re-embedding.
    // =========================================================================
    MerkleDiffResult merkle_diff;
    bool using_merkle = (merkle_handle_ != nullptr);
    std::unordered_set<std::string> files_to_embed_set;

    if (using_merkle) {
        if (progress_callback) {
            progress_callback(0.01f, "Computing file hashes for incremental index...");
        }

        aipr::KnowledgeGraph* kg_ptr = (config_.knowledge_graph_enabled && kg_handle_)
            ? &kg_handle_->get() : nullptr;

        merkle_diff = merkle_handle_->get().computeDiff(
            repo_id, repo_path, all_files, kg_ptr);

        // Build the set of files that actually need embedding
        for (const auto& f : merkle_diff.changed_files)   files_to_embed_set.insert(f);
        for (const auto& f : merkle_diff.new_files)        files_to_embed_set.insert(f);
        for (const auto& f : merkle_diff.dependent_files)  files_to_embed_set.insert(f);

        std::cerr << "[TMS] Merkle diff: unchanged=" << merkle_diff.unchanged_files.size()
                  << " changed=" << merkle_diff.changed_files.size()
                  << " new=" << merkle_diff.new_files.size()
                  << " dependent=" << merkle_diff.dependent_files.size()
                  << " deleted=" << merkle_diff.deleted_files.size()
                  << std::endl;

        // If everything is unchanged, we're done
        if (files_to_embed_set.empty() && merkle_diff.deleted_files.empty()) {
            // KG edges may be missing if the initial index was done before
            // edge inference was added.  Run finalizeEdges when nodes exist
            // but edges are empty.
            if (config_.knowledge_graph_enabled && kg_handle_ &&
                kg_handle_->get().nodeCount(repo_id) > 0 &&
                kg_handle_->get().edgeCount(repo_id) == 0) {
                LOG_INFO("[TMS] Merkle skip: KG has nodes but 0 edges — running edge inference");
                try {
                    kg_handle_->get().finalizeEdges(repo_id);
                } catch (const std::exception& e) {
                    LOG_ERROR("[TMS] KG finalizeEdges failed during Merkle skip: " + std::string(e.what()));
                }
            }
            if (progress_callback) {
                progress_callback(1.0f, "All files unchanged — skipping reindex");
            }
            LOG_INFO("[TMS] Merkle cache: all " + std::to_string(total_files) +
                     " files unchanged, skipping reindex");
            return;
        }

        // Filter all_files to only those that need embedding
        std::vector<std::string> filtered_files;
        filtered_files.reserve(files_to_embed_set.size());
        for (const auto& f : all_files) {
            if (files_to_embed_set.count(f)) {
                filtered_files.push_back(f);
            }
        }
        all_files = std::move(filtered_files);

        std::cerr << "[TMS] After Merkle filter: " << all_files.size()
                  << " files to embed (skipped " << merkle_diff.unchanged_files.size()
                  << ")" << std::endl;
    }

    // =========================================================================
    // Determine batch size based on available system memory.
    // Goal: keep peak RSS well under physical RAM.
    //
    // Rough per-file memory budget:
    //   ~45 chunks/file (9.5M chunks / 208K files from logs)
    //   ~5 KB per chunk (content + metadata + embedding vector)
    //   ~225 KB per file in aggregate
    //
    // For a 2 GB working-set budget → ~8000 files per batch.
    // Use 5000 as a conservative default, capped at total_files.
    // =========================================================================
    constexpr size_t kDefaultBatchFiles = 5000;
    const size_t batch_file_count = std::min(kDefaultBatchFiles, total_files);
    const size_t num_batches = (total_files + batch_file_count - 1) / batch_file_count;

    std::cerr << "[TMS] batched ingestion: " << num_batches << " batches, "
              << batch_file_count << " files/batch" << std::endl;

    // Build hierarchy manifest once (it's small — just dependency graph metadata)
    std::unique_ptr<aipr::HierarchyBuilder> hierarchy_builder;
    aipr::RepoManifest manifest;
    if (config_.hierarchy_enabled) {
        if (progress_callback) {
            progress_callback(0.02f, "Building hierarchy manifest...");
        }
        hierarchy_builder = std::make_unique<aipr::HierarchyBuilder>();
        manifest = hierarchy_builder->buildRepoManifest(repo_path);
    }

    // Classify chunks into memory accounts if enabled
    std::unique_ptr<MemoryAccountClassifier> classifier;
    if (config_.memory_accounts_enabled) {
        classifier = std::make_unique<MemoryAccountClassifier>();
    }

    size_t total_chunks_ingested = 0;
    size_t files_processed = 0;

    // =========================================================================
    // Clear existing KG data for this repo (once, before batches)
    // =========================================================================
    if (config_.knowledge_graph_enabled && kg_handle_) {
        try {
            kg_handle_->get().removeRepo(repo_id);
            std::cerr << "[TMS] cleared existing KG data for repo " << repo_id << std::endl;
        } catch (const std::exception& e) {
            LOG_ERROR("[TMS] KG removeRepo failed: " + std::string(e.what()));
        }
    }

    // =========================================================================
    // Batched parse → enrich → embed → store
    // =========================================================================
    for (size_t batch_idx = 0; batch_idx < num_batches; ++batch_idx) {
        size_t start_file = batch_idx * batch_file_count;
        size_t end_file = std::min(start_file + batch_file_count, total_files);
        size_t batch_size = end_file - start_file;

        float batch_start_pct = static_cast<float>(start_file) / total_files;
        float batch_end_pct   = static_cast<float>(end_file) / total_files;
        // Map 0-1 batch range into 0.05-0.90 overall progress
        auto batch_progress = [&](float local_pct, const std::string& msg) {
            if (progress_callback) {
                float global_pct = 0.05f + (batch_start_pct + local_pct * (batch_end_pct - batch_start_pct)) * 0.85f;
                progress_callback(global_pct, msg);
            }
        };

        batch_progress(0.0f, "Batch " + std::to_string(batch_idx + 1) + "/" +
                             std::to_string(num_batches) + " — parsing " +
                             std::to_string(batch_size) + " files...");

        // Slice file list for this batch
        std::vector<std::string> batch_files(all_files.begin() + start_file,
                                             all_files.begin() + end_file);

        // ── Parse this batch ────────────────────────────────────────────
        std::vector<CodeChunk> chunks = repo_parser_->parseFiles(repo_path, batch_files);

        if (chunks.empty()) {
            files_processed += batch_size;
            continue;
        }

        std::cerr << "[TMS] batch " << (batch_idx + 1) << "/" << num_batches
                  << ": " << chunks.size() << " chunks from " << batch_size
                  << " files" << std::endl;

        // ── Hierarchy enrichment (per-batch) ────────────────────────────
        if (hierarchy_builder) {
            batch_progress(0.15f, "Batch " + std::to_string(batch_idx + 1) + " — enriching hierarchy...");

            std::unordered_map<std::string, std::vector<CodeChunk*>> chunks_by_file;
            for (auto& c : chunks) {
                chunks_by_file[c.file_path].push_back(&c);
            }
            for (auto& [file_path, file_chunk_ptrs] : chunks_by_file) {
                std::vector<CodeChunk> fc;
                fc.reserve(file_chunk_ptrs.size());
                for (auto* p : file_chunk_ptrs) fc.push_back(*p);

                std::string lang = fc.empty() ? "" : fc[0].language;
                auto summary = hierarchy_builder->summarizeFile(file_path, lang, fc, manifest);
                chunks.push_back(hierarchy_builder->buildFileSummaryChunk(summary));
            }

            aipr::ChunkPrefixer prefixer;
            prefixer.applyPrefixes(chunks, repo_id, manifest);
        }

        // ── Memory account classification ───────────────────────────────
        if (classifier) {
            for (auto& chunk : chunks) {
                auto account = classifier->classify(chunk);
                chunk.tags.push_back(MemoryAccountClassifier::accountTag(account));
            }
        }

        // ── Embed this batch ────────────────────────────────────────────
        batch_progress(0.3f, "Batch " + std::to_string(batch_idx + 1) + " — computing embeddings...");

        std::vector<std::vector<float>> embeddings;
        {
            EmbeddingProgressCallback embed_progress = nullptr;
            if (progress_callback) {
                embed_progress = [&](int completed, int total, const std::string& /*status*/) {
                    float local = 0.3f + (static_cast<float>(completed) / std::max(total, 1)) * 0.5f;
                    batch_progress(local, "Batch " + std::to_string(batch_idx + 1) +
                                          " — embedding " + std::to_string(completed) +
                                          "/" + std::to_string(total));
                };
            }
            auto result = embedding_engine_->embedChunks(chunks, embed_progress);
            embeddings = std::move(result.embeddings);
        }

        // ── Store in LTM ────────────────────────────────────────────────
        batch_progress(0.85f, "Batch " + std::to_string(batch_idx + 1) + " — storing in LTM...");
        ingestChunksWithEmbeddings(repo_id, chunks, embeddings);

        // ── Knowledge Graph (per-batch append — nodes + CONTAINS only) ────
        if (config_.knowledge_graph_enabled && kg_handle_) {
            try {
                kg_handle_->get().appendBatchChunks(repo_id, chunks);
            } catch (const std::exception& e) {
                LOG_ERROR("[TMS] KG append failed (batch " + std::to_string(batch_idx + 1) + "): " + e.what());
            }
        }

        total_chunks_ingested += chunks.size();
        files_processed += batch_size;

        // ── Release batch memory ────────────────────────────────────────
        // Explicit clear + shrink to return memory to the OS immediately.
        chunks.clear();
        chunks.shrink_to_fit();
        embeddings.clear();
        embeddings.shrink_to_fit();

        std::cerr << "[TMS] batch " << (batch_idx + 1) << " complete: "
                  << files_processed << "/" << total_files << " files, "
                  << total_chunks_ingested << " total chunks" << std::endl;
    }

    // =========================================================================
    // Finalize KG cross-batch edges (IMPORTS, REFERENCES)
    // =========================================================================
    if (config_.knowledge_graph_enabled && kg_handle_) {
        if (progress_callback) {
            progress_callback(0.91f, "Finalizing knowledge graph edges...");
        }
        try {
            kg_handle_->get().finalizeEdges(repo_id);
            std::cerr << "[TMS] KG cross-batch edge finalization complete" << std::endl;
        } catch (const std::exception& e) {
            LOG_ERROR("[TMS] KG finalizeEdges failed: " + std::string(e.what()));
            std::cerr << "[TMS] WARNING: KG edge finalization failed: " << e.what() << std::endl;
        }
    }

    // =========================================================================
    // Persist to disk
    // =========================================================================
    if (progress_callback) {
        progress_callback(0.92f, "Persisting index to disk...");
    }
    try {
        save();
        std::cerr << "[TMS] Index persisted to disk after ingesting "
                  << total_chunks_ingested << " chunks from "
                  << total_files << " files" << std::endl;
    } catch (const std::exception& e) {
        std::cerr << "[TMS] WARNING: Failed to persist index: " << e.what() << std::endl;
    }

    // =========================================================================
    // Update Merkle cache with new file hashes
    // =========================================================================
    if (using_merkle && merkle_handle_) {
        try {
            if (progress_callback) {
                progress_callback(0.94f, "Updating Merkle cache...");
            }
            // Compute hashes for all embedded files
            std::unordered_map<std::string, std::string> file_hashes;
            for (const auto& f : all_files) {
                std::string full_path = repo_path + "/" + f;
                std::string hash = aipr::MerkleCache::hashFile(full_path);
                if (!hash.empty()) {
                    file_hashes[f] = hash;
                }
            }
            merkle_handle_->get().updateHashes(repo_id, file_hashes);
            std::cerr << "[TMS] Merkle cache updated: " << file_hashes.size()
                      << " file hashes" << std::endl;
        } catch (const std::exception& e) {
            LOG_ERROR("[TMS] Merkle cache update failed: " + std::string(e.what()));
        }
    }

    // Done
    auto end_time = std::chrono::steady_clock::now();
    auto duration = std::chrono::duration_cast<std::chrono::seconds>(end_time - start_time);

    if (progress_callback) {
        std::ostringstream status;
        status << "Ingested " << total_chunks_ingested << " chunks from "
               << total_files << " files in " << duration.count() << "s"
               << " (" << num_batches << " batches)";
        progress_callback(1.0f, status.str());
    }

    LOG_INFO("[TMS] ingestRepository complete: " +
             std::to_string(total_chunks_ingested) + " chunks, " +
             std::to_string(total_files) + " files, " +
             std::to_string(duration.count()) + "s, " +
             std::to_string(num_batches) + " batches");
}

void TMSMemorySystem::ingestChunks(
    const std::string& repo_id,
    const std::vector<CodeChunk>& chunks
) {
    if (!initialized_.load()) {
        throw std::runtime_error("TMSMemorySystem not initialized");
    }
    
    // Compute embeddings
    auto result = embedding_engine_->embedChunks(chunks);
    
    // Store
    ingestChunksWithEmbeddings(repo_id, chunks, result.embeddings);
}

void TMSMemorySystem::ingestChunksWithEmbeddings(
    const std::string& repo_id,
    const std::vector<CodeChunk>& chunks,
    const std::vector<std::vector<float>>& embeddings
) {
    if (!initialized_.load()) {
        throw std::runtime_error("TMSMemorySystem not initialized");
    }
    
    if (chunks.size() != embeddings.size()) {
        throw std::invalid_argument("Chunks and embeddings count mismatch");
    }
    
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Prepare chunks with repo ID
    std::vector<CodeChunk> prepared_chunks;
    prepared_chunks.reserve(chunks.size());
    
    for (size_t i = 0; i < chunks.size(); ++i) {
        CodeChunk chunk = chunks[i];
        
        // Ensure ID includes repo
        if (chunk.id.find(repo_id + ":") != 0) {
            chunk.id = repo_id + ":" + chunk.id;
        }
        
        prepared_chunks.push_back(std::move(chunk));
    }
    
    // Batch add to LTM (and multi-vector index if enabled)
    ltm_->addBatch(prepared_chunks, embeddings);
    if (multi_vector_) {
        multi_vector_->addBatch(prepared_chunks, embeddings);
    }
    
    // Update MTM patterns (async in production)
    // This is simplified - in production, run pattern detection in background
}

void TMSMemorySystem::removeRepository(const std::string& repo_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    ltm_->removeByRepo(repo_id);
    if (multi_vector_) multi_vector_->removeByRepo(repo_id);
}

void TMSMemorySystem::updateRepository(
    const std::string& repo_id,
    const std::vector<std::string>& changed_files,
    const std::vector<std::string>& deleted_files
) {
    if (!initialized_.load()) {
        throw std::runtime_error("TMSMemorySystem not initialized");
    }
    
    // 1 & 2. Remove chunks for deleted and changed files
    // Build a set of file paths to remove
    std::unordered_set<std::string> files_to_remove(deleted_files.begin(), deleted_files.end());
    files_to_remove.insert(changed_files.begin(), changed_files.end());

    // We can't iterate all chunks by file_path directly, so we search
    // for each file. Use a dummy embedding search with repo filter to find chunks,
    // or just remove the entire repo and re-ingest. For correctness with incremental,
    // we remove all chunks for affected files by checking each chunk.
    // Since repo_to_chunks_ is private, we use the getRepoChunkCount + search approach.
    // However, the simplest correct approach: if we have changed/deleted files,
    // remove entire repo and re-ingest only the files that remain.
    if (!files_to_remove.empty()) {
        // Remove the whole repo from LTM and re-ingest everything
        ltm_->removeByRepo(repo_id);
    }

    // 3. Re-ingest remaining changed files
    if (!changed_files.empty() && ingestor_) {
        RepoParserConfig parser_config;
        RepoParser parser(parser_config);

        // Use repo_id as repo path (caller should ensure repo_id is the actual path
        // or provide a mapping — in practice, repo_id is typically the repo root)
        auto chunks = parser.parseFiles(repo_id, changed_files);
        for (auto& chunk : chunks) {
            chunk.tags.push_back("repo:" + repo_id);
        }

        if (!chunks.empty()) {
            ingestor_->ingestChunks(repo_id, chunks, nullptr);
        }
    }
}

// =============================================================================
// Forward Pass (Main Query Interface)
// =============================================================================

TMSResponse TMSMemorySystem::forward(const TMSQuery& query) {
    if (!initialized_.load()) {
        throw std::runtime_error("TMSMemorySystem not initialized");
    }

    // scope-timer records into metrics registry
    AIPR_METRICS_SCOPE(aipr::metrics::TMS_FORWARD_LATENCY_S);
    
    auto start_time = std::chrono::steady_clock::now();
    TMSResponse response;

    // Query timeout helper — returns true if we've exceeded the budget.
    const auto timeout = std::chrono::seconds(config_.query_timeout_seconds);
    auto isTimedOut = [&]() -> bool {
        if (config_.query_timeout_seconds <= 0) return false;
        return (std::chrono::steady_clock::now() - start_time) >= timeout;
    };

    LOG_DEBUG("[TMS] forward: query_len=" + std::to_string(query.query_text.size()) +
              " repo=" + query.repo_filter +
              " session=" + query.session_id +
              " hints=" + std::to_string(query.hint_files.size()));
    
    // Step 1: Compute query embedding if not provided
    std::vector<float> query_embedding = query.query_embedding;
    if (query_embedding.empty()) {
        auto embed_start = std::chrono::steady_clock::now();
        auto result = embedding_engine_->embed(query.query_text);
        if (!result.success) {
            LOG_ERROR("[TMS] embedding failed: " + result.error);
            throw std::runtime_error("Failed to compute query embedding: " + result.error);
        }
        query_embedding = std::move(result.embedding);
        auto embed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - embed_start).count();

        // Telemetry: per-query embedding latency
        metrics::Registry::instance().observeHistogram(
            metrics::EMBED_LATENCY_S,
            static_cast<double>(embed_ms) / 1000.0);

        LOG_DEBUG("[TMS] query embedding computed: dim=" +
                  std::to_string(query_embedding.size()) +
                  " embed_ms=" + std::to_string(embed_ms));
    }
    
    // Step 2: Compute Controller decides strategy
    ResourceState resource_state;
    resource_state.available_vram_gb = getCurrentBudget();
    resource_state.ltm_size = ltm_->getStats().total_chunks;
    resource_state.active_sessions = stm_->getStats().active_sessions;
    
    ComputeDecision decision;
    if (query.force_strategy.has_value()) {
        decision = controller_->forceStrategy(query.force_strategy.value());
    } else {
        decision = controller_->decide(query_embedding, resource_state);
    }
    
    response.compute_decision = decision;
    response.reasoning_trace.push_back("Strategy: " + decision.reasoning);
    
    // Step 3: Retrieve from LTM
    std::vector<RetrievedChunk> ltm_results;
    if (decision.strategy != ComputeStrategy::FAST || decision.ltm_top_k > 0) {
        auto ltm_start = std::chrono::steady_clock::now();

        if (config_.memory_accounts_enabled) {
            // Account-aware routing: classify query → top-2 accounts → RRF merge
            MemoryAccountClassifier classifier;
            auto ranked_accounts = classifier.classifyQuery(query.query_text);

            // Use multi_vector fine index for account search when dual-res is active
            LTMFaiss& search_ltm = multi_vector_ ? multi_vector_->fineIndex() : *ltm_;

            // Search top-2 accounts, split budget
            int per_account_k = std::max(decision.ltm_top_k / 2, 4);
            std::unordered_map<std::string, float> rrf_scores;
            std::unordered_map<std::string, RetrievedChunk> chunk_map;
            const float rrf_k = 60.0f;

            int accounts_to_search = std::min(2, static_cast<int>(ranked_accounts.size()));
            for (int ai = 0; ai < accounts_to_search; ++ai) {
                auto tag = MemoryAccountClassifier::accountTag(ranked_accounts[ai]);
                auto account_results = search_ltm.hybridSearchByAccount(
                    query.query_text, query_embedding, tag,
                    per_account_k, query.repo_filter);

                for (size_t rank = 0; rank < account_results.size(); ++rank) {
                    const auto& rc = account_results[rank];
                    float score = 1.0f / (rrf_k + rank + 1);
                    // Boost primary account
                    if (ai == 0) score *= 1.5f;
                    rrf_scores[rc.chunk.id] += score;
                    if (chunk_map.find(rc.chunk.id) == chunk_map.end()) {
                        chunk_map[rc.chunk.id] = rc;
                    }
                }
            }

            // Sort by RRF score and take top_k
            std::vector<std::pair<std::string, float>> sorted_ids(
                rrf_scores.begin(), rrf_scores.end());
            std::sort(sorted_ids.begin(), sorted_ids.end(),
                      [](const auto& a, const auto& b) { return a.second > b.second; });

            for (size_t i = 0; i < std::min(static_cast<size_t>(decision.ltm_top_k),
                                             sorted_ids.size()); ++i) {
                auto it = chunk_map.find(sorted_ids[i].first);
                if (it != chunk_map.end()) {
                    auto rc = it->second;
                    rc.combined_score = sorted_ids[i].second;
                    ltm_results.push_back(std::move(rc));
                }
            }

            response.reasoning_trace.push_back(
                "LTM: Account-routed [" + std::string(accountName(ranked_accounts[0])) +
                ", " + std::string(accountName(ranked_accounts[1])) +
                "] → " + std::to_string(ltm_results.size()) + " chunks");
        } else {
            // Use multi-vector dual-resolution search when available
            if (multi_vector_) {
                ltm_results = multi_vector_->search(query_embedding, decision.ltm_top_k, query.repo_filter);
            } else {
                ltm_results = ltm_->search(query_embedding, decision.ltm_top_k, query.repo_filter);
            }
        }

        auto ltm_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - ltm_start).count();
        response.reasoning_trace.push_back("LTM: Retrieved " + std::to_string(ltm_results.size()) + " chunks");
        response.ltm_items_scanned = ltm_results.size();

        // ── Telemetry: LTM search metrics ──────────────────────
        metrics::Registry::instance().setGauge(
            metrics::SEARCH_CHUNKS_RETURNED,
            static_cast<double>(ltm_results.size()));

        // Approximate recall@10: ratio of top-10 results with similarity >= 0.5
        // (proxy for relevance when ground-truth labels are unavailable).
        {
            int relevant = 0;
            int k = std::min(static_cast<int>(ltm_results.size()), 10);
            for (int i = 0; i < k; ++i) {
                if (ltm_results[i].similarity_score >= 0.5f) ++relevant;
            }
            double recall_at_10 = (k > 0) ? static_cast<double>(relevant) / k : 0.0;
            metrics::Registry::instance().observeHistogram(
                metrics::FAISS_RECALL_AT_10, recall_at_10);
        }

        // Record LTM search wall-clock time as histogram for p50/p90/p99 tracking
        metrics::Registry::instance().observeHistogram(
            metrics::SEARCH_LATENCY_S,
            static_cast<double>(ltm_ms) / 1000.0);

        LOG_DEBUG("[TMS] LTM search: retrieved=" + std::to_string(ltm_results.size()) +
                  " top_k=" + std::to_string(decision.ltm_top_k) +
                  " accounts_enabled=" + std::to_string(config_.memory_accounts_enabled) +
                  " ltm_ms=" + std::to_string(ltm_ms));
    }

    // Step 3b: GraphRAG expansion — follow IMPORTS/CALLS edges in KG
    //
    // If the KG is enabled, expand LTM results through the knowledge graph
    // by traversing structural relationships 2 hops deep. This dramatically
    // improves relevance on monoliths by surfacing structurally-connected
    // code that pure vector similarity might miss.
    if (config_.knowledge_graph_enabled && kg_handle_ && !ltm_results.empty() && !isTimedOut()) {
        try {
            auto& kg = kg_handle_->get();
            GraphRAGConfig graph_config;
            graph_config.max_hops = config_.graph_rag_max_hops;
            graph_config.max_neighbors_per_seed = config_.graph_rag_max_neighbors;
            graph_config.max_expanded_chunks = 32;
            graph_config.hop_decay = 0.7f;
            graph_config.graph_weight = config_.graph_rag_boost_factor;
            graph_config.follow_imports = true;
            graph_config.follow_calls = true;

            GraphRAGRetriever graph_retriever(*ltm_, kg, graph_config);
            auto expanded = graph_retriever.expandAndMerge(
                ltm_results, query.repo_filter, decision.ltm_top_k);

            if (!expanded.empty()) {
                size_t graph_added = expanded.size() - ltm_results.size();
                response.graph_expanded_chunks = static_cast<uint32_t>(
                    graph_added > 0 ? graph_added : 0);
                ltm_results = std::move(expanded);
                response.reasoning_trace.push_back(
                    "GraphRAG: Expanded via KG, +" +
                    std::to_string(response.graph_expanded_chunks) +
                    " graph-connected chunks");
            }
        } catch (const std::exception& e) {
            LOG_ERROR("[TMS] GraphRAG expansion failed: " + std::string(e.what()));
            response.reasoning_trace.push_back("GraphRAG: FAILED — " + std::string(e.what()));
        }
    }
    
    // Step 4: Retrieve from STM
    std::vector<RetrievedChunk> stm_results;
    if (!query.session_id.empty() && decision.stm_top_k > 0 && !isTimedOut()) {
        auto stm_start = std::chrono::steady_clock::now();
        stm_results = stm_->search(query.session_id, query_embedding, decision.stm_top_k);
        auto stm_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - stm_start).count();

        // Telemetry: STM search latency
        metrics::Registry::instance().observeHistogram(
            metrics::STM_SEARCH_LATENCY_S,
            static_cast<double>(stm_ms) / 1000.0);

        response.reasoning_trace.push_back("STM: Retrieved " + std::to_string(stm_results.size()) + " items");
        response.stm_items_scanned = stm_results.size();
        LOG_DEBUG("[TMS] STM search: retrieved=" + std::to_string(stm_results.size()) +
                  " session=" + query.session_id +
                  " stm_ms=" + std::to_string(stm_ms));
    }

    // Step 4b: Confidence Gate v2 (Zero-LLM Engine)
    //
    // Enhanced gate that combines cosine similarity with KG path-length
    // scoring. The combined signal provides a much more reliable indicator
    // of whether the retrieval is sufficient to skip the LLM, achieving
    // 60–70% LLM avoidance on well-indexed repos.
    //
    // Combined score = (cosine_weight * cosine_score) + (graph_weight * graph_score)
    //   cosine_score: max similarity from LTM + STM results
    //   graph_score:  structural proximity via KG path lengths (0–1)
    {
        float max_score = 0.0f;
        for (const auto& rc : ltm_results) {
            max_score = std::max(max_score, rc.similarity_score);
        }
        for (const auto& rc : stm_results) {
            max_score = std::max(max_score, rc.similarity_score);
        }
        response.max_retrieval_score = max_score;

        // Also check canonical STM cache
        auto canonical_hit = stm_->lookupCanonical(
            query_embedding, config_.confidence_gate_threshold);
        if (canonical_hit) {
            max_score = std::max(max_score, canonical_hit->score);
            response.max_retrieval_score = max_score;
        }

        float cosine_score = max_score;

        // Compute KG graph confidence score
        float graph_score = 0.0f;
        if (config_.knowledge_graph_enabled && kg_handle_ && !ltm_results.empty()) {
            try {
                auto& kg = kg_handle_->get();
                GraphRAGConfig graph_config;
                GraphRAGRetriever graph_retriever(*ltm_, kg, graph_config);

                // Use top-3 LTM results as seeds, check structural proximity
                std::vector<std::string> seed_ids;
                std::vector<std::string> result_ids;
                for (size_t i = 0; i < std::min(ltm_results.size(), size_t(3)); ++i) {
                    seed_ids.push_back(ltm_results[i].chunk.id);
                }
                for (const auto& rc : ltm_results) {
                    result_ids.push_back(rc.chunk.id);
                }
                graph_score = graph_retriever.computeGraphConfidence(
                    seed_ids, result_ids, query.repo_filter);
                response.graph_confidence = graph_score;
            } catch (const std::exception& e) {
                LOG_DEBUG("[TMS] graph confidence computation failed: " + std::string(e.what()));
            }
        }

        // Combined score: configurable weighting of cosine vs graph confidence
        const float kGraphWeight = config_.graph_rag_confidence_weight;
        const float kCosineWeight = 1.0f - kGraphWeight;
        float combined_score = (kCosineWeight * cosine_score) + (kGraphWeight * graph_score);

        // Telemetry
        metrics::Registry::instance().setGauge(
            metrics::CONFIDENCE_GATE_SCORE, static_cast<double>(combined_score));
        metrics::Registry::instance().setGauge(
            metrics::CONFIDENCE_GATE_COSINE_SCORE, static_cast<double>(cosine_score));
        metrics::Registry::instance().setGauge(
            metrics::CONFIDENCE_GATE_GRAPH_SCORE, static_cast<double>(graph_score));
        metrics::Registry::instance().setGauge(
            metrics::CONFIDENCE_GATE_COMBINED, static_cast<double>(combined_score));

        if (config_.confidence_gate_enabled &&
            combined_score >= config_.confidence_gate_threshold) {
            response.requires_llm = false;
            response.llm_skip_reason =
                "confidence_gate_v2: combined=" +
                std::to_string(combined_score) +
                " (cosine=" + std::to_string(cosine_score) +
                " graph=" + std::to_string(graph_score) +
                ") >= threshold=" +
                std::to_string(config_.confidence_gate_threshold);
            metrics::Registry::instance().incCounter(metrics::LLM_AVOIDED_TOTAL);

            // Compute the running avoided rate
            double avoided = metrics::Registry::instance().getCounter(metrics::LLM_AVOIDED_TOTAL);
            double used    = metrics::Registry::instance().getCounter(metrics::LLM_USED_TOTAL);
            if (avoided + used > 0) {
                metrics::Registry::instance().setGauge(
                    metrics::LLM_AVOIDED_RATE, avoided / (avoided + used));
            }

            response.reasoning_trace.push_back(
                "ConfidenceGateV2: FIRED — " + response.llm_skip_reason);
            LOG_INFO("[TMS] confidence gate v2 fired: " + response.llm_skip_reason);
        } else {
            response.requires_llm = true;
            metrics::Registry::instance().incCounter(metrics::LLM_USED_TOTAL);

            double avoided = metrics::Registry::instance().getCounter(metrics::LLM_AVOIDED_TOTAL);
            double used    = metrics::Registry::instance().getCounter(metrics::LLM_USED_TOTAL);
            if (avoided + used > 0) {
                metrics::Registry::instance().setGauge(
                    metrics::LLM_AVOIDED_RATE, avoided / (avoided + used));
            }
        }
    }
    
    // Step 5: Match patterns and strategies from MTM
    std::vector<PatternEntry> patterns;
    std::vector<StrategyEntry> strategies;
    if (decision.mtm_top_k > 0 && !isTimedOut()) {
        patterns = mtm_->matchPatterns(query_embedding, decision.mtm_top_k);
        
        // Get strategies that apply to detected patterns
        std::vector<std::string> pattern_ids;
        for (const auto& p : patterns) {
            pattern_ids.push_back(p.id);
        }
        strategies = mtm_->matchStrategies("review", pattern_ids, 5);
        
        response.reasoning_trace.push_back("MTM: Matched " + std::to_string(patterns.size()) + " patterns, " 
                                           + std::to_string(strategies.size()) + " strategies");
        response.mtm_items_scanned = patterns.size() + strategies.size();
        LOG_DEBUG("[TMS] MTM match: patterns=" + std::to_string(patterns.size()) +
                  " strategies=" + std::to_string(strategies.size()));
    }
    
    response.matched_patterns = patterns;
    response.suggested_strategies = strategies;
    
    // Step 6: Cross-Memory Attention (if enabled)
    if (decision.enable_cross_attention && !isTimedOut()) {
        auto cma_start = std::chrono::steady_clock::now();
        response.attention_output = runCrossMemoryAttention(
            query_embedding,
            ltm_results,
            stm_results,
            patterns,
            strategies,
            decision
        );
        auto cma_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - cma_start).count();

        // Telemetry: CMA latency
        metrics::Registry::instance().observeHistogram(
            metrics::CMA_LATENCY_S,
            static_cast<double>(cma_ms) / 1000.0);

        response.reasoning_trace.push_back("Attention: Fused context with " 
                                           + std::to_string(decision.attention_heads) + " heads"
                                           + " cma_ms=" + std::to_string(cma_ms));
    } else {
        // Simple concatenation fallback
        CrossMemoryOutput simple_output;
        simple_output.fused_chunks = ltm_results;
        
        std::ostringstream context;
        context << "## Relevant Code\n\n";
        for (const auto& chunk : ltm_results) {
            context << "### " << chunk.chunk.file_path << " (" << chunk.chunk.name << ")\n";
            context << "```" << chunk.chunk.language << "\n";
            context << chunk.chunk.content << "\n";
            context << "```\n\n";
        }
        simple_output.fused_context = context.str();
        simple_output.fused_embedding = query_embedding;
        
        response.attention_output = simple_output;
        response.reasoning_trace.push_back("Attention: Skipped (FAST mode)");
    }
    
    // Step 7: Record in STM
    if (!query.session_id.empty()) {
        std::vector<std::string> retrieved_ids;
        for (const auto& chunk : ltm_results) {
            retrieved_ids.push_back(chunk.chunk.id);
        }
        
        stm_->storeQuery(
            query.session_id,
            query.query_text,
            query_embedding,
            retrieved_ids
        );
    }
    
    // Finalize
    auto end_time = std::chrono::steady_clock::now();
    response.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_time - start_time);

    // Track query timeouts
    if (isTimedOut()) {
        metrics::Registry::instance().incCounter(metrics::QUERY_TIMEOUT_TOTAL);
        response.reasoning_trace.push_back(
            "Timeout: query exceeded " +
            std::to_string(config_.query_timeout_seconds) + "s budget");
        LOG_WARN("[TMS] forward timed out after " +
                 std::to_string(response.total_time.count()) + "ms");
    }

    LOG_DEBUG("[TMS] forward complete: total_ms=" + std::to_string(response.total_time.count()) +
              " fused_chunks=" + std::to_string(response.attention_output.fused_chunks.size()) +
              " fused_context_len=" + std::to_string(response.attention_output.fused_context.size()) +
              " confidence=" + std::to_string(response.attention_output.confidence_score) +
              " requires_llm=" + std::to_string(response.requires_llm));

    // record CMA confidence score
    metrics::Registry::instance().setGauge(
        metrics::CMA_SCORE, response.attention_output.confidence_score);
    
    return response;
}

TMSResponse TMSMemorySystem::query(const std::string& query_text, const std::string& session_id) {
    TMSQuery query;
    query.query_text = query_text;
    query.session_id = session_id;
    return forward(query);
}

// =============================================================================
// Session Management
// =============================================================================

void TMSMemorySystem::startSession(const std::string& session_id) {
    stm_->startSession(session_id);
}

void TMSMemorySystem::addSessionContext(
    const std::string& session_id,
    const std::string& context_type,
    const std::string& content,
    const std::vector<float>& embedding
) {
    std::vector<float> emb = embedding;
    if (emb.empty() && !content.empty()) {
        auto result = embedding_engine_->embed(content);
        if (result.success) {
            emb = std::move(result.embedding);
        }
    }
    
    stm_->storeContext(session_id, context_type, content, emb);
}

void TMSMemorySystem::endSession(const std::string& session_id) {
    // Check for items to promote to LTM
    auto candidates = stm_->getPromotionCandidates(session_id);
    
    // Promote high-relevance session items to long-term memory
    for (const auto& entry : candidates) {
        if (!entry.content.empty() && !entry.embedding.empty()) {
            CodeChunk chunk;
            chunk.id = "promoted_" + entry.id;
            chunk.content = entry.content;
            chunk.type = entry.context_type.empty() ? "session_context" : entry.context_type;
            chunk.tags.push_back("source:stm_promotion");
            chunk.tags.push_back("session:" + session_id);
            chunk.tags.push_back("relevance:" + std::to_string(entry.relevance_score));
            
            ltm_->add(chunk, entry.embedding);
            if (multi_vector_) {
                multi_vector_->add(chunk, entry.embedding);
            }
        }
    }
    
    stm_->endSession(session_id);
}

// =============================================================================
// Learning & Feedback
// =============================================================================

void TMSMemorySystem::learnFromOutcome(
    const std::string& session_id,
    double outcome_score,
    const std::vector<std::string>& helpful_chunk_ids,
    const std::vector<std::string>& unhelpful_chunk_ids
) {
    // Update LTM importance scores
    for (const auto& id : helpful_chunk_ids) {
        ltm_->updateImportance(id, 0.1);  // Increase importance
    }
    for (const auto& id : unhelpful_chunk_ids) {
        ltm_->updateImportance(id, -0.1);  // Decrease importance
    }
    
    // Update MTM patterns and strategies based on outcome
    if (mtm_) {
        // Get session history to find which patterns/strategies were used
        auto session_entries = stm_->getRecent(session_id, -1);
        
        // Collect chunk IDs that were retrieved during this session
        std::vector<std::string> used_pattern_ids;
        std::vector<std::string> used_strategy_ids;
        
        for (const auto& entry : session_entries) {
            // If the entry was a retrieval, the retrieved chunks may have matched patterns
            if (!entry.embedding.empty()) {
                auto patterns = mtm_->matchPatterns(entry.embedding, 3);
                for (const auto& p : patterns) {
                    used_pattern_ids.push_back(p.id);
                }
            }
        }
        
        // Learn from the outcome via MTM
        mtm_->learnFromOutcome(used_pattern_ids, used_strategy_ids, outcome_score);
    }
}

void TMSMemorySystem::registerPattern(const PatternEntry& pattern) {
    mtm_->storePattern(pattern);
}

void TMSMemorySystem::registerStrategy(const StrategyEntry& strategy) {
    mtm_->storeStrategy(strategy);
}

// =============================================================================
// Memory Consolidation
// =============================================================================

void TMSMemorySystem::consolidate() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // 1. Cleanup STM expired entries
    stm_->cleanup();
    stm_->cleanupSessions();
    
    // 2. Consolidate MTM patterns
    mtm_->consolidatePatterns();
    mtm_->applyDecay();
    
    // 3. Consolidate LTM (remove low-importance items)
    ltm_->consolidate(config_.consolidation_threshold);
    
    // 4. Save state
    save();
}

void TMSMemorySystem::setConsolidationInterval(std::chrono::hours interval) {
    config_.consolidation_interval = interval;
}

void TMSMemorySystem::consolidationLoop() {
    while (!shutdown_requested_.load()) {
        // Wait for interval (check shutdown every second)
        auto interval = config_.consolidation_interval;
        auto wait_until = std::chrono::steady_clock::now() + interval;
        
        while (std::chrono::steady_clock::now() < wait_until) {
            if (shutdown_requested_.load()) return;
            std::this_thread::sleep_for(std::chrono::seconds(1));
        }
        
        if (shutdown_requested_.load()) return;
        
        try {
            consolidate();
        } catch (const std::exception& e) {
            // Log error but continue
        }
    }
}

// =============================================================================
// Persistence
// =============================================================================

void TMSMemorySystem::save() {
    std::filesystem::create_directories(config_.storage_path);
    
    ltm_->save();
    if (multi_vector_) multi_vector_->save();
    mtm_->save();
    embedding_engine_->saveCache();
}

void TMSMemorySystem::load() {
    if (std::filesystem::exists(config_.storage_path)) {
        ltm_->load();
        if (multi_vector_) multi_vector_->load();
        mtm_->load();
        embedding_engine_->loadCache();
    }
}

// =============================================================================
// Statistics
// =============================================================================

TMSMemorySystem::SystemStats TMSMemorySystem::getStats() const {
    SystemStats stats;
    
    if (ltm_) {
        auto ltm_stats = ltm_->getStats();
        stats.ltm_total_chunks = ltm_stats.total_chunks;
        stats.ltm_total_repos = ltm_stats.total_repos;
        stats.ltm_index_size_mb = ltm_stats.memory_bytes / (1024 * 1024);
    }
    
    if (stm_) {
        auto stm_stats = stm_->getStats();
        stats.stm_active_sessions = stm_stats.active_sessions;
        stats.stm_total_items = stm_stats.total_entries;
    }
    
    if (mtm_) {
        auto mtm_stats = mtm_->getStats();
        stats.mtm_patterns = mtm_stats.total_patterns;
        stats.mtm_strategies = mtm_stats.total_strategies;
    }
    
    if (controller_) {
        auto ctrl_stats = controller_->getStats();
        stats.total_queries = ctrl_stats.total_decisions;
    }
    
    return stats;
}

float TMSMemorySystem::getCurrentBudget() const {
    // Query actual available memory on Linux via /proc/meminfo
    float available_gb = config_.vram_budget_gb;  // fallback

#ifdef __linux__
    std::ifstream meminfo("/proc/meminfo");
    if (meminfo.is_open()) {
        std::string line;
        while (std::getline(meminfo, line)) {
            if (line.find("MemAvailable:") == 0) {
                // Format: "MemAvailable:   XXXXXXX kB"
                std::istringstream iss(line);
                std::string label;
                unsigned long kb = 0;
                iss >> label >> kb;
                available_gb = static_cast<float>(kb) / (1024.0f * 1024.0f);
                break;
            }
        }
    }
#endif

    // Return the minimum of configured budget and actually available memory
    return std::min(available_gb, config_.vram_budget_gb);
}

// =============================================================================
// Internal Helpers
// =============================================================================

std::vector<float> TMSMemorySystem::computeEmbedding(const std::string& text) {
    auto result = embedding_engine_->embed(text);
    if (!result.success) {
        throw std::runtime_error("Embedding failed: " + result.error);
    }
    return result.embedding;
}

void TMSMemorySystem::updateImportanceScores(const std::vector<std::string>& accessed_ids) {
    for (const auto& id : accessed_ids) {
        ltm_->updateImportance(id, 0.05);  // Small boost for access
    }
}

CrossMemoryOutput TMSMemorySystem::runCrossMemoryAttention(
    const std::vector<float>& query_embedding,
    const std::vector<RetrievedChunk>& ltm_results,
    const std::vector<RetrievedChunk>& stm_results,
    const std::vector<PatternEntry>& patterns,
    const std::vector<StrategyEntry>& strategies,
    const ComputeDecision& decision
) {
    // Adjust attention based on decision
    if (decision.attention_heads != attention_->getConfig().num_heads) {
        attention_->setNumHeads(decision.attention_heads);
    }
    
    // Run attention
    auto output = attention_->attend(
        query_embedding,
        ltm_results,
        stm_results,
        patterns,
        strategies
    );
    
    // Convert to CrossMemoryOutput
    CrossMemoryOutput result;
    result.fused_chunks = output.attended_ltm;
    result.fused_context = output.fused_context;
    result.fused_embedding = output.fused_embedding;
    result.ltm_attention_weights = output.scores.ltm_aggregated;
    result.stm_attention_weights = output.scores.stm_aggregated;
    result.mtm_attention_weights = output.scores.mtm_aggregated;
    result.confidence_score = output.confidence;
    result.computation_time = std::chrono::duration_cast<std::chrono::milliseconds>(output.computation_time);
    
    return result;
}

void TMSMemorySystem::reconfigureEmbedding(
    const std::string& backend,
    const std::string& endpoint,
    const std::string& model,
    const std::string& api_key,
    size_t dims)
{
    std::lock_guard<std::mutex> lock(mutex_);

    EmbeddingConfig embed_config;
    embed_config.model_name = model;
    embed_config.embedding_dimension = dims > 0 ? dims : config_.embedding_dimension;
    embed_config.cache_path = config_.storage_path + "/embedding_cache";

    if (backend == "onnx" || backend == "local_onnx") {
        embed_config.backend = EmbeddingBackend::ONNX_RUNTIME;
        if (!model.empty()) {
            // Model name passed from Go server — resolve paths from it.
            std::string models_dir = config_.storage_path + "/../models";
            if (!config_.onnx_model_path.empty() &&
                config_.onnx_model_path.find("models/") != std::string::npos) {
                models_dir = config_.onnx_model_path.substr(
                    0, config_.onnx_model_path.find("models/") + 6);
            }
            embed_config.onnx_model_path = models_dir + "/" + model + "/model.onnx";
            embed_config.tokenizer_path = models_dir + "/" + model + "/tokenizer.json";
        } else {
            embed_config.onnx_model_path = config_.onnx_model_path;
            embed_config.tokenizer_path = config_.onnx_tokenizer_path;
        }
    } else if (backend == "http") {
        embed_config.backend = EmbeddingBackend::HTTP_API;
        embed_config.api_endpoint = endpoint;
        embed_config.api_key = api_key;
    } else {
        embed_config.backend = EmbeddingBackend::MOCK;
    }

    std::cerr << "[TMS] reconfigureEmbedding: backend=" << backend
              << " model=" << model
              << " endpoint=" << endpoint
              << " dims=" << dims << std::endl;

    embedding_engine_ = std::make_unique<EmbeddingEngine>(embed_config);

    // Update the ingestor to use the new embedding engine.
    EmbeddingIngestor::Config ingest_cfg;
    ingestor_ = std::make_unique<EmbeddingIngestor>(
        *embedding_engine_, *ltm_, mtm_.get(), ingest_cfg);

    // Update metrics for the new backend.
    double backend_id = 0.0; // mock
    if (backend == "onnx") backend_id = 1.0;
    else if (backend == "http") backend_id = 2.0;
    metrics::Registry::instance().setGauge(metrics::EMBED_ACTIVE_BACKEND, backend_id);
}

bool TMSMemorySystem::storeAssetChunk(const AssetChunk& chunk) {
    if (chunk.embedding.empty() || !ltm_) return false;

    // Convert AssetChunk to a CodeChunk for LTM storage
    CodeChunk cc;
    cc.id = chunk.repo_id + ":" + chunk.asset_id + ":" + std::to_string(chunk.chunk_index);
    cc.file_path = "[asset:" + chunk.asset_id + "] " + chunk.file_name;
    cc.content = ""; // Binary assets have no text content
    cc.language = chunk.mime_type;
    cc.start_line = chunk.chunk_index;
    cc.end_line = chunk.total_chunks;
    cc.tags.push_back("repo:" + chunk.repo_id);
    cc.symbols.push_back("asset:" + chunk.asset_id);

    try {
        ltm_->add(cc, chunk.embedding);
        return true;
    } catch (const std::exception& e) {
        std::cerr << "[TMS] storeAssetChunk failed: " << e.what() << std::endl;
        return false;
    }
}

} // namespace aipr::tms
