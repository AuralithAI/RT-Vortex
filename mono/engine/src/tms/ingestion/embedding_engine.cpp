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

namespace aipr::tms {

// =============================================================================
// Constructor / Destructor
// =============================================================================

EmbeddingEngine::EmbeddingEngine(const EmbeddingConfig& config)
    : config_(config)
    , initialized_(false) {
    
    // Initialize cache with max size
    // In production, could be backed by disk storage
}

EmbeddingEngine::~EmbeddingEngine() {
    shutdown();
}

// =============================================================================
// Initialization
// =============================================================================

bool EmbeddingEngine::initialize() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (initialized_) {
        return true;
    }
    
    try {
        switch (config_.provider) {
            case EmbeddingProvider::HTTP_API:
                initialized_ = initializeHttpProvider();
                break;
                
            case EmbeddingProvider::ONNX_RUNTIME:
                initialized_ = initializeOnnxProvider();
                break;
                
            case EmbeddingProvider::SENTENCE_TRANSFORMERS:
                initialized_ = initializeSentenceTransformers();
                break;
                
            case EmbeddingProvider::LOCAL_MOCK:
            default:
                initialized_ = initializeLocalMock();
                break;
        }
        
        if (initialized_) {
            stats_.initialized = true;
        }
        
        return initialized_;
        
    } catch (const std::exception& e) {
        last_error_ = std::string("Initialization failed: ") + e.what();
        return false;
    }
}

void EmbeddingEngine::shutdown() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    // Cleanup provider-specific resources
    http_client_.reset();
    onnx_session_.reset();
    
    initialized_ = false;
}

// =============================================================================
// Embedding Operations
// =============================================================================

std::vector<float> EmbeddingEngine::embed(const std::string& text) {
    auto result = embedBatch({text});
    if (result.empty() || result[0].empty()) {
        return std::vector<float>(config_.dimension, 0.0f);
    }
    return result[0];
}

std::vector<float> EmbeddingEngine::embedCode(const std::string& code, const std::string& language) {
    // Add language-specific prefix for better code embeddings
    std::string prefixed;
    if (config_.use_language_prefix && !language.empty()) {
        prefixed = "[" + language + "] " + code;
    } else {
        prefixed = code;
    }
    
    return embed(prefixed);
}

std::vector<float> EmbeddingEngine::embedQuery(const std::string& query) {
    // Queries may need different handling
    std::string prefixed;
    if (config_.use_query_prefix) {
        prefixed = "Query: " + query;
    } else {
        prefixed = query;
    }
    
    return embed(prefixed);
}

std::vector<std::vector<float>> EmbeddingEngine::embedBatch(const std::vector<std::string>& texts) {
    auto start = std::chrono::steady_clock::now();
    
    std::vector<std::vector<float>> results(texts.size());
    std::vector<size_t> uncached_indices;
    std::vector<std::string> uncached_texts;
    
    // Check cache first
    if (config_.enable_cache) {
        std::lock_guard<std::mutex> lock(cache_mutex_);
        
        for (size_t i = 0; i < texts.size(); ++i) {
            std::string key = computeCacheKey(texts[i]);
            auto it = embedding_cache_.find(key);
            if (it != embedding_cache_.end()) {
                results[i] = it->second;
                stats_.cache_hits++;
            } else {
                uncached_indices.push_back(i);
                uncached_texts.push_back(texts[i]);
                stats_.cache_misses++;
            }
        }
    } else {
        for (size_t i = 0; i < texts.size(); ++i) {
            uncached_indices.push_back(i);
            uncached_texts.push_back(texts[i]);
        }
    }
    
    // Embed uncached texts
    if (!uncached_texts.empty()) {
        std::vector<std::vector<float>> new_embeddings;
        
        // Process in batches
        for (size_t offset = 0; offset < uncached_texts.size(); offset += config_.batch_size) {
            size_t end = std::min(offset + static_cast<size_t>(config_.batch_size), uncached_texts.size());
            std::vector<std::string> batch(uncached_texts.begin() + offset, 
                                            uncached_texts.begin() + end);
            
            std::vector<std::vector<float>> batch_embeddings;
            
            switch (config_.provider) {
                case EmbeddingProvider::HTTP_API:
                    batch_embeddings = embedBatchHttp(batch);
                    break;
                    
                case EmbeddingProvider::ONNX_RUNTIME:
                    batch_embeddings = embedBatchOnnx(batch);
                    break;
                    
                case EmbeddingProvider::SENTENCE_TRANSFORMERS:
                    batch_embeddings = embedBatchSentenceTransformers(batch);
                    break;
                    
                case EmbeddingProvider::LOCAL_MOCK:
                default:
                    batch_embeddings = embedBatchMock(batch);
                    break;
            }
            
            for (auto& emb : batch_embeddings) {
                new_embeddings.push_back(std::move(emb));
            }
        }
        
        // Store in cache and results
        if (config_.enable_cache) {
            std::lock_guard<std::mutex> lock(cache_mutex_);
            
            for (size_t i = 0; i < new_embeddings.size(); ++i) {
                size_t result_idx = uncached_indices[i];
                results[result_idx] = new_embeddings[i];
                
                std::string key = computeCacheKey(uncached_texts[i]);
                embedding_cache_[key] = new_embeddings[i];
                
                // Evict if cache too large
                if (embedding_cache_.size() > config_.cache_size) {
                    evictOldestCacheEntry();
                }
            }
        } else {
            for (size_t i = 0; i < new_embeddings.size(); ++i) {
                results[uncached_indices[i]] = new_embeddings[i];
            }
        }
    }
    
    // Update stats
    auto end = std::chrono::steady_clock::now();
    auto duration = std::chrono::duration_cast<std::chrono::microseconds>(end - start);
    
    stats_.total_embeddings += texts.size();
    stats_.total_batches++;
    double n = static_cast<double>(stats_.total_batches);
    stats_.avg_batch_time_us = (stats_.avg_batch_time_us * (n - 1) + duration.count()) / n;
    
    return results;
}

