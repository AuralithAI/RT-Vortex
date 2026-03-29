/**
 * GraphRAG Retriever — Implementation
 *
 * Hybrid Graph + Semantic retrieval for the TMS forward path.
 * See graph_rag.h for architecture documentation.
 */

#include "tms/graph_rag.h"
#include "metrics.h"
#include "logging.h"

#include <algorithm>
#include <queue>
#include <cmath>
#include <chrono>

namespace aipr::tms {

// ─────────────────────────────────────────────────────────────────────────────
// Construction
// ─────────────────────────────────────────────────────────────────────────────

GraphRAGRetriever::GraphRAGRetriever(
    LTMFaiss& ltm,
    KnowledgeGraph& kg,
    const GraphRAGConfig& config)
    : ltm_(ltm)
    , kg_(kg)
    , config_(config)
{}

// ─────────────────────────────────────────────────────────────────────────────
// Edge type selection
// ─────────────────────────────────────────────────────────────────────────────

std::vector<std::string> GraphRAGRetriever::activeEdgeTypes() const {
    std::vector<std::string> types;
    if (config_.follow_imports)    types.push_back("IMPORTS");
    if (config_.follow_calls)      types.push_back("CALLS");
    if (config_.follow_references) types.push_back("REFERENCES");
    if (config_.follow_contains)   types.push_back("CONTAINS");
    return types;
}

// ─────────────────────────────────────────────────────────────────────────────
// KG node lookup by FAISS ID
// ─────────────────────────────────────────────────────────────────────────────

std::vector<KGNode> GraphRAGRetriever::nodesByFaissId(
    int64_t faiss_id,
    const std::string& repo_id)
{
    // KnowledgeGraph stores faiss_id on nodes; we search by it.
    // The KG API doesn't have a direct byFaissId query, so we use
    // the node's linked faiss_id field.  In practice this is a small
    // SQLite query: SELECT * FROM kg_nodes WHERE faiss_id = ? AND repo_id = ?
    return kg_.nodesByFaissId(faiss_id, repo_id);
}

// ─────────────────────────────────────────────────────────────────────────────
// Single-seed expansion via BFS
// ─────────────────────────────────────────────────────────────────────────────

GraphExpansion GraphRAGRetriever::expandSeed(
    int64_t faiss_id,
    const std::string& seed_chunk_id,
    const std::string& repo_id,
    std::unordered_set<int64_t>& visited_faiss_ids)
{
    GraphExpansion expansion;
    expansion.seed_chunk_id = seed_chunk_id;
    expansion.seed_faiss_id = static_cast<int>(faiss_id);

    auto edge_types = activeEdgeTypes();
    if (edge_types.empty()) return expansion;

    // Resolve seed FAISS ID → KG node(s)
    auto seed_nodes = nodesByFaissId(faiss_id, repo_id);
    if (seed_nodes.empty()) return expansion;

    // BFS from seed nodes, up to max_hops
    // Queue entries: (node_id, hop_distance, edge_path)
    struct BFSEntry {
        std::string node_id;
        int hop;
        std::string path;
    };

    std::queue<BFSEntry> bfs_queue;
    std::unordered_set<std::string> visited_node_ids;

    for (const auto& seed : seed_nodes) {
        visited_node_ids.insert(seed.id);
        bfs_queue.push({seed.id, 0, ""});
    }

    int collected = 0;

    while (!bfs_queue.empty() && collected < config_.max_neighbors_per_seed) {
        auto current = bfs_queue.front();
        bfs_queue.pop();

        if (current.hop >= config_.max_hops) continue;

        // Get neighbors for each active edge type
        for (const auto& edge_type : edge_types) {
            auto edges = kg_.neighbors(current.node_id, edge_type);

            for (const auto& edge : edges) {
                // The destination node ID is on the edge
                std::string neighbor_id = edge.dst_id;
                if (neighbor_id == current.node_id) {
                    // For bidirectional edges, take the other end
                    neighbor_id = edge.src_id;
                }

                if (visited_node_ids.count(neighbor_id)) continue;
                visited_node_ids.insert(neighbor_id);

                // Resolve the neighbor node to get its faiss_id
                auto neighbor_node = kg_.getNode(neighbor_id);
                if (!neighbor_node) continue;

                int hop_dist = current.hop + 1;
                std::string path = current.path.empty()
                    ? edge_type
                    : current.path + "->" + edge_type;

                // Skip if this neighbor's FAISS ID is already in our global visited set
                if (neighbor_node->faiss_id >= 0 && visited_faiss_ids.count(neighbor_node->faiss_id)) continue;

                // Record this neighbor as an expanded result
                if (neighbor_node->faiss_id >= 0) {
                    visited_faiss_ids.insert(neighbor_node->faiss_id);
                    expansion.expanded_nodes.push_back(*neighbor_node);
                    expansion.hop_distances.push_back(hop_dist);
                    expansion.edge_paths.push_back(path);
                    ++collected;
                }

                // Continue BFS if we haven't reached max depth
                if (hop_dist < config_.max_hops) {
                    bfs_queue.push({neighbor_id, hop_dist, path});
                }

                if (collected >= config_.max_neighbors_per_seed) break;
            }

            if (collected >= config_.max_neighbors_per_seed) break;
        }
    }

    return expansion;
}

// ─────────────────────────────────────────────────────────────────────────────
// Expand and merge (main entry point)
// ─────────────────────────────────────────────────────────────────────────────

std::vector<RetrievedChunk> GraphRAGRetriever::expandAndMerge(
    const std::vector<RetrievedChunk>& semantic_results,
    const std::string& repo_id,
    int top_k)
{
    auto start = std::chrono::steady_clock::now();

    if (semantic_results.empty()) return {};

    // Track all FAISS IDs we've already seen (from semantic results + expansions)
    std::unordered_set<int64_t> visited_faiss_ids;
    for (const auto& rc : semantic_results) {
        // Map chunk_id to faiss_id via LTM's internal mapping
        // We'll rely on the chunk.id being present; the LTM stores chunk_id→faiss_id
        // For now, we'll just track chunk IDs and use LTM to resolve
    }

    // ── Graph expansion ────────────────────────────────────────
    std::vector<GraphExpansion> expansions;
    int total_expanded = 0;

    // Build a set of semantic result chunk IDs for deduplication
    std::unordered_set<std::string> semantic_chunk_ids;
    for (const auto& rc : semantic_results) {
        semantic_chunk_ids.insert(rc.chunk.id);
    }

    for (const auto& rc : semantic_results) {
        if (total_expanded >= config_.max_expanded_chunks) break;

        // Resolve this chunk's FAISS ID via LTM (the chunk knows its own id)
        // We need to find the KG nodes associated with this chunk.
        // The chunk.id format is typically "repo_id:file_path:symbol_name:..."
        // The KG links nodes to faiss_ids during ingestion via linkFaissId().

        // Try to find KG nodes that match this chunk's file_path and name
        auto kg_nodes = kg_.nodesByFilePath(rc.chunk.file_path, repo_id);
        
        for (const auto& node : kg_nodes) {
            if (node.faiss_id < 0) continue;
            if (total_expanded >= config_.max_expanded_chunks) break;

            auto expansion = expandSeed(
                node.faiss_id, rc.chunk.id, repo_id, visited_faiss_ids);
            total_expanded += static_cast<int>(expansion.expanded_nodes.size());
            expansions.push_back(std::move(expansion));
        }
    }

    // ── Retrieve expanded chunks from LTM ──────────────────────
    // Build a map of graph-expanded chunk scores: chunk_id → (score, hop_distance)
    struct GraphScore {
        float score;
        int hop_distance;
        std::string edge_path;
    };
    std::unordered_map<std::string, GraphScore> graph_scores;

    for (const auto& expansion : expansions) {
        for (size_t i = 0; i < expansion.expanded_nodes.size(); ++i) {
            const auto& node = expansion.expanded_nodes[i];
            int hop = expansion.hop_distances[i];

            // Find the corresponding chunk in LTM
            auto chunk_opt = ltm_.get(node.id);
            if (!chunk_opt) {
                // Try with the repo-prefixed ID format
                chunk_opt = ltm_.get(repo_id + ":" + node.id);
            }

            std::string chunk_id = chunk_opt ? chunk_opt->id : node.id;

            // Skip if this is already in the semantic results
            if (semantic_chunk_ids.count(chunk_id)) continue;

            // Score = base relevance of the seed * decay^hop
            float base_score = 0.0f;
            for (const auto& rc : semantic_results) {
                if (rc.chunk.id == expansion.seed_chunk_id) {
                    base_score = rc.similarity_score;
                    break;
                }
            }
            float graph_score = base_score * std::pow(config_.hop_decay, hop);

            // Keep highest score if seen multiple times
            auto it = graph_scores.find(chunk_id);
            if (it == graph_scores.end() || it->second.score < graph_score) {
                graph_scores[chunk_id] = {graph_score, hop, expansion.edge_paths[i]};
            }
        }
    }

    // ── RRF merge ──────────────────────────────────────────────
    // Assign RRF scores: semantic results get rank-based scores with (1-graph_weight),
    // graph results get rank-based scores with graph_weight.
    
    std::unordered_map<std::string, float> rrf_scores;
    std::unordered_map<std::string, RetrievedChunk> chunk_map;

    // Semantic results (already ranked by combined_score)
    float semantic_weight = 1.0f - config_.graph_weight;
    for (size_t rank = 0; rank < semantic_results.size(); ++rank) {
        const auto& rc = semantic_results[rank];
        float score = semantic_weight / (config_.rrf_k + rank + 1);
        rrf_scores[rc.chunk.id] += score;
        chunk_map[rc.chunk.id] = rc;
    }

    // Graph-expanded results (ranked by graph_score descending)
    std::vector<std::pair<std::string, GraphScore>> sorted_graph(
        graph_scores.begin(), graph_scores.end());
    std::sort(sorted_graph.begin(), sorted_graph.end(),
              [](const auto& a, const auto& b) { return a.second.score > b.second.score; });

    for (size_t rank = 0; rank < sorted_graph.size(); ++rank) {
        const auto& [chunk_id, gs] = sorted_graph[rank];
        float score = config_.graph_weight / (config_.rrf_k + rank + 1);
        rrf_scores[chunk_id] += score;

        // If not already in chunk_map, retrieve from LTM
        if (chunk_map.find(chunk_id) == chunk_map.end()) {
            auto chunk_opt = ltm_.get(chunk_id);
            if (chunk_opt) {
                RetrievedChunk rc;
                rc.chunk = *chunk_opt;
                rc.similarity_score = gs.score;
                rc.memory_source = "LTM_GRAPH";
                chunk_map[chunk_id] = std::move(rc);
            }
        }
    }

    // ── Sort by RRF score and take top_k ───────────────────────
    std::vector<std::pair<std::string, float>> final_ranked(
        rrf_scores.begin(), rrf_scores.end());
    std::sort(final_ranked.begin(), final_ranked.end(),
              [](const auto& a, const auto& b) { return a.second > b.second; });

    std::vector<RetrievedChunk> results;
    results.reserve(std::min(static_cast<size_t>(top_k), final_ranked.size()));

    for (size_t i = 0; i < final_ranked.size() && static_cast<int>(results.size()) < top_k; ++i) {
        auto it = chunk_map.find(final_ranked[i].first);
        if (it != chunk_map.end()) {
            auto rc = it->second;
            rc.combined_score = final_ranked[i].second;
            results.push_back(std::move(rc));
        }
    }

    // ── Telemetry ───────────────────────────────────────────────────────
    auto elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::steady_clock::now() - start).count();

