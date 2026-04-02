/**
 * Cross-Repo Service — gRPC Implementation
 *
 * See cross_repo_service_impl.h for design notes.
 */

#include "cross_repo_service_impl.h"
#include "hierarchy_builder.h"
#include "logging.h"
#include "metrics.h"

#include <algorithm>
#include <cmath>
#include <chrono>
#include <future>
#include <numeric>
#include <thread>

namespace aipr {
namespace server {

CrossRepoServiceImpl::CrossRepoServiceImpl(Engine* engine)
    : engine_(engine)
{
    LOG_INFO("CrossRepoService initialized");
}

// ============================================================================
// GetRepoManifest
// ============================================================================

grpc::Status CrossRepoServiceImpl::GetRepoManifest(
    grpc::ServerContext* context,
    const aipr::engine::v1::RepoManifestRequest* request,
    aipr::engine::v1::RepoManifestResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "cancelled");
    }

    const auto& repo_id = request->repo_id();
    if (repo_id.empty()) {
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT, "repo_id is required");
    }

    // Check cache first.
    {
        std::lock_guard<std::mutex> lock(manifest_cache_mutex_);
        auto it = manifest_cache_.find(repo_id);
        if (it != manifest_cache_.end()) {
            response->set_found(true);
            *response->mutable_manifest() = it->second;
            return grpc::Status::OK;
        }
    }

    // Ask the engine for the repo's local clone path.
    auto repo_path = engine_->getRepoPath(repo_id);
    if (repo_path.empty()) {
        response->set_found(false);
        return grpc::Status::OK;
    }

    // Build manifest using the existing HierarchyBuilder.
    HierarchyBuilder builder;
    auto manifest = builder.buildRepoManifest(repo_path);

    aipr::engine::v1::RepoManifestProto proto;
    toProto(manifest, repo_id, &proto);

    // Cache it.
    {
        std::lock_guard<std::mutex> lock(manifest_cache_mutex_);
        manifest_cache_[repo_id] = proto;
    }

    response->set_found(true);
    *response->mutable_manifest() = proto;
    return grpc::Status::OK;
}

// ============================================================================
//  GetCrossRepoDependencies
// ============================================================================

grpc::Status CrossRepoServiceImpl::GetCrossRepoDependencies(
    grpc::ServerContext* context,
    const aipr::engine::v1::CrossRepoDepsRequest* request,
    aipr::engine::v1::CrossRepoDepsResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "cancelled");
    }

    const auto& source_repo_id = request->source_repo_id();
    if (source_repo_id.empty()) {
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT, "source_repo_id is required");
    }

    // Get the source manifest.
    auto source_path = engine_->getRepoPath(source_repo_id);
    if (source_path.empty()) {
        return grpc::Status(grpc::StatusCode::NOT_FOUND, "source repo not indexed");
    }

    HierarchyBuilder builder;
    auto source_manifest = builder.buildRepoManifest(source_path);

    // Collect target repo manifests.
    std::vector<std::pair<std::string, RepoManifest>> targets;
    if (request->target_repo_ids_size() > 0) {
        for (const auto& tid : request->target_repo_ids()) {
            auto tp = engine_->getRepoPath(tid);
            if (!tp.empty()) {
                targets.emplace_back(tid, builder.buildRepoManifest(tp));
            }
        }
    }

    // Cross-reference: for each import in source files, check if it matches
    // a package/module name exposed by any target repo.
    uint32_t edge_count = 0;

    // Get all indexed chunks from the source repo to extract imports.
    auto source_chunks = engine_->getCodeChunksForRepo(source_repo_id);

    for (const auto& chunk : source_chunks) {
        for (const auto& dep : chunk.dependencies) {
            // Check each target manifest for a matching module/package.
            for (const auto& [target_id, target_manifest] : targets) {
                // Match against module names.
                for (const auto& [module_name, files] : target_manifest.module_to_files) {
                    if (dep.find(module_name) != std::string::npos) {
                        auto* edge = response->add_dependencies();
                        edge->set_source_repo_id(source_repo_id);
                        edge->set_source_file(chunk.file_path);
                        edge->set_source_symbol(dep);
                        edge->set_target_repo_id(target_id);
                        edge->set_target_symbol(module_name);
                        edge->set_dependency_type("import");
                        edge->set_confidence(0.7f);  // heuristic match
                        edge_count++;
                    }
                }

                // Match against build target names.
                for (const auto& target : target_manifest.targets) {
                    if (dep.find(target.name) != std::string::npos) {
                        auto* edge = response->add_dependencies();
                        edge->set_source_repo_id(source_repo_id);
                        edge->set_source_file(chunk.file_path);
                        edge->set_source_symbol(dep);
                        edge->set_target_repo_id(target_id);
                        edge->set_target_symbol(target.name);
                        edge->set_dependency_type("package");
                        edge->set_confidence(0.85f);
                        edge_count++;
                    }
                }
            }
        }
    }

    response->set_total_edges(edge_count);
    return grpc::Status::OK;
}

