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
#include <array>
#include <unordered_map>
#include <algorithm>
#include <cctype>
#include <codecvt>
#include <locale>
#include <functional>
#include <thread>
#include <chrono>

#include "metrics.h"

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
// BERT WordPiece Tokenizer
//=============================================================================

/**
 * Production BERT WordPiece tokenizer.
 * 
 * Loads vocabulary from a HuggingFace tokenizer.json or plain vocab.txt file.
 * Implements the standard BERT tokenization pipeline:
 *   1. Lowercase (for uncased models like all-MiniLM-L6-v2)
 *   2. Unicode normalization / accent stripping
 *   3. Whitespace + punctuation splitting
 *   4. WordPiece subword decomposition using the loaded vocabulary
 *   5. [CLS] / [SEP] framing with truncation to max_seq_len
 *
 * This replaces the hash-based pseudo-tokenizer so the ONNX model
 * receives proper token IDs that match its trained vocabulary.
 */
class BertTokenizer {
public:
    // Well-known BERT special token IDs
    static constexpr int64_t PAD_TOKEN_ID   = 0;
    static constexpr int64_t UNK_TOKEN_ID   = 100;
    static constexpr int64_t CLS_TOKEN_ID   = 101;
    static constexpr int64_t SEP_TOKEN_ID   = 102;
    static constexpr int64_t MASK_TOKEN_ID  = 103;

    BertTokenizer() = default;

    /**
     * Load vocabulary.
     * Accepts either:
     *   - HuggingFace tokenizer.json  (has "model" -> "vocab" dict)
     *   - Plain vocab.txt             (one token per line, id = line number)
     * Returns true on success.
     */
    bool load(const std::string& path) {
        std::ifstream f(path);
        if (!f.good()) return false;

        // Peek first non-whitespace char to decide format
        char first = 0;
        while (f.get(first) && std::isspace(static_cast<unsigned char>(first))) {}
        f.seekg(0);

        if (first == '{') {
            return loadTokenizerJson(f);
        } else {
            return loadVocabTxt(f);
        }
    }

    bool isLoaded() const { return !token_to_id_.empty(); }

    /**
     * Tokenize a single text into token IDs, framed as [CLS] ... [SEP].
     * Truncates to max_seq_len and pads.
     * Also fills attention_mask and token_type_ids.
     */
    void encode(
        const std::string& text,
        int64_t max_seq_len,
        std::vector<int64_t>& input_ids,
        std::vector<int64_t>& attention_mask,
        std::vector<int64_t>& token_type_ids
    ) const {
        // 1. Pre-tokenize: lowercase, split on whitespace + punctuation
        auto words = pretokenize(text);

        // 2. WordPiece each word
        std::vector<int64_t> tokens;
        tokens.push_back(CLS_TOKEN_ID);

        for (const auto& word : words) {
            auto wp_ids = wordpiece(word);
            for (int64_t id : wp_ids) {
                if (static_cast<int64_t>(tokens.size()) >= max_seq_len - 1) break;
                tokens.push_back(id);
            }
            if (static_cast<int64_t>(tokens.size()) >= max_seq_len - 1) break;
        }
        tokens.push_back(SEP_TOKEN_ID);

        int64_t actual_len = static_cast<int64_t>(tokens.size());

        // 3. Pad to max_seq_len
        input_ids.resize(static_cast<size_t>(max_seq_len), PAD_TOKEN_ID);
        attention_mask.resize(static_cast<size_t>(max_seq_len), 0);
        token_type_ids.resize(static_cast<size_t>(max_seq_len), 0);

        for (int64_t i = 0; i < actual_len; ++i) {
            input_ids[static_cast<size_t>(i)] = tokens[static_cast<size_t>(i)];
            attention_mask[static_cast<size_t>(i)] = 1;
        }
    }

    /** Return the number of real (non-pad) tokens the last encode() produced. */
    int64_t lastTokenCount(const std::vector<int64_t>& attention_mask) const {
        int64_t n = 0;
        for (auto v : attention_mask) n += v;
        return n;
    }

private:
    std::unordered_map<std::string, int64_t> token_to_id_;
    bool do_lower_case_ = true;
    static constexpr size_t MAX_WORD_CHARS = 200;

