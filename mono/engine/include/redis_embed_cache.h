/**
 * Redis-backed LRU Embedding Cache
 *
 * Two-tier caching strategy for embedding vectors:
 *   L1: In-process LRU cache (existing EmbeddingCache in embedding_engine.h)
 *   L2: Redis-backed cache with composite key:
 *       embed:{repo}:{commit}:{file_or_url_hash}:{dim}
 *
 * TTL is configurable (default 45 min, range 30–60 min).
 * The cache is used by the embedding engine before calling the external
 * provider, and populated after successful embed calls.
 *
 * The Go server provides the Redis proxy endpoints:
 *   GET  /api/v1/engine/embed-cache/{repoID}/{chunkHash}
 *   PUT  /api/v1/engine/embed-cache/{repoID}/{chunkHash}
 *
 * This class wraps HTTP calls to that proxy so the C++ engine doesn't
 * need a direct Redis dependency.
 */

#pragma once

#include <string>
#include <vector>
#include <optional>
#include <chrono>
#include <mutex>
#include <list>
#include <unordered_map>

namespace aipr {

struct RedisEmbedCacheConfig {
    /// URL of the Go API server that proxies Redis.
    /// Resolved at construction time from (in order):
    ///   1. This field, if non-empty
    ///   2. ENGINE_GO_SERVER_URL environment variable
    ///   3. Fallback: "http://localhost:8080"
    /// In production, set ENGINE_GO_SERVER_URL or pass the URL from
    /// rtserverprops.xml <engine go-server-url="...">.
    std::string go_server_url;
    int ttl_minutes = 45;
    size_t lru_capacity = 50000;
    int timeout_ms = 2000;
};

class RedisEmbedCache {
public:
    explicit RedisEmbedCache(const RedisEmbedCacheConfig& config = {});
    ~RedisEmbedCache();

    RedisEmbedCache(const RedisEmbedCache&) = delete;
    RedisEmbedCache& operator=(const RedisEmbedCache&) = delete;

    /**
     * Build a composite cache key.
     * Format: {repo_id}:{commit}:{sha256(file_path_or_url)}:{dim}
     */
    static std::string buildKey(
        const std::string& repo_id,
        const std::string& commit_sha,
        const std::string& file_or_url,
        size_t dimension
    );

    /**
     * Look up a cached embedding.
     * Checks L1 (in-process LRU) first, then L2 (Redis via Go proxy).
     * Returns std::nullopt on miss.
     */
    std::optional<std::vector<float>> get(
        const std::string& repo_id,
        const std::string& chunk_hash
    );

    /**
     * Store an embedding in both L1 and L2.
     */
    void put(
        const std::string& repo_id,
        const std::string& chunk_hash,
        const std::vector<float>& embedding
    );

    struct Stats {
        size_t l1_size = 0;
        size_t l1_hits = 0;
        size_t l1_misses = 0;
        size_t l2_hits = 0;
        size_t l2_misses = 0;
        size_t l2_errors = 0;
    };

    Stats getStats() const;
    void clearL1();

    /**
     * Update the Go server URL at runtime (thread-safe).
     * Called when ConfigureStorage receives a server_callback_url from the Go server.
     */
    void setGoServerUrl(const std::string& url);

private:
    RedisEmbedCacheConfig config_;

    /**
     * Return the effective Go server URL.  Re-checks the ENGINE_GO_SERVER_URL
     * environment variable when the stored URL is the default fallback, so that
     * a late ConfigureStorage call is picked up automatically.
     */
    std::string effectiveGoServerUrl() const;

    // L1: in-process LRU
    using Key = std::string;
    using Value = std::vector<float>;
    using ListIt = std::list<std::pair<Key, Value>>::iterator;

    mutable std::mutex mu_;
    std::list<std::pair<Key, Value>> lru_list_;
    std::unordered_map<Key, ListIt> lru_map_;

    mutable Stats stats_;

    void l1Put(const std::string& key, const std::vector<float>& vec);
    std::optional<std::vector<float>> l1Get(const std::string& key);

    std::optional<std::vector<float>> l2Get(const std::string& repo_id, const std::string& chunk_hash);
    void l2Put(const std::string& repo_id, const std::string& chunk_hash, const std::vector<float>& vec);
};

} // namespace aipr
