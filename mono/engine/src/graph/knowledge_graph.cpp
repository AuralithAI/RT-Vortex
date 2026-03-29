/**
 * KnowledgeGraph — SQLite WAL implementation
 *
 * Persistent graph of code entities and relationships.
 * Uses the SQLite amalgamation compiled into the engine binary.
 */

#include "knowledge_graph.h"
#include "logging.h"
#include "metrics.h"

#include <sqlite3.h>

#include <algorithm>
#include <filesystem>
#include <sstream>
#include <stdexcept>
#include <unordered_map>
#include <unordered_set>
#include <nlohmann/json.hpp>

namespace aipr {

// ── Helpers ────────────────────────────────────────────────────────────────

static void throwOnSqlError(int rc, sqlite3* db, const std::string& context) {
    if (rc != SQLITE_OK && rc != SQLITE_DONE && rc != SQLITE_ROW) {
        std::string msg = context + ": " + (db ? sqlite3_errmsg(db) : "null db");
        throw std::runtime_error(msg);
    }
}

// ── Construction / Destruction ─────────────────────────────────────────────

KnowledgeGraph::KnowledgeGraph(const std::string& db_path)
    : db_path_(db_path) {}

KnowledgeGraph::~KnowledgeGraph() {
    try { close(); } catch (...) {}
}

// ── Lifecycle ──────────────────────────────────────────────────────────────

void KnowledgeGraph::open() {
    if (db_) return;  // already open

    // Ensure parent directory exists
    auto parent = std::filesystem::path(db_path_).parent_path();
    if (!parent.empty()) {
        std::filesystem::create_directories(parent);
    }

    int rc = sqlite3_open(db_path_.c_str(), &db_);
    throwOnSqlError(rc, db_, "sqlite3_open(" + db_path_ + ")");

    // WAL mode for concurrent readers
    exec("PRAGMA journal_mode=WAL");
    exec("PRAGMA synchronous=NORMAL");
    exec("PRAGMA foreign_keys=ON");

    ensureSchema();
    prepareStatements();

    LOG_INFO("[KG] opened " + db_path_);
}

void KnowledgeGraph::close() {
    if (!db_) return;

    finalizeStatements();

    // Checkpoint WAL before closing
    sqlite3_wal_checkpoint_v2(db_, nullptr, SQLITE_CHECKPOINT_TRUNCATE,
                              nullptr, nullptr);

    sqlite3_close(db_);
    db_ = nullptr;
    LOG_INFO("[KG] closed " + db_path_);
}

bool KnowledgeGraph::isOpen() const { return db_ != nullptr; }

// ── Schema ─────────────────────────────────────────────────────────────────

void KnowledgeGraph::ensureSchema() {
    const char* ddl = R"SQL(
        CREATE TABLE IF NOT EXISTS kg_nodes (
            id        TEXT PRIMARY KEY,
            node_type TEXT NOT NULL,
            name      TEXT NOT NULL,
            file_path TEXT,
            language  TEXT,
            faiss_id  INTEGER DEFAULT -1,
            repo_id   TEXT NOT NULL,
            metadata  TEXT
        );
        CREATE INDEX IF NOT EXISTS idx_nodes_repo ON kg_nodes(repo_id);
        CREATE INDEX IF NOT EXISTS idx_nodes_type ON kg_nodes(node_type);
        CREATE INDEX IF NOT EXISTS idx_nodes_file ON kg_nodes(file_path);
        CREATE INDEX IF NOT EXISTS idx_nodes_name ON kg_nodes(name);

        CREATE TABLE IF NOT EXISTS kg_edges (
            id        INTEGER PRIMARY KEY AUTOINCREMENT,
            src_id    TEXT NOT NULL,
            dst_id    TEXT NOT NULL,
            edge_type TEXT NOT NULL,
            weight    REAL DEFAULT 1.0,
            repo_id   TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_edges_src  ON kg_edges(src_id);
        CREATE INDEX IF NOT EXISTS idx_edges_dst  ON kg_edges(dst_id);
        CREATE INDEX IF NOT EXISTS idx_edges_repo ON kg_edges(repo_id);
        CREATE INDEX IF NOT EXISTS idx_edges_type ON kg_edges(edge_type);
    )SQL";
    exec(ddl);
}

void KnowledgeGraph::exec(const std::string& sql) {
    char* err = nullptr;
    int rc = sqlite3_exec(db_, sql.c_str(), nullptr, nullptr, &err);
    if (rc != SQLITE_OK) {
        std::string msg = "exec: " + std::string(err ? err : "unknown error");
        sqlite3_free(err);
        throw std::runtime_error(msg);
    }
}

// ── Prepared statements ────────────────────────────────────────────────────

void KnowledgeGraph::prepareStatements() {
    auto prep = [&](const char* sql, sqlite3_stmt*& stmt) {
        int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
        throwOnSqlError(rc, db_, std::string("prepare: ") + sql);
    };

    prep("INSERT OR REPLACE INTO kg_nodes "
         "(id, node_type, name, file_path, language, faiss_id, repo_id, metadata) "
         "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
         stmt_insert_node_);

    prep("INSERT INTO kg_edges (src_id, dst_id, edge_type, weight, repo_id) "
         "VALUES (?, ?, ?, ?, ?)",
         stmt_insert_edge_);

    prep("SELECT id, src_id, dst_id, edge_type, weight, repo_id "
         "FROM kg_edges WHERE src_id = ? OR dst_id = ?",
         stmt_neighbors_);

    prep("SELECT id, src_id, dst_id, edge_type, weight, repo_id "
         "FROM kg_edges WHERE (src_id = ? OR dst_id = ?) AND edge_type = ?",
         stmt_neighbors_type_);
}

void KnowledgeGraph::finalizeStatements() {
    auto fin = [](sqlite3_stmt*& s) {
        if (s) { sqlite3_finalize(s); s = nullptr; }
    };
    fin(stmt_insert_node_);
    fin(stmt_insert_edge_);
    fin(stmt_neighbors_);
    fin(stmt_neighbors_type_);
}

// ── Bulk build ─────────────────────────────────────────────────────────────

void KnowledgeGraph::buildFromChunks(
    const std::string& repo_id,
    const std::vector<tms::CodeChunk>& chunks)
{
    if (!db_) throw std::runtime_error("KG not open");
    if (chunks.empty()) return;

    // Transaction for atomicity + speed
    exec("BEGIN TRANSACTION");

    try {
        // 1. Remove old data for this repo
        removeRepo(repo_id);

        // 2. Insert nodes
        for (const auto& chunk : chunks) {
            sqlite3_reset(stmt_insert_node_);
            sqlite3_bind_text(stmt_insert_node_, 1, chunk.id.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 2, chunk.type.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 3, chunk.name.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 4, chunk.file_path.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 5, chunk.language.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_int64(stmt_insert_node_, 6, -1);
            sqlite3_bind_text(stmt_insert_node_, 7, repo_id.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_null(stmt_insert_node_, 8);

            int rc = sqlite3_step(stmt_insert_node_);
            if (rc != SQLITE_DONE) {
                throwOnSqlError(rc, db_, "insert node " + chunk.id);
            }
        }

        // 3. Infer edges
        inferContainsEdges(repo_id, chunks);
        inferImportsEdges(repo_id, chunks);
        inferReferenceEdges(repo_id, chunks);

        exec("COMMIT");

    } catch (...) {
        exec("ROLLBACK");
        throw;
    }

    // Update metrics
    metrics::Registry::instance().setGauge(
        metrics::KG_NODES_TOTAL, static_cast<double>(nodeCount(repo_id)));
    metrics::Registry::instance().setGauge(
        metrics::KG_EDGES_TOTAL, static_cast<double>(edgeCount(repo_id)));

    LOG_INFO("[KG] built graph for repo=" + repo_id +
             " nodes=" + std::to_string(nodeCount(repo_id)) +
             " edges=" + std::to_string(edgeCount(repo_id)));
}

// ── Edge inference ─────────────────────────────────────────────────────────

// ── Batch-append (streaming ingestion) ─────────────────────────────────────

void KnowledgeGraph::appendBatchChunks(
    const std::string& repo_id,
    const std::vector<tms::CodeChunk>& chunks)
{
    if (!db_) throw std::runtime_error("KG not open");
    if (chunks.empty()) return;

    exec("BEGIN TRANSACTION");

    try {
        // Insert nodes (INSERT OR REPLACE handles duplicates safely)
        // Persist dependencies + symbols in the metadata column as JSON
        // so cross-batch edge inference can use them later via SQL.
        for (const auto& chunk : chunks) {
            sqlite3_reset(stmt_insert_node_);
            sqlite3_bind_text(stmt_insert_node_, 1, chunk.id.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 2, chunk.type.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 3, chunk.name.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 4, chunk.file_path.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_insert_node_, 5, chunk.language.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_int64(stmt_insert_node_, 6, -1);
            sqlite3_bind_text(stmt_insert_node_, 7, repo_id.c_str(), -1, SQLITE_TRANSIENT);

            // Build metadata JSON with dependencies and symbols
            if (!chunk.dependencies.empty() || !chunk.symbols.empty()) {
                nlohmann::json meta;
                if (!chunk.dependencies.empty())
                    meta["deps"] = chunk.dependencies;
                if (!chunk.symbols.empty())
                    meta["syms"] = chunk.symbols;
                std::string meta_str = meta.dump();
                sqlite3_bind_text(stmt_insert_node_, 8, meta_str.c_str(), -1, SQLITE_TRANSIENT);
            } else {
                sqlite3_bind_null(stmt_insert_node_, 8);
            }

            int rc = sqlite3_step(stmt_insert_node_);
            if (rc != SQLITE_DONE) {
                throwOnSqlError(rc, db_, "insert node " + chunk.id);
            }
        }

        // Only infer CONTAINS edges per-batch (cheap, intra-file structural)
        inferContainsEdges(repo_id, chunks);

        exec("COMMIT");

    } catch (...) {
        exec("ROLLBACK");
        throw;
    }

    LOG_INFO("[KG] appended batch for repo=" + repo_id +
             " nodes_in_batch=" + std::to_string(chunks.size()) +
             " total_nodes=" + std::to_string(nodeCount(repo_id)));
}

// ── Finalize cross-batch edges ─────────────────────────────────────────────

void KnowledgeGraph::finalizeEdges(const std::string& repo_id) {
    if (!db_) throw std::runtime_error("KG not open");

    LOG_INFO("[KG] finalizing cross-batch edges for repo=" + repo_id +
             " (nodes=" + std::to_string(nodeCount(repo_id)) + ")");

    inferImportsEdgesSQL(repo_id);
    inferReferenceEdgesSQL(repo_id);

    // Update metrics
    metrics::Registry::instance().setGauge(
        metrics::KG_NODES_TOTAL, static_cast<double>(nodeCount(repo_id)));
    metrics::Registry::instance().setGauge(
        metrics::KG_EDGES_TOTAL, static_cast<double>(edgeCount(repo_id)));

    LOG_INFO("[KG] finalized graph for repo=" + repo_id +
             " nodes=" + std::to_string(nodeCount(repo_id)) +
             " edges=" + std::to_string(edgeCount(repo_id)));
}

// ── SQL-based cross-batch edge inference ───────────────────────────────────

void KnowledgeGraph::inferImportsEdgesSQL(const std::string& repo_id) {
    // Cross-batch IMPORTS inference using dependencies stored in node metadata.
    // Each node's metadata JSON may contain a "deps" array of import paths.
    // We match each dependency against file_path suffixes of file_summary nodes
    // (or any node) in the same repo to create IMPORTS edges.

    LOG_INFO("[KG] inferring cross-batch IMPORTS edges via SQL...");

    // Step 1: Load all nodes that have dependencies in their metadata
    const char* sql_with_deps =
        "SELECT id, file_path, metadata FROM kg_nodes "
        "WHERE repo_id = ? AND metadata IS NOT NULL AND metadata LIKE '%deps%'";

    sqlite3_stmt* stmt_deps = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql_with_deps, -1, &stmt_deps, nullptr);
    if (rc != SQLITE_OK) {
        LOG_ERROR("[KG] failed to prepare deps query: " + std::string(sqlite3_errmsg(db_)));
        return;
    }
    sqlite3_bind_text(stmt_deps, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);

    struct NodeWithDeps {
        std::string id;
        std::string file_path;
        std::vector<std::string> deps;
    };
    std::vector<NodeWithDeps> nodes_with_deps;
    while (sqlite3_step(stmt_deps) == SQLITE_ROW) {
        auto id_ptr   = sqlite3_column_text(stmt_deps, 0);
        auto fp_ptr   = sqlite3_column_text(stmt_deps, 1);
        auto meta_ptr = sqlite3_column_text(stmt_deps, 2);
        if (!id_ptr || !meta_ptr) continue;

        try {
            auto meta = nlohmann::json::parse(reinterpret_cast<const char*>(meta_ptr));
            if (meta.contains("deps") && meta["deps"].is_array()) {
                NodeWithDeps n;
                n.id = reinterpret_cast<const char*>(id_ptr);
                n.file_path = fp_ptr ? reinterpret_cast<const char*>(fp_ptr) : "";
                for (const auto& d : meta["deps"]) {
                    if (d.is_string()) n.deps.push_back(d.get<std::string>());
                }
                if (!n.deps.empty()) nodes_with_deps.push_back(std::move(n));
            }
        } catch (...) {
            // Skip nodes with malformed metadata
        }
    }
    sqlite3_finalize(stmt_deps);

    LOG_INFO("[KG] found " + std::to_string(nodes_with_deps.size()) +
             " nodes with dependencies for IMPORTS inference");

    if (nodes_with_deps.empty()) return;

    // Step 2: Build file_path → representative node_id map from DB
    // Prefer file_summary nodes, fall back to first node per file
    const char* sql_files =
        "SELECT id, file_path, node_type FROM kg_nodes "
        "WHERE repo_id = ? AND file_path IS NOT NULL "
        "ORDER BY CASE WHEN node_type = 'file_summary' THEN 0 ELSE 1 END";

    sqlite3_stmt* stmt_files = nullptr;
    rc = sqlite3_prepare_v2(db_, sql_files, -1, &stmt_files, nullptr);
    if (rc != SQLITE_OK) {
        LOG_ERROR("[KG] failed to prepare files query: " + std::string(sqlite3_errmsg(db_)));
        return;
    }
    sqlite3_bind_text(stmt_files, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);

    std::unordered_map<std::string, std::string> file_to_node;
    while (sqlite3_step(stmt_files) == SQLITE_ROW) {
        auto id_ptr = sqlite3_column_text(stmt_files, 0);
        auto fp_ptr = sqlite3_column_text(stmt_files, 1);
        if (!id_ptr || !fp_ptr) continue;
        std::string fp = reinterpret_cast<const char*>(fp_ptr);
        // Only keep the first entry per file_path (file_summary preferred due to ORDER BY)
        if (file_to_node.find(fp) == file_to_node.end()) {
            file_to_node[fp] = reinterpret_cast<const char*>(id_ptr);
        }
    }
    sqlite3_finalize(stmt_files);

    // Step 3: For each node's dependencies, match against file paths (suffix match)
    exec("BEGIN TRANSACTION");
    size_t edges_added = 0;

    try {
        for (const auto& node : nodes_with_deps) {
            for (const auto& dep : node.deps) {
                if (dep.empty()) continue;
                for (const auto& [fp, nid] : file_to_node) {
                    // Suffix match: "utils.h" matches "src/utils.h"
                    if (fp.size() >= dep.size() &&
                        fp.compare(fp.size() - dep.size(), dep.size(), dep) == 0) {
                        sqlite3_reset(stmt_insert_edge_);
                        sqlite3_bind_text(stmt_insert_edge_, 1, node.id.c_str(), -1, SQLITE_TRANSIENT);
                        sqlite3_bind_text(stmt_insert_edge_, 2, nid.c_str(), -1, SQLITE_TRANSIENT);
                        sqlite3_bind_text(stmt_insert_edge_, 3, "IMPORTS", -1, SQLITE_STATIC);
                        sqlite3_bind_double(stmt_insert_edge_, 4, 1.0);
                        sqlite3_bind_text(stmt_insert_edge_, 5, repo_id.c_str(), -1, SQLITE_TRANSIENT);
                        sqlite3_step(stmt_insert_edge_);
                        ++edges_added;
                        break;  // one edge per (node, dep) pair
                    }
                }
            }
        }
        exec("COMMIT");
    } catch (...) {
        exec("ROLLBACK");
        throw;
    }

    LOG_INFO("[KG] added " + std::to_string(edges_added) + " cross-batch IMPORTS edges");
}

void KnowledgeGraph::inferReferenceEdgesSQL(const std::string& repo_id) {
    // Cross-batch REFERENCES via SQL:
    // For each unique (name, node_type) that looks like a symbol definition
    // (function, class, method, struct, enum, interface), find other nodes
    // in the same repo whose file_path differs and whose name matches.
    // This catches cross-file symbol usage/calls.

    LOG_INFO("[KG] inferring cross-batch REFERENCES edges via SQL...");

    // Step 1: Get all "defining" nodes (functions, classes, etc.) with names >= 4 chars
    const char* sql_defs =
        "SELECT id, name, file_path FROM kg_nodes "
        "WHERE repo_id = ? "
        "AND node_type IN ('function', 'class', 'method', 'struct', 'enum', 'interface') "
        "AND LENGTH(name) >= 4";

    sqlite3_stmt* stmt_defs = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql_defs, -1, &stmt_defs, nullptr);
    if (rc != SQLITE_OK) {
        LOG_ERROR("[KG] failed to prepare defs query: " + std::string(sqlite3_errmsg(db_)));
        return;
    }
    sqlite3_bind_text(stmt_defs, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);

    struct DefNode {
        std::string id;
        std::string name;
        std::string file_path;
    };
    std::vector<DefNode> defs;
    while (sqlite3_step(stmt_defs) == SQLITE_ROW) {
        DefNode d;
        d.id        = reinterpret_cast<const char*>(sqlite3_column_text(stmt_defs, 0));
        d.name      = reinterpret_cast<const char*>(sqlite3_column_text(stmt_defs, 1));
        auto fp     = sqlite3_column_text(stmt_defs, 2);
        d.file_path = fp ? reinterpret_cast<const char*>(fp) : "";
        defs.push_back(std::move(d));
    }
    sqlite3_finalize(stmt_defs);

    LOG_INFO("[KG] found " + std::to_string(defs.size()) + " defining symbols for cross-ref");

    if (defs.empty()) return;

    // For each defining symbol, find nodes in OTHER files that have
    // the same name (exact match on name — catches usages/calls).
    // We batch this into a single large transaction.
    const char* sql_refs =
        "SELECT id FROM kg_nodes "
        "WHERE repo_id = ? AND name = ? AND file_path != ? AND id != ?";

    sqlite3_stmt* stmt_refs = nullptr;
    rc = sqlite3_prepare_v2(db_, sql_refs, -1, &stmt_refs, nullptr);
    if (rc != SQLITE_OK) {
        LOG_ERROR("[KG] failed to prepare refs query: " + std::string(sqlite3_errmsg(db_)));
        return;
    }

    exec("BEGIN TRANSACTION");
    size_t edges_added = 0;

    try {
        for (const auto& def : defs) {
            sqlite3_reset(stmt_refs);
            sqlite3_bind_text(stmt_refs, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_refs, 2, def.name.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_refs, 3, def.file_path.c_str(), -1, SQLITE_TRANSIENT);
            sqlite3_bind_text(stmt_refs, 4, def.id.c_str(), -1, SQLITE_TRANSIENT);

            while (sqlite3_step(stmt_refs) == SQLITE_ROW) {
                auto ref_id = reinterpret_cast<const char*>(sqlite3_column_text(stmt_refs, 0));

                sqlite3_reset(stmt_insert_edge_);
                sqlite3_bind_text(stmt_insert_edge_, 1, ref_id, -1, SQLITE_TRANSIENT);
                sqlite3_bind_text(stmt_insert_edge_, 2, def.id.c_str(), -1, SQLITE_TRANSIENT);
                sqlite3_bind_text(stmt_insert_edge_, 3, "REFERENCES", -1, SQLITE_STATIC);
                sqlite3_bind_double(stmt_insert_edge_, 4, 0.5);
                sqlite3_bind_text(stmt_insert_edge_, 5, repo_id.c_str(), -1, SQLITE_TRANSIENT);
                sqlite3_step(stmt_insert_edge_);
                ++edges_added;
            }
        }
        exec("COMMIT");
    } catch (...) {
        exec("ROLLBACK");
        throw;
    }

    sqlite3_finalize(stmt_refs);

    LOG_INFO("[KG] added " + std::to_string(edges_added) + " cross-batch REFERENCES edges");
}

void KnowledgeGraph::inferContainsEdges(
    const std::string& repo_id,
    const std::vector<tms::CodeChunk>& chunks)
{
    // Build a set of valid node ids for this batch
    std::unordered_set<std::string> node_ids;
    for (const auto& c : chunks) node_ids.insert(c.id);

    for (const auto& chunk : chunks) {
        if (chunk.parent_chunk_id.empty()) continue;
        if (node_ids.find(chunk.parent_chunk_id) == node_ids.end()) continue;

        sqlite3_reset(stmt_insert_edge_);
        sqlite3_bind_text(stmt_insert_edge_, 1, chunk.parent_chunk_id.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_bind_text(stmt_insert_edge_, 2, chunk.id.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_bind_text(stmt_insert_edge_, 3, "CONTAINS", -1, SQLITE_STATIC);
        sqlite3_bind_double(stmt_insert_edge_, 4, 1.0);
        sqlite3_bind_text(stmt_insert_edge_, 5, repo_id.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_step(stmt_insert_edge_);
    }
}

void KnowledgeGraph::inferImportsEdges(
    const std::string& repo_id,
    const std::vector<tms::CodeChunk>& chunks)
{
    // Map file_path → node_id (for file_summary or first chunk per file)
    std::unordered_map<std::string, std::string> file_to_node;
    for (const auto& c : chunks) {
        if (c.type == "file_summary") {
            file_to_node[c.file_path] = c.id;
        }
    }
    // Fallback: use first chunk per file if no file_summary
    for (const auto& c : chunks) {
        if (file_to_node.find(c.file_path) == file_to_node.end()) {
            file_to_node[c.file_path] = c.id;
        }
    }

    for (const auto& chunk : chunks) {
        for (const auto& dep : chunk.dependencies) {
            // dep might be a module path like "fmt" or a file like "./utils.h"
            // Try to match against known file paths
            for (const auto& [fp, nid] : file_to_node) {
                // Simple suffix match: "utils.h" matches "src/utils.h"
                if (fp.size() >= dep.size() &&
                    fp.compare(fp.size() - dep.size(), dep.size(), dep) == 0) {
                    sqlite3_reset(stmt_insert_edge_);
                    sqlite3_bind_text(stmt_insert_edge_, 1, chunk.id.c_str(), -1, SQLITE_TRANSIENT);
                    sqlite3_bind_text(stmt_insert_edge_, 2, nid.c_str(), -1, SQLITE_TRANSIENT);
                    sqlite3_bind_text(stmt_insert_edge_, 3, "IMPORTS", -1, SQLITE_STATIC);
                    sqlite3_bind_double(stmt_insert_edge_, 4, 1.0);
                    sqlite3_bind_text(stmt_insert_edge_, 5, repo_id.c_str(), -1, SQLITE_TRANSIENT);
                    sqlite3_step(stmt_insert_edge_);
                    break;  // one edge per (chunk, dep) pair
                }
            }
        }
    }
}

void KnowledgeGraph::inferReferenceEdges(
    const std::string& repo_id,
    const std::vector<tms::CodeChunk>& chunks)
{
    // Build symbol → defining chunk map
    std::unordered_map<std::string, std::string> symbol_to_chunk;
    for (const auto& c : chunks) {
        for (const auto& sym : c.symbols) {
            // Skip very short symbols (likely false positives)
            if (sym.size() < 3) continue;
            symbol_to_chunk[sym] = c.id;
        }
    }

    if (symbol_to_chunk.empty()) return;

    // For each chunk, scan its content for references to defined symbols
    for (const auto& chunk : chunks) {
        if (chunk.content.empty()) continue;

        for (const auto& [sym, def_id] : symbol_to_chunk) {
            if (def_id == chunk.id) continue;  // skip self-references

            // Check if symbol appears in this chunk's content
            if (chunk.content.find(sym) != std::string::npos) {
                sqlite3_reset(stmt_insert_edge_);
                sqlite3_bind_text(stmt_insert_edge_, 1, chunk.id.c_str(), -1, SQLITE_TRANSIENT);
                sqlite3_bind_text(stmt_insert_edge_, 2, def_id.c_str(), -1, SQLITE_TRANSIENT);
                sqlite3_bind_text(stmt_insert_edge_, 3, "REFERENCES", -1, SQLITE_STATIC);
                sqlite3_bind_double(stmt_insert_edge_, 4, 0.5);  // lower weight for heuristic
                sqlite3_bind_text(stmt_insert_edge_, 5, repo_id.c_str(), -1, SQLITE_TRANSIENT);
                sqlite3_step(stmt_insert_edge_);
            }
        }
    }
}

// ── linkFaissId ────────────────────────────────────────────────────────────

void KnowledgeGraph::linkFaissId(const std::string& node_id, int64_t faiss_id) {
    if (!db_) return;

    const char* sql = "UPDATE kg_nodes SET faiss_id = ? WHERE id = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return;

    sqlite3_bind_int64(stmt, 1, faiss_id);
    sqlite3_bind_text(stmt, 2, node_id.c_str(), -1, SQLITE_TRANSIENT);
    sqlite3_step(stmt);
    sqlite3_finalize(stmt);
}

// ── removeRepo ─────────────────────────────────────────────────────────────

void KnowledgeGraph::removeRepo(const std::string& repo_id) {
    if (!db_) return;

    const char* del_edges = "DELETE FROM kg_edges WHERE repo_id = ?";
    const char* del_nodes = "DELETE FROM kg_nodes WHERE repo_id = ?";

    auto run = [&](const char* sql) {
        sqlite3_stmt* stmt = nullptr;
        int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
        if (rc != SQLITE_OK) return;
        sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_step(stmt);
        sqlite3_finalize(stmt);
    };

    run(del_edges);
    run(del_nodes);
}

// ── Queries ────────────────────────────────────────────────────────────────

std::vector<KGEdge> KnowledgeGraph::neighbors(
    const std::string& node_id,
    const std::string& edge_type) const
{
    if (!db_) return {};

    sqlite3_stmt* stmt = edge_type.empty() ? stmt_neighbors_ : stmt_neighbors_type_;
    sqlite3_reset(stmt);
    sqlite3_bind_text(stmt, 1, node_id.c_str(), -1, SQLITE_TRANSIENT);
    sqlite3_bind_text(stmt, 2, node_id.c_str(), -1, SQLITE_TRANSIENT);
    if (!edge_type.empty()) {
        sqlite3_bind_text(stmt, 3, edge_type.c_str(), -1, SQLITE_TRANSIENT);
    }

    std::vector<KGEdge> results;
    while (sqlite3_step(stmt) == SQLITE_ROW) {
        KGEdge e;
        e.id        = sqlite3_column_int64(stmt, 0);
        e.src_id    = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 1));
        e.dst_id    = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 2));
        e.edge_type = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 3));
        e.weight    = sqlite3_column_double(stmt, 4);
        e.repo_id   = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 5));
        results.push_back(e);
    }
    return results;
}

