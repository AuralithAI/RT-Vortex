/**
 * Redis-backed LRU Embedding Cache — Implementation
 *
 * L1: in-process LRU (fast, process-local)
 * L2: Redis via Go server HTTP proxy (shared across engine restarts)
 */

#include "redis_embed_cache.h"
#include "logging.h"

#include <sstream>
#include <iomanip>
#include <cstring>
#include <functional>

#ifdef AIPR_HAS_CURL
#include <curl/curl.h>
#endif

#include <nlohmann/json.hpp>
#include <cstdlib>

namespace aipr {

// ── Helpers ─────────────────────────────────────────────────────────────────

static std::string sha256Hex(const std::string& input) {
    // Simple FNV-1a 64-bit hash (fast, no crypto needed for cache keys).
    uint64_t hash = 14695981039346656037ULL;
    for (unsigned char c : input) {
        hash ^= c;
        hash *= 1099511628211ULL;
    }
    std::ostringstream oss;
    oss << std::hex << std::setfill('0') << std::setw(16) << hash;
    return oss.str();
}

std::string RedisEmbedCache::buildKey(
    const std::string& repo_id,
    const std::string& commit_sha,
    const std::string& file_or_url,
    size_t dimension)
{
    return repo_id + ":" + commit_sha.substr(0, 8) + ":" +
           sha256Hex(file_or_url) + ":" + std::to_string(dimension);
}

// ── Constructor ─────────────────────────────────────────────────────────────

// Resolve at construction time; may be updated later via setGoServerUrl()
// or re-resolved lazily via effectiveGoServerUrl().
static std::string resolveGoServerUrl(const std::string& configured) {
    // 1. Explicit config value (from caller).
    if (!configured.empty()) return configured;
    // 2. Environment variable (set by ConfigureStorage at runtime).
    if (const char* env = std::getenv("ENGINE_GO_SERVER_URL")) {
        if (env[0] != '\0') return env;
    }
    // 3. Fallback for local development only.
    return "http://localhost:8080";
}

RedisEmbedCache::RedisEmbedCache(const RedisEmbedCacheConfig& config)
    : config_(config)
{
    config_.go_server_url = resolveGoServerUrl(config.go_server_url);
    // Strip trailing slash if present.
    while (!config_.go_server_url.empty() && config_.go_server_url.back() == '/') {
        config_.go_server_url.pop_back();
    }
}

RedisEmbedCache::~RedisEmbedCache() = default;

void RedisEmbedCache::setGoServerUrl(const std::string& url) {
    if (url.empty()) return;
    config_.go_server_url = url;
    // Strip trailing slash if present.
    while (!config_.go_server_url.empty() && config_.go_server_url.back() == '/') {
        config_.go_server_url.pop_back();
    }
    LOG_INFO("[RedisEmbedCache] Go server URL updated to " + config_.go_server_url);
}

/// Return the effective Go server URL.  If the stored URL is the local
/// fallback, re-check the environment in case ConfigureStorage updated it
/// after this cache was constructed.
std::string RedisEmbedCache::effectiveGoServerUrl() const {
    if (config_.go_server_url != "http://localhost:8080") {
        return config_.go_server_url;          // explicitly configured
    }
    if (const char* env = std::getenv("ENGINE_GO_SERVER_URL")) {
        if (env[0] != '\0') return env;        // runtime override from Go server
    }
    return config_.go_server_url;              // fallback
}

// ── L1 LRU ──────────────────────────────────────────────────────────────────

void RedisEmbedCache::l1Put(const std::string& key, const std::vector<float>& vec) {
    std::lock_guard<std::mutex> lock(mu_);
    auto it = lru_map_.find(key);
    if (it != lru_map_.end()) {
        lru_list_.erase(it->second);
        lru_map_.erase(it);
    }
    lru_list_.push_front({key, vec});
    lru_map_[key] = lru_list_.begin();

    while (lru_map_.size() > config_.lru_capacity) {
        auto last = std::prev(lru_list_.end());
        lru_map_.erase(last->first);
        lru_list_.pop_back();
    }
}

std::optional<std::vector<float>> RedisEmbedCache::l1Get(const std::string& key) {
    std::lock_guard<std::mutex> lock(mu_);
    auto it = lru_map_.find(key);
    if (it == lru_map_.end()) return std::nullopt;
    // Move to front (most recently used)
    lru_list_.splice(lru_list_.begin(), lru_list_, it->second);
    return it->second->second;
}

// ── L2 Redis via Go proxy ───────────────────────────────────────────────────

#ifdef AIPR_HAS_CURL
static size_t writeCallback(char* ptr, size_t size, size_t nmemb, std::string* data) {
    data->append(ptr, size * nmemb);
    return size * nmemb;
}
#endif

std::optional<std::vector<float>> RedisEmbedCache::l2Get(
    const std::string& repo_id, const std::string& chunk_hash)
{
#ifdef AIPR_HAS_CURL
    CURL* curl = curl_easy_init();
    if (!curl) return std::nullopt;

    std::string url = effectiveGoServerUrl() +
        "/api/v1/engine/embed-cache/" + repo_id + "/" + chunk_hash;

    std::string response;
    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_TIMEOUT_MS, static_cast<long>(config_.timeout_ms));
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, writeCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response);
    curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);

    CURLcode res = curl_easy_perform(curl);
    long http_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK || http_code != 200 || response.empty()) {
        return std::nullopt;
    }

    try {
        auto j = nlohmann::json::parse(response);
        if (!j.contains("embedding") || !j["embedding"].is_array()) return std::nullopt;
        std::vector<float> vec;
        vec.reserve(j["embedding"].size());
        for (const auto& v : j["embedding"]) {
            vec.push_back(v.get<float>());
        }
        return vec;
    } catch (...) {
        return std::nullopt;
    }