// ============================================================================
// BuildDependencyGraph
// ============================================================================

grpc::Status CrossRepoServiceImpl::BuildDependencyGraph(
    grpc::ServerContext* context,
    const aipr::engine::v1::BuildDepGraphRequest* request,
    aipr::engine::v1::BuildDepGraphResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "cancelled");
    }

    std::vector<std::string> repo_ids(
        request->repo_ids().begin(), request->repo_ids().end());

    if (repo_ids.empty()) {
        // The Go server should always pass the pre-authorized list.
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT,
            "repo_ids required (Go server must pre-filter authorized repos)");
    }

    HierarchyBuilder builder;
    std::unordered_map<std::string, RepoManifest> manifests;

    for (const auto& rid : repo_ids) {
        auto path = engine_->getRepoPath(rid);
        if (path.empty()) continue;

        if (request->force_rescan()) {
            // Invalidate cache.
            std::lock_guard<std::mutex> lock(manifest_cache_mutex_);
            manifest_cache_.erase(rid);
        }

        manifests[rid] = builder.buildRepoManifest(path);
    }

    // Build nodes: one per repo + one per module.
    uint32_t node_count = 0;
    for (const auto& [rid, manifest] : manifests) {
        auto* node = response->add_nodes();
        node->set_id(rid);
        node->set_repo_id(rid);
        node->set_label(manifest.repo_root);
        node->set_node_type("repo");
        node->set_language(manifest.primary_language);
        node->set_repo_type(static_cast<aipr::engine::v1::RepoType>(manifest.repo_type));
        node_count++;

        for (const auto& [module_name, files] : manifest.module_to_files) {
            auto module_id = rid + "::" + module_name;
            auto* mnode = response->add_nodes();
            mnode->set_id(module_id);
            mnode->set_repo_id(rid);
            mnode->set_label(module_name);
            mnode->set_node_type("module");
            mnode->set_language(manifest.primary_language);
            node_count++;

            // Edge: module belongs to repo.
            auto* belongs_edge = response->add_edges();
            belongs_edge->set_source_node_id(module_id);
            belongs_edge->set_target_node_id(rid);
            belongs_edge->set_edge_type("submodule_of");
            belongs_edge->set_weight(1.0f);
        }
    }

    // Build cross-repo dependency edges using chunk imports.
    uint32_t dep_edge_count = 0;
    for (const auto& [source_rid, source_manifest] : manifests) {
        auto chunks = engine_->getCodeChunksForRepo(source_rid);
        for (const auto& chunk : chunks) {
            for (const auto& dep : chunk.dependencies) {
                for (const auto& [target_rid, target_manifest] : manifests) {
                    if (target_rid == source_rid) continue;

                    for (const auto& [module_name, files] : target_manifest.module_to_files) {
                        if (dep.find(module_name) != std::string::npos) {
                            auto* edge = response->add_edges();
                            edge->set_source_node_id(source_rid);
                            edge->set_target_node_id(target_rid + "::" + module_name);
                            edge->set_edge_type("depends_on");
                            edge->set_weight(1.0f);
                            dep_edge_count++;
                        }
                    }
                }
            }
        }
    }

    response->set_success(true);
    response->set_repos_scanned(static_cast<uint32_t>(manifests.size()));
    response->set_total_nodes(node_count);
    response->set_total_edges(dep_edge_count + static_cast<uint32_t>(response->edges_size()));

    LOG_INFO("BuildDependencyGraph: " + std::to_string(manifests.size()) + " repos, "
             + std::to_string(node_count) + " nodes, "
             + std::to_string(response->edges_size()) + " edges");

    return grpc::Status::OK;
}

// ============================================================================
// FederatedSearch
// ============================================================================

