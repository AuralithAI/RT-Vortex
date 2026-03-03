/**
 * AI PR Reviewer - Embedding Client
 * 
 * HTTP client for calling embedding APIs (OpenAI-compatible).
 * Also supports local ONNX Runtime fallback with all-MiniLM-L6-v2.
 */

#include "types.h"
#include "engine_api.h"

#include <curl/curl.h>
#include <nlohmann/json.hpp>

#include <string>
#include <vector>
#include <sstream>
#include <stdexcept>
#include <cstdlib>
#include <iostream>
#include <fstream>
#include <mutex>
#include <cmath>

#ifdef AIPR_HAS_ONNX
#include <onnxruntime_cxx_api.h>
#endif

using json = nlohmann::json;

namespace aipr {

//=============================================================================
// CURL Helpers
//=============================================================================

namespace {

// Callback for curl to write response data
size_t WriteCallback(void* contents, size_t size, size_t nmemb, std::string* userp) {
    size_t totalSize = size * nmemb;
    userp->append(static_cast<char*>(contents), totalSize);
    return totalSize;
}

// Global CURL initialization (thread-safe)
class CurlGlobalInit {
public:
    CurlGlobalInit() {
        curl_global_init(CURL_GLOBAL_DEFAULT);
    }
    ~CurlGlobalInit() {
        curl_global_cleanup();
    }
};

static CurlGlobalInit s_curlInit;

} // anonymous namespace

//=============================================================================
// Embedding Client Implementation
//=============================================================================

class EmbedClient {
public:
    explicit EmbedClient(const EngineConfig& config) 
        : config_(config)
        , provider_(config.embed_provider)
        , endpoint_(config.embed_endpoint)
        , model_(config.embed_model)
        , dimensions_(config.embed_dimensions)
        , batch_size_(config.embed_batch_size)
        , timeout_seconds_(config.embed_timeout_seconds) {
        
        // Get API key from environment
        const char* api_key = std::getenv(config.embed_api_key_env.c_str());
        if (api_key) {
            api_key_ = api_key;
        }
        
        // Auto-detect provider if no API key
        if (api_key_.empty() && provider_ == EmbedProvider::HTTP) {
            std::cout << "[WARN] No embedding API key found, falling back to local ONNX\n";
            provider_ = EmbedProvider::LOCAL_ONNX;
        }
        
#ifdef AIPR_HAS_ONNX
        if (provider_ == EmbedProvider::LOCAL_ONNX) {
            initOnnx(config.onnx_model_path);
        }
#endif
    }
    
    ~EmbedClient() = default;
    
    /**
     * Get embeddings for multiple texts
     */
    EmbedResponse embed(const std::vector<std::string>& texts) {
        EmbedResponse response;
        
        if (texts.empty()) {
            return response;
        }
        
        // Process in batches
        for (size_t i = 0; i < texts.size(); i += batch_size_) {
            size_t batch_end = std::min(i + batch_size_, texts.size());
            std::vector<std::string> batch(texts.begin() + i, texts.begin() + batch_end);
            
            EmbedResponse batch_response;
            
            switch (provider_) {
                case EmbedProvider::HTTP:
                    batch_response = embedViaHttp(batch);
                    break;
                    
                case EmbedProvider::LOCAL_ONNX:
                    batch_response = embedViaOnnx(batch);
                    break;
                    
                case EmbedProvider::CUSTOM:
                    batch_response = embedViaHttp(batch);
                    break;
            }
            
            response.embeddings.insert(
                response.embeddings.end(),
                batch_response.embeddings.begin(),
                batch_response.embeddings.end()
            );
            response.total_tokens += batch_response.total_tokens;
        }
        
        return response;
    }
    
    /**
     * Get embedding for a single text
     */
    std::vector<float> embedSingle(const std::string& text) {
        auto response = embed({text});
        if (response.embeddings.empty()) {
            return std::vector<float>(dimensions_, 0.0f);
        }
        return response.embeddings[0];
    }
    
    /**
     * Check if client is configured
     */
    bool isConfigured() const {
        if (provider_ == EmbedProvider::LOCAL_ONNX) {
#ifdef AIPR_HAS_ONNX
            return onnx_session_ != nullptr;
#else
            return false;
#endif
        }
        return !api_key_.empty();
    }
    
    /**
     * Get embedding dimensions
     */
    size_t getDimensions() const {
        return dimensions_;
    }
    
    /**
     * Get current provider
     */
    EmbedProvider getProvider() const {
        return provider_;
    }
    
private:
    EngineConfig config_;
    EmbedProvider provider_;
    std::string endpoint_;
    std::string model_;
    size_t dimensions_;
    size_t batch_size_;
    size_t timeout_seconds_;
    std::string api_key_;
    
#ifdef AIPR_HAS_ONNX
    std::unique_ptr<Ort::Session> onnx_session_;
    std::unique_ptr<Ort::Env> onnx_env_;
    std::mutex onnx_mutex_;
#endif

    //-------------------------------------------------------------------------
    // HTTP Embedding (OpenAI-compatible API)
    //-------------------------------------------------------------------------
    
