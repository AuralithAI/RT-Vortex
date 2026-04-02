/**
 * AI PR Reviewer - Engine gRPC Service Implementation
 *
 * This header declares the gRPC service implementation that wraps
 * the core Engine API and exposes it via gRPC for the Java server.
 */

#ifndef AIPR_ENGINE_SERVICE_IMPL_H
#define AIPR_ENGINE_SERVICE_IMPL_H

#include "engine_api.h"
#include "storage_backend.h"
#include "engine.grpc.pb.h"

#include <grpcpp/grpcpp.h>
#include <memory>
#include <atomic>
#include <chrono>
#include <mutex>
#include <semaphore>
#include <condition_variable>

namespace aipr {
namespace server {

/**
 * gRPC service implementation for the Engine
 *
 * This class implements all RPC methods defined in engine.proto by delegating
 * to the core Engine instance. It handles proto <-> C++ struct mapping.
 */
class EngineServiceImpl final : public aipr::engine::v1::EngineService::Service {
public:
    /**
     * Create service implementation with an existing engine instance
     *
     * @param engine Shared pointer to the engine (takes ownership)
     */
    explicit EngineServiceImpl(std::unique_ptr<Engine> engine);

    ~EngineServiceImpl() override = default;

    // Disable copy
    EngineServiceImpl(const EngineServiceImpl&) = delete;
    EngineServiceImpl& operator=(const EngineServiceImpl&) = delete;

    //-------------------------------------------------------------------------
    // Indexing Operations
    //-------------------------------------------------------------------------

    grpc::Status IndexRepository(
        grpc::ServerContext* context,
        const aipr::engine::v1::IndexRequest* request,
        aipr::engine::v1::IndexResponse* response
    ) override;

    grpc::Status IndexRepositoryStream(
        grpc::ServerContext* context,
        const aipr::engine::v1::IndexRequest* request,
        grpc::ServerWriter<aipr::engine::v1::IndexProgressUpdate>* writer
    ) override;

    grpc::Status IncrementalIndex(
        grpc::ServerContext* context,
        const aipr::engine::v1::IncrementalIndexRequest* request,
        aipr::engine::v1::IndexResponse* response
    ) override;

    grpc::Status GetIndexStats(
        grpc::ServerContext* context,
        const aipr::engine::v1::IndexStatsRequest* request,
        aipr::engine::v1::IndexStatsResponse* response
    ) override;