grpc::Status CrossRepoServiceImpl::FederatedSearch(
    grpc::ServerContext* context,
    const aipr::engine::v1::FederatedSearchRequest* request,
    aipr::engine::v1::FederatedSearchResponse* response)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "cancelled");
    }

    if (request->repo_ids_size() == 0) {
        return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT, "at least one repo_id required");
    }

    auto start = std::chrono::steady_clock::now();

    int max_concurrent = request->max_concurrent() > 0
        ? static_cast<int>(request->max_concurrent())
        : kDefaultMaxConcurrent;

    std::string normalization = request->score_normalization();
    if (normalization.empty()) {
        normalization = "min_max";
    }

    // Fan out: search each repo in parallel with a concurrency budget.
    std::vector<std::future<PerRepoResult>> futures;
    std::vector<PerRepoResult> results;

    // Simple semaphore-like approach using a chunked fan-out.
    std::vector<std::string> repo_ids(
        request->repo_ids().begin(), request->repo_ids().end());

    for (size_t i = 0; i < repo_ids.size(); i += max_concurrent) {
        futures.clear();
        size_t batch_end = std::min(i + static_cast<size_t>(max_concurrent), repo_ids.size());

        for (size_t j = i; j < batch_end; j++) {
            futures.push_back(std::async(std::launch::async,
                [this, &request, &repo_ids, j]() {
                    return searchSingleRepo(repo_ids[j], *request);
                }));
        }

        for (auto& f : futures) {
            results.push_back(f.get());
        }
    }

    // Merge all chunks.
    std::vector<aipr::engine::v1::FederatedContextChunk> all_chunks;
    auto* metrics = response->mutable_metrics();
    uint32_t repos_searched = 0;
    uint32_t repos_failed = 0;
    uint32_t total_candidates = 0;

    for (auto& r : results) {
        if (r.success) {
            repos_searched++;
            (*metrics->mutable_per_repo_time_ms())[r.repo_id] = r.search_time_ms;
            (*metrics->mutable_per_repo_results())[r.repo_id] =
                static_cast<uint32_t>(r.chunks.size());
            total_candidates += static_cast<uint32_t>(r.chunks.size());
            for (auto& c : r.chunks) {
                all_chunks.push_back(std::move(c));
            }
        } else {
            repos_failed++;
            LOG_WARN("FederatedSearch: repo " + r.repo_id + " failed: " + r.error);
        }
    }

    // Normalize scores across repos.
    normalizeScores(all_chunks, normalization);

    // Sort by normalized score descending.
    std::sort(all_chunks.begin(), all_chunks.end(),
        [](const auto& a, const auto& b) {
            return a.normalized_score() > b.normalized_score();
        });

    // Truncate to max_total_results.
    uint32_t max_results = request->max_total_results() > 0
        ? request->max_total_results()
        : 50;  // default

    if (all_chunks.size() > max_results) {
        all_chunks.resize(max_results);
    }

    // Populate response.
    for (auto& c : all_chunks) {
        *response->add_chunks() = std::move(c);
    }

    auto elapsed = std::chrono::steady_clock::now() - start;
    auto total_ms = std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();

    metrics->set_repos_searched(repos_searched);
    metrics->set_repos_failed(repos_failed);
    metrics->set_total_candidates(total_candidates);
    metrics->set_total_search_time_ms(static_cast<uint64_t>(total_ms));
    metrics->set_normalization_used(normalization);

    LOG_INFO("FederatedSearch: " + std::to_string(repos_searched) + " repos, "
             + std::to_string(all_chunks.size()) + " results in "
             + std::to_string(total_ms) + "ms");

    return grpc::Status::OK;
}

// ============================================================================
// FederatedSearchStream
// ============================================================================

grpc::Status CrossRepoServiceImpl::FederatedSearchStream(
    grpc::ServerContext* context,
    const aipr::engine::v1::FederatedSearchRequest* request,
    grpc::ServerWriter<aipr::engine::v1::FederatedContextChunk>* writer)
{
    if (context->IsCancelled()) {
        return grpc::Status(grpc::StatusCode::CANCELLED, "cancelled");
    }

    // For the streaming variant, we search repos sequentially and stream
    // results as they arrive. No cross-repo normalization is possible
    // until all repos are done, so we stream raw scores.

    for (const auto& repo_id : request->repo_ids()) {
        if (context->IsCancelled()) break;

        auto result = searchSingleRepo(repo_id, *request);
        if (!result.success) continue;

        for (auto& chunk : result.chunks) {
            chunk.set_normalized_score(chunk.raw_score());  // no normalization in stream mode
            if (!writer->Write(chunk)) {
                return grpc::Status(grpc::StatusCode::CANCELLED, "client disconnected");
            }
        }
    }

    return grpc::Status::OK;
}

// ============================================================================
// Internal helpers
// ============================================================================

