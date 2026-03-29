/**
 * Merkle Cache — Implementation
 *
 * SQLite-backed file hash cache for incremental reindex.
 * See merkle_cache.h for architecture documentation.
 */

#include "merkle_cache.h"
#include "metrics.h"
#include "logging.h"

#include <sqlite3.h>
#include <fstream>
#include <sstream>
#include <iomanip>
#include <filesystem>
#include <algorithm>
#include <queue>

// Use OpenSSL SHA-256 if available, otherwise a lightweight implementation
#include <openssl/sha.h>

namespace aipr {

// ─────────────────────────────────────────────────────────────────────────────
// Construction / Lifecycle
// ─────────────────────────────────────────────────────────────────────────────

MerkleCache::MerkleCache(const std::string& db_path, const MerkleCacheConfig& config)
    : db_path_(db_path)
    , config_(config)
{}

MerkleCache::~MerkleCache() {
    close();
}

void MerkleCache::open() {
    if (db_) return;

    std::filesystem::create_directories(std::filesystem::path(db_path_).parent_path());

    int rc = sqlite3_open_v2(
        db_path_.c_str(), &db_,
        SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_NOMUTEX,
        nullptr);

    if (rc != SQLITE_OK) {
        std::string err = db_ ? sqlite3_errmsg(db_) : "unknown error";
        sqlite3_close(db_);
        db_ = nullptr;
        throw std::runtime_error("MerkleCache: failed to open DB: " + err);
    }

    exec("PRAGMA journal_mode=WAL");
    exec("PRAGMA synchronous=NORMAL");
    exec("PRAGMA busy_timeout=5000");

    ensureSchema();
}

void MerkleCache::close() {
    if (db_) {
        sqlite3_close(db_);
        db_ = nullptr;
    }
}

void MerkleCache::exec(const std::string& sql) {
    char* errmsg = nullptr;
    int rc = sqlite3_exec(db_, sql.c_str(), nullptr, nullptr, &errmsg);
    if (rc != SQLITE_OK) {
        std::string err = errmsg ? errmsg : "unknown";
        sqlite3_free(errmsg);
        throw std::runtime_error("MerkleCache SQL error: " + err + " [" + sql.substr(0, 120) + "]");
    }
    if (errmsg) sqlite3_free(errmsg);
}

void MerkleCache::ensureSchema() {
    exec(R"(
        CREATE TABLE IF NOT EXISTS merkle_hashes (
            repo_id   TEXT NOT NULL,
            file_path TEXT NOT NULL,
            hash      TEXT NOT NULL,
            updated_at TEXT NOT NULL DEFAULT (datetime('now')),
            PRIMARY KEY (repo_id, file_path)
        )
    )");

    exec("CREATE INDEX IF NOT EXISTS idx_merkle_repo ON merkle_hashes(repo_id)");
}

// ─────────────────────────────────────────────────────────────────────────────
// SHA-256 file hashing
// ─────────────────────────────────────────────────────────────────────────────

std::string MerkleCache::hashFile(const std::string& full_path) {
    std::ifstream file(full_path, std::ios::binary);
    if (!file.is_open()) return "";

    SHA256_CTX ctx;
    SHA256_Init(&ctx);

    char buffer[8192];
    while (file.read(buffer, sizeof(buffer))) {
        SHA256_Update(&ctx, buffer, file.gcount());
    }
    // Final partial read
    if (file.gcount() > 0) {
        SHA256_Update(&ctx, buffer, file.gcount());
    }

    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256_Final(hash, &ctx);

    // Convert to hex string
    std::ostringstream hex;
    hex << std::hex << std::setfill('0');
    for (int i = 0; i < SHA256_DIGEST_LENGTH; ++i) {
        hex << std::setw(2) << static_cast<int>(hash[i]);
    }
    return hex.str();
}

// ─────────────────────────────────────────────────────────────────────────────
// Merkle diff computation
// ─────────────────────────────────────────────────────────────────────────────

MerkleDiffResult MerkleCache::computeDiff(
    const std::string& repo_id,
    const std::string& repo_path,
    const std::vector<std::string>& all_files,
    KnowledgeGraph* kg)
{
    MerkleDiffResult result;

    if (!db_) {
        // No cache available — all files are new
        result.new_files = all_files;
        return result;
    }

    // Load all stored hashes for this repo
    std::unordered_map<std::string, std::string> stored_hashes;
    {
        const char* sql = "SELECT file_path, hash FROM merkle_hashes WHERE repo_id = ?";
        sqlite3_stmt* stmt = nullptr;
        int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
        if (rc == SQLITE_OK) {
            sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
            while (sqlite3_step(stmt) == SQLITE_ROW) {
                auto fp = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
                auto h  = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 1));
                if (fp && h) stored_hashes[fp] = h;
            }
            sqlite3_finalize(stmt);
        }
    }

    // Track which stored files are still present (using relative paths).
    // We build this set incrementally in the loop below so that both absolute
    // and relative inputs are normalised to relative keys.
    std::unordered_set<std::string> current_files_set;

    // Compute new hashes and compare
    std::unordered_map<std::string, std::string> new_hashes;

    for (const auto& file_path : all_files) {
        // file_path may be absolute (from walkDirectory) or relative.
        // If it already starts with repo_path, use it directly.
        std::string full_path;
        if (!file_path.empty() && file_path[0] == '/' ) {
            full_path = file_path;
        } else {
            full_path = repo_path + "/" + file_path;
        }

        // Normalise the key stored in the cache to a relative path so that
        // lookups are consistent regardless of the clone location.
        std::string rel_path = file_path;
        if (rel_path.size() > repo_path.size() + 1 &&
            rel_path.compare(0, repo_path.size(), repo_path) == 0) {
            rel_path = rel_path.substr(repo_path.size() + 1); // strip repo_path + "/"
        }

        std::string current_hash = hashFile(full_path);

        if (current_hash.empty()) continue; // unreadable file

        new_hashes[rel_path] = current_hash;
        current_files_set.insert(rel_path);

        auto it = stored_hashes.find(rel_path);
        if (it == stored_hashes.end()) {
            result.new_files.push_back(rel_path);
        } else if (it->second != current_hash) {
            result.changed_files.push_back(rel_path);
        } else {
            result.unchanged_files.push_back(rel_path);
        }
    }

    // Find deleted files (in stored but not in current scan)
    for (const auto& [fp, _] : stored_hashes) {
        if (!current_files_set.count(fp)) {
            result.deleted_files.push_back(fp);
        }
    }

    // Find dependent files via KG traversal
    if (config_.propagate_dependents && kg &&
        (!result.changed_files.empty() || !result.deleted_files.empty())) {

        std::vector<std::string> triggers;
        triggers.insert(triggers.end(),
                        result.changed_files.begin(), result.changed_files.end());
        triggers.insert(triggers.end(),
                        result.deleted_files.begin(), result.deleted_files.end());

        auto dependents = findDependentFiles(triggers, repo_id, kg);

        // Only include dependents that are currently unchanged
        std::unordered_set<std::string> changed_set(
            result.changed_files.begin(), result.changed_files.end());
        std::unordered_set<std::string> new_set(
            result.new_files.begin(), result.new_files.end());

        for (const auto& dep : dependents) {
            if (!changed_set.count(dep) && !new_set.count(dep) &&
                current_files_set.count(dep)) {
                result.dependent_files.push_back(dep);
            }
        }
    }

    // Telemetry
    metrics::Registry::instance().setGauge(
        metrics::MERKLE_FILES_SKIPPED,
        static_cast<double>(result.unchanged_files.size()));
    metrics::Registry::instance().setGauge(
        metrics::MERKLE_FILES_REEMBEDDED,
        static_cast<double>(result.totalToEmbed()));
    metrics::Registry::instance().setGauge(
        metrics::MERKLE_DEPENDENT_FILES,
        static_cast<double>(result.dependent_files.size()));

    double total = static_cast<double>(all_files.size());
    if (total > 0) {
        metrics::Registry::instance().setGauge(
            metrics::MERKLE_CACHE_HIT_RATE,
            static_cast<double>(result.unchanged_files.size()) / total);
    }

    LOG_INFO("[MerkleCache] diff: unchanged=" +
             std::to_string(result.unchanged_files.size()) +
             " changed=" + std::to_string(result.changed_files.size()) +
             " new=" + std::to_string(result.new_files.size()) +
             " deleted=" + std::to_string(result.deleted_files.size()) +
             " dependent=" + std::to_string(result.dependent_files.size()));

    return result;
}

