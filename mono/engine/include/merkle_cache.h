/**
 * Merkle Cache — File-level Content Hashing for Incremental Reindex
 *
 * Maintains a SHA-256 Merkle hash per file. During reindex:
 *   1. Compute hash of each file's content.
 *   2. Compare against stored hash → skip unchanged files.
 *   3. For changed files, also re-embed KG-dependent files
 *      (files that IMPORT or CALL symbols from the changed file).
 *   4. LRU eviction for embed cache entries.
 *
 * Storage: SQLite table `merkle_hashes` alongside the KnowledgeGraph DB.
 *
 * Expected speedup: 5–10× on incremental reindex for repos where
 * only 5–10% of files change between commits.
 */

#pragma once

#include "knowledge_graph.h"
#include <string>
#include <vector>
#include <unordered_map>
#include <unordered_set>
#include <functional>

struct sqlite3;

namespace aipr {

/**
 * MerkleCache configuration
 */
struct MerkleCacheConfig {
    size_t max_embed_cache_entries = 100000; // LRU cap for embedding cache
    bool propagate_dependents = true;        // Re-embed files that depend on changed files
    int max_dependent_depth = 2;             // Max KG traversal depth for dependent discovery
};

/**
 * Result of a Merkle-based diff analysis
 */
struct MerkleDiffResult {
    std::vector<std::string> unchanged_files;   // Hash matches → skip
    std::vector<std::string> changed_files;     // Hash changed → re-embed
    std::vector<std::string> new_files;         // Not in cache → embed
    std::vector<std::string> deleted_files;     // In cache but not on disk
    std::vector<std::string> dependent_files;   // Unchanged but depend on changed files

    size_t totalToEmbed() const {
        return changed_files.size() + new_files.size() + dependent_files.size();
    }
    size_t totalSkipped() const { return unchanged_files.size(); }
};

/**
 * MerkleCache — file-level content hashing for incremental embedding.
 */
class MerkleCache {
public:
    /**
     * @param db_path  Path to SQLite file (can share DB with KnowledgeGraph).
     * @param config   Cache configuration.
     */
    MerkleCache(const std::string& db_path, const MerkleCacheConfig& config = {});
    ~MerkleCache();

    MerkleCache(const MerkleCache&) = delete;
    MerkleCache& operator=(const MerkleCache&) = delete;

    /** Open (or create) the database and run migrations. */
    void open();

    /** Close the database. */
    void close();

    /**
     * Compute which files need re-embedding by comparing Merkle hashes.
     *
     * @param repo_id       Repository identifier
     * @param repo_path     Filesystem path to repository root
     * @param all_files     All files discovered in the current scan
     * @param kg            KnowledgeGraph for dependent file discovery (nullable)
     * @return MerkleDiffResult categorizing files by change status
     */
    MerkleDiffResult computeDiff(
        const std::string& repo_id,
        const std::string& repo_path,
        const std::vector<std::string>& all_files,
        KnowledgeGraph* kg = nullptr
    );

    /**
     * Update stored hashes after successful embedding.
     * Call this after files have been successfully embedded.
     *
     * @param repo_id    Repository identifier
     * @param file_hashes  Map of file_path → SHA-256 hash
     */
    void updateHashes(
        const std::string& repo_id,
        const std::unordered_map<std::string, std::string>& file_hashes
    );

    /**
     * Remove all hashes for a repository.
     */
    void removeRepo(const std::string& repo_id);

    /**
     * Get the stored hash for a file.
     */
    std::string getHash(const std::string& repo_id, const std::string& file_path) const;

    /**
     * Compute SHA-256 hash of a file's content.
     */
    static std::string hashFile(const std::string& full_path);

    /**
     * Get cache statistics for a repo.
     */
    struct Stats {
        size_t total_entries = 0;
        size_t repo_entries = 0;
    };
    Stats getStats(const std::string& repo_id = "") const;

private:
    std::string db_path_;
    MerkleCacheConfig config_;
    sqlite3* db_ = nullptr;

    void ensureSchema();
    void exec(const std::string& sql);

    /**
     * Find files that transitively depend on a set of changed files
     * by traversing IMPORTS edges in the KG.
     */
    std::vector<std::string> findDependentFiles(
        const std::vector<std::string>& changed_files,
        const std::string& repo_id,
        KnowledgeGraph* kg
    );
};

} // namespace aipr