void CrossRepoServiceImpl::toProto(
    const RepoManifest& manifest,
    const std::string& repo_id,
    aipr::engine::v1::RepoManifestProto* out)
{
    out->set_repo_id(repo_id);
    out->set_repo_root(manifest.repo_root);
    out->set_primary_language(manifest.primary_language);
    out->set_build_system(manifest.build_system);
    out->set_repo_type(static_cast<aipr::engine::v1::RepoType>(manifest.repo_type));

    for (const auto& target : manifest.targets) {
        auto* t = out->add_targets();
        t->set_name(target.name);
        t->set_type(target.type);
        for (const auto& glob : target.source_globs) {
            t->add_source_globs(glob);
        }
    }

    for (const auto& [module_name, files] : manifest.module_to_files) {
        auto* mf = &(*out->mutable_module_to_files())[module_name];
        for (const auto& f : files) {
            mf->add_files(f);
        }
    }
}

CrossRepoServiceImpl::PerRepoResult CrossRepoServiceImpl::searchSingleRepo(
    const std::string& repo_id,
    const aipr::engine::v1::FederatedSearchRequest& request) const
{
    PerRepoResult result;
    result.repo_id = repo_id;

    auto start = std::chrono::steady_clock::now();

    try {
        // Determine top_k from request config.
        size_t top_k = 20;
        if (request.has_config() && request.config().top_k() > 0) {
            top_k = static_cast<size_t>(request.config().top_k());
        }

        // Build the query string — combine the text query with touched symbols
        // for better retrieval coverage.
        std::string query_text = request.query();
        for (const auto& sym : request.touched_symbols()) {
            if (!query_text.empty()) query_text += " ";
            query_text += sym;
        }

        auto search_results = engine_->search(repo_id, query_text, top_k);

        for (const auto& chunk : search_results) {
            aipr::engine::v1::FederatedContextChunk fc;
            fc.set_repo_id(repo_id);
            // repo_name will be filled by the Go server (it has the DB).
            auto* c = fc.mutable_chunk();
            c->set_id(chunk.id);
            c->set_file_path(chunk.file_path);
            c->set_start_line(static_cast<uint32_t>(chunk.start_line));
            c->set_end_line(static_cast<uint32_t>(chunk.end_line));
            c->set_content(chunk.content);
            c->set_language(chunk.language);
            for (const auto& sym : chunk.symbols) {
                c->add_symbols(sym);
            }
            c->set_relevance_score(chunk.relevance_score);
            c->set_chunk_type(chunk.type);

            fc.set_raw_score(chunk.relevance_score);
            fc.set_normalized_score(chunk.relevance_score);  // pre-normalization

            result.chunks.push_back(std::move(fc));
        }
    } catch (const std::exception& e) {
        result.success = false;
        result.error = e.what();
    }

    auto elapsed = std::chrono::steady_clock::now() - start;
    result.search_time_ms = std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();

    return result;
}

void CrossRepoServiceImpl::normalizeScores(
    std::vector<aipr::engine::v1::FederatedContextChunk>& chunks,
    const std::string& strategy)
{
    if (chunks.empty() || strategy == "none") return;

    if (strategy == "z_score") {
        // Z-score normalization.
        double sum = 0.0;
        for (const auto& c : chunks) sum += c.raw_score();
        double mean = sum / static_cast<double>(chunks.size());

        double sq_sum = 0.0;
        for (const auto& c : chunks) {
            double diff = c.raw_score() - mean;
            sq_sum += diff * diff;
        }
        double stddev = std::sqrt(sq_sum / static_cast<double>(chunks.size()));

        if (stddev < 1e-9) {
            // All scores are the same — normalize to 0.5.
            for (auto& c : chunks) c.set_normalized_score(0.5f);
        } else {
            for (auto& c : chunks) {
                float z = static_cast<float>((c.raw_score() - mean) / stddev);
                // Clamp to [0, 1] using sigmoid-like mapping.
                c.set_normalized_score(1.0f / (1.0f + std::exp(-z)));
            }
        }
    } else {
        // Default: min-max normalization per-repo, then global.
        // Group by repo.
        std::unordered_map<std::string, std::pair<float, float>> repo_ranges;
        for (const auto& c : chunks) {
            auto& [mn, mx] = repo_ranges[c.repo_id()];
            if (repo_ranges[c.repo_id()].first == 0 && repo_ranges[c.repo_id()].second == 0) {
                mn = c.raw_score();
                mx = c.raw_score();
            } else {
                mn = std::min(mn, c.raw_score());
                mx = std::max(mx, c.raw_score());
            }
        }

        // Normalize within each repo to [0, 1].
        for (auto& c : chunks) {
            auto& [mn, mx] = repo_ranges[c.repo_id()];
            float range = mx - mn;
            if (range < 1e-9f) {
                c.set_normalized_score(0.5f);
            } else {
                c.set_normalized_score((c.raw_score() - mn) / range);
            }
        }
    }
}

} // namespace server
} // namespace aipr
