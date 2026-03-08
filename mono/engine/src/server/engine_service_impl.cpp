/**
 * AI PR Reviewer - Engine gRPC Service Implementation
 *
 * Implements the EngineService::Service interface defined in engine.proto
 * by delegating to the core Engine API.
 */

#include "engine_service_impl.h"
#include "logging.h"

#include <sstream>
#include <mutex>
#include <chrono>

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
        // Note: IndexConfig settings from request are not yet supported
        // in the core engine API. The request proto config fields are
        // reserved for future use.

        IndexStats stats = engine_->indexRepository(
            request->repo_id(),
            request->repo_path(),
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

        IndexStats stats = engine_->indexRepository(
            repo_id,
            request->repo_path(),
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

        std::vector<ContextChunk> chunks = engine_->search(
            request->repo_id(),
            request->query(),
            top_k
        );

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

        // Set metrics (simplified - full metrics would come from engine)
        auto* metrics = response->mutable_metrics();
        metrics->set_total_candidates(static_cast<uint32_t>(chunks.size()));

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
        // Phase 1: Parsing diff
        send_progress(5, "parsing_diff");

        // Phase 2: Building review context (does parsing, TMS search,
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

        // Phase 3: Building context pack proto
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

        // Phase 4: Send final completion with context_pack
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

} // namespace server
} // namespace aipr
