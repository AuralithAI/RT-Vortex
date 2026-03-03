/**
 * TMS Embedding Engine Implementation
 * 
 * Wraps various embedding providers (HTTP API, ONNX, SentenceTransformers)
 * with caching and batch optimization.
 */

#include "tms/embedding_engine.h"
#include <algorithm>
#include <cmath>
#include <fstream>
#include <sstream>
#include <random>
#include <functional>
#include <unordered_map>

namespace aipr::tms {

// =============================================================================
// BackendImpl (pimpl)
// =============================================================================

class EmbeddingEngine::BackendImpl {
public:
    explicit BackendImpl(const EmbeddingConfig& config)
        : config_(config), initialized_(false) {}

    ~BackendImpl() = default;

    bool initialize() {
        switch (config_.backend) {
            case EmbeddingBackend::HTTP_API:
                initialized_ = initializeHttp();
                break;
            case EmbeddingBackend::ONNX_RUNTIME:
                initialized_ = initializeOnnx();
                break;
            case EmbeddingBackend::SENTENCE_TRANSFORMERS:
                initialized_ = initializeSentenceTransformers();
                break;
            case EmbeddingBackend::MOCK:
            default:
                initialized_ = true;
                break;
        }
        return initialized_;
    }

    void shutdown() { initialized_ = false; }
    bool isInitialized() const { return initialized_; }

    std::vector<float> embed(const std::string& text) {
        auto results = embedBatch({text});
        if (results.empty() || results[0].empty()) {
            return std::vector<float>(config_.embedding_dimension, 0.0f);
        }
        return results[0];
    }

    std::vector<std::vector<float>> embedBatch(const std::vector<std::string>& texts) {
        switch (config_.backend) {
            case EmbeddingBackend::HTTP_API:
                return embedBatchHttp(texts);
            case EmbeddingBackend::ONNX_RUNTIME:
                return embedBatchOnnx(texts);
            case EmbeddingBackend::SENTENCE_TRANSFORMERS:
                return embedBatchSentenceTransformers(texts);
            case EmbeddingBackend::MOCK:
            default:
                return embedBatchMock(texts);
        }
    }

private:
    EmbeddingConfig config_;
    bool initialized_;

    bool initializeHttp() {
        return !config_.api_endpoint.empty();
    }

    bool initializeOnnx() {
        if (config_.onnx_model_path.empty()) return false;
        std::ifstream f(config_.onnx_model_path);
        return f.good();
    }

    bool initializeSentenceTransformers() {
        return !config_.model_name.empty();
    }

    // In production these would call real backends. For now fall back to mock.
    std::vector<std::vector<float>> embedBatchHttp(const std::vector<std::string>& texts) {
        return embedBatchMock(texts);
    }
    std::vector<std::vector<float>> embedBatchOnnx(const std::vector<std::string>& texts) {
        return embedBatchMock(texts);
    }
    std::vector<std::vector<float>> embedBatchSentenceTransformers(const std::vector<std::string>& texts) {
        return embedBatchMock(texts);
    }

    std::vector<std::vector<float>> embedBatchMock(const std::vector<std::string>& texts) {
        std::vector<std::vector<float>> results;
        results.reserve(texts.size());
        for (const auto& text : texts) {
            results.push_back(generateMockEmbedding(text));
        }
        return results;
    }

    std::vector<float> generateMockEmbedding(const std::string& text) {
        std::hash<std::string> hasher;
        size_t hash = hasher(text);
        std::mt19937 gen(static_cast<unsigned>(hash));
        std::normal_distribution<float> dist(0.0f, 1.0f);

        int dim = static_cast<int>(config_.embedding_dimension);
        std::vector<float> embedding(dim);
        float norm = 0.0f;
        for (int i = 0; i < dim; ++i) {
            embedding[i] = dist(gen);
            norm += embedding[i] * embedding[i];
        }
        norm = std::sqrt(norm);
        if (norm > 0) {
            for (float& v : embedding) v /= norm;
        }
        return embedding;
    }
};

// =============================================================================
// EmbeddingCache (pimpl)
// =============================================================================

class EmbeddingEngine::EmbeddingCache {
public:
    explicit EmbeddingCache(size_t max_size) : max_size_(max_size) {}

    std::optional<std::vector<float>> get(const std::string& key) {
        auto it = cache_.find(key);
        if (it != cache_.end()) { hits_++; return it->second; }
        misses_++;
        return std::nullopt;
    }

    void put(const std::string& key, const std::vector<float>& value) {
        if (cache_.size() >= max_size_ && cache_.find(key) == cache_.end()) {
            cache_.erase(cache_.begin());
        }
        cache_[key] = value;
    }

