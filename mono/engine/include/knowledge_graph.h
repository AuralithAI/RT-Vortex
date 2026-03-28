/**
 * Knowledge Graph — Persistent Architectural Understanding
 *
 * SQLite WAL-backed graph of code entities and their relationships.
 * Stored per-repo at <storage_path>/graph/knowledge_graph.db.
 *
 * Nodes represent CodeChunks (functions, classes, files, modules).
 * Edges capture structural relationships: CONTAINS, CALLS, IMPORTS,
 * DEFINED_IN, REFERENCES.
 *
 * Edge inference strategy (callees/callers may be empty from chunker):
 *   1. CONTAINS  — parent_chunk_id → parent relationship
 *   2. DEFINED_IN — symbol defined in a chunk → file node
 *   3. IMPORTS   — dependencies list (populated for some languages)
 *   4. REFERENCES — symbol-name matching across chunks (heuristic)
 *
 * Gated: config.knowledge_graph_enabled = false (default OFF).
 */

#pragma once

#include <memory>
#include <string>
#include <vector>
#include <unordered_map>
#include <unordered_set>
#include "tms/tms_types.h"

// Forward-declare sqlite3 to avoid leaking the amalgamation header
struct sqlite3;
struct sqlite3_stmt;

namespace aipr {

// ── Node / Edge PODs ───────────────────────────────────────────────────────

struct KGNode {
    std::string id;                 // chunk.id  (repo_id:file:name)
    std::string node_type;          // "function", "class", "file_summary", …
    std::string name;
    std::string file_path;
    std::string language;
    int64_t     faiss_id = -1;      // link to FAISS vector index
    std::string repo_id;
    std::string metadata;           // free-form JSON blob
};

struct KGEdge {
    int64_t     id = 0;             // auto-increment
    std::string src_id;
    std::string dst_id;
    std::string edge_type;          // "CALLS", "CONTAINS", "IMPORTS", …
    double      weight = 1.0;
    std::string repo_id;
};

// ── KnowledgeGraph ─────────────────────────────────────────────────────────

class KnowledgeGraph {
public:
    /**
     * @param db_path  Full path to the SQLite file.
     *                 Parent directories must exist.
     */
    explicit KnowledgeGraph(const std::string& db_path);
    ~KnowledgeGraph();

    // Non-copyable
    KnowledgeGraph(const KnowledgeGraph&) = delete;
    KnowledgeGraph& operator=(const KnowledgeGraph&) = delete;

    // ── Lifecycle ───────────────────────────────────────────────────────

    /** Open (or create) the database and run migrations. */
    void open();

    /** Flush WAL and close the database. */
    void close();

    bool isOpen() const;

    // ── Bulk operations ─────────────────────────────────────────────────

    /**
     * Build the graph from a set of chunks.
     * Replaces any existing data for @p repo_id.
     *
     * Steps:
     *  1. DELETE existing nodes/edges for repo_id
     *  2. INSERT one node per chunk
     *  3. Infer edges (CONTAINS, IMPORTS, REFERENCES, DEFINED_IN)
     */
    void buildFromChunks(const std::string& repo_id,
                         const std::vector<tms::CodeChunk>& chunks);

    /**
     * Append a batch of chunks to the graph without deleting existing data.
     * Used during streaming batched ingestion.
     *
     * Steps:
     *  1. INSERT OR REPLACE nodes for this batch
     *  2. Infer intra-batch CONTAINS edges only (cheap, structural)
     *
     * Call finalizeEdges() after all batches are appended to infer
     * cross-batch IMPORTS and REFERENCES edges.
     */
    void appendBatchChunks(const std::string& repo_id,
                           const std::vector<tms::CodeChunk>& chunks);

    /**
     * Infer cross-batch IMPORTS and REFERENCES edges using SQL.
     * Called once after all batches have been appended via appendBatchChunks().
     * Operates entirely on already-persisted node data — no in-memory chunks needed.
     */
    void finalizeEdges(const std::string& repo_id);

    /**
     * Associate a FAISS vector id with a KG node.
     * Called after addBatch() to link KG ↔ LTM.
     */
    void linkFaissId(const std::string& node_id, int64_t faiss_id);

    /**
     * Remove all data for a repository.
     */
    void removeRepo(const std::string& repo_id);

    // ── Queries ─────────────────────────────────────────────────────────

    /** 1-hop neighbors of @p node_id, optionally filtered by edge_type. */
    std::vector<KGEdge> neighbors(const std::string& node_id,
                                  const std::string& edge_type = "") const;

    /** 2-hop neighborhood (node_id → X → Y). */
    std::vector<KGEdge> neighbors2(const std::string& node_id,
                                   const std::string& edge_type = "") const;

    /** Get a node by id. Returns empty optional if not found. */
    std::optional<KGNode> getNode(const std::string& node_id) const;

    /** All nodes for a repo. */
    std::vector<KGNode> getNodes(const std::string& repo_id) const;

    // ── Statistics ──────────────────────────────────────────────────────

    size_t nodeCount(const std::string& repo_id = "") const;
    size_t edgeCount(const std::string& repo_id = "") const;

private:
    std::string db_path_;
    sqlite3*    db_ = nullptr;

    // Prepared statements (lazy-init)
    mutable sqlite3_stmt* stmt_insert_node_    = nullptr;
    mutable sqlite3_stmt* stmt_insert_edge_    = nullptr;
    mutable sqlite3_stmt* stmt_neighbors_      = nullptr;
    mutable sqlite3_stmt* stmt_neighbors_type_ = nullptr;

    void ensureSchema();
    void exec(const std::string& sql);
    void prepareStatements();
    void finalizeStatements();

    // Edge inference helpers (in-memory, per-batch)
    void inferContainsEdges(const std::string& repo_id,
                            const std::vector<tms::CodeChunk>& chunks);
    void inferImportsEdges(const std::string& repo_id,
                           const std::vector<tms::CodeChunk>& chunks);
    void inferReferenceEdges(const std::string& repo_id,
                             const std::vector<tms::CodeChunk>& chunks);

    // Cross-batch edge inference (SQL-based, no in-memory chunks)
    void inferImportsEdgesSQL(const std::string& repo_id);
    void inferReferenceEdgesSQL(const std::string& repo_id);
};

} // namespace aipr
