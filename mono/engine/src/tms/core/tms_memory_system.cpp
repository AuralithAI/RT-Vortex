/**
 * TMS Memory System - Main Orchestrator Implementation
 * 
 * Coordinates all TMS components for brain-inspired code understanding.
 */

#include "tms/tms_memory_system.h"
#include "tms/repo_parser.h"
#include "tms/embedding_engine.h"
#include "tms/memory_accounts.h"
#include "hierarchy_builder.h"
#include "chunk_prefixer.h"
#include "knowledge_graph.h"
#include "logging.h"
#include "metrics.h"
#include <chrono>
#include <algorithm>
#include <sstream>
#include <fstream>
#include <iostream>
#include <filesystem>
#include <unordered_set>

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
    ltm_config.default_top_k = config_.ltm_default_top_k;
    ltm_config.storage_path = config_.storage_path + "/ltm";
    
    ltm_ = std::make_unique<LTMFaiss>(ltm_config);
    
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
    
    // Phase 1: Parse repository
    if (progress_callback) {
        progress_callback(0.0f, "Parsing repository...");
    }
    
    std::vector<CodeChunk> chunks;
    {
        ParseProgressCallback parser_progress = nullptr;
        if (progress_callback) {
            parser_progress = [&](float p, const std::string& file, const std::string& status) {
                progress_callback(p * 0.3f, "Parsing: " + file);  // 0-30%
            };
        }
        
        chunks = repo_parser_->parseRepository(repo_path, parser_progress);
    }
    
    std::cerr << "[TMS] ingestRepository: repo_path=" << repo_path
              << " chunks=" << chunks.size() << std::endl;

    if (chunks.empty()) {
        if (progress_callback) {
            progress_callback(1.0f, "No chunks extracted");
        }
        return;
    }
    
    // Hierarchical context enrichment (feature-gated)
    if (config_.hierarchy_enabled) {
        if (progress_callback) {
            progress_callback(0.25f, "Building hierarchy...");
        }

        aipr::HierarchyBuilder hierarchy_builder;
        aipr::RepoManifest manifest = hierarchy_builder.buildRepoManifest(repo_path);

        // Generate file-summary chunks and append them
        std::unordered_map<std::string, std::vector<CodeChunk*>> chunks_by_file;
        for (auto& c : chunks) {
            chunks_by_file[c.file_path].push_back(&c);
        }

        for (auto& [file_path, file_chunk_ptrs] : chunks_by_file) {
            std::vector<CodeChunk> fc;
            fc.reserve(file_chunk_ptrs.size());
            for (auto* p : file_chunk_ptrs) fc.push_back(*p);

            std::string lang = fc.empty() ? "" : fc[0].language;
            auto summary = hierarchy_builder.summarizeFile(file_path, lang, fc, manifest);
            chunks.push_back(hierarchy_builder.buildFileSummaryChunk(summary));
        }

        // Apply structural prefixes to all chunks
        aipr::ChunkPrefixer prefixer;
        size_t prefixed = prefixer.applyPrefixes(chunks, repo_id, manifest);
        LOG_INFO("[TMS] hierarchy: prefixed " + std::to_string(prefixed) +
                 " chunks, avg_prefix=" + std::to_string(static_cast<int>(prefixer.avgPrefixLength())) + " chars");
    }

    // Phase 2: Compute embeddings
    if (progress_callback) {
        progress_callback(0.3f, "Computing embeddings...");
    }
    
    std::vector<std::vector<float>> embeddings;
    {
        EmbeddingProgressCallback embed_progress = nullptr;
        if (progress_callback) {
            embed_progress = [&](int completed, int total, const std::string& status) {
                float p = 0.3f + (static_cast<float>(completed) / total) * 0.5f;  // 30-80%
                progress_callback(p, "Embedding: " + std::to_string(completed) + "/" + std::to_string(total));
            };
        }
        
        auto result = embedding_engine_->embedChunks(chunks, embed_progress);
        embeddings = std::move(result.embeddings);
    }
    
    // Memory account classification (feature-gated)
    if (config_.memory_accounts_enabled) {
        if (progress_callback) {
            progress_callback(0.78f, "Classifying memory accounts...");
        }
        MemoryAccountClassifier classifier;
        for (auto& chunk : chunks) {
            auto account = classifier.classify(chunk);
            chunk.tags.push_back(MemoryAccountClassifier::accountTag(account));
        }
        LOG_INFO("[TMS] account tags applied to " + std::to_string(chunks.size()) + " chunks");
    }

    // Phase 3: Store in LTM
    if (progress_callback) {
        progress_callback(0.8f, "Storing in LTM...");
    }
    
    ingestChunksWithEmbeddings(repo_id, chunks, embeddings);

    // Knowledge Graph build (feature-gated)
    if (config_.knowledge_graph_enabled && kg_handle_) {
        if (progress_callback) {
            progress_callback(0.9f, "Building knowledge graph...");
        }
        try {
            kg_handle_->get().buildFromChunks(repo_id, chunks);
            LOG_INFO("[TMS] knowledge graph built for " + repo_id);
        } catch (const std::exception& e) {
            LOG_ERROR("[TMS] KG build failed: " + std::string(e.what()));
        }
    }
    
    // Phase 4: Persist to disk
    if (progress_callback) {
        progress_callback(0.95f, "Persisting index to disk...");
    }
    try {
        save();
        std::cerr << "[TMS] Index persisted to disk after ingesting " << chunks.size() << " chunks" << std::endl;
    } catch (const std::exception& e) {
        std::cerr << "[TMS] WARNING: Failed to persist index: " << e.what() << std::endl;
    }

    // Done
    auto end_time = std::chrono::steady_clock::now();
    auto duration = std::chrono::duration_cast<std::chrono::seconds>(end_time - start_time);
    
    if (progress_callback) {
        std::ostringstream status;
        status << "Ingested " << chunks.size() << " chunks in " << duration.count() << "s";
        progress_callback(1.0f, status.str());
    }
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
    
    // Batch add to LTM
    ltm_->addBatch(prepared_chunks, embeddings);
    
    // Update MTM patterns (async in production)
    // This is simplified - in production, run pattern detection in background
}

