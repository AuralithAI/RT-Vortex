/**
 * AI PR Reviewer - Engine gRPC Service Implementation
 *
 * Implements the EngineService::Service interface defined in engine.proto
 * by delegating to the core Engine API.
 */

#include "engine_service_impl.h"
#include "logging.h"
#include "metrics.h"

#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <mutex>
#include <chrono>
#include <thread>
#include <algorithm>

namespace aipr {
namespace server {

EngineServiceImpl::EngineServiceImpl(std::unique_ptr<Engine> engine)
    : engine_(std::move(engine))
    , start_time_(std::chrono::steady_clock::now())
{
}

uint64_t EngineServiceImpl::getUptimeSeconds() const {
    auto now = std::chrono::steady_clock::now();
    return std::chrono::duration_cast<std::chrono::seconds>(now - start_time_).count();
}

//=============================================================================
// Indexing Operations
//=============================================================================

grpc::Status EngineServiceImpl::IndexRepository(
    grpc::ServerContext* context,
    const aipr::engine::v1::IndexRequest* request,
    aipr::engine::v1::IndexResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        // Apply per-request embedding configuration from the gRPC IndexConfig.
        // The API key is passed transiently — it is NOT stored in the engine.
        if (request->has_config()) {
            const auto& cfg = request->config();
            if (!cfg.embedding_provider().empty()) {
                std::string provider = cfg.embedding_provider();
                // Normalise: the Go server sends "HTTP" but the engine expects "http".
                std::transform(provider.begin(), provider.end(), provider.begin(),
                               [](unsigned char c) { return std::tolower(c); });
                engine_->configureEmbedding(
                    provider,
                    cfg.embedding_endpoint(),
                    cfg.embedding_model(),
                    cfg.embedding_api_key(),
                    static_cast<size_t>(cfg.embedding_dimensions())
                );
            }
            // Pass transient VCS clone token for authenticated git clone.
            if (!cfg.clone_token().empty()) {
                engine_->setCloneToken(cfg.clone_token());
            }
        }

        IndexStats stats = engine_->indexRepositoryWithAction(
            request->repo_id(),
            request->repo_path(),
            request->has_config() ? request->config().index_action() : "index",
            request->has_config() ? request->config().target_branch() : "",
            nullptr  // Progress callback - could wire to streaming in future
        );

        response->set_success(stats.is_complete);
        response->set_message(stats.is_complete ? "Index completed successfully" : "Index incomplete");
        toProto(stats, response->mutable_stats());

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        response->set_success(false);
        response->set_message(std::string("Indexing failed: ") + e.what());
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::IndexRepositoryStream(
    grpc::ServerContext* context,
    const aipr::engine::v1::IndexRequest* request,
    grpc::ServerWriter<aipr::engine::v1::IndexProgressUpdate>* writer)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    const std::string& repo_id = request->repo_id();

    // ── Acquire concurrency slot ────────────────────────────────────────
    {
        std::unique_lock<std::mutex> lock(index_sem_mutex_);
        // Wait until a slot opens up, checking for cancellation
        while (active_index_jobs_ >= kMaxConcurrentIndexJobs) {
            // Send a "queued" progress update while waiting
            aipr::engine::v1::IndexProgressUpdate queued;
            queued.set_repo_id(repo_id);
            queued.set_progress(0);
            queued.set_phase("queued");
            queued.set_eta_seconds(-1);
            queued.set_done(false);
            writer->Write(queued);

            // Wait up to 5 seconds before re-checking
            index_sem_cv_.wait_for(lock, std::chrono::seconds(5));

            if (context->IsCancelled()) {
                return grpc::Status(grpc::StatusCode::CANCELLED,
                    "Request cancelled while waiting in queue");
            }
        }
        active_index_jobs_++;
    }

    // RAII guard to release the slot on any exit path
    struct SlotGuard {
        EngineServiceImpl* self;
        ~SlotGuard() {
            std::lock_guard<std::mutex> lock(self->index_sem_mutex_);
            self->active_index_jobs_--;
            self->index_sem_cv_.notify_one();
        }
    } slot_guard{this};

    try {
        auto start_time = std::chrono::steady_clock::now();

        // Apply per-request embedding configuration from the gRPC IndexConfig.
        if (request->has_config()) {
            const auto& cfg = request->config();
            if (!cfg.embedding_provider().empty()) {
                std::string provider = cfg.embedding_provider();
                std::transform(provider.begin(), provider.end(), provider.begin(),
                               [](unsigned char c) { return std::tolower(c); });
                engine_->configureEmbedding(
                    provider,
                    cfg.embedding_endpoint(),
                    cfg.embedding_model(),
                    cfg.embedding_api_key(),
                    static_cast<size_t>(cfg.embedding_dimensions())
                );
            }
            // Pass transient VCS clone token for authenticated git clone.
            if (!cfg.clone_token().empty()) {
                engine_->setCloneToken(cfg.clone_token());
            }
        }

        // gRPC ServerWriter::Write() is NOT thread-safe, but the engine's
        // repo_parser calls our progress callback from a thread-pool.
        // We protect all writes with a mutex AND throttle to avoid flooding.
        std::mutex writer_mutex;
        auto last_write_time = std::chrono::steady_clock::now();
        int last_written_pct = -1;
        constexpr auto kMinWriteInterval = std::chrono::milliseconds(500);
        constexpr int kMinPctDelta = 2;

        // Progress callback — streams updates to the gRPC client
        auto progress_cb = [&](size_t current, size_t total, const std::string& status_msg) {
            if (context->IsCancelled()) return;

            int pct = total > 0 ? static_cast<int>((current * 100) / total) : 0;

            // Throttle: skip unless enough time or progress has changed
            auto now = std::chrono::steady_clock::now();
            {
                std::lock_guard<std::mutex> lock(writer_mutex);

                bool time_elapsed = (now - last_write_time) >= kMinWriteInterval;
                bool pct_jumped = (pct - last_written_pct) >= kMinPctDelta;
                bool is_first = (last_written_pct < 0);
                bool is_last = (current == total && total > 0);

                if (!is_first && !is_last && !time_elapsed && !pct_jumped) {
                    return;  // skip — too soon, not enough change
                }

                aipr::engine::v1::IndexProgressUpdate update;
                update.set_repo_id(repo_id);
                update.set_progress(pct);
                update.set_files_total(total);
                update.set_files_processed(current);
                update.set_current_file(status_msg);
                update.set_done(false);

                // Determine phase from progress
                if (pct < 10)       update.set_phase("cloning");
                else if (pct < 30)  update.set_phase("scanning");
                else if (pct < 70)  update.set_phase("chunking");
                else if (pct < 90)  update.set_phase("embedding");
                else                update.set_phase("finalizing");

                // Compute ETA
                auto elapsed = now - start_time;
                auto elapsed_secs = std::chrono::duration_cast<std::chrono::seconds>(elapsed).count();
                if (current > 0 && total > 0 && current < total) {
                    double rate = static_cast<double>(current) / static_cast<double>(elapsed_secs > 0 ? elapsed_secs : 1);
                    double remaining = static_cast<double>(total - current) / rate;
                    update.set_eta_seconds(static_cast<int64_t>(remaining));
                } else {
                    update.set_eta_seconds(-1);
                }

                writer->Write(update);
                last_write_time = now;
                last_written_pct = pct;
            }
        };

        IndexStats stats = engine_->indexRepositoryWithAction(
            repo_id,
            request->repo_path(),
            request->has_config() ? request->config().index_action() : "index",
            request->has_config() ? request->config().target_branch() : "",
            progress_cb
        );

        // Send final completion update
        aipr::engine::v1::IndexProgressUpdate final_update;
        final_update.set_repo_id(repo_id);
        final_update.set_progress(100);
        final_update.set_phase("completed");
        final_update.set_done(true);
        final_update.set_success(stats.is_complete);
        final_update.set_eta_seconds(0);
        toProto(stats, final_update.mutable_final_stats());
        writer->Write(final_update);

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        // Send failure update
        aipr::engine::v1::IndexProgressUpdate fail_update;
        fail_update.set_repo_id(repo_id);
        fail_update.set_done(true);
        fail_update.set_success(false);
        fail_update.set_error(e.what());
        fail_update.set_phase("failed");
        writer->Write(fail_update);

        return grpc::Status::OK;  // Return OK — error is in the stream payload
    }
}

grpc::Status EngineServiceImpl::IncrementalIndex(
    grpc::ServerContext* context,
    const aipr::engine::v1::IncrementalIndexRequest* request,
    aipr::engine::v1::IndexResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        // Convert repeated string to vector
        std::vector<std::string> changed_files;
        changed_files.reserve(request->changed_files_size());
        for (const auto& file : request->changed_files()) {
            changed_files.push_back(file);
        }

        IndexStats stats = engine_->updateIndex(
            request->repo_id(),
            changed_files,
            request->base_commit(),
            request->head_commit()
        );

        response->set_success(stats.is_complete);
        response->set_message("Incremental index completed");
        toProto(stats, response->mutable_stats());

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        response->set_success(false);
        response->set_message(std::string("Incremental indexing failed: ") + e.what());
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::GetIndexStats(
    grpc::ServerContext* context,
    const aipr::engine::v1::IndexStatsRequest* request,
    aipr::engine::v1::IndexStatsResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        IndexStats stats = engine_->getIndexStats(request->repo_id());

        // Check if index exists (repo_id will be empty if not found)
        bool found = !stats.repo_id.empty();
        response->set_found(found);

        if (found) {
            toProto(stats, response->mutable_stats());
        }

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::DeleteIndex(
    grpc::ServerContext* context,
    const aipr::engine::v1::DeleteIndexRequest* request,
    aipr::engine::v1::DeleteIndexResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        bool deleted = engine_->deleteIndex(request->repo_id());

        response->set_success(deleted);
        response->set_message(deleted ? "Index deleted" : "Index not found");

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        response->set_success(false);
        response->set_message(std::string("Delete failed: ") + e.what());
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// Retrieval Operations
//=============================================================================

grpc::Status EngineServiceImpl::Search(
    grpc::ServerContext* context,
    const aipr::engine::v1::SearchRequest* request,
    aipr::engine::v1::SearchResponse* response)
{
    LOG_INFO("Search called: repo=" + request->repo_id() +
             " query_len=" + std::to_string(request->query().size()) +
             " query=\"" + request->query().substr(0, 120) + "\"");

    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        auto search_start = std::chrono::steady_clock::now();

        // Determine top_k from config or use default
        size_t top_k = 20;
        if (request->has_config() && request->config().top_k() > 0) {
            top_k = request->config().top_k();
        }

        auto sr = engine_->searchWithMeta(
            request->repo_id(),
            request->query(),
            top_k
        );
        const auto& chunks = sr.chunks;

        auto search_end = std::chrono::steady_clock::now();
        auto search_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            search_end - search_start).count();

        // Compute total context size in characters (for token estimation)
        size_t total_context_chars = 0;
        for (const auto& chunk : chunks) {
            total_context_chars += chunk.content.size();
        }

        LOG_INFO("Search completed: repo=" + request->repo_id() +
                 " chunks_retrieved=" + std::to_string(chunks.size()) +
                 " top_k=" + std::to_string(top_k) +
                 " context_chars=" + std::to_string(total_context_chars) +
                 " est_tokens=" + std::to_string(total_context_chars / 4) +
                 " search_ms=" + std::to_string(search_ms));

        // Log individual chunk details at DEBUG level
        for (size_t i = 0; i < chunks.size(); ++i) {
            LOG_DEBUG("  chunk[" + std::to_string(i) + "]: " +
                      chunks[i].file_path + ":" +
                      std::to_string(chunks[i].start_line) + "-" +
                      std::to_string(chunks[i].end_line) +
                      " score=" + std::to_string(chunks[i].relevance_score) +
                      " chars=" + std::to_string(chunks[i].content.size()));
        }

        // Convert results to proto
        for (const auto& chunk : chunks) {
            toProto(chunk, response->add_chunks());
        }

        // Set metrics
        auto* metrics = response->mutable_metrics();
        metrics->set_total_candidates(static_cast<uint32_t>(chunks.size()));

        // Confidence gate + GraphRAG metadata from TMS forward
        response->set_requires_llm(sr.requires_llm);
        response->set_max_retrieval_score(sr.max_retrieval_score);
        response->set_graph_confidence_score(sr.graph_confidence);
        response->set_graph_expanded_count(sr.graph_expanded_chunks);

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        LOG_ERROR("Search failed: repo=" + request->repo_id() +
                  " error=" + std::string(e.what()));
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::SearchStream(
    grpc::ServerContext* context,
    const aipr::engine::v1::SearchRequest* request,
    grpc::ServerWriter<aipr::engine::v1::ContextChunk>* writer)
{
    LOG_INFO("SearchStream called: repo=" + request->repo_id() +
             " query_len=" + std::to_string(request->query().size()) +
             " query=\"" + request->query().substr(0, 120) + "\"");

    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        auto search_start = std::chrono::steady_clock::now();

        size_t top_k = 20;
        if (request->has_config() && request->config().top_k() > 0) {
            top_k = request->config().top_k();
        }

        std::vector<ContextChunk> chunks = engine_->search(
            request->repo_id(),
            request->query(),
            top_k
        );

        auto search_end = std::chrono::steady_clock::now();
        auto search_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
            search_end - search_start).count();

        size_t total_context_chars = 0;
        for (const auto& chunk : chunks) {
            total_context_chars += chunk.content.size();
        }

        LOG_INFO("SearchStream retrieval done: repo=" + request->repo_id() +
                 " chunks_retrieved=" + std::to_string(chunks.size()) +
                 " top_k=" + std::to_string(top_k) +
                 " context_chars=" + std::to_string(total_context_chars) +
                 " est_tokens=" + std::to_string(total_context_chars / 4) +
                 " search_ms=" + std::to_string(search_ms));

        // Stream results one by one
        size_t streamed = 0;
        for (const auto& chunk : chunks) {
            if (context->IsCancelled()) {
                LOG_WARN("SearchStream cancelled by client after " +
                         std::to_string(streamed) + "/" +
                         std::to_string(chunks.size()) + " chunks");
                return grpc::Status(grpc::StatusCode::CANCELLED, "Stream cancelled by client");
            }

            aipr::engine::v1::ContextChunk proto_chunk;
            toProto(chunk, &proto_chunk);

            if (!writer->Write(proto_chunk)) {
                LOG_ERROR("SearchStream write failed after " +
                          std::to_string(streamed) + " chunks");
                return grpc::Status(grpc::StatusCode::UNKNOWN, "Failed to write to stream");
            }
            streamed++;
        }

        LOG_DEBUG("SearchStream completed: streamed " +
                  std::to_string(streamed) + " chunks to client");

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        LOG_ERROR("SearchStream failed: repo=" + request->repo_id() +
                  " error=" + std::string(e.what()));
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// Review Operations
//=============================================================================

grpc::Status EngineServiceImpl::BuildReviewContext(
    grpc::ServerContext* context,
    const aipr::engine::v1::ReviewContextRequest* request,
    aipr::engine::v1::ReviewContextResponse* response)
{
    LOG_INFO("BuildReviewContext called for repo=" + request->repo_id() +
             " pr_title=" + request->pr_title() +
             " diff_size=" + std::to_string(request->diff().size()));

    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        ContextPack pack = engine_->buildReviewContext(
            request->repo_id(),
            request->diff(),
            request->pr_title(),
            request->pr_description()
        );

        // Convert to proto
        auto* proto_pack = response->mutable_context_pack();
        proto_pack->set_repo_id(pack.repo_id);
        proto_pack->set_pr_title(pack.pr_title);
        proto_pack->set_pr_description(pack.pr_description);
        proto_pack->set_diff(pack.diff);

        for (const auto& chunk : pack.context_chunks) {
            toProto(chunk, proto_pack->add_context_chunks());
        }

        for (const auto& symbol : pack.touched_symbols) {
            toProto(symbol, proto_pack->add_touched_symbols());
        }

        for (const auto& warning : pack.heuristic_warnings) {
            proto_pack->add_heuristic_warnings(warning);
        }

        // Estimate tokens (rough: 4 chars per token)
        size_t total_chars = pack.diff.size();
        for (const auto& chunk : pack.context_chunks) {
            total_chars += chunk.content.size();
        }
        proto_pack->set_total_tokens_estimate(total_chars / 4);

        LOG_INFO("BuildReviewContext completed: chunks=" +
                 std::to_string(pack.context_chunks.size()) +
                 " symbols=" + std::to_string(pack.touched_symbols.size()) +
                 " warnings=" + std::to_string(pack.heuristic_warnings.size()) +
                 " est_tokens=" + std::to_string(total_chars / 4));

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        LOG_ERROR("BuildReviewContext failed: " + std::string(e.what()));
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::BuildReviewContextStream(
    grpc::ServerContext* context,
    const aipr::engine::v1::ReviewContextRequest* request,
    grpc::ServerWriter<aipr::engine::v1::PREmbedProgressUpdate>* writer)
{
    LOG_INFO("BuildReviewContextStream called for repo=" + request->repo_id() +
             " pr_title=" + request->pr_title() +
             " diff_size=" + std::to_string(request->diff().size()));

    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    const std::string& repo_id = request->repo_id();

    auto send_progress = [&](int pct, const std::string& phase,
                             const std::string& current_file = "",
                             uint32_t files_processed = 0,
                             uint32_t files_total = 0) {
        if (context->IsCancelled()) return;
        aipr::engine::v1::PREmbedProgressUpdate update;
        update.set_repo_id(repo_id);
        update.set_progress(pct);
        update.set_phase(phase);
        update.set_files_processed(files_processed);
        update.set_files_total(files_total);
        update.set_current_file(current_file);
        update.set_eta_seconds(-1);
        update.set_done(false);
        writer->Write(update);
    };

    try {
        // Step 1: Parsing diff
        send_progress(5, "parsing_diff");

        // Step 2: Building review context (does parsing, TMS search,
        //          symbol resolution, and heuristic checks internally)
        send_progress(15, "resolving_symbols");

        ContextPack pack = engine_->buildReviewContext(
            repo_id,
            request->diff(),
            request->pr_title(),
            request->pr_description()
        );

        if (context->IsCancelled()) {
            return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
        }

        // Step 3: Building context pack proto
        send_progress(70, "building_context",
                      "", pack.context_chunks.size(), pack.context_chunks.size());

        auto* proto_pack = new aipr::engine::v1::ContextPack();
        proto_pack->set_repo_id(pack.repo_id);
        proto_pack->set_pr_title(pack.pr_title);
        proto_pack->set_pr_description(pack.pr_description);
        proto_pack->set_diff(pack.diff);

        for (const auto& chunk : pack.context_chunks) {
            toProto(chunk, proto_pack->add_context_chunks());
        }

        for (const auto& symbol : pack.touched_symbols) {
            toProto(symbol, proto_pack->add_touched_symbols());
        }

        for (const auto& warning : pack.heuristic_warnings) {
            proto_pack->add_heuristic_warnings(warning);
        }

        // Estimate tokens (rough: 4 chars per token)
        size_t total_chars = pack.diff.size();
        for (const auto& chunk : pack.context_chunks) {
            total_chars += chunk.content.size();
        }
        proto_pack->set_total_tokens_estimate(total_chars / 4);

        send_progress(90, "finalizing");

        // Step 4: Send final completion with context_pack
        aipr::engine::v1::PREmbedProgressUpdate final_update;
        final_update.set_repo_id(repo_id);
        final_update.set_progress(100);
        final_update.set_phase("completed");
        final_update.set_done(true);
        final_update.set_success(true);
        final_update.set_eta_seconds(0);
        final_update.set_allocated_context_pack(proto_pack);  // transfers ownership
        writer->Write(final_update);

        LOG_INFO("BuildReviewContextStream completed: chunks=" +
                 std::to_string(pack.context_chunks.size()) +
                 " symbols=" + std::to_string(pack.touched_symbols.size()) +
                 " est_tokens=" + std::to_string(total_chars / 4));

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        LOG_ERROR("BuildReviewContextStream failed: " + std::string(e.what()));

        // Send failure update
        aipr::engine::v1::PREmbedProgressUpdate fail_update;
        fail_update.set_repo_id(repo_id);
        fail_update.set_done(true);
        fail_update.set_success(false);
        fail_update.set_error(e.what());
        fail_update.set_phase("failed");
        writer->Write(fail_update);

        return grpc::Status::OK;  // Return OK — error is in the stream payload
    }
}

grpc::Status EngineServiceImpl::RunHeuristics(
    grpc::ServerContext* context,
    const aipr::engine::v1::HeuristicsRequest* request,
    aipr::engine::v1::HeuristicsResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        std::vector<HeuristicFinding> findings = engine_->runHeuristics(request->diff());

        for (const auto& finding : findings) {
            toProto(finding, response->add_findings());
        }

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// Health & Diagnostics
//=============================================================================

grpc::Status EngineServiceImpl::HealthCheck(
    grpc::ServerContext* context,
    const aipr::engine::v1::HealthCheckRequest* request,
    aipr::engine::v1::HealthCheckResponse* response)
{
    (void)context;  // Unused
    (void)request;  // Unused

    response->set_healthy(true);
    response->set_version(engine_->getVersion());
    response->set_uptime_seconds(getUptimeSeconds());
    response->set_metrics_enabled(true);
    response->set_active_metric_streams(active_metric_streams_.load());

    // Add component status
    (*response->mutable_components())["engine"] = "healthy";
    (*response->mutable_components())["indexer"] = "ready";
    (*response->mutable_components())["retriever"] = "ready";

    return grpc::Status::OK;
}

grpc::Status EngineServiceImpl::GetDiagnostics(
    grpc::ServerContext* context,
    const aipr::engine::v1::DiagnosticsRequest* request,
    aipr::engine::v1::DiagnosticsResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        DiagnosticResult diag = engine_->runDiagnostics();

        // Memory stats
        if (request->include_memory()) {
            auto* mem = response->mutable_memory();
            mem->set_heap_used_bytes(0);  // Would need platform-specific code
            mem->set_heap_total_bytes(0);
            mem->set_rss_bytes(0);
        }

        // Index info
        if (request->include_indices()) {
            // Would iterate over loaded indices from engine
            // For now, just show engine is working
        }

        // Config info
        (*response->mutable_config())["engine_version"] = diag.engine_version;
        (*response->mutable_config())["platform"] = diag.platform;
        (*response->mutable_config())["healthy"] = diag.healthy ? "true" : "false";

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// Embedding Statistics
//=============================================================================

grpc::Status EngineServiceImpl::GetEmbedStats(
    grpc::ServerContext* context,
    const aipr::engine::v1::EmbedStatsRequest* request,
    aipr::engine::v1::EmbedStatsResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        auto stats = engine_->getEmbedStats(request->repo_id());

        response->set_active_model(stats.active_model);
        response->set_embedding_dimension(static_cast<uint32_t>(stats.embedding_dimension));
        response->set_backend_type(stats.backend_type);
        response->set_total_chunks(stats.total_chunks);
        response->set_total_vectors(stats.total_vectors);
        response->set_index_size_bytes(stats.index_size_bytes);
        response->set_kg_nodes(stats.kg_nodes);
        response->set_kg_edges(stats.kg_edges);
        response->set_kg_enabled(stats.kg_enabled);
        response->set_merkle_cached_files(stats.merkle_cached_files);
        response->set_merkle_cache_hit_rate(stats.merkle_cache_hit_rate);
        response->set_avg_embed_latency_ms(stats.avg_embed_latency_ms);
        response->set_avg_search_latency_ms(stats.avg_search_latency_ms);
        response->set_total_queries(stats.total_queries);
        response->set_embed_cache_size(stats.embed_cache_size);
        response->set_embed_cache_hit_rate(stats.embed_cache_hit_rate);
        response->set_llm_avoided_rate(stats.llm_avoided_rate);
        response->set_avg_confidence_score(stats.avg_confidence_score);
        response->set_llm_avoided_count(stats.llm_avoided_count);
        response->set_llm_used_count(stats.llm_used_count);
        response->set_avg_graph_expansion_ms(stats.avg_graph_expansion_ms);
        response->set_avg_graph_expanded_chunks(stats.avg_graph_expanded_chunks);
        response->set_model_swaps_total(stats.model_swaps_total);
        response->set_multi_vector_enabled(stats.multi_vector_enabled);
        response->set_coarse_dimension(stats.coarse_dimension);
        response->set_fine_dimension(stats.fine_dimension);
        response->set_coarse_index_vectors(stats.coarse_index_vectors);
        response->set_fine_index_vectors(stats.fine_index_vectors);

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        LOG_ERROR("GetEmbedStats failed: " + std::string(e.what()));
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// GetRepoFileMap — Knowledge Graph intra-repo file dependency map
//=============================================================================

grpc::Status EngineServiceImpl::GetRepoFileMap(
    grpc::ServerContext* context,
    const aipr::engine::v1::RepoFileMapRequest* request,
    aipr::engine::v1::RepoFileMapResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        std::vector<std::string> node_types(
            request->node_types().begin(), request->node_types().end());
        std::vector<std::string> edge_types(
            request->edge_types().begin(), request->edge_types().end());

        auto file_map = engine_->getRepoFileMap(
            request->repo_id(), node_types, edge_types);

        for (const auto& n : file_map.nodes) {
            auto* proto_node = response->add_nodes();
            proto_node->set_id(n.id);
            proto_node->set_node_type(n.node_type);
            proto_node->set_name(n.name);
            proto_node->set_file_path(n.file_path);
            proto_node->set_language(n.language);
            proto_node->set_repo_id(request->repo_id());
            proto_node->set_metadata(n.metadata);
        }

        for (const auto& e : file_map.edges) {
            auto* proto_edge = response->add_edges();
            proto_edge->set_id(e.id);
            proto_edge->set_src_id(e.src_id);
            proto_edge->set_dst_id(e.dst_id);
            proto_edge->set_edge_type(e.edge_type);
            proto_edge->set_weight(static_cast<float>(e.weight));
            proto_edge->set_repo_id(request->repo_id());
        }

        response->set_total_nodes(static_cast<uint32_t>(file_map.total_nodes));
        response->set_total_edges(static_cast<uint32_t>(file_map.total_edges));

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        LOG_ERROR("GetRepoFileMap failed: " + std::string(e.what()));
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// Proto Mapping Helpers
//=============================================================================

void EngineServiceImpl::toProto(const IndexStats& stats, aipr::engine::v1::IndexStats* proto) {
    proto->set_repo_id(stats.repo_id);
    proto->set_index_version(stats.index_version);
    proto->set_total_files(stats.total_files);
    proto->set_indexed_files(stats.indexed_files);
    proto->set_total_chunks(stats.total_chunks);
    proto->set_total_symbols(stats.total_symbols);
    proto->set_index_size_bytes(stats.index_size_bytes);
    proto->set_last_updated(stats.last_updated);
    proto->set_is_complete(stats.is_complete);
}

void EngineServiceImpl::toProto(const ContextChunk& chunk, aipr::engine::v1::ContextChunk* proto) {
    proto->set_id(chunk.id);
    proto->set_file_path(chunk.file_path);
    proto->set_start_line(static_cast<uint32_t>(chunk.start_line));
    proto->set_end_line(static_cast<uint32_t>(chunk.end_line));
    proto->set_content(chunk.content);
    proto->set_language(chunk.language);
    proto->set_relevance_score(chunk.relevance_score);

    for (const auto& symbol : chunk.symbols) {
        proto->add_symbols(symbol);
    }
}

void EngineServiceImpl::toProto(const TouchedSymbol& touched, aipr::engine::v1::TouchedSymbol* proto) {
    proto->set_name(touched.symbol.name);
    proto->set_qualified_name(touched.symbol.qualified_name);
    proto->set_kind(touched.symbol.kind);
    proto->set_file_path(touched.symbol.file_path);
    proto->set_line(static_cast<uint32_t>(touched.symbol.line));
    proto->set_change_type(changeTypeToString(touched.change_type));

    for (const auto& caller : touched.callers) {
        proto->add_callers(caller);
    }
    for (const auto& callee : touched.callees) {
        proto->add_callees(callee);
    }
}

void EngineServiceImpl::toProto(const HeuristicFinding& finding, aipr::engine::v1::HeuristicFinding* proto) {
    proto->set_id(finding.id);
    proto->set_category(toProtoCategory(finding.category));
    proto->set_severity(toProtoSeverity(finding.severity));
    proto->set_confidence(finding.confidence);
    proto->set_file_path(finding.file_path);
    proto->set_line(static_cast<uint32_t>(finding.line));
    proto->set_message(finding.message);
    proto->set_suggestion(finding.suggestion);
    proto->set_evidence(finding.evidence);
}

aipr::engine::v1::Severity EngineServiceImpl::toProtoSeverity(Severity severity) {
    switch (severity) {
        case Severity::Info:
            return aipr::engine::v1::SEVERITY_INFO;
        case Severity::Warning:
            return aipr::engine::v1::SEVERITY_WARNING;
        case Severity::Error:
            return aipr::engine::v1::SEVERITY_ERROR;
        case Severity::Critical:
            return aipr::engine::v1::SEVERITY_CRITICAL;
        default:
            return aipr::engine::v1::SEVERITY_UNSPECIFIED;
    }
}

aipr::engine::v1::CheckCategory EngineServiceImpl::toProtoCategory(CheckCategory category) {
    switch (category) {
        case CheckCategory::Security:
            return aipr::engine::v1::CATEGORY_SECURITY;
        case CheckCategory::Performance:
            return aipr::engine::v1::CATEGORY_PERFORMANCE;
        case CheckCategory::Reliability:
            return aipr::engine::v1::CATEGORY_RELIABILITY;
        case CheckCategory::Style:
            return aipr::engine::v1::CATEGORY_STYLE;
        case CheckCategory::Architecture:
            return aipr::engine::v1::CATEGORY_ARCHITECTURE;
        case CheckCategory::Testing:
            return aipr::engine::v1::CATEGORY_TESTING;
        case CheckCategory::Documentation:
            return aipr::engine::v1::CATEGORY_DOCUMENTATION;
        default:
            return aipr::engine::v1::CATEGORY_UNSPECIFIED;
    }
}

//=============================================================================
// Configuration — storage config pushed from Java server
//=============================================================================

CloudProvider EngineServiceImpl::toCloudProvider(aipr::engine::v1::StorageProvider provider) {
    switch (provider) {
        case aipr::engine::v1::STORAGE_PROVIDER_AWS:
            return CloudProvider::AWS;
        case aipr::engine::v1::STORAGE_PROVIDER_GCP:
            return CloudProvider::GCP;
        case aipr::engine::v1::STORAGE_PROVIDER_AZURE:
            return CloudProvider::Azure;
        case aipr::engine::v1::STORAGE_PROVIDER_OCI:
            return CloudProvider::OCI;
        case aipr::engine::v1::STORAGE_PROVIDER_MINIO:
            return CloudProvider::MinIO;
        case aipr::engine::v1::STORAGE_PROVIDER_CUSTOM:
            return CloudProvider::Custom;
        case aipr::engine::v1::STORAGE_PROVIDER_LOCAL:
        default:
            return CloudProvider::Local;
    }
}

//=============================================================================
// File Content (Swarm)
//=============================================================================

grpc::Status EngineServiceImpl::GetFileContent(
    grpc::ServerContext* context,
    const aipr::engine::v1::FileContentRequest* request,
    aipr::engine::v1::FileContentResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    const std::string& repo_id  = request->repo_id();
    const std::string& rel_path = request->file_path();
    const std::string& ref      = request->ref();

    if (repo_id.empty() || rel_path.empty()) {
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT,
                            "repo_id and file_path are required");
    }

    // Reject path traversal attempts.
    if (rel_path.find("..") != std::string::npos) {
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT,
                            "file_path must not contain '..'");
    }

    try {
        // Resolve the local clone directory.
        // The engine stores clones at  <storage_path>/repos/<repo_id>.
        auto* eng = engine_.get();
        if (!eng) {
            return grpc::Status(grpc::StatusCode::INTERNAL, "engine not initialised");
        }

        // Check that the repository has been indexed.
        IndexStats stats = eng->getIndexStats(repo_id);
        if (stats.total_chunks == 0) {
            return grpc::Status(grpc::StatusCode::NOT_FOUND,
                                "repository not indexed: " + repo_id);
        }

        // Retrieve the storage_path from the engine.
        std::string storage_path = eng->getStoragePath();
        if (storage_path.empty()) {
            return grpc::Status(grpc::StatusCode::INTERNAL,
                                "engine storage_path not configured");
        }

        namespace fs = std::filesystem;

        fs::path repo_dir = fs::path(storage_path) / "repos" / repo_id;
        if (!fs::exists(repo_dir)) {
            return grpc::Status(grpc::StatusCode::NOT_FOUND,
                                "local clone not found for " + repo_id);
        }

        // If a git ref was requested, checkout that ref temporarily.
        if (!ref.empty()) {
            std::string checkout_cmd = "cd " + repo_dir.string() +
                                       " && git checkout " + ref + " 2>&1";
            int rc = std::system(checkout_cmd.c_str());
            if (rc != 0) {
                return grpc::Status(grpc::StatusCode::NOT_FOUND,
                                    "git ref not found: " + ref);
            }
        }

        fs::path full_path = repo_dir / rel_path;
        if (!fs::exists(full_path) || !fs::is_regular_file(full_path)) {
            return grpc::Status(grpc::StatusCode::NOT_FOUND,
                                "file not found: " + rel_path);
        }

        // Size guard — refuse files larger than 10 MB.
        auto file_size = fs::file_size(full_path);
        if (file_size > 10 * 1024 * 1024) {
            return grpc::Status(grpc::StatusCode::RESOURCE_EXHAUSTED,
                                "file too large (" + std::to_string(file_size) + " bytes)");
        }

        // Read file content.
        std::ifstream ifs(full_path, std::ios::binary);
        if (!ifs) {
            return grpc::Status(grpc::StatusCode::INTERNAL,
                                "failed to open file: " + rel_path);
        }

        std::string content((std::istreambuf_iterator<char>(ifs)),
                             std::istreambuf_iterator<char>());

        // Detect binary: check for null bytes in the first 8KB.
        bool is_binary = false;
        size_t check_len = std::min(content.size(), size_t(8192));
        for (size_t i = 0; i < check_len; ++i) {
            if (content[i] == '\0') {
                is_binary = true;
                break;
            }
        }

        if (is_binary) {
            // Base64 encode binary content.
            // Simple base64 implementation for binary files.
            static const char b64_table[] =
                "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
            std::string encoded;
            encoded.reserve(((content.size() + 2) / 3) * 4);
            for (size_t i = 0; i < content.size(); i += 3) {
                uint32_t n = (static_cast<uint8_t>(content[i]) << 16);
                if (i + 1 < content.size()) n |= (static_cast<uint8_t>(content[i + 1]) << 8);
                if (i + 2 < content.size()) n |= (static_cast<uint8_t>(content[i + 2]));
                encoded += b64_table[(n >> 18) & 0x3F];
                encoded += b64_table[(n >> 12) & 0x3F];
                encoded += (i + 1 < content.size()) ? b64_table[(n >> 6) & 0x3F] : '=';
                encoded += (i + 2 < content.size()) ? b64_table[n & 0x3F] : '=';
            }
            response->set_content(encoded);
            response->set_encoding("base64");
            response->set_is_binary(true);
        } else {
            response->set_content(content);
            response->set_encoding("utf-8");
            response->set_is_binary(false);
        }

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL,
                            std::string("GetFileContent failed: ") + e.what());
    }
}

//=============================================================================
// Storage Configuration
//=============================================================================

grpc::Status EngineServiceImpl::ConfigureStorage(
    grpc::ServerContext* context,
    const aipr::engine::v1::StorageConfigRequest* request,
    aipr::engine::v1::StorageConfigResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        StorageConfig config;
        config.provider = toCloudProvider(request->provider());

        // Common fields
        config.base_path = request->base_path();
        config.bucket = request->bucket();
        config.region = request->region();
        config.endpoint_url = request->endpoint_url();

        // Authentication
        config.access_key = request->access_key();
        config.secret_key = request->secret_key();
        config.session_token = request->session_token();

        // AWS specific
        config.use_irsa = request->use_irsa();
        config.role_arn = request->role_arn();

        // GCP specific
        config.use_workload_identity = request->use_workload_identity();

        // Azure specific
        config.azure_account_name = request->azure_account_name();
        config.azure_account_key = request->azure_account_key();
        config.azure_sas_token = request->azure_sas_token();
        config.use_azure_ad = request->use_azure_ad();

        // OCI specific
        config.oci_tenancy = request->oci_tenancy();
        config.oci_user = request->oci_user();
        config.oci_fingerprint = request->oci_fingerprint();
        config.oci_key_file = request->oci_key_file();
        config.oci_namespace = request->oci_namespace();

        // Connection settings
        if (request->timeout_ms() > 0) {
            config.timeout_ms = request->timeout_ms();
        }
        if (request->max_retries() > 0) {
            config.max_retries = request->max_retries();
        }
        config.use_ssl = request->use_ssl();
        config.verify_ssl = request->verify_ssl();
        config.ca_bundle_path = request->ca_bundle_path();

        // Create storage backend
        auto storage = StorageBackend::create(config);

        // Swap atomically
        {
            std::lock_guard<std::mutex> lock(storage_mutex_);
            storage_ = std::move(storage);

            // Store the Go server callback URL so components like
            // RedisEmbedCache can reach the API server.
            if (!request->server_callback_url().empty()) {
                server_callback_url_ = request->server_callback_url();
                // Also publish as an env-var so any lazily-constructed
                // component (e.g. RedisEmbedCache) picks it up without
                // needing a pointer back to this service.
                ::setenv("ENGINE_GO_SERVER_URL",
                         server_callback_url_.c_str(), /*overwrite=*/1);
                LOG_INFO("[ConfigureStorage] server_callback_url set to "
                         + server_callback_url_);
            }
        }

        // Map provider to human-readable name
        std::string providerName;
        switch (config.provider) {
            case CloudProvider::Local:  providerName = "local"; break;
            case CloudProvider::AWS:    providerName = "s3"; break;
            case CloudProvider::GCP:    providerName = "gcs"; break;
            case CloudProvider::Azure:  providerName = "azure"; break;
            case CloudProvider::OCI:    providerName = "oci"; break;
            case CloudProvider::MinIO:  providerName = "minio"; break;
            case CloudProvider::Custom: providerName = "custom"; break;
            default:                    providerName = "unknown"; break;
        }

        response->set_success(true);
        response->set_message("Storage backend configured successfully");
        response->set_active_provider(providerName);

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        response->set_success(false);
        response->set_message(std::string("Failed to configure storage: ") + e.what());
        return grpc::Status::OK;  // Return OK with error in response body
    }
}

//=============================================================================
// Metrics Streaming
//=============================================================================

grpc::Status EngineServiceImpl::StreamEngineMetrics(
    grpc::ServerContext* context,
    const aipr::engine::v1::EngineMetricsRequest* request,
    grpc::ServerWriter<aipr::engine::v1::EngineMetricsSnapshot>* writer)
{
    uint32_t interval_ms = request->interval_ms();
    if (interval_ms == 0) interval_ms = 1000;
    if (interval_ms < 200) interval_ms = 200;

    active_metric_streams_.fetch_add(1);
    LOG_INFO("[Metrics] Stream subscriber connected (interval=" +
             std::to_string(interval_ms) + "ms, active=" +
             std::to_string(active_metric_streams_.load()) + ")");

    auto cleanup = [this]() {
        active_metric_streams_.fetch_sub(1);
        LOG_INFO("[Metrics] Stream subscriber disconnected (active=" +
                 std::to_string(active_metric_streams_.load()) + ")");
    };

    while (!context->IsCancelled()) {
        // Collect snapshot from the metrics registry
        auto snap = metrics::Registry::instance().snapshot();

        aipr::engine::v1::EngineMetricsSnapshot proto;
        proto.set_timestamp_ms(snap.timestamp_ms);
        proto.set_uptime_s(snap.uptime_s);

        for (const auto& [name, mv] : snap.metrics) {
            aipr::engine::v1::MetricValueProto* mvp =
                &(*proto.mutable_metrics())[name];

            switch (mv.type) {
                case metrics::MetricType::COUNTER:
                    mvp->set_type(aipr::engine::v1::MetricValueProto::COUNTER);
                    mvp->set_scalar(mv.scalar);
                    break;
                case metrics::MetricType::GAUGE:
                    mvp->set_type(aipr::engine::v1::MetricValueProto::GAUGE);
                    mvp->set_scalar(mv.scalar);
                    break;
                case metrics::MetricType::HISTOGRAM: {
                    mvp->set_type(aipr::engine::v1::MetricValueProto::HISTOGRAM);
                    auto* hp = mvp->mutable_histogram();
                    hp->set_count(mv.histogram.count);
                    hp->set_sum(mv.histogram.sum);
                    hp->set_min_val(mv.histogram.min_val);
                    hp->set_max_val(mv.histogram.max_val);
                    hp->set_avg(mv.histogram.avg);
                    hp->set_p50(mv.histogram.p50);
                    hp->set_p90(mv.histogram.p90);
                    hp->set_p95(mv.histogram.p95);
                    hp->set_p99(mv.histogram.p99);
                    break;
                }
            }
        }

        // ── Populate structured KG fields from existing gauges ────
        {
            auto kg_it = snap.metrics.find(metrics::KG_NODES_TOTAL);
            if (kg_it != snap.metrics.end()) {
                proto.set_knowledge_graph_nodes(static_cast<uint64_t>(kg_it->second.scalar));
            }
            auto ke_it = snap.metrics.find(metrics::KG_EDGES_TOTAL);
            if (ke_it != snap.metrics.end()) {
                proto.set_knowledge_graph_edges(static_cast<uint64_t>(ke_it->second.scalar));
            }
        }

        // ── Scan per-repo index sizes from metrics prefix ─────────
        {
            const std::string prefix = metrics::INDEX_SIZES_PREFIX;
            for (const auto& [name, mv] : snap.metrics) {
                if (name.compare(0, prefix.size(), prefix) == 0) {
                    std::string repo_id = name.substr(prefix.size());
                    (*proto.mutable_index_sizes_bytes())[repo_id] =
                        static_cast<uint64_t>(mv.scalar);
                }
            }
        }

        if (!writer->Write(proto)) {
            cleanup();
            return grpc::Status(grpc::StatusCode::CANCELLED, "Client disconnected");
        }

        std::this_thread::sleep_for(std::chrono::milliseconds(interval_ms));
    }

    cleanup();
    return grpc::Status::OK;
}

//=============================================================================
// Asset Ingestion (PDFs, URLs, Documents → Embeddings)
//=============================================================================

grpc::Status EngineServiceImpl::IngestAsset(
    grpc::ServerContext* context,
    const aipr::engine::v1::IngestAssetRequest* request,
    aipr::engine::v1::IngestAssetResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    const std::string& repo_id = request->repo_id();
    const std::string& content = request->content();
    const std::string& asset_type = request->asset_type();
    const std::string& mime_type = request->mime_type();
    const std::string& file_name = request->file_name();
    const auto& binary_data = request->binary_data();

    if (repo_id.empty()) {
        response->set_status("error: repo_id is required");
        response->set_chunks_created(0);
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT,
                            "repo_id is required");
    }

    try {
        // ── Binary asset path: image/audio via multimodal embedder ──
        if (!binary_data.empty() &&
            (asset_type == "image" || asset_type == "audio" ||
             mime_type.find("image/") == 0 || mime_type.find("audio/") == 0)) {

            std::vector<uint8_t> data(binary_data.begin(), binary_data.end());
            std::string asset_id = request->file_name().empty()
                ? ("asset_" + repo_id + "_" + std::to_string(
                       std::chrono::system_clock::now().time_since_epoch().count()))
                : file_name;

            bool ok = engine_->embedBinaryAsset(
                repo_id, data, mime_type, file_name, asset_id);

            if (ok) {
                response->set_chunks_created(1);
                response->set_status("ok");
                response->set_asset_id(asset_id);
                // Detect type
                if (mime_type.find("image/") == 0 || asset_type == "image") {
                    response->set_detected_type(aipr::engine::v1::ASSET_TYPE_IMAGE);
                } else {
                    response->set_detected_type(aipr::engine::v1::ASSET_TYPE_AUDIO);
                }
            } else {
                response->set_chunks_created(0);
                response->set_status("error: multimodal embedding failed");
                return grpc::Status(grpc::StatusCode::INTERNAL,
                                    "multimodal embedding failed for " + asset_type);
            }
            return grpc::Status::OK;
        }

        // ── Text asset path: chunk + text embed ─────────────────────
        if (content.empty()) {
            response->set_status("error: content or binary_data is required");
            response->set_chunks_created(0);
            return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT,
                                "content or binary_data is required");
        }

        // Chunk the content into embedding-sized pieces.
        std::vector<std::string> chunks;
        const size_t max_chunk_chars = 1500;
        const size_t overlap_chars = 200;

        size_t pos = 0;
        while (pos < content.size()) {
            size_t end = std::min(pos + max_chunk_chars, content.size());
            // Try to break at paragraph or sentence boundary
            if (end < content.size()) {
                size_t para = content.rfind("\n\n", end);
                if (para != std::string::npos && para > pos + max_chunk_chars / 2) {
                    end = para + 2;
                } else {
                    size_t sent = content.rfind(". ", end);
                    if (sent != std::string::npos && sent > pos + max_chunk_chars / 2) {
                        end = sent + 2;
                    }
                }
            }
            chunks.push_back(content.substr(pos, end - pos));
            pos = (end > overlap_chars) ? end - overlap_chars : end;
            if (pos >= content.size()) break;
        }

        // Build metadata prefix for each chunk
        std::string source = request->source_url().empty() ? asset_type : request->source_url();
        int embedded = 0;

        for (size_t i = 0; i < chunks.size(); ++i) {
            std::string prefixed = "[" + asset_type + "] " + source +
                                   " (chunk " + std::to_string(i + 1) +
                                   "/" + std::to_string(chunks.size()) + ")\n" +
                                   chunks[i];
            bool ok = engine_->embedAndStoreAssetChunk(
                repo_id, prefixed, source, asset_type,
                std::map<std::string, std::string>(
                    request->metadata().begin(), request->metadata().end()));
            if (ok) embedded++;
        }

        response->set_chunks_created(embedded);
        response->set_status("ok");
        return grpc::Status::OK;

    } catch (const std::exception& e) {
        response->set_chunks_created(0);
        response->set_status(std::string("error: ") + e.what());
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

//=============================================================================
// Multimodal Embedding Operations
//=============================================================================

grpc::Status EngineServiceImpl::GetMultimodalConfig(
    grpc::ServerContext* context,
    const aipr::engine::v1::GetMultimodalConfigRequest* /*request*/,
    aipr::engine::v1::GetMultimodalConfigResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        auto configs = engine_->getMultimodalConfig();
        bool image_enabled = false;
        bool audio_enabled = false;

        for (const auto& cfg : configs) {
            auto* mc = response->add_modalities();
            mc->set_modality(cfg.modality);
            mc->set_model_name(cfg.model_name);
            mc->set_enabled(cfg.enabled);
            mc->set_status(cfg.status);
            mc->set_download_progress(cfg.download_progress);

            if (cfg.modality == "image") image_enabled = cfg.enabled;
            if (cfg.modality == "audio") audio_enabled = cfg.enabled;
        }
        response->set_unified_dimension(1024);
        response->set_image_enabled(image_enabled);
        response->set_audio_enabled(audio_enabled);
        return grpc::Status::OK;
    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::ConfigureMultimodal(
    grpc::ServerContext* context,
    const aipr::engine::v1::ConfigureMultimodalRequest* request,
    aipr::engine::v1::ConfigureMultimodalResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        std::vector<std::string> loaded;

        auto img = engine_->setModalityEnabled("image", request->enable_image());
        if (img.enabled && img.status == "ready") {
            loaded.push_back("image:" + img.model_name);
        }

        auto aud = engine_->setModalityEnabled("audio", request->enable_audio());
        if (aud.enabled && aud.status == "ready") {
            loaded.push_back("audio:" + aud.model_name);
        }

        response->set_success(true);
        response->set_message("Multimodal configuration updated");
        for (const auto& m : loaded) {
            response->add_loaded_models(m);
        }
        return grpc::Status::OK;
    } catch (const std::exception& e) {
        response->set_success(false);
        response->set_message(std::string("error: ") + e.what());
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::DownloadModel(
    grpc::ServerContext* context,
    const aipr::engine::v1::ConfigureMultimodalRequest* request,
    grpc::ServerWriter<aipr::engine::v1::ModelDownloadProgress>* writer)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        // Determine which modalities to enable/download
        std::vector<std::pair<std::string, bool>> modalities;
        modalities.push_back({"image", request->enable_image()});
        modalities.push_back({"audio", request->enable_audio()});

        for (const auto& [modality, enabled] : modalities) {
            if (!enabled) continue;

            // Send initial progress
            aipr::engine::v1::ModelDownloadProgress progress;
            std::string model_name = (modality == "image") ? "siglip-base" : "clap-general";
            progress.set_model_name(model_name);
            progress.set_phase("downloading");
            progress.set_progress(0);
            progress.set_done(false);
            progress.set_success(false);
            writer->Write(progress);

            // Trigger download via enable
            auto result = engine_->setModalityEnabled(modality, true);

            // Send completion
            progress.set_phase(result.status == "ready" ? "ready" : result.status);
            progress.set_progress(result.status == "ready" ? 100 : result.download_progress);
            progress.set_done(true);
            progress.set_success(result.status == "ready");
            writer->Write(progress);
        }

        return grpc::Status::OK;
    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

} // namespace server
} // namespace aipr