// ─────────────────────────────────────────────────────────────────────────────
// Hash storage
// ─────────────────────────────────────────────────────────────────────────────

void MerkleCache::updateHashes(
    const std::string& repo_id,
    const std::unordered_map<std::string, std::string>& file_hashes)
{
    if (!db_ || file_hashes.empty()) return;

    exec("BEGIN TRANSACTION");

    const char* sql = "INSERT OR REPLACE INTO merkle_hashes "
                      "(repo_id, file_path, hash, updated_at) "
                      "VALUES (?, ?, ?, datetime('now'))";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) {
        exec("ROLLBACK");
        return;
    }

    for (const auto& [file_path, hash] : file_hashes) {
        sqlite3_reset(stmt);
        sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_bind_text(stmt, 2, file_path.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_bind_text(stmt, 3, hash.c_str(), -1, SQLITE_TRANSIENT);
        sqlite3_step(stmt);
    }

    sqlite3_finalize(stmt);
    exec("COMMIT");
}

void MerkleCache::removeRepo(const std::string& repo_id) {
    if (!db_) return;

    const char* sql = "DELETE FROM merkle_hashes WHERE repo_id = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return;

    sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
    sqlite3_step(stmt);
    sqlite3_finalize(stmt);
}

std::string MerkleCache::getHash(const std::string& repo_id,
                                  const std::string& file_path) const {
    if (!db_) return "";

    const char* sql = "SELECT hash FROM merkle_hashes "
                      "WHERE repo_id = ? AND file_path = ?";
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) return "";

    sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
    sqlite3_bind_text(stmt, 2, file_path.c_str(), -1, SQLITE_TRANSIENT);

    std::string hash;
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        auto h = sqlite3_column_text(stmt, 0);
        if (h) hash = reinterpret_cast<const char*>(h);
    }
    sqlite3_finalize(stmt);
    return hash;
}