std::vector<std::vector<float>> EmbeddingEngine::embedCodeBatch(
    const std::vector<std::string>& codes,
    const std::vector<std::string>& languages
) {
    std::vector<std::string> prefixed;
    prefixed.reserve(codes.size());
    
    for (size_t i = 0; i < codes.size(); ++i) {
        std::string lang = (i < languages.size()) ? languages[i] : "";
        if (config_.use_language_prefix && !lang.empty()) {
            prefixed.push_back("[" + lang + "] " + codes[i]);
        } else {
            prefixed.push_back(codes[i]);
        }
    }
    
    return embedBatch(prefixed);
}

// =============================================================================
// Provider Initialization
// =============================================================================

bool EmbeddingEngine::initializeHttpProvider() {
    // In production, initialize CURL or other HTTP client
    // For now, just validate config
    if (config_.api_endpoint.empty()) {
        last_error_ = "HTTP API endpoint not specified";
        return false;
    }
    
    // Could do a test call here
    return true;
}

bool EmbeddingEngine::initializeOnnxProvider() {
#ifdef AIPR_HAS_ONNX
    // Initialize ONNX Runtime session
    if (config_.model_path.empty()) {
        last_error_ = "ONNX model path not specified";
        return false;
    }
    
    // Check if model file exists
    std::ifstream f(config_.model_path);
    if (!f.good()) {
        last_error_ = "ONNX model file not found: " + config_.model_path;
        return false;
    }
    
    // In production, actually create ONNX session
    return true;
#else
    last_error_ = "ONNX Runtime not available";
    return false;
#endif
}

bool EmbeddingEngine::initializeSentenceTransformers() {
    // In production, initialize Python embedding via pybind or subprocess
    if (config_.model_name.empty()) {
        last_error_ = "SentenceTransformers model name not specified";
        return false;
    }
    
    return true;
}

bool EmbeddingEngine::initializeLocalMock() {
    // Mock provider always succeeds
    return true;
}

// =============================================================================
// Batch Embedding Implementations
// =============================================================================

std::vector<std::vector<float>> EmbeddingEngine::embedBatchHttp(
    const std::vector<std::string>& texts
) {
    // In production, make HTTP API call
    // For now, fall back to mock
    return embedBatchMock(texts);
}

std::vector<std::vector<float>> EmbeddingEngine::embedBatchOnnx(
    const std::vector<std::string>& texts
) {
#ifdef AIPR_HAS_ONNX
    // In production, run ONNX inference
    // For now, fall back to mock
#endif
    return embedBatchMock(texts);
}

std::vector<std::vector<float>> EmbeddingEngine::embedBatchSentenceTransformers(
    const std::vector<std::string>& texts
) {
    // In production, call Python SentenceTransformers
    // For now, fall back to mock
    return embedBatchMock(texts);
}