std::vector<KGEdge> KnowledgeGraph::neighbors2(
    const std::string& node_id,
    const std::string& edge_type) const
{
    if (!db_) return {};

    // 2-hop: find 1-hop neighbors, then their neighbors
    auto hop1 = neighbors(node_id, edge_type);

    std::unordered_set<std::string> hop1_ids;
    for (const auto& e : hop1) {
        hop1_ids.insert(e.src_id == node_id ? e.dst_id : e.src_id);
    }

    std::vector<KGEdge> results = hop1;
    for (const auto& nid : hop1_ids) {
        auto hop2 = neighbors(nid, edge_type);
        for (auto& e : hop2) {
            // Avoid returning edges back to the origin
            if (e.src_id == node_id || e.dst_id == node_id) continue;
            results.push_back(std::move(e));
        }
    }

    return results;
}

std::optional<KGNode> KnowledgeGraph::getNode(const std::string& node_id) const {
    if (!db_) return std::nullopt;

    const char* sql = "SELECT id, node_type, name, file_path, language, "
                      "faiss_id, repo_id, metadata FROM kg_nodes WHERE id = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return std::nullopt;

    sqlite3_bind_text(stmt, 1, node_id.c_str(), -1, SQLITE_TRANSIENT);

    std::optional<KGNode> result;
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        KGNode n;
        n.id        = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
        n.node_type = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 1));
        n.name      = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 2));
        auto fp     = sqlite3_column_text(stmt, 3);
        n.file_path = fp ? reinterpret_cast<const char*>(fp) : "";
        auto lang   = sqlite3_column_text(stmt, 4);
        n.language  = lang ? reinterpret_cast<const char*>(lang) : "";
        n.faiss_id  = sqlite3_column_int64(stmt, 5);
        n.repo_id   = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 6));
        auto meta   = sqlite3_column_text(stmt, 7);
        n.metadata  = meta ? reinterpret_cast<const char*>(meta) : "";
        result = n;
    }
    sqlite3_finalize(stmt);
    return result;
}