    void clear() { cache_.clear(); hits_ = 0; misses_ = 0; }
    size_t size() const { return cache_.size(); }
    size_t hits() const { return hits_; }
    size_t misses() const { return misses_; }

private:
    std::unordered_map<std::string, std::vector<float>> cache_;
    size_t max_size_;
    size_t hits_ = 0;
    size_t misses_ = 0;
};

// =============================================================================
// RateLimiter (pimpl)
// =============================================================================

class EmbeddingEngine::RateLimiter {
public:
    RateLimiter(int max_rpm, int max_tpm)
        : max_rpm_(max_rpm), max_tpm_(max_tpm) {}
    bool tryAcquire(int /*tokens*/) { return true; }
private:
    int max_rpm_;
    int max_tpm_;
};

// =============================================================================
// Constructor / Destructor
// =============================================================================

EmbeddingEngine::EmbeddingEngine(const EmbeddingConfig& config)
    : config_(config) {
    backend_ = std::make_unique<BackendImpl>(config);
    cache_ = std::make_unique<EmbeddingCache>(config.cache_size);
    rate_limiter_ = std::make_unique<RateLimiter>(
        config.max_requests_per_minute, config.max_tokens_per_minute);
    backend_->initialize();
}

EmbeddingEngine::~EmbeddingEngine() = default;

// =============================================================================
// Main Embedding Interface
// =============================================================================

EmbeddingResult EmbeddingEngine::embed(const std::string& text) {
    auto start = std::chrono::steady_clock::now();
    EmbeddingResult result;

    if (config_.enable_cache) {
        std::string hash = computeHash(text);
        auto cached = cache_->get(hash);
        if (cached) {
            result.embedding = *cached;
            result.from_cache = true;
            result.success = true;
            auto end_t = std::chrono::steady_clock::now();
            result.computation_time = std::chrono::duration_cast<std::chrono::microseconds>(end_t - start);
            updateStats(result);
            return result;
        }
    }

    try {
        std::string processed = config_.normalize_code ? normalizeCode(text) : text;
        result.embedding = backend_->embed(processed);
        result.success = true;
        if (config_.enable_cache) {
            cache_->put(computeHash(text), result.embedding);
        }
    } catch (const std::exception& e) {
        result.error = e.what();
        result.success = false;
    }

    auto end_t = std::chrono::steady_clock::now();
    result.computation_time = std::chrono::duration_cast<std::chrono::microseconds>(end_t - start);
    updateStats(result);
    return result;
}

EmbeddingResult EmbeddingEngine::embedCode(const CodeChunk& chunk) {
    std::string input = prepareCodeInput(chunk);
    return embed(input);
}

BatchEmbeddingResult EmbeddingEngine::embedBatch(
    const std::vector<std::string>& texts,
    EmbeddingProgressCallback progress
) {
    auto start = std::chrono::steady_clock::now();
    BatchEmbeddingResult result;
    result.embeddings.resize(texts.size());
    result.tokens_used.resize(texts.size(), 0);
    result.from_cache.resize(texts.size(), false);
    result.errors.resize(texts.size());

    std::vector<size_t> uncached_indices;
    std::vector<std::string> uncached_texts;

    if (config_.enable_cache) {
        for (size_t i = 0; i < texts.size(); ++i) {
            std::string hash = computeHash(texts[i]);
            auto cached = cache_->get(hash);
            if (cached) {
                result.embeddings[i] = *cached;
                result.from_cache[i] = true;
                result.successful_count++;
            } else {
                uncached_indices.push_back(i);
                uncached_texts.push_back(texts[i]);
            }
        }
    } else {
        for (size_t i = 0; i < texts.size(); ++i) {
            uncached_indices.push_back(i);
            uncached_texts.push_back(texts[i]);
        }
    }

    for (size_t offset = 0; offset < uncached_texts.size();
         offset += static_cast<size_t>(config_.batch_size)) {
        size_t end_idx = std::min(offset + static_cast<size_t>(config_.batch_size),
                                  uncached_texts.size());
        std::vector<std::string> batch(uncached_texts.begin() + offset,
                                        uncached_texts.begin() + end_idx);
        try {
            auto batch_embs = backend_->embedBatch(batch);
            for (size_t j = 0; j < batch_embs.size(); ++j) {
                size_t orig_idx = uncached_indices[offset + j];
                result.embeddings[orig_idx] = batch_embs[j];
                result.successful_count++;
                if (config_.enable_cache) {
                    cache_->put(computeHash(uncached_texts[offset + j]), batch_embs[j]);
                }
            }
        } catch (const std::exception& e) {
            for (size_t j = 0; j < batch.size(); ++j) {
                size_t orig_idx = uncached_indices[offset + j];
                result.errors[orig_idx] = e.what();
                result.failed_count++;
            }
        }

        if (progress) {
            int completed = static_cast<int>(std::min(offset + batch.size(), uncached_texts.size()));
            progress(completed, static_cast<int>(uncached_texts.size()), "Embedding...");
        }
    }

    auto end_t = std::chrono::steady_clock::now();
    result.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_t - start);
    return result;
}