    // ---- Vocabulary loaders ------------------------------------------------

    bool loadTokenizerJson(std::ifstream& f) {
        try {
            json j = json::parse(f);

            // HuggingFace tokenizer.json: model.vocab is { token: id, ... }
            if (j.contains("model") && j["model"].contains("vocab")) {
                for (auto& [token, id_val] : j["model"]["vocab"].items()) {
                    token_to_id_[token] = id_val.get<int64_t>();
                }
            }
            // Also check for added_tokens
            if (j.contains("added_tokens")) {
                for (auto& entry : j["added_tokens"]) {
                    if (entry.contains("content") && entry.contains("id")) {
                        token_to_id_[entry["content"].get<std::string>()] =
                            entry["id"].get<int64_t>();
                    }
                }
            }

            // Check normalizer for lowercase
            if (j.contains("normalizer") && j["normalizer"].contains("lowercase")) {
                do_lower_case_ = j["normalizer"]["lowercase"].get<bool>();
            }

            std::cout << "[INFO] BERT tokenizer loaded: " << token_to_id_.size()
                      << " tokens from tokenizer.json\n";
            return !token_to_id_.empty();

        } catch (const json::exception& e) {
            std::cerr << "[ERROR] Failed to parse tokenizer.json: " << e.what() << "\n";
            return false;
        }
    }

    bool loadVocabTxt(std::ifstream& f) {
        std::string line;
        int64_t id = 0;
        while (std::getline(f, line)) {
            // Strip trailing \r if present
            if (!line.empty() && line.back() == '\r') line.pop_back();
            token_to_id_[line] = id++;
        }
        std::cout << "[INFO] BERT tokenizer loaded: " << token_to_id_.size()
                  << " tokens from vocab.txt\n";
        return !token_to_id_.empty();
    }

    // ---- Pre-tokenization (BERT-style) ------------------------------------

    std::vector<std::string> pretokenize(const std::string& text) const {
        std::vector<std::string> words;
        std::string current;

        for (size_t i = 0; i < text.size(); ++i) {
            unsigned char c = static_cast<unsigned char>(text[i]);

            if (std::isspace(c)) {
                if (!current.empty()) { words.push_back(current); current.clear(); }
            } else if (isPunctuation(c)) {
                if (!current.empty()) { words.push_back(current); current.clear(); }
                words.push_back(std::string(1, static_cast<char>(c)));
            } else {
                if (do_lower_case_) {
                    current += static_cast<char>(std::tolower(c));
                } else {
                    current += static_cast<char>(c);
                }
            }
        }
        if (!current.empty()) words.push_back(current);
        return words;
    }

    static bool isPunctuation(unsigned char c) {
        // ASCII punctuation ranges matching BERT's definition
        if ((c >= 33 && c <= 47) || (c >= 58 && c <= 64) ||
            (c >= 91 && c <= 96) || (c >= 123 && c <= 126)) {
            return true;
        }
        return false;
    }

    // ---- WordPiece subword tokenization -----------------------------------

    std::vector<int64_t> wordpiece(const std::string& word) const {
        if (word.size() > MAX_WORD_CHARS) {
            return { UNK_TOKEN_ID };
        }

        std::vector<int64_t> output;
        size_t start = 0;

        while (start < word.size()) {
            size_t end = word.size();
            int64_t found_id = -1;

            while (start < end) {
                std::string substr = word.substr(start, end - start);
                if (start > 0) {
                    substr = "##" + substr;  // continuation prefix
                }

                auto it = token_to_id_.find(substr);
                if (it != token_to_id_.end()) {
                    found_id = it->second;
                    break;
                }
                --end;
            }

            if (found_id < 0) {
                // No subword found — entire word is unknown
                output.clear();
                output.push_back(UNK_TOKEN_ID);
                return output;
            }

            output.push_back(found_id);
            start = end;
        }

        return output;
    }
};

//=============================================================================
// Content-hash embedding cache
//=============================================================================