std::vector<KGNode> KnowledgeGraph::getNodes(const std::string& repo_id) const {
    if (!db_) return {};

    const char* sql = "SELECT id, node_type, name, file_path, language, "
                      "faiss_id, repo_id, metadata FROM kg_nodes WHERE repo_id = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return {};

    sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);

    std::vector<KGNode> results;
    while (sqlite3_step(stmt) == SQLITE_ROW) {
        KGNode n;
        n.id        = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
        n.node_type = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 1));
        n.name      = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 2));
        auto fp     = sqlite3_column_text(stmt, 3);
        n.file_path = fp ? reinterpret_cast<const char*>(fp) : "";
        auto lang   = sqlite3_column_text(stmt, 4);
        n.language  = lang ? reinterpret_cast<const char*>(lang) : "";
        n.faiss_id  = sqlite3_column_int64(stmt, 5);
        n.repo_id   = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 6));
        auto meta   = sqlite3_column_text(stmt, 7);
        n.metadata  = meta ? reinterpret_cast<const char*>(meta) : "";
        results.push_back(n);
    }
    sqlite3_finalize(stmt);
    return results;
}

std::vector<KGNode> KnowledgeGraph::nodesByFaissId(int64_t faiss_id,
                                                    const std::string& repo_id) const {
    if (!db_) return {};

    const char* sql = "SELECT id, node_type, name, file_path, language, "
                      "faiss_id, repo_id, metadata FROM kg_nodes "
                      "WHERE faiss_id = ? AND repo_id = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return {};

    sqlite3_bind_int64(stmt, 1, faiss_id);
    sqlite3_bind_text(stmt, 2, repo_id.c_str(), -1, SQLITE_TRANSIENT);

    std::vector<KGNode> results;
    while (sqlite3_step(stmt) == SQLITE_ROW) {
        KGNode n;
        n.id        = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
        n.node_type = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 1));
        n.name      = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 2));
        auto fp     = sqlite3_column_text(stmt, 3);
        n.file_path = fp ? reinterpret_cast<const char*>(fp) : "";
        auto lang   = sqlite3_column_text(stmt, 4);
        n.language  = lang ? reinterpret_cast<const char*>(lang) : "";
        n.faiss_id  = sqlite3_column_int64(stmt, 5);
        n.repo_id   = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 6));
        auto meta   = sqlite3_column_text(stmt, 7);
        n.metadata  = meta ? reinterpret_cast<const char*>(meta) : "";
        results.push_back(n);
    }
    sqlite3_finalize(stmt);
    return results;
}

