/**
 * AI PR Reviewer - Engine gRPC Service Implementation
 *
 * Implements the EngineService::Service interface defined in engine.proto
 * by delegating to the core Engine API.
 */

#include "engine_service_impl.h"

#include <sstream>
#include <mutex>

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
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
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

        // Convert results to proto
        for (const auto& chunk : chunks) {
            toProto(chunk, response->add_chunks());
        }

        // Set metrics (simplified - full metrics would come from engine)
        auto* metrics = response->mutable_metrics();
        metrics->set_total_candidates(static_cast<uint32_t>(chunks.size()));

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
    }
}

grpc::Status EngineServiceImpl::SearchStream(
    grpc::ServerContext* context,
    const aipr::engine::v1::SearchRequest* request,
    grpc::ServerWriter<aipr::engine::v1::ContextChunk>* writer)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "Request cancelled by client");
    }

    try {
        size_t top_k = 20;
        if (request->has_config() && request->config().top_k() > 0) {
            top_k = request->config().top_k();
        }

        std::vector<ContextChunk> chunks = engine_->search(
            request->repo_id(),
            request->query(),
            top_k
        );

        // Stream results one by one
        for (const auto& chunk : chunks) {
            if (context->IsCancelled()) {
                return grpc::Status(grpc::StatusCode::CANCELLED, "Stream cancelled by client");
            }

            aipr::engine::v1::ContextChunk proto_chunk;
            toProto(chunk, &proto_chunk);

            if (!writer->Write(proto_chunk)) {
                return grpc::Status(grpc::StatusCode::UNKNOWN, "Failed to write to stream");
            }
        }

        return grpc::Status::OK;

    } catch (const std::exception& e) {
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

        return grpc::Status::OK;

    } catch (const std::exception& e) {
        return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
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