MerkleCache::Stats MerkleCache::getStats(const std::string& repo_id) const {
    Stats stats;
    if (!db_) return stats;

    // Total entries
    {
        const char* sql = "SELECT COUNT(*) FROM merkle_hashes";
        sqlite3_stmt* stmt = nullptr;
        if (sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr) == SQLITE_OK) {
            if (sqlite3_step(stmt) == SQLITE_ROW) {
                stats.total_entries = static_cast<size_t>(sqlite3_column_int64(stmt, 0));
            }
            sqlite3_finalize(stmt);
        }
    }

    // Per-repo entries
    if (!repo_id.empty()) {
        const char* sql = "SELECT COUNT(*) FROM merkle_hashes WHERE repo_id = ?";
        sqlite3_stmt* stmt = nullptr;
        if (sqlite3_prepare_v2(db_, sql, -1, &stmt, nullptr) == SQLITE_OK) {
            sqlite3_bind_text(stmt, 1, repo_id.c_str(), -1, SQLITE_TRANSIENT);
            if (sqlite3_step(stmt) == SQLITE_ROW) {
                stats.repo_entries = static_cast<size_t>(sqlite3_column_int64(stmt, 0));
            }
            sqlite3_finalize(stmt);
        }
    }

    return stats;
}

// ─────────────────────────────────────────────────────────────────────────────
// Dependent file discovery via KG
// ─────────────────────────────────────────────────────────────────────────────

std::vector<std::string> MerkleCache::findDependentFiles(
    const std::vector<std::string>& changed_files,
    const std::string& repo_id,
    KnowledgeGraph* kg)
{
    if (!kg) return {};

    // Strategy: for each changed file, find KG nodes in that file,
    // then follow reverse-IMPORTS edges (who imports this file?)
    // up to max_dependent_depth hops.

    std::unordered_set<std::string> dependent_file_paths;
    std::unordered_set<std::string> changed_set(changed_files.begin(), changed_files.end());

    for (const auto& file_path : changed_files) {
        auto nodes = kg->nodesByFilePath(file_path, repo_id);

        // BFS from each node following reverse edges
        std::queue<std::pair<std::string, int>> bfs;  // (node_id, depth)
        std::unordered_set<std::string> visited;

        for (const auto& node : nodes) {
            bfs.push({node.id, 0});
            visited.insert(node.id);
        }

        while (!bfs.empty()) {
            auto [node_id, depth] = bfs.front();
            bfs.pop();

            if (depth >= config_.max_dependent_depth) continue;

            // Find nodes that IMPORT this node
            auto edges = kg->neighbors(node_id, "IMPORTS");
            for (const auto& edge : edges) {
                // We want nodes that import this node, i.e., src_id → IMPORTS → dst_id
                // where dst_id is our node. So look at src_id.
                std::string importer_id = edge.src_id;
                if (importer_id == node_id) continue; // skip self
                if (visited.count(importer_id)) continue;
                visited.insert(importer_id);

                // Resolve the importing node to get its file_path
                auto importer_node = kg->getNode(importer_id);
                if (importer_node && !importer_node->file_path.empty()) {
                    if (!changed_set.count(importer_node->file_path)) {
                        dependent_file_paths.insert(importer_node->file_path);
                    }
                }

                bfs.push({importer_id, depth + 1});
            }

            // Also follow CALLS edges (callers of changed functions)
            auto call_edges = kg->neighbors(node_id, "CALLS");
            for (const auto& edge : call_edges) {
                std::string caller_id = edge.src_id;
                if (caller_id == node_id) continue;
                if (visited.count(caller_id)) continue;
                visited.insert(caller_id);

                auto caller_node = kg->getNode(caller_id);
                if (caller_node && !caller_node->file_path.empty()) {
                    if (!changed_set.count(caller_node->file_path)) {
                        dependent_file_paths.insert(caller_node->file_path);
                    }
                }

                bfs.push({caller_id, depth + 1});
            }
        }
    }

    return {dependent_file_paths.begin(), dependent_file_paths.end()};
}

} // namespace aipr