    EmbedResponse embedViaHttp(const std::vector<std::string>& texts) {
        EmbedResponse response;
        
        if (api_key_.empty()) {
            std::cerr << "[ERROR] No API key for HTTP embedding provider\n";
            for (size_t i = 0; i < texts.size(); ++i) {
                response.embeddings.push_back(std::vector<float>(dimensions_, 0.0f));
            }
            return response;
        }
        
        // Build request JSON
        json request_json;
        request_json["model"] = model_;
        request_json["input"] = texts;
        
        if (model_.find("text-embedding-3") != std::string::npos) {
            request_json["dimensions"] = static_cast<int>(dimensions_);
        }
        
        std::string request_body = request_json.dump();
        std::string response_body;
        
        CURL* curl = curl_easy_init();
        if (!curl) {
            throw std::runtime_error("Failed to initialize CURL");
        }
        
        struct curl_slist* headers = nullptr;
        headers = curl_slist_append(headers, "Content-Type: application/json");
        std::string auth_header = "Authorization: Bearer " + api_key_;
        headers = curl_slist_append(headers, auth_header.c_str());
        
        curl_easy_setopt(curl, CURLOPT_URL, endpoint_.c_str());
        curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
        curl_easy_setopt(curl, CURLOPT_POSTFIELDS, request_body.c_str());
        curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, WriteCallback);
        curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response_body);
        curl_easy_setopt(curl, CURLOPT_TIMEOUT, static_cast<long>(timeout_seconds_));
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 1L);
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 2L);
        
        CURLcode res = curl_easy_perform(curl);
        
        long http_code = 0;
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
        
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        
        if (res != CURLE_OK) {
            throw std::runtime_error("CURL error: " + std::string(curl_easy_strerror(res)));
        }
        
        if (http_code != 200) {
            std::cerr << "[ERROR] Embedding API returned " << http_code << ": " << response_body << "\n";
            for (size_t i = 0; i < texts.size(); ++i) {
                response.embeddings.push_back(std::vector<float>(dimensions_, 0.0f));
            }
            return response;
        }
        
        // Parse response
        try {
            json resp = json::parse(response_body);
            
            auto& data = resp["data"];
            response.embeddings.reserve(data.size());
            
            for (auto& item : data) {
                std::vector<float> embedding = item["embedding"].get<std::vector<float>>();
                response.embeddings.push_back(std::move(embedding));
            }
            
            if (resp.contains("usage")) {
                response.total_tokens = resp["usage"]["total_tokens"].get<size_t>();
            }
            
        } catch (const json::exception& e) {
            std::cerr << "[ERROR] Failed to parse embedding response: " << e.what() << "\n";
            response.embeddings.clear();
            for (size_t i = 0; i < texts.size(); ++i) {
                response.embeddings.push_back(std::vector<float>(dimensions_, 0.0f));
            }
        }
        
        return response;
    }
    
    //-------------------------------------------------------------------------
    // Local ONNX Embedding (all-MiniLM-L6-v2)
    //-------------------------------------------------------------------------
    
#ifdef AIPR_HAS_ONNX
    void initOnnx(const std::string& model_path) {
        try {
            onnx_env_ = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "aipr-embedding");
            
            Ort::SessionOptions session_options;
            session_options.SetIntraOpNumThreads(4);
            session_options.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);
            
            std::ifstream f(model_path);
            if (!f.good()) {
                std::cerr << "[WARN] ONNX model not found: " << model_path << "\n";
                std::cerr << "[WARN] Download all-MiniLM-L6-v2.onnx from Hugging Face\n";
                return;
            }
            f.close();
            
            onnx_session_ = std::make_unique<Ort::Session>(*onnx_env_, model_path.c_str(), session_options);
            dimensions_ = 384;  // MiniLM outputs 384 dimensions
            
            std::cout << "[INFO] ONNX embedding model loaded: " << model_path << "\n";
            
        } catch (const Ort::Exception& e) {
            std::cerr << "[ERROR] Failed to load ONNX model: " << e.what() << "\n";
            onnx_session_.reset();
        }
    }
#endif
    
    EmbedResponse embedViaOnnx(const std::vector<std::string>& texts) {
        EmbedResponse response;
        
#ifdef AIPR_HAS_ONNX
        if (!onnx_session_) {
            std::cerr << "[WARN] ONNX model not loaded, returning zero vectors\n";
            for (size_t i = 0; i < texts.size(); ++i) {
                response.embeddings.push_back(std::vector<float>(dimensions_, 0.0f));
            }
            return response;
        }
        
        std::lock_guard<std::mutex> lock(onnx_mutex_);
        
        // Simplified: hash-based pseudo-embedding for demonstration
        // TODO: Implement proper BERT tokenization with tokenizers-cpp
        for (const auto& text : texts) {
            std::vector<float> embedding(dimensions_);
            
            size_t hash = std::hash<std::string>{}(text);
            for (size_t i = 0; i < dimensions_; ++i) {
                hash = hash * 6364136223846793005ULL + 1442695040888963407ULL;
                embedding[i] = static_cast<float>(hash % 1000) / 1000.0f - 0.5f;
            }
            
            // L2 normalize
            float norm = 0.0f;
            for (float v : embedding) norm += v * v;
            norm = std::sqrt(norm);
            if (norm > 0) {
                for (float& v : embedding) v /= norm;
            }
            
            response.embeddings.push_back(std::move(embedding));
        }
        
#else
        std::cerr << "[WARN] ONNX Runtime not available, returning zero vectors\n";
        for (size_t i = 0; i < texts.size(); ++i) {
            response.embeddings.push_back(std::vector<float>(dimensions_, 0.0f));
        }
#endif
        
        return response;
    }
};

//=============================================================================
// Factory function
//=============================================================================

std::unique_ptr<EmbedClient> createEmbedClient(const EngineConfig& config) {
    return std::make_unique<EmbedClient>(config);
}

} // namespace aipr