void TMSMemorySystem::removeRepository(const std::string& repo_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    ltm_->removeByRepo(repo_id);
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

            // Search top-2 accounts, split budget
            int per_account_k = std::max(decision.ltm_top_k / 2, 4);
            std::unordered_map<std::string, float> rrf_scores;
            std::unordered_map<std::string, RetrievedChunk> chunk_map;
            const float rrf_k = 60.0f;

            int accounts_to_search = std::min(2, static_cast<int>(ranked_accounts.size()));
            for (int ai = 0; ai < accounts_to_search; ++ai) {
                auto tag = MemoryAccountClassifier::accountTag(ranked_accounts[ai]);
                auto account_results = ltm_->hybridSearchByAccount(
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
            ltm_results = ltm_->search(query_embedding, decision.ltm_top_k, query.repo_filter);
        }

        auto ltm_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            std::chrono::steady_clock::now() - ltm_start).count();
        response.reasoning_trace.push_back("LTM: Retrieved " + std::to_string(ltm_results.size()) + " chunks");
        response.ltm_items_scanned = ltm_results.size();
        LOG_DEBUG("[TMS] LTM search: retrieved=" + std::to_string(ltm_results.size()) +
                  " top_k=" + std::to_string(decision.ltm_top_k) +
                  " accounts_enabled=" + std::to_string(config_.memory_accounts_enabled) +
                  " ltm_ms=" + std::to_string(ltm_ms));
    }
    
    // Step 4: Retrieve from STM
    std::vector<RetrievedChunk> stm_results;
    if (!query.session_id.empty() && decision.stm_top_k > 0) {
        stm_results = stm_->search(query.session_id, query_embedding, decision.stm_top_k);
        response.reasoning_trace.push_back("STM: Retrieved " + std::to_string(stm_results.size()) + " items");
        response.stm_items_scanned = stm_results.size();
        LOG_DEBUG("[TMS] STM search: retrieved=" + std::to_string(stm_results.size()) +
                  " session=" + query.session_id);
    }
    
    // Step 5: Match patterns and strategies from MTM
    std::vector<PatternEntry> patterns;
    std::vector<StrategyEntry> strategies;
    if (decision.mtm_top_k > 0) {
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
    if (decision.enable_cross_attention) {
        response.attention_output = runCrossMemoryAttention(
            query_embedding,
            ltm_results,
            stm_results,
            patterns,
            strategies,
            decision
        );
        response.reasoning_trace.push_back("Attention: Fused context with " 
                                           + std::to_string(decision.attention_heads) + " heads");
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

    LOG_DEBUG("[TMS] forward complete: total_ms=" + std::to_string(response.total_time.count()) +
              " fused_chunks=" + std::to_string(response.attention_output.fused_chunks.size()) +
              " fused_context_len=" + std::to_string(response.attention_output.fused_context.size()) +
              " confidence=" + std::to_string(response.attention_output.confidence_score));

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
    mtm_->save();
    embedding_engine_->saveCache();
}

void TMSMemorySystem::load() {
    if (std::filesystem::exists(config_.storage_path)) {
        ltm_->load();
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

    if (backend == "onnx") {
        embed_config.backend = EmbeddingBackend::ONNX_RUNTIME;
        embed_config.onnx_model_path = config_.onnx_model_path;
        embed_config.tokenizer_path = config_.onnx_tokenizer_path;
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

} // namespace aipr::tms