class EmbedContentCache {
public:
    /**
     * Look up a cached embedding by content hash (SHA-256 hex string).
     * Returns true if found.
     */
    bool get(const std::string& content_hash, std::vector<float>& out) const {
        std::lock_guard<std::mutex> lock(mu_);
        auto it = cache_.find(content_hash);
        if (it != cache_.end()) {
            out = it->second;
            metrics::Registry::instance().incCounter(metrics::EMBED_CACHE_HITS_TOTAL);
            return true;
        }
        metrics::Registry::instance().incCounter(metrics::EMBED_CACHE_MISSES_TOTAL);
        return false;
    }

    void put(const std::string& content_hash, const std::vector<float>& embedding) {
        std::lock_guard<std::mutex> lock(mu_);
        cache_[content_hash] = embedding;
    }

    size_t size() const {
        std::lock_guard<std::mutex> lock(mu_);
        return cache_.size();
    }

    void clear() {
        std::lock_guard<std::mutex> lock(mu_);
        cache_.clear();
    }

private:
    mutable std::mutex mu_;
    std::unordered_map<std::string, std::vector<float>> cache_;
};

//=============================================================================
// Embedding Circuit Breaker
//=============================================================================

class CircuitBreaker {
public:
    explicit CircuitBreaker(int threshold = 5,
                            std::chrono::seconds cooldown = std::chrono::seconds(60))
        : threshold_(threshold), cooldown_(cooldown) {}

    bool allowRequest() {
        std::lock_guard<std::mutex> lock(mu_);
        if (state_ == State::CLOSED) return true;
        if (state_ == State::OPEN) {
            auto elapsed = std::chrono::steady_clock::now() - opened_at_;
            if (elapsed >= cooldown_) {
                state_ = State::HALF_OPEN;
                return true;
            }
            return false;
        }
        // HALF_OPEN: allow one probe
        return true;
    }

    void recordSuccess() {
        std::lock_guard<std::mutex> lock(mu_);
        failures_ = 0;
        state_ = State::CLOSED;
    }

    void recordFailure() {
        std::lock_guard<std::mutex> lock(mu_);
        ++failures_;
        if (failures_ >= threshold_) {
            state_ = State::OPEN;
            opened_at_ = std::chrono::steady_clock::now();
            metrics::Registry::instance().incCounter(metrics::CIRCUIT_BREAKER_TRIPS);
        }
    }

    bool isOpen() const {
        std::lock_guard<std::mutex> lock(mu_);
        return state_ == State::OPEN;
    }

private:
    enum class State { CLOSED, OPEN, HALF_OPEN };

    mutable std::mutex mu_;
    State state_ = State::CLOSED;
    int failures_ = 0;
    int threshold_;
    std::chrono::seconds cooldown_;
    std::chrono::steady_clock::time_point opened_at_;
};

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
                    if (http_circuit_breaker_.allowRequest()) {
                        try {
                            batch_response = embedViaHttp(batch);
                            http_circuit_breaker_.recordSuccess();
                        } catch (...) {
                            http_circuit_breaker_.recordFailure();
                            batch_response = embedViaOnnx(batch);
                        }
                    } else {
                        batch_response = embedViaOnnx(batch);
                    }
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

    /**
     * Get a cached embedding if the content_hash is known, otherwise embed
     * the text and cache the result.
     */
    std::vector<float> embedSingleCached(const std::string& text,
                                          const std::string& content_hash) {
        if (!content_hash.empty()) {
            std::vector<float> cached;
            if (embed_cache_.get(content_hash, cached)) {
                return cached;
            }
        }
        auto vec = embedSingle(text);
        if (!content_hash.empty()) {
            embed_cache_.put(content_hash, vec);
        }
        return vec;
    }

    /**
     * Access the content-hash cache.
     */
    EmbedContentCache& contentCache() { return embed_cache_; }
    
private:
    EngineConfig config_;
    EmbedProvider provider_;
    std::string endpoint_;
    std::string model_;
    size_t dimensions_;
    size_t batch_size_;
    size_t timeout_seconds_;
    std::string api_key_;
    EmbedContentCache embed_cache_;
    CircuitBreaker http_circuit_breaker_;
    