std::vector<std::vector<float>> EmbeddingEngine::embedBatchMock(
    const std::vector<std::string>& texts
) {
    std::vector<std::vector<float>> results;
    results.reserve(texts.size());
    
    for (const auto& text : texts) {
        results.push_back(generateMockEmbedding(text));
    }
    
    return results;
}

std::vector<float> EmbeddingEngine::generateMockEmbedding(const std::string& text) {
    // Generate deterministic embedding based on text hash
    // This ensures same text always gets same embedding
    std::hash<std::string> hasher;
    size_t hash = hasher(text);
    
    std::mt19937 gen(hash);
    std::normal_distribution<float> dist(0.0f, 1.0f);
    
    std::vector<float> embedding(config_.dimension);
    float norm = 0.0f;
    
    for (int i = 0; i < config_.dimension; ++i) {
        embedding[i] = dist(gen);
        norm += embedding[i] * embedding[i];
    }
    
    // Normalize
    norm = std::sqrt(norm);
    if (norm > 0) {
        for (float& v : embedding) {
            v /= norm;
        }
    }
    
    return embedding;
}

// =============================================================================
// Cache Management
// =============================================================================

std::string EmbeddingEngine::computeCacheKey(const std::string& text) {
    // Simple hash-based key
    std::hash<std::string> hasher;
    return std::to_string(hasher(text));
}

void EmbeddingEngine::evictOldestCacheEntry() {
    // Simple eviction - remove random entry
    // In production, use LRU or similar
    if (!embedding_cache_.empty()) {
        embedding_cache_.erase(embedding_cache_.begin());
    }
}

void EmbeddingEngine::clearCache() {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    embedding_cache_.clear();
}

void EmbeddingEngine::warmCache(const std::vector<std::string>& texts) {
    // Pre-compute embeddings for given texts
    embedBatch(texts);
}

// =============================================================================
// Similarity Operations
// =============================================================================

float EmbeddingEngine::cosineSimilarity(
    const std::vector<float>& a,
    const std::vector<float>& b
) {
    if (a.size() != b.size()) return 0.0f;
    
    float dot = 0.0f;
    float norm_a = 0.0f;
    float norm_b = 0.0f;
    
    for (size_t i = 0; i < a.size(); ++i) {
        dot += a[i] * b[i];
        norm_a += a[i] * a[i];
        norm_b += b[i] * b[i];
    }
    
    float denom = std::sqrt(norm_a) * std::sqrt(norm_b);
    if (denom < 1e-10f) return 0.0f;
    
    return dot / denom;
}

float EmbeddingEngine::l2Distance(
    const std::vector<float>& a,
    const std::vector<float>& b
) {
    if (a.size() != b.size()) return std::numeric_limits<float>::max();
    
    float sum = 0.0f;
    for (size_t i = 0; i < a.size(); ++i) {
        float diff = a[i] - b[i];
        sum += diff * diff;
    }
    
    return std::sqrt(sum);
}

std::vector<std::pair<int, float>> EmbeddingEngine::findMostSimilar(
    const std::vector<float>& query,
    const std::vector<std::vector<float>>& corpus,
    int top_k
) {
    std::vector<std::pair<int, float>> similarities;
    similarities.reserve(corpus.size());
    
    for (size_t i = 0; i < corpus.size(); ++i) {
        float sim = cosineSimilarity(query, corpus[i]);
        similarities.emplace_back(static_cast<int>(i), sim);
    }
    
    // Partial sort for top k
    if (static_cast<size_t>(top_k) < similarities.size()) {
        std::partial_sort(similarities.begin(),
                          similarities.begin() + top_k,
                          similarities.end(),
                          [](const auto& a, const auto& b) { return a.second > b.second; });
        similarities.resize(top_k);
    } else {
        std::sort(similarities.begin(), similarities.end(),
                  [](const auto& a, const auto& b) { return a.second > b.second; });
    }
    
    return similarities;
}

// =============================================================================
// Statistics
// =============================================================================

EmbeddingEngine::Stats EmbeddingEngine::getStats() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return stats_;
}

void EmbeddingEngine::resetStats() {
    std::lock_guard<std::mutex> lock(mutex_);
    stats_ = Stats{};
    stats_.initialized = initialized_;
}

// =============================================================================
// Validation
// =============================================================================

bool EmbeddingEngine::isReady() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return initialized_;
}

std::string EmbeddingEngine::getLastError() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return last_error_;
}

int EmbeddingEngine::getDimension() const {
    return config_.dimension;
}

EmbeddingProvider EmbeddingEngine::getProvider() const {
    return config_.provider;
}

} // namespace aipr::tms