BatchEmbeddingResult EmbeddingEngine::embedChunks(
    const std::vector<CodeChunk>& chunks,
    EmbeddingProgressCallback progress
) {
    std::vector<std::string> texts;
    texts.reserve(chunks.size());
    for (const auto& chunk : chunks) {
        texts.push_back(prepareCodeInput(chunk));
    }
    return embedBatch(texts, progress);
}

// =============================================================================
// Cache Management
// =============================================================================

std::optional<std::vector<float>> EmbeddingEngine::getCached(const std::string& content_hash) {
    return cache_->get(content_hash);
}

void EmbeddingEngine::clearCache() { cache_->clear(); }
void EmbeddingEngine::saveCache() { /* TODO: persist cache to disk */ }
void EmbeddingEngine::loadCache() { /* TODO: load cache from disk */ }

EmbeddingEngine::CacheStats EmbeddingEngine::getCacheStats() const {
    CacheStats cs;
    cs.size = cache_->size();
    cs.hits = cache_->hits();
    cs.misses = cache_->misses();
    size_t total = cs.hits + cs.misses;
    cs.hit_rate = total > 0 ? static_cast<double>(cs.hits) / total : 0.0;
    return cs;
}

// =============================================================================
// Configuration
// =============================================================================

void EmbeddingEngine::setApiKey(const std::string& key) { config_.api_key = key; }
void EmbeddingEngine::setEndpoint(const std::string& endpoint) { config_.api_endpoint = endpoint; }

// =============================================================================
// Statistics
// =============================================================================

EmbeddingEngine::Stats EmbeddingEngine::getStats() const {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    return stats_;
}

void EmbeddingEngine::resetStats() {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    stats_ = Stats{};
}

// =============================================================================
// Helpers
// =============================================================================

std::string EmbeddingEngine::prepareCodeInput(const CodeChunk& chunk) {
    std::ostringstream oss;
    if (!chunk.language.empty()) oss << "[" << chunk.language << "] ";
    if (!chunk.name.empty()) oss << chunk.name << ": ";
    oss << chunk.content;
    return oss.str();
}

std::string EmbeddingEngine::normalizeCode(const std::string& code) {
    std::string result;
    result.reserve(code.size());
    bool last_ws = false;
    for (char c : code) {
        if (std::isspace(static_cast<unsigned char>(c))) {
            if (!last_ws) { result += ' '; last_ws = true; }
        } else {
            result += c;
            last_ws = false;
        }
    }
    while (!result.empty() && result.back() == ' ') result.pop_back();
    return result;
}

std::string EmbeddingEngine::computeHash(const std::string& content) {
    std::hash<std::string> hasher;
    return std::to_string(hasher(content));
}

void EmbeddingEngine::updateStats(const EmbeddingResult& result) {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    stats_.total_embeddings++;
    stats_.total_tokens += result.tokens_used;
    if (!result.success) {
        stats_.api_errors++;
    } else {
        double n = static_cast<double>(stats_.total_embeddings);
        double ms = static_cast<double>(result.computation_time.count()) / 1000.0;
        stats_.avg_embedding_time_ms = (stats_.avg_embedding_time_ms * (n - 1) + ms) / n;
    }
}

// =============================================================================
// EmbeddingIngestor Implementation (Stubs)
// =============================================================================

EmbeddingIngestor::EmbeddingIngestor(
    EmbeddingEngine& embedding_engine,
    LTMFaiss& ltm,
    MTMGraph* mtm)
    : embedding_engine_(embedding_engine)
    , ltm_(ltm)
    , mtm_(mtm)
    , config_()
{
}

EmbeddingIngestor::EmbeddingIngestor(
    EmbeddingEngine& embedding_engine,
    LTMFaiss& ltm,
    MTMGraph* mtm,
    const Config& config)
    : embedding_engine_(embedding_engine)
    , ltm_(ltm)
    , mtm_(mtm)
    , config_(config)
{
}

EmbeddingIngestor::~EmbeddingIngestor() = default;

EmbeddingIngestor::IngestResult EmbeddingIngestor::ingestRepository(
    const std::string& repo_path,
    const std::string& repo_id,
    EmbeddingProgressCallback progress)
{
    (void)repo_path;
    (void)repo_id;
    (void)progress;
    // TODO: Implement full ingestion pipeline
    return IngestResult{};
}

EmbeddingIngestor::IngestResult EmbeddingIngestor::ingestChunks(
    const std::string& repo_id,
    const std::vector<CodeChunk>& chunks,
    EmbeddingProgressCallback progress)
{
    (void)repo_id;
    (void)chunks;
    (void)progress;
    // TODO: Implement chunk ingestion
    return IngestResult{};
}

EmbeddingIngestor::IngestResult EmbeddingIngestor::ingestChunksWithEmbeddings(
    const std::string& repo_id,
    const std::vector<CodeChunk>& chunks,
    const std::vector<std::vector<float>>& embeddings)
{
    (void)repo_id;
    (void)chunks;
    (void)embeddings;
    // TODO: Implement pre-embedded chunk ingestion
    return IngestResult{};
}

} // namespace aipr::tms