    metrics::Registry::instance().observeHistogram(
        metrics::GRAPH_TRAVERSAL_LATENCY_S,
        static_cast<double>(elapsed_ms) / 1000.0);
    metrics::Registry::instance().setGauge(
        metrics::GRAPH_EXPANDED_CHUNKS,
        static_cast<double>(total_expanded));

    // Average hop depth
    double total_hops = 0.0;
    int hop_count = 0;
    for (const auto& expansion : expansions) {
        for (int h : expansion.hop_distances) {
            total_hops += h;
            ++hop_count;
        }
    }
    if (hop_count > 0) {
        metrics::Registry::instance().setGauge(
            metrics::GRAPH_HOP_DEPTH_AVG, total_hops / hop_count);
    }

    LOG_INFO("[GraphRAG] expand_and_merge: semantic=" +
             std::to_string(semantic_results.size()) +
             " graph_expanded=" + std::to_string(total_expanded) +
             " final=" + std::to_string(results.size()) +
             " ms=" + std::to_string(elapsed_ms));

    return results;
}

// ─────────────────────────────────────────────────────────────────────────────
// Graph confidence score (for Confidence Gate v2)
// ─────────────────────────────────────────────────────────────────────────────

float GraphRAGRetriever::computeGraphConfidence(
    const std::vector<std::string>& seed_chunk_ids,
    const std::vector<std::string>& result_chunk_ids,
    const std::string& repo_id)
{
    if (seed_chunk_ids.empty() || result_chunk_ids.empty()) return 0.0f;

    // For each result chunk, find the shortest KG path to any seed chunk.
    // Score = mean(1 / (1 + shortest_path_length)) across all result chunks.
    // A score of 1.0 means all results are direct neighbors (1-hop).

    // Build a set of seed node IDs from the KG
    std::unordered_set<std::string> seed_node_ids;
    for (const auto& chunk_id : seed_chunk_ids) {
        // Look up KG nodes for this chunk
        // We parse the chunk_id to extract file_path
        // chunk_id format: "repo_id:path/to/file:symbol"
        size_t first_colon = chunk_id.find(':');
        if (first_colon == std::string::npos) continue;
        size_t second_colon = chunk_id.find(':', first_colon + 1);
        std::string file_path = (second_colon != std::string::npos)
            ? chunk_id.substr(first_colon + 1, second_colon - first_colon - 1)
            : chunk_id.substr(first_colon + 1);

        auto nodes = kg_.nodesByFilePath(file_path, repo_id);
        for (const auto& n : nodes) {
            seed_node_ids.insert(n.id);
        }
    }

    if (seed_node_ids.empty()) return 0.0f;

    float total_score = 0.0f;
    int scored = 0;

    for (const auto& result_id : result_chunk_ids) {
        size_t first_colon = result_id.find(':');
        if (first_colon == std::string::npos) continue;
        size_t second_colon = result_id.find(':', first_colon + 1);
        std::string file_path = (second_colon != std::string::npos)
            ? result_id.substr(first_colon + 1, second_colon - first_colon - 1)
            : result_id.substr(first_colon + 1);

        auto result_nodes = kg_.nodesByFilePath(file_path, repo_id);

        float best_path_score = 0.0f;

        for (const auto& rn : result_nodes) {
            // Check 1-hop: direct neighbor of any seed?
            auto edges_1 = kg_.neighbors(rn.id, "");  // all edge types
            for (const auto& e1 : edges_1) {
                std::string neighbor_id = (e1.dst_id == rn.id) ? e1.src_id : e1.dst_id;
                if (seed_node_ids.count(neighbor_id)) {
                    best_path_score = std::max(best_path_score, 1.0f / (1.0f + 1.0f)); // hop=1
                }
            }

            // If already found 1-hop, no need to check 2-hop
            if (best_path_score >= 0.5f) break;

            // Check 2-hop via neighbors2 which returns edges
            auto edges_2 = kg_.neighbors2(rn.id, "");
            for (const auto& e2 : edges_2) {
                std::string neighbor_id = (e2.dst_id == rn.id) ? e2.src_id : e2.dst_id;
                if (seed_node_ids.count(neighbor_id)) {
                    best_path_score = std::max(best_path_score, 1.0f / (1.0f + 2.0f)); // hop=2
                }
            }
        }

        total_score += best_path_score;
        ++scored;
    }

    return (scored > 0) ? total_score / scored : 0.0f;
}

} // namespace aipr::tms