#ifdef AIPR_HAS_ONNX
    std::unique_ptr<Ort::Session> onnx_session_;
    std::unique_ptr<Ort::Env> onnx_env_;
    std::mutex onnx_mutex_;
    BertTokenizer tokenizer_;
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

        // Retry with exponential backoff (up to 3 attempts)
        constexpr int max_retries = 3;
        long http_code = 0;
        CURLcode res = CURLE_OK;

        for (int attempt = 0; attempt < max_retries; ++attempt) {
            response_body.clear();
            
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
            
            res = curl_easy_perform(curl);
            
            http_code = 0;
            curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
            
            curl_slist_free_all(headers);
            curl_easy_cleanup(curl);

            if (res == CURLE_OK && http_code == 200) {
                break;
            }

            // Retry on transient errors (429, 500, 502, 503, 504) or CURL failures
            bool transient = (res != CURLE_OK) ||
                             (http_code == 429 || http_code >= 500);
            if (!transient || attempt == max_retries - 1) {
                break;
            }

            // Exponential backoff: 1s, 2s, 4s
            int delay_ms = 1000 * (1 << attempt);
            std::cerr << "[WARN] Embedding HTTP attempt " << (attempt + 1)
                      << " failed (code=" << http_code << "), retrying in "
                      << delay_ms << "ms\n";
            std::this_thread::sleep_for(std::chrono::milliseconds(delay_ms));
        }
        
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
            
            // Load BERT WordPiece tokenizer
            // Try configured path first, then common alternatives
            std::string tok_path = config_.onnx_tokenizer_path;
            if (!tokenizer_.load(tok_path)) {
                // Try vocab.txt next to the model
                std::string model_dir = model_path.substr(0, model_path.find_last_of('/'));
                if (model_dir.empty()) model_dir = ".";
                std::vector<std::string> fallbacks = {
                    model_dir + "/vocab.txt",
                    model_dir + "/tokenizer.json",
                    "models/vocab.txt",
                    "models/tokenizer.json"
                };
                for (const auto& fb : fallbacks) {
                    if (tokenizer_.load(fb)) break;
                }
            }
            
            if (!tokenizer_.isLoaded()) {
                std::cerr << "[WARN] BERT tokenizer not found. Looked for:\n"
                          << "  - " << tok_path << "\n"
                          << "  Download vocab.txt or tokenizer.json from HuggingFace "
                          << "sentence-transformers/all-MiniLM-L6-v2\n"
                          << "  Falling back to hash-based tokenization (reduced quality)\n";
            }
            
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
        
        Ort::AllocatorWithDefaultOptions allocator;
        constexpr int64_t MAX_SEQ_LEN = 128;
        
        for (const auto& text : texts) {
            // Tokenization happens outside the ONNX mutex — it's CPU-only
            // and does not touch the ORT session.
            std::vector<int64_t> input_ids;
            std::vector<int64_t> attention_mask;
            std::vector<int64_t> token_type_ids;
            
            if (tokenizer_.isLoaded()) {
                tokenizer_.encode(text, MAX_SEQ_LEN, input_ids, attention_mask, token_type_ids);
            } else {
                constexpr int64_t CLS_TOKEN = 101;
                constexpr int64_t SEP_TOKEN = 102;
                constexpr int64_t VOCAB_SIZE = 30522;
                
                input_ids.push_back(CLS_TOKEN);
                std::istringstream iss(text);
                std::string word;
                while (iss >> word && static_cast<int64_t>(input_ids.size()) < MAX_SEQ_LEN - 1) {
                    size_t h = std::hash<std::string>{}(word);
                    int64_t token_id = static_cast<int64_t>(h % (VOCAB_SIZE - 200)) + 200;
                    input_ids.push_back(token_id);
                }
                input_ids.push_back(SEP_TOKEN);
                
                int64_t seq_len_init = static_cast<int64_t>(input_ids.size());
                while (static_cast<int64_t>(input_ids.size()) < MAX_SEQ_LEN) {
                    input_ids.push_back(0);
                }
                attention_mask.resize(static_cast<size_t>(MAX_SEQ_LEN), 0);
                for (int64_t i = 0; i < seq_len_init; ++i) attention_mask[static_cast<size_t>(i)] = 1;
                token_type_ids.resize(static_cast<size_t>(MAX_SEQ_LEN), 0);
            }
            
            int64_t seq_len = 0;
            for (auto v : attention_mask) seq_len += v;
            
            // Create tensors (stack-local, safe outside lock)
            std::array<int64_t, 2> shape = {1, MAX_SEQ_LEN};
            auto memory_info = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
            
            auto ids_tensor = Ort::Value::CreateTensor<int64_t>(
                memory_info, input_ids.data(), input_ids.size(), shape.data(), shape.size());
            auto mask_tensor = Ort::Value::CreateTensor<int64_t>(
                memory_info, attention_mask.data(), attention_mask.size(), shape.data(), shape.size());
            auto type_tensor = Ort::Value::CreateTensor<int64_t>(
                memory_info, token_type_ids.data(), token_type_ids.size(), shape.data(), shape.size());
            
            // Lock only for session metadata queries + inference
            std::lock_guard<std::mutex> lock(onnx_mutex_);

            size_t num_inputs = onnx_session_->GetInputCount();
            std::vector<std::string> input_names_str;
            std::vector<const char*> input_names;
            for (size_t i = 0; i < num_inputs; ++i) {
                auto name = onnx_session_->GetInputNameAllocated(i, allocator);
                input_names_str.push_back(name.get());
                input_names.push_back(input_names_str.back().c_str());
            }
            
            size_t num_outputs = onnx_session_->GetOutputCount();
            std::vector<std::string> output_names_str;
            std::vector<const char*> output_names;
            for (size_t i = 0; i < num_outputs; ++i) {
                auto name = onnx_session_->GetOutputNameAllocated(i, allocator);
                output_names_str.push_back(name.get());
                output_names.push_back(output_names_str.back().c_str());
            }
            
            std::vector<Ort::Value> ort_inputs;
            ort_inputs.push_back(std::move(ids_tensor));
            ort_inputs.push_back(std::move(mask_tensor));
            if (num_inputs >= 3) {
                ort_inputs.push_back(std::move(type_tensor));
            }
            
            try {
                auto output_tensors = onnx_session_->Run(
                    Ort::RunOptions{nullptr},
                    input_names.data(), ort_inputs.data(), ort_inputs.size(),
                    output_names.data(), output_names.size());
                
                // Extract embedding via attention-masked mean pooling
                auto& output = output_tensors[0];
                auto output_shape = output.GetTensorTypeAndShapeInfo().GetShape();
                const float* output_data = output.GetTensorData<float>();
                
                std::vector<float> embedding(dimensions_, 0.0f);
                
                if (output_shape.size() == 3) {
                    int64_t out_seq = output_shape[1];
                    int64_t hidden = output_shape[2];
                    int actual_dim = std::min(static_cast<int>(hidden), static_cast<int>(dimensions_));
                    
                    float token_count = 0.0f;
                    for (int64_t t = 0; t < std::min(seq_len, out_seq); ++t) {
                        float mask_val = static_cast<float>(attention_mask[static_cast<size_t>(t)]);
                        for (int d = 0; d < actual_dim; ++d) {
                            embedding[static_cast<size_t>(d)] += output_data[t * hidden + d] * mask_val;
                        }
                        token_count += mask_val;
                    }
                    if (token_count > 0.0f) {
                        for (size_t d = 0; d < dimensions_; ++d) {
                            embedding[d] /= token_count;
                        }
                    }
                } else if (output_shape.size() == 2) {
                    int actual_dim = std::min(static_cast<int>(output_shape[1]), static_cast<int>(dimensions_));
                    for (int d = 0; d < actual_dim; ++d) {
                        embedding[static_cast<size_t>(d)] = output_data[d];
                    }
                }
                
                // L2 normalize for cosine similarity
                float norm = 0.0f;
                for (float v : embedding) norm += v * v;
                norm = std::sqrt(norm);
                if (norm > 1e-12f) {
                    for (float& v : embedding) v /= norm;
                }
                
                response.embeddings.push_back(std::move(embedding));
                response.total_tokens += static_cast<size_t>(seq_len);
            } catch (const Ort::Exception& e) {
                std::cerr << "[WARN] ONNX inference failed: " << e.what() << "\n";
                response.embeddings.push_back(std::vector<float>(dimensions_, 0.0f));
            }
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