    grpc::Status DeleteIndex(
        grpc::ServerContext* context,
        const aipr::engine::v1::DeleteIndexRequest* request,
        aipr::engine::v1::DeleteIndexResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Retrieval Operations
    //-------------------------------------------------------------------------

    grpc::Status Search(
        grpc::ServerContext* context,
        const aipr::engine::v1::SearchRequest* request,
        aipr::engine::v1::SearchResponse* response
    ) override;

    grpc::Status SearchStream(
        grpc::ServerContext* context,
        const aipr::engine::v1::SearchRequest* request,
        grpc::ServerWriter<aipr::engine::v1::ContextChunk>* writer
    ) override;

    //-------------------------------------------------------------------------
    // Review Operations
    //-------------------------------------------------------------------------

    grpc::Status BuildReviewContext(
        grpc::ServerContext* context,
        const aipr::engine::v1::ReviewContextRequest* request,
        aipr::engine::v1::ReviewContextResponse* response
    ) override;

    grpc::Status BuildReviewContextStream(
        grpc::ServerContext* context,
        const aipr::engine::v1::ReviewContextRequest* request,
        grpc::ServerWriter<aipr::engine::v1::PREmbedProgressUpdate>* writer
    ) override;

    grpc::Status RunHeuristics(
        grpc::ServerContext* context,
        const aipr::engine::v1::HeuristicsRequest* request,
        aipr::engine::v1::HeuristicsResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Health & Diagnostics
    //-------------------------------------------------------------------------

    grpc::Status HealthCheck(
        grpc::ServerContext* context,
        const aipr::engine::v1::HealthCheckRequest* request,
        aipr::engine::v1::HealthCheckResponse* response
    ) override;

    grpc::Status GetDiagnostics(
        grpc::ServerContext* context,
        const aipr::engine::v1::DiagnosticsRequest* request,
        aipr::engine::v1::DiagnosticsResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Embedding Statistics
    //-------------------------------------------------------------------------

    grpc::Status GetEmbedStats(
        grpc::ServerContext* context,
        const aipr::engine::v1::EmbedStatsRequest* request,
        aipr::engine::v1::EmbedStatsResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Knowledge Graph — Intra-Repo File Map
    //-------------------------------------------------------------------------

    grpc::Status GetRepoFileMap(
        grpc::ServerContext* context,
        const aipr::engine::v1::RepoFileMapRequest* request,
        aipr::engine::v1::RepoFileMapResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // File Content (Swarm)
    //-------------------------------------------------------------------------

    grpc::Status GetFileContent(
        grpc::ServerContext* context,
        const aipr::engine::v1::FileContentRequest* request,
        aipr::engine::v1::FileContentResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Asset Ingestion (Swarm — PDFs, URLs, Documents → Embeddings)
    //-------------------------------------------------------------------------

    grpc::Status IngestAsset(
        grpc::ServerContext* context,
        const aipr::engine::v1::IngestAssetRequest* request,
        aipr::engine::v1::IngestAssetResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Multimodal Embedding Operations
    //-------------------------------------------------------------------------

    grpc::Status GetMultimodalConfig(
        grpc::ServerContext* context,
        const aipr::engine::v1::GetMultimodalConfigRequest* request,
        aipr::engine::v1::GetMultimodalConfigResponse* response
    ) override;

    grpc::Status ConfigureMultimodal(
        grpc::ServerContext* context,
        const aipr::engine::v1::ConfigureMultimodalRequest* request,
        aipr::engine::v1::ConfigureMultimodalResponse* response
    ) override;

    grpc::Status DownloadModel(
        grpc::ServerContext* context,
        const aipr::engine::v1::ConfigureMultimodalRequest* request,
        grpc::ServerWriter<aipr::engine::v1::ModelDownloadProgress>* writer
    ) override;

    //-------------------------------------------------------------------------
    // Configuration
    //-------------------------------------------------------------------------

    grpc::Status ConfigureStorage(
        grpc::ServerContext* context,
        const aipr::engine::v1::StorageConfigRequest* request,
        aipr::engine::v1::StorageConfigResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Metrics Streaming
    //-------------------------------------------------------------------------

    grpc::Status StreamEngineMetrics(
        grpc::ServerContext* context,
        const aipr::engine::v1::EngineMetricsRequest* request,
        grpc::ServerWriter<aipr::engine::v1::EngineMetricsSnapshot>* writer
    ) override;

    /**
     * Get the active storage backend (may be null if not configured)
     */
    StorageBackend* getStorage() const { return storage_.get(); }

    /**
     * Get the Go server callback URL received via ConfigureStorage.
     * Empty until the Go server pushes its config at startup.
     */
    std::string getServerCallbackUrl() const {
        std::lock_guard<std::mutex> lock(storage_mutex_);
        return server_callback_url_;
    }

    //-------------------------------------------------------------------------
    // Utility
    //-------------------------------------------------------------------------

    /**
     * Get the uptime of this service instance
     */
    uint64_t getUptimeSeconds() const;

    /**
     * Get the underlying engine instance (for diagnostics)
     */
    Engine* getEngine() const { return engine_.get(); }

private:
    std::unique_ptr<Engine> engine_;
    std::chrono::steady_clock::time_point start_time_;

    // Storage backend — configured via gRPC ConfigureStorage from Go server
    std::unique_ptr<StorageBackend> storage_;
    mutable std::mutex storage_mutex_;

    // Go API server callback URL — set by ConfigureStorage, used by Redis embed
    // cache and any component that needs to call back into the Go server.
    std::string server_callback_url_;

    // Active metrics stream subscribers
    std::atomic<uint32_t> active_metric_streams_{0};

    // Concurrency control for indexing — limits parallel indexing jobs
    static constexpr int kMaxConcurrentIndexJobs = 3;
    std::mutex index_sem_mutex_;
    std::condition_variable index_sem_cv_;
    int active_index_jobs_ = 0;

    // Convert proto StorageProvider enum to C++ CloudProvider
    static CloudProvider toCloudProvider(aipr::engine::v1::StorageProvider provider);

    //-------------------------------------------------------------------------
    // Proto <-> C++ Struct Mapping Helpers
    //-------------------------------------------------------------------------

    // Convert C++ IndexStats to proto
    static void toProto(const IndexStats& stats, aipr::engine::v1::IndexStats* proto);

    // Convert C++ ContextChunk to proto
    static void toProto(const ContextChunk& chunk, aipr::engine::v1::ContextChunk* proto);

    // Convert C++ TouchedSymbol to proto
    static void toProto(const TouchedSymbol& symbol, aipr::engine::v1::TouchedSymbol* proto);

    // Convert C++ HeuristicFinding to proto
    static void toProto(const HeuristicFinding& finding, aipr::engine::v1::HeuristicFinding* proto);

    // Convert C++ Severity to proto
    static aipr::engine::v1::Severity toProtoSeverity(Severity severity);

    // Convert C++ CheckCategory to proto
    static aipr::engine::v1::CheckCategory toProtoCategory(CheckCategory category);
};

} // namespace server
} // namespace aipr

#endif // AIPR_ENGINE_SERVICE_IMPL_H
