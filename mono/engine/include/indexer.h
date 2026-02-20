/**
 * AI PR Reviewer - Indexer Interface
 */

#ifndef AIPR_INDEXER_H
#define AIPR_INDEXER_H

#include "types.h"
#include <string>
#include <vector>
#include <memory>
#include <functional>

namespace aipr {

/**
 * Indexing configuration
 */
struct IndexerConfig {
    // File filtering
    std::vector<std::string> include_patterns;
    std::vector<std::string> exclude_patterns;
    size_t max_file_size_kb = 1024;
    
    // Chunking
    size_t chunk_size = 512;        // Target chunk size in tokens
    size_t chunk_overlap = 64;      // Overlap between chunks
    bool enable_ast_chunking = true;
    
    // Languages to index (empty = all detected)
    std::vector<std::string> languages;
    
    // Tiered indexing (for large repos)
    bool tiered_enabled = false;
    std::vector<std::string> hot_paths;
    std::vector<std::string> cold_paths;
};

/**
 * Manifest entry for a file
 */
struct ManifestEntry {
    std::string file_path;
    std::string blob_sha;
    std::vector<std::string> chunk_ids;
    std::string language;
    size_t size_bytes = 0;
    std::string last_indexed;
};

/**
 * Index manifest (tracks what's indexed)
 */
struct IndexManifest {
    std::string repo_id;
    std::string version;
    std::string commit_sha;
    std::vector<ManifestEntry> entries;
    std::string created_at;
    std::string updated_at;
};

/**
 * Progress callback
 */
using IndexProgressCallback = std::function<void(size_t current, size_t total, const std::string& file)>;

/**
 * Indexer interface
 */
class Indexer {
public:
    virtual ~Indexer() = default;
    
    /**
     * Scan repository and discover files to index
     */
    virtual std::vector<FileInfo> scanRepository(
        const std::string& repo_path,
        const IndexerConfig& config
    ) = 0;
    
    /**
     * Chunk a file into indexable units
     */
    virtual std::vector<Chunk> chunkFile(
        const std::string& file_path,
        const std::string& content,
        const std::string& language
    ) = 0;
    
    /**
     * Create or update index for a repository
     */
    virtual IndexManifest indexRepository(
        const std::string& repo_id,
        const std::string& repo_path,
        const IndexerConfig& config,
        IndexProgressCallback progress = nullptr
    ) = 0;
    
    /**
     * Update index incrementally
     */
    virtual IndexManifest updateIndex(
        const std::string& repo_id,
        const IndexManifest& current_manifest,
        const std::vector<std::string>& changed_files,
        const std::string& new_commit_sha
    ) = 0;
    
    /**
     * Get current manifest
     */
    virtual IndexManifest getManifest(const std::string& repo_id) = 0;
    
    /**
     * Factory method
     */
    static std::unique_ptr<Indexer> create(const std::string& storage_path);
};

}

#endif