std::vector<KGNode> KnowledgeGraph::nodesByFilePath(const std::string& file_path,
                                                     const std::string& repo_id) const {
    if (!db_) return {};

    const char* sql = "SELECT id, node_type, name, file_path, language, "
                      "faiss_id, repo_id, metadata FROM kg_nodes "
                      "WHERE file_path = ? AND repo_id = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return {};

    sqlite3_bind_text(stmt, 1, file_path.c_str(), -1, SQLITE_TRANSIENT);
    sqlite3_bind_text(stmt, 2, repo_id.c_str(), -1, SQLITE_TRANSIENT);

    std::vector<KGNode> results;
    while (sqlite3_step(stmt) == SQLITE_ROW) {
        KGNode n;
        n.id        = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
        n.node_type = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 1));
        n.name      = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 2));
        auto fp     = sqlite3_column_text(stmt, 3);
        n.file_path = fp ? reinterpret_cast<const char*>(fp) : "";
        auto lang   = sqlite3_column_text(stmt, 4);
        n.language  = lang ? reinterpret_cast<const char*>(lang) : "";
        n.faiss_id  = sqlite3_column_int64(stmt, 5);
        n.repo_id   = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 6));
        auto meta   = sqlite3_column_text(stmt, 7);
        n.metadata  = meta ? reinterpret_cast<const char*>(meta) : "";
        results.push_back(n);
    }
    sqlite3_finalize(stmt);
    return results;
}

// ── Statistics ─────────────────────────────────────────────────────────────

size_t KnowledgeGraph::nodeCount(const std::string& repo_id) const {
    if (!db_) return 0;

    std::string sql = repo_id.empty()
        ? "SELECT COUNT(*) FROM kg_nodes"
        : "SELECT COUNT(*) FROM kg_nodes WHERE repo_id = ?";

    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql.c_str(), -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return 0;

    if (!repo_id.empty()) {
        sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
    }

    size_t count = 0;
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        count = static_cast<size_t>(sqlite3_column_int64(stmt, 0));
    }
    sqlite3_finalize(stmt);
    return count;
}

size_t KnowledgeGraph::edgeCount(const std::string& repo_id) const {
    if (!db_) return 0;

    std::string sql = repo_id.empty()
        ? "SELECT COUNT(*) FROM kg_edges"
        : "SELECT COUNT(*) FROM kg_edges WHERE repo_id = ?";

    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql.c_str(), -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return 0;

    if (!repo_id.empty()) {
        sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
    }

    size_t count = 0;
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        count = static_cast<size_t>(sqlite3_column_int64(stmt, 0));
    }
    sqlite3_finalize(stmt);
    return count;
}

} // namespace aipr
