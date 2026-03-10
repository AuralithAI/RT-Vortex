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
