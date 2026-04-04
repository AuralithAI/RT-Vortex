/**
 * Cross-Repo Service — gRPC Implementation
 *
 * Implements CrossRepoService defined in engine.proto.
 * This is a SEPARATE service from EngineService — it operates on multiple
 * repo indices simultaneously for dependency graph and federated search.
 *
 * Authorization is handled by the Go server's CrossRepoAuthorizer.
 * By the time a request reaches this service, the repo list has already
 * been validated.
 */

#ifndef AIPR_CROSS_REPO_SERVICE_IMPL_H
#define AIPR_CROSS_REPO_SERVICE_IMPL_H

#include "engine_api.h"
#include "hierarchy_builder.h"
#include "engine.grpc.pb.h"

#include <grpcpp/grpcpp.h>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include <unordered_map>

namespace aipr {
namespace server {

/**
 * gRPC service implementation for CrossRepoService.
 *
 * Holds a pointer to the same Engine instance used by EngineServiceImpl.
 * The Engine already manages per-repo FAISS indices — this service orchestrates
 * queries across multiple indices.
 */
class CrossRepoServiceImpl final
    : public aipr::engine::v1::CrossRepoService::Service {
public:
    /**
     * @param engine  Non-owning pointer to the shared Engine instance
     *                (owned by EngineServiceImpl / main).
     */
    explicit CrossRepoServiceImpl(Engine* engine);

    ~CrossRepoServiceImpl() override = default;

    // Disable copy
    CrossRepoServiceImpl(const CrossRepoServiceImpl&) = delete;
    CrossRepoServiceImpl& operator=(const CrossRepoServiceImpl&) = delete;

    //-------------------------------------------------------------------------
    // Dependency Graph
    //-------------------------------------------------------------------------

    grpc::Status GetRepoManifest(
        grpc::ServerContext* context,
        const aipr::engine::v1::RepoManifestRequest* request,
        aipr::engine::v1::RepoManifestResponse* response
    ) override;

    grpc::Status GetCrossRepoDependencies(
        grpc::ServerContext* context,
        const aipr::engine::v1::CrossRepoDepsRequest* request,
        aipr::engine::v1::CrossRepoDepsResponse* response
    ) override;

    grpc::Status BuildDependencyGraph(
        grpc::ServerContext* context,
        const aipr::engine::v1::BuildDepGraphRequest* request,
        aipr::engine::v1::BuildDepGraphResponse* response
    ) override;

    //-------------------------------------------------------------------------
    // Federated Search
    //-------------------------------------------------------------------------

    grpc::Status FederatedSearch(
        grpc::ServerContext* context,
        const aipr::engine::v1::FederatedSearchRequest* request,
        aipr::engine::v1::FederatedSearchResponse* response
    ) override;

    grpc::Status FederatedSearchStream(
        grpc::ServerContext* context,
        const aipr::engine::v1::FederatedSearchRequest* request,
        grpc::ServerWriter<aipr::engine::v1::FederatedContextChunk>* writer
    ) override;

private:
    Engine* engine_;  // non-owning; shared with EngineServiceImpl

    // Default concurrency cap for federated search fan-out.
    static constexpr int kDefaultMaxConcurrent = 4;

    // Manifest cache — avoids re-scanning on every GetRepoManifest call.
    mutable std::mutex manifest_cache_mutex_;
    std::unordered_map<std::string, aipr::engine::v1::RepoManifestProto> manifest_cache_;

    //-------------------------------------------------------------------------
    // Internal helpers
    //-------------------------------------------------------------------------

    // Convert the C++ RepoManifest to the proto message.
    static void toProto(const RepoManifest& manifest,
                        const std::string& repo_id,
                        aipr::engine::v1::RepoManifestProto* out);

    // Perform a single-repo search (delegates to Engine::search).
    // Returns chunks with repo attribution.
    struct PerRepoResult {
        std::string repo_id;
        std::vector<aipr::engine::v1::FederatedContextChunk> chunks;
        uint64_t search_time_ms = 0;
        bool success = true;
        std::string error;
    };

    PerRepoResult searchSingleRepo(
        const std::string& repo_id,
        const aipr::engine::v1::FederatedSearchRequest& request) const;

    // Normalize scores across repos using min-max or z-score.
    static void normalizeScores(
        std::vector<aipr::engine::v1::FederatedContextChunk>& chunks,
        const std::string& strategy);
};

} // namespace server
} // namespace aipr

#endif // AIPR_CROSS_REPO_SERVICE_IMPL_H