#else
    (void)repo_id;
    (void)chunk_hash;
    return std::nullopt;
#endif
}

void RedisEmbedCache::l2Put(
    const std::string& repo_id,
    const std::string& chunk_hash,
    const std::vector<float>& vec)
{
#ifdef AIPR_HAS_CURL
    CURL* curl = curl_easy_init();
    if (!curl) return;

    std::string url = effectiveGoServerUrl() +
        "/api/v1/engine/embed-cache/" + repo_id + "/" + chunk_hash;

    nlohmann::json body;
    body["embedding"] = vec;
    std::string payload = body.dump();

    struct curl_slist* headers = nullptr;
    headers = curl_slist_append(headers, "Content-Type: application/json");

    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_CUSTOMREQUEST, "PUT");
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, payload.c_str());
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT_MS, static_cast<long>(config_.timeout_ms));
    curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);

    curl_easy_perform(curl);
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);
#else
    (void)repo_id;
    (void)chunk_hash;
    (void)vec;
#endif
}

// ── Public interface ────────────────────────────────────────────────────────

std::optional<std::vector<float>> RedisEmbedCache::get(
    const std::string& repo_id, const std::string& chunk_hash)
{
    std::string key = repo_id + ":" + chunk_hash;

    // L1 check
    auto l1 = l1Get(key);
    if (l1.has_value()) {
        std::lock_guard<std::mutex> lock(mu_);
        stats_.l1_hits++;
        return l1;
    }
    {
        std::lock_guard<std::mutex> lock(mu_);
        stats_.l1_misses++;
    }

    // L2 check
    auto l2 = l2Get(repo_id, chunk_hash);
    if (l2.has_value()) {
        l1Put(key, l2.value());
        std::lock_guard<std::mutex> lock(mu_);
        stats_.l2_hits++;
        return l2;
    }
    {
        std::lock_guard<std::mutex> lock(mu_);
        stats_.l2_misses++;
    }

    return std::nullopt;
}

void RedisEmbedCache::put(
    const std::string& repo_id,
    const std::string& chunk_hash,
    const std::vector<float>& embedding)
{
    std::string key = repo_id + ":" + chunk_hash;
    l1Put(key, embedding);
    l2Put(repo_id, chunk_hash, embedding);
}

RedisEmbedCache::Stats RedisEmbedCache::getStats() const {
    std::lock_guard<std::mutex> lock(mu_);
    Stats s = stats_;
    s.l1_size = lru_map_.size();
    return s;
}

void RedisEmbedCache::clearL1() {
    std::lock_guard<std::mutex> lock(mu_);
    lru_list_.clear();
    lru_map_.clear();
}

} // namespace aipr
