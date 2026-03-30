/**
 * GraphRAG Retriever — Hybrid Graph + Semantic Retrieval
 *
 * Augments LTM vector search results by traversing IMPORTS/CALLS edges
 * in the KnowledgeGraph up to N hops, then RRF-merges graph-expanded
 * chunks with the original semantic results.
 *
 * Architecture:
 *   1. LTM produces initial candidate set via FAISS hybrid search.
 *   2. For each candidate, look up its KG node(s) by faiss_id.
 *   3. Traverse IMPORTS and CALLS edges up to max_hops (default 2).
 *   4. Collect neighbor faiss_ids → retrieve their CodeChunks from LTM.
 *   5. Score graph-expanded results: base_score * decay^hop_distance.
 *   6. RRF-merge semantic + graph results.
 *
 * The traversal is bounded:
 *   - Max expanded neighbors per seed: 8
 *   - Max total graph-expanded chunks: 32
 *   - Timeout: inherits from forward() query budget
 */

#pragma once

#include "tms_types.h"
#include "ltm_faiss.h"
#include "knowledge_graph.h"
#include <vector>
#include <string>
#include <unordered_set>
#include <unordered_map>

namespace aipr::tms {

/**
 * GraphRAG configuration
 */
struct GraphRAGConfig {
    int max_hops = 2;                          // Maximum traversal depth
    int max_neighbors_per_seed = 8;            // Cap neighbors per seed node
    int max_expanded_chunks = 32;              // Total cap on graph-expanded chunks
    float hop_decay = 0.7f;                    // Score decay per hop distance
    float graph_weight = 0.3f;                 // Weight of graph results in RRF merge (vs 1-graph_weight for semantic)
    float rrf_k = 60.0f;                       // RRF constant
    bool follow_imports = true;                // Traverse IMPORTS edges
    bool follow_calls = true;                  // Traverse CALLS edges
    bool follow_references = false;            // Traverse REFERENCES edges (expensive)
    bool follow_contains = false;              // Traverse CONTAINS edges
};

/**
 * Graph expansion result for a single seed chunk
 */
struct GraphExpansion {
    std::string seed_chunk_id;                 // Original LTM result that seeded this expansion
    int seed_faiss_id = -1;                    // FAISS ID of seed
    std::vector<KGNode> expanded_nodes;        // KG nodes found by traversal
    std::vector<int> hop_distances;            // Hop distance for each expanded node
    std::vector<std::string> edge_paths;       // Edge types followed to reach each node
};

/**
 * GraphRAG retriever — wires KG graph traversal into the retrieval path.
 */
class GraphRAGRetriever {
public:
    GraphRAGRetriever(LTMFaiss& ltm, KnowledgeGraph& kg, const GraphRAGConfig& config = {});

    /**
     * Expand LTM search results via KG graph traversal and RRF-merge.
     *
     * @param semantic_results   Initial results from LTM hybrid search
     * @param repo_id            Repository filter for KG traversal
     * @param top_k              Final number of results to return
     * @return Merged results with graph-expanded chunks interleaved
     */
    std::vector<RetrievedChunk> expandAndMerge(
        const std::vector<RetrievedChunk>& semantic_results,
        const std::string& repo_id,
        int top_k
    );

    /**
     * Compute a KG path-length score between two sets of chunks.
     *
     * Used by Confidence Gate v2: shorter paths between query-relevant
     * chunks and retrieved chunks indicate higher structural confidence.
     *
     * @param seed_chunk_ids   Chunk IDs from the query context
     * @param result_chunk_ids Chunk IDs from retrieval results
     * @param repo_id          Repository filter
     * @return Score in [0, 1] where 1 = all results are 1-hop neighbors
     */
    float computeGraphConfidence(
        const std::vector<std::string>& seed_chunk_ids,
        const std::vector<std::string>& result_chunk_ids,
        const std::string& repo_id
    );

    const GraphRAGConfig& config() const { return config_; }

private:
    LTMFaiss& ltm_;
    KnowledgeGraph& kg_;
    GraphRAGConfig config_;

    /**
     * Expand a single seed node through the KG.
     */
    GraphExpansion expandSeed(
        int64_t faiss_id,
        const std::string& seed_chunk_id,
        const std::string& repo_id,
        std::unordered_set<int64_t>& visited_faiss_ids
    );

    /**
     * Build the list of edge types to follow based on config.
     */
    std::vector<std::string> activeEdgeTypes() const;

    /**
     * Look up the KG node for a FAISS ID within a repo.
     */
    std::vector<KGNode> nodesByFaissId(int64_t faiss_id, const std::string& repo_id);
};

} // namespace aipr::tms
