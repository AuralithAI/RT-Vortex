/**
 * TMS Embedding Engine Implementation
 * 
 * Wraps various embedding providers (HTTP API, ONNX, SentenceTransformers)
 * with caching and batch optimization.
 */

#include "tms/embedding_engine.h"
#include "tms/repo_parser.h"
#include "tms/ltm_faiss.h"
#include "tms/mtm_graph.h"
#include "metrics.h"
#include <algorithm>
#include <cmath>
#include <fstream>
#include <iostream>
#include <sstream>
#include <random>
#include <functional>
#include <unordered_map>
#include <thread>
#include <future>
#include <nlohmann/json.hpp>

#ifdef AIPR_HAS_ONNX
#include <onnxruntime_cxx_api.h>
#endif

#ifdef AIPR_HAS_CURL
#include <curl/curl.h>
#endif

namespace aipr::tms {

// =============================================================================
// WordPiece Tokenizer  —  tokenizer.json (HuggingFace format)
// =============================================================================

class WordPieceTokenizer {
public:
    bool load(const std::string& path) {
        try {
            std::ifstream f(path);
            if (!f.is_open()) return false;
            auto j = nlohmann::json::parse(f);

            // Load vocab from model.vocab
            auto& vocab = j["model"]["vocab"];
            for (auto it = vocab.begin(); it != vocab.end(); ++it) {
                token_to_id_[it.key()] = it.value().get<int>();
                id_to_token_[it.value().get<int>()] = it.key();
            }

            // Special tokens
            if (token_to_id_.count("[CLS]")) cls_id_ = token_to_id_["[CLS]"];
            if (token_to_id_.count("[SEP]")) sep_id_ = token_to_id_["[SEP]"];
            if (token_to_id_.count("[PAD]")) pad_id_ = token_to_id_["[PAD]"];
            if (token_to_id_.count("[UNK]")) unk_id_ = token_to_id_["[UNK]"];

            loaded_ = true;
            return true;
        } catch (const std::exception& e) {
            std::cerr << "[TOKENIZER] Failed to load " << path << ": " << e.what() << std::endl;
            return false;
        }
    }

    struct TokenizerOutput {
        std::vector<int64_t> input_ids;
        std::vector<int64_t> attention_mask;
        std::vector<int64_t> token_type_ids;
    };

    TokenizerOutput encode(const std::string& text, int max_length = 512) const {
        TokenizerOutput output;
        if (!loaded_) return output;

        // Tokenize
        auto tokens = tokenize(text);

        // Truncate (account for [CLS] and [SEP])
        if (static_cast<int>(tokens.size()) > max_length - 2) {
            tokens.resize(max_length - 2);
        }

        // Build input_ids: [CLS] + tokens + [SEP]
        output.input_ids.push_back(cls_id_);
        for (const auto& tok : tokens) {
            auto it = token_to_id_.find(tok);
            output.input_ids.push_back(it != token_to_id_.end() ? it->second : unk_id_);
        }
        output.input_ids.push_back(sep_id_);

        // Attention mask and token type IDs
        output.attention_mask.resize(output.input_ids.size(), 1);
        output.token_type_ids.resize(output.input_ids.size(), 0);

        // Pad to max_length
        while (static_cast<int>(output.input_ids.size()) < max_length) {
            output.input_ids.push_back(pad_id_);
            output.attention_mask.push_back(0);
            output.token_type_ids.push_back(0);
        }

        return output;
    }

    bool isLoaded() const { return loaded_; }

private:
    std::vector<std::string> tokenize(const std::string& text) const {
        // Basic pre-tokenization: lowercase and split on whitespace/punctuation
        std::string lower;
        lower.reserve(text.size());
        for (char c : text) lower += static_cast<char>(std::tolower(static_cast<unsigned char>(c)));

        std::vector<std::string> words;
        std::string current;
        for (char c : lower) {
            if (std::isspace(static_cast<unsigned char>(c)) || std::ispunct(static_cast<unsigned char>(c))) {
                if (!current.empty()) { words.push_back(current); current.clear(); }
                if (std::ispunct(static_cast<unsigned char>(c))) {
                    words.push_back(std::string(1, c));
                }
            } else {
                current += c;
            }
        }
        if (!current.empty()) words.push_back(current);

        // WordPiece tokenization
        std::vector<std::string> tokens;
        for (const auto& word : words) {
            wordPieceTokenize(word, tokens);
        }
        return tokens;
    }

    void wordPieceTokenize(const std::string& word, std::vector<std::string>& output) const {
        if (word.empty()) return;

        size_t start = 0;
        bool is_bad = false;
        while (start < word.size()) {
            size_t end = word.size();
            std::string cur_substr;
            bool found = false;
            while (start < end) {
                std::string substr = word.substr(start, end - start);
                if (start > 0) substr = "##" + substr;
                if (token_to_id_.count(substr)) {
                    cur_substr = substr;
                    found = true;
                    break;
                }
                end--;
            }
            if (!found) {
                is_bad = true;
                break;
            }
            output.push_back(cur_substr);
            start = end;
        }
        if (is_bad) {
            output.push_back("[UNK]");
        }
    }

    std::unordered_map<std::string, int> token_to_id_;
    std::unordered_map<int, std::string> id_to_token_;
    int cls_id_ = 101;
    int sep_id_ = 102;
    int pad_id_ = 0;
    int unk_id_ = 100;
    bool loaded_ = false;
};

// =============================================================================
// CircuitBreaker — prevents cascading failures to external HTTP embedding APIs.
//
// States: CLOSED (normal) → OPEN (after N failures) → HALF_OPEN (after timeout)
//         HALF_OPEN allows one probe request; success → CLOSED, failure → OPEN.
// =============================================================================

class CircuitBreaker {
public:
    enum class State { CLOSED, OPEN, HALF_OPEN };

    explicit CircuitBreaker(int failure_threshold = 5,
                            std::chrono::seconds open_duration = std::chrono::seconds(60))
        : failure_threshold_(failure_threshold)
        , open_duration_(open_duration) {}

    /// Returns true if the request is allowed through.
    bool allowRequest() {
        std::lock_guard<std::mutex> lock(mu_);
        switch (state_) {
            case State::CLOSED:
                return true;
            case State::OPEN: {
                auto now = std::chrono::steady_clock::now();
                if (now - opened_at_ >= open_duration_) {
                    state_ = State::HALF_OPEN;
                    return true;  // allow probe
                }
                return false;
            }
            case State::HALF_OPEN:
                return false;  // only one probe at a time
        }
        return true;
    }

    void recordSuccess() {
        std::lock_guard<std::mutex> lock(mu_);
        consecutive_failures_ = 0;
        state_ = State::CLOSED;
    }

    void recordFailure() {
        std::lock_guard<std::mutex> lock(mu_);
        ++consecutive_failures_;
        if (consecutive_failures_ >= failure_threshold_) {
            if (state_ != State::OPEN) {
                state_ = State::OPEN;
                opened_at_ = std::chrono::steady_clock::now();
                metrics::Registry::instance().incCounter(metrics::CIRCUIT_BREAKER_TRIPS);
                std::cerr << "[EMBED] Circuit breaker OPEN after "
                          << consecutive_failures_ << " consecutive failures" << std::endl;
            }
        }
    }

    State getState() const {
        std::lock_guard<std::mutex> lock(mu_);
        return state_;
    }

private:
    int failure_threshold_;
    std::chrono::seconds open_duration_;
    mutable std::mutex mu_;
    int consecutive_failures_ = 0;
    State state_ = State::CLOSED;
    std::chrono::steady_clock::time_point opened_at_;
};

// =============================================================================
// BackendImpl (pimpl)
// =============================================================================

class EmbeddingEngine::BackendImpl {
public:
    explicit BackendImpl(const EmbeddingConfig& config)
        : config_(config), initialized_(false) {}

    ~BackendImpl() {
#ifdef AIPR_HAS_ONNX
        ort_session_.reset();
#endif
    }

    bool initialize() {
        std::cerr << "[EMBED] Initializing backend: " << static_cast<int>(config_.backend)
                  << " model_path=" << config_.onnx_model_path
                  << " tokenizer_path=" << config_.tokenizer_path
                  << " dimension=" << config_.embedding_dimension << std::endl;
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
                std::cerr << "[EMBED] Using MOCK embedding backend" << std::endl;
                initialized_ = true;
                break;
        }
        return initialized_;
    }

    void shutdown() {
        initialized_ = false;
#ifdef AIPR_HAS_ONNX
        ort_session_.reset();
#endif
    }
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
    CircuitBreaker circuit_breaker_;  // HTTP embedding circuit breaker

#ifdef AIPR_HAS_ONNX
    std::unique_ptr<Ort::Env> ort_env_;
    std::unique_ptr<Ort::Session> ort_session_;
    WordPieceTokenizer tokenizer_;
#endif

    bool initializeHttp() {
        return !config_.api_endpoint.empty();
    }

    bool initializeOnnx() {
#ifdef AIPR_HAS_ONNX
        if (config_.onnx_model_path.empty()) {
            std::cerr << "[EMBED] ONNX model path is empty" << std::endl;
            return false;
        }
        {
            std::ifstream f(config_.onnx_model_path);
            if (!f.good()) {
                std::cerr << "[EMBED] ONNX model not found: " << config_.onnx_model_path << std::endl;
                return false;
            }
        }

        // Load tokenizer
        if (!config_.tokenizer_path.empty()) {
            if (!tokenizer_.load(config_.tokenizer_path)) {
                std::cerr << "[EMBED] Failed to load tokenizer: " << config_.tokenizer_path << std::endl;
                return false;
            }
            std::cerr << "[EMBED] Tokenizer loaded from " << config_.tokenizer_path << std::endl;
        } else {
            std::cerr << "[EMBED] No tokenizer path configured" << std::endl;
            return false;
        }

        // Initialize ONNX Runtime
        try {
            ort_env_ = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "rtvortex-embed");

            Ort::SessionOptions session_opts;
            int intra_threads = config_.onnx_intra_op_threads > 0
                                    ? config_.onnx_intra_op_threads : 4;
            session_opts.SetIntraOpNumThreads(intra_threads);
            session_opts.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);

            ort_session_ = std::make_unique<Ort::Session>(*ort_env_, config_.onnx_model_path.c_str(), session_opts);

            std::cerr << "[EMBED] ONNX Runtime initialized: " << config_.onnx_model_path
                      << " (intra_threads=" << intra_threads << ")" << std::endl;
            std::cerr << "[EMBED] Embedding dimension: " << config_.embedding_dimension << std::endl;
            return true;
        } catch (const Ort::Exception& e) {
            std::cerr << "[EMBED] ONNX Runtime init failed: " << e.what() << std::endl;
            return false;
        }
#else
        std::cerr << "[EMBED] ONNX Runtime not compiled in (AIPR_HAS_ONNX not defined)" << std::endl;
        return false;
#endif
    }

    bool initializeSentenceTransformers() {
        return !config_.model_name.empty();
    }

    // ── HTTP backend (production — libcurl + OpenAI-compatible API) ──
    std::vector<std::vector<float>> embedBatchHttp(const std::vector<std::string>& texts) {
#ifdef AIPR_HAS_CURL
        // Circuit breaker: when OPEN, fall back to ONNX (if available) or mock
        if (!circuit_breaker_.allowRequest()) {
            std::cerr << "[EMBED] Circuit breaker OPEN — falling back to local backend" << std::endl;
#ifdef AIPR_HAS_ONNX
            if (ort_session_) return embedBatchOnnx(texts);
#endif
            return embedBatchMock(texts);
        }

        if (config_.api_endpoint.empty()) {
            std::cerr << "[EMBED] HTTP endpoint not configured, falling back to mock" << std::endl;
            return embedBatchMock(texts);
        }
        if (config_.api_key.empty()) {
            std::cerr << "[EMBED] HTTP API key not set, falling back to mock" << std::endl;
            return embedBatchMock(texts);
        }

        // Detect provider type from endpoint for request/response format differences.
        // - OpenAI / Voyage / ollama: POST { model, input: [...] }
        // - Cohere:                  POST { model, texts: [...], input_type }
        const bool is_cohere = config_.api_endpoint.find("cohere") != std::string::npos;

        std::vector<std::vector<float>> all_embeddings;
        all_embeddings.reserve(texts.size());

        // Process in batches to stay within API limits.
        const size_t api_batch = std::min(static_cast<size_t>(config_.batch_size), size_t(2048));

        for (size_t offset = 0; offset < texts.size(); offset += api_batch) {
            size_t end = std::min(offset + api_batch, texts.size());

            // Build JSON payload.
            nlohmann::json payload;
            payload["model"] = config_.model_name;

            if (is_cohere) {
                // Cohere Embed v3 uses "texts" array + "input_type".
                nlohmann::json text_arr = nlohmann::json::array();
                for (size_t i = offset; i < end; ++i) {
                    text_arr.push_back(texts[i]);
                }
                payload["texts"] = text_arr;
                payload["input_type"] = "search_document";
                payload["truncate"] = "END";
            } else {
                // OpenAI-compatible (OpenAI, Voyage, ollama, vLLM, etc.)
                nlohmann::json input_arr = nlohmann::json::array();
                for (size_t i = offset; i < end; ++i) {
                    input_arr.push_back(texts[i]);
                }
                payload["input"] = input_arr;
                // Send dimensions only if explicitly configured and > 0.
                if (config_.embedding_dimension > 0) {
                    payload["dimensions"] = config_.embedding_dimension;
                }
            }

            std::string body = payload.dump();

            // Execute HTTP request with retry logic.
            std::string response_body;
            bool success = false;
            std::string last_error;

            for (int attempt = 0; attempt <= config_.max_retries; ++attempt) {
                if (attempt > 0) {
                    int delay = config_.retry_delay_ms * (1 << (attempt - 1)); // exponential backoff
                    std::cerr << "[EMBED] HTTP retry " << attempt << "/" << config_.max_retries
                              << " after " << delay << "ms" << std::endl;
                    std::this_thread::sleep_for(std::chrono::milliseconds(delay));
                }

                response_body.clear();
                long http_code = 0;

                CURL* curl = curl_easy_init();
                if (!curl) {
                    last_error = "curl_easy_init failed";
                    continue;
                }

                // Response write callback.
                auto write_cb = +[](char* ptr, size_t size, size_t nmemb, void* userdata) -> size_t {
                    auto* buf = static_cast<std::string*>(userdata);
                    buf->append(ptr, size * nmemb);
                    return size * nmemb;
                };

                struct curl_slist* headers = nullptr;
                headers = curl_slist_append(headers, "Content-Type: application/json");

                // Authorization header — Cohere uses "Bearer" same as OpenAI/Voyage.
                std::string auth_header = "Authorization: Bearer " + config_.api_key;
                headers = curl_slist_append(headers, auth_header.c_str());

                curl_easy_setopt(curl, CURLOPT_URL, config_.api_endpoint.c_str());
                curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
                curl_easy_setopt(curl, CURLOPT_POSTFIELDS, body.c_str());
                curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE, static_cast<long>(body.size()));
                curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, write_cb);
                curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response_body);
                curl_easy_setopt(curl, CURLOPT_TIMEOUT_MS, static_cast<long>(config_.timeout_ms));
                curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 10L);
                // Accept gzip/deflate for faster transfers.
                curl_easy_setopt(curl, CURLOPT_ACCEPT_ENCODING, "");

                CURLcode res = curl_easy_perform(curl);
                curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
                curl_slist_free_all(headers);
                curl_easy_cleanup(curl);

                if (res != CURLE_OK) {
                    last_error = std::string("curl error: ") + curl_easy_strerror(res);
                    continue;
                }

                if (http_code == 429) {
                    // Rate limited — retry with backoff.
                    last_error = "rate limited (429)";
                    metrics::Registry::instance().incCounter("embed_http_rate_limits");
                    continue;
                }
                if (http_code >= 500) {
                    // Server error — retry.
                    last_error = "server error (" + std::to_string(http_code) + ")";
                    continue;
                }
                if (http_code != 200) {
                    last_error = "HTTP " + std::to_string(http_code) + ": " + response_body.substr(0, 200);
                    std::cerr << "[EMBED] HTTP API error: " << last_error << std::endl;
                    metrics::Registry::instance().incCounter("embed_http_errors");
                    break; // 4xx client errors don't benefit from retry.
                }

                success = true;
                break;
            }

            if (!success) {
                std::cerr << "[EMBED] HTTP API failed after retries: " << last_error << std::endl;
                metrics::Registry::instance().incCounter("embed_http_errors");
                circuit_breaker_.recordFailure();
                // Fill this batch with zero vectors.
                for (size_t i = offset; i < end; ++i) {
                    all_embeddings.push_back(std::vector<float>(config_.embedding_dimension, 0.0f));
                }
                continue;
            }

            // Parse response.
            try {
                auto resp = nlohmann::json::parse(response_body);
                circuit_breaker_.recordSuccess();

                if (is_cohere) {
                    // Cohere response: { embeddings: [[...], [...]], ... }
                    // or Cohere v2:    { embeddings: { float: [[...], ...] }, ... }
                    nlohmann::json embed_data;
                    if (resp.contains("embeddings") && resp["embeddings"].is_array()) {
                        embed_data = resp["embeddings"];
                    } else if (resp.contains("embeddings") && resp["embeddings"].is_object()
                               && resp["embeddings"].contains("float")) {
                        embed_data = resp["embeddings"]["float"];
                    } else {
                        throw std::runtime_error("unexpected Cohere response format");
                    }

                    for (auto& emb : embed_data) {
                        std::vector<float> vec;
                        vec.reserve(emb.size());
                        for (auto& v : emb) {
                            vec.push_back(v.get<float>());
                        }
                        all_embeddings.push_back(std::move(vec));
                    }

                    // Token usage (Cohere).
                    if (resp.contains("meta") && resp["meta"].contains("billed_units")
                        && resp["meta"]["billed_units"].contains("input_tokens")) {
                        int tokens = resp["meta"]["billed_units"]["input_tokens"].get<int>();
                        metrics::Registry::instance().incCounter("embed_http_tokens_used", tokens);
                    }
                } else {
                    // OpenAI-compatible response: { data: [{ embedding: [...], index: N }], usage: {...} }
                    if (!resp.contains("data") || !resp["data"].is_array()) {
                        throw std::runtime_error("missing 'data' array in response");
                    }

                    // Sort by index to ensure correct order.
                    auto& data_arr = resp["data"];
                    std::vector<std::pair<int, std::vector<float>>> indexed_embeddings;
                    indexed_embeddings.reserve(data_arr.size());

                    for (auto& item : data_arr) {
                        int idx = item.value("index", static_cast<int>(indexed_embeddings.size()));
                        std::vector<float> vec;
                        auto& emb = item["embedding"];
                        vec.reserve(emb.size());
                        for (auto& v : emb) {
                            vec.push_back(v.get<float>());
                        }
                        indexed_embeddings.emplace_back(idx, std::move(vec));
                    }

                    std::sort(indexed_embeddings.begin(), indexed_embeddings.end(),
                              [](const auto& a, const auto& b) { return a.first < b.first; });

                    for (auto& [_, vec] : indexed_embeddings) {
                        all_embeddings.push_back(std::move(vec));
                    }

                    // Token usage (OpenAI / Voyage).
                    if (resp.contains("usage") && resp["usage"].contains("total_tokens")) {
                        int tokens = resp["usage"]["total_tokens"].get<int>();
                        metrics::Registry::instance().incCounter("embed_http_tokens_used", tokens);
                    }
                }

                metrics::Registry::instance().incCounter("embed_http_requests");

            } catch (const std::exception& e) {
                std::cerr << "[EMBED] Failed to parse HTTP response: " << e.what() << std::endl;
                metrics::Registry::instance().incCounter("embed_http_errors");
                // Fill remaining with zero vectors.
                for (size_t i = offset; i < end; ++i) {
                    if (all_embeddings.size() <= i) {
                        all_embeddings.push_back(std::vector<float>(config_.embedding_dimension, 0.0f));
                    }
                }
            }
        }

        return all_embeddings;
#else
        std::cerr << "[EMBED] libcurl not compiled in (AIPR_HAS_CURL not defined), falling back to mock" << std::endl;
        return embedBatchMock(texts);
#endif
    }

    // ── ONNX Runtime backend (real inference) ──
    std::vector<std::vector<float>> embedBatchOnnx(const std::vector<std::string>& texts) {
#ifdef AIPR_HAS_ONNX
        if (!ort_session_ || !tokenizer_.isLoaded()) {
            std::cerr << "[EMBED] ONNX session or tokenizer not ready, falling back to mock" << std::endl;
            return embedBatchMock(texts);
        }

        std::vector<std::vector<float>> all_embeddings;
        all_embeddings.reserve(texts.size());

        // Process in mini-batches to manage memory
        const size_t mini_batch = 32;
        for (size_t offset = 0; offset < texts.size(); offset += mini_batch) {
            size_t end = std::min(offset + mini_batch, texts.size());
            size_t batch_size = end - offset;

            // Tokenize batch
            const int max_seq_len = 256;  // MiniLM max 512, use 256 for speed
            std::vector<int64_t> all_input_ids(batch_size * max_seq_len);
            std::vector<int64_t> all_attention_mask(batch_size * max_seq_len);
            std::vector<int64_t> all_token_type_ids(batch_size * max_seq_len);

            for (size_t i = 0; i < batch_size; i++) {
                auto encoded = tokenizer_.encode(texts[offset + i], max_seq_len);
                std::copy(encoded.input_ids.begin(), encoded.input_ids.end(),
                          all_input_ids.begin() + static_cast<long>(i * max_seq_len));
                std::copy(encoded.attention_mask.begin(), encoded.attention_mask.end(),
                          all_attention_mask.begin() + static_cast<long>(i * max_seq_len));
                std::copy(encoded.token_type_ids.begin(), encoded.token_type_ids.end(),
                          all_token_type_ids.begin() + static_cast<long>(i * max_seq_len));
            }

            // Create ONNX tensors
            std::array<int64_t, 2> shape = {static_cast<int64_t>(batch_size), max_seq_len};
            auto memory_info = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);

            auto input_ids_tensor = Ort::Value::CreateTensor<int64_t>(
                memory_info, all_input_ids.data(), all_input_ids.size(),
                shape.data(), shape.size());
            auto attention_mask_tensor = Ort::Value::CreateTensor<int64_t>(
                memory_info, all_attention_mask.data(), all_attention_mask.size(),
                shape.data(), shape.size());
            auto token_type_ids_tensor = Ort::Value::CreateTensor<int64_t>(
                memory_info, all_token_type_ids.data(), all_token_type_ids.size(),
                shape.data(), shape.size());

            // Run inference
            try {
                const char* input_names[] = {"input_ids", "attention_mask", "token_type_ids"};
                const char* output_names[] = {"last_hidden_state"};

                std::vector<Ort::Value> inputs;
                inputs.push_back(std::move(input_ids_tensor));
                inputs.push_back(std::move(attention_mask_tensor));
                inputs.push_back(std::move(token_type_ids_tensor));

                auto outputs = ort_session_->Run(
                    Ort::RunOptions{nullptr},
                    input_names, inputs.data(), inputs.size(),
                    output_names, 1);

                // Extract embeddings via mean pooling over token dimension
                auto& output_tensor = outputs[0];
                auto type_info = output_tensor.GetTensorTypeAndShapeInfo();
                auto out_shape = type_info.GetShape();
                // out_shape = [batch_size, seq_len, hidden_dim]
                int64_t hidden_dim = out_shape[2];
                const float* output_data = output_tensor.GetTensorData<float>();

                for (size_t i = 0; i < batch_size; i++) {
                    std::vector<float> embedding(hidden_dim, 0.0f);
                    float count = 0.0f;

                    // Mean pooling: average over non-padded tokens
                    for (int s = 0; s < max_seq_len; s++) {
                        if (all_attention_mask[i * max_seq_len + s] == 0) continue;
                        count += 1.0f;
                        for (int64_t d = 0; d < hidden_dim; d++) {
                            embedding[d] += output_data[i * max_seq_len * hidden_dim + s * hidden_dim + d];
                        }
                    }

                    if (count > 0) {
                        for (int64_t d = 0; d < hidden_dim; d++) {
                            embedding[d] /= count;
                        }
                    }

                    // L2 normalize
                    float norm = 0.0f;
                    for (float v : embedding) norm += v * v;
                    norm = std::sqrt(norm);
                    if (norm > 0) {
                        for (float& v : embedding) v /= norm;
                    }

                    all_embeddings.push_back(std::move(embedding));
                }
            } catch (const Ort::Exception& e) {
                std::cerr << "[EMBED] ONNX inference error: " << e.what() << std::endl;
                // Fill failed batch with zero vectors
                for (size_t i = 0; i < batch_size; i++) {
                    all_embeddings.push_back(std::vector<float>(config_.embedding_dimension, 0.0f));
                }
            }
        }

        return all_embeddings;
#else
        return embedBatchMock(texts);
#endif
    }

    std::vector<std::vector<float>> embedBatchSentenceTransformers(const std::vector<std::string>& texts) {
        // SentenceTransformers backend: spawns a Python subprocess that loads
        // the configured model, encodes the input texts, and writes the
        // embeddings as newline-delimited JSON arrays to a temp file.
        //
        // Uses temp files for I/O since popen() only supports unidirectional
        // pipes.  The Python script reads from an input .jsonl and writes
        // embeddings to an output .jsonl.

        if (config_.model_name.empty()) {
            std::cerr << "[EMBED] SentenceTransformers model_name not configured, falling back to mock" << std::endl;
            return embedBatchMock(texts);
        }

        std::string tmp_input  = "/tmp/aipr_st_input_"  + std::to_string(getpid()) + ".jsonl";
        std::string tmp_output = "/tmp/aipr_st_output_" + std::to_string(getpid()) + ".jsonl";

        // Write input texts to temp file.
        {
            std::ofstream ofs(tmp_input);
            if (!ofs) {
                std::cerr << "[EMBED] Failed to write SentenceTransformers input file" << std::endl;
                return embedBatchMock(texts);
            }
            for (const auto& text : texts) {
                nlohmann::json jt = text;
                ofs << jt.dump() << "\n";
            }
        }

        // Build the file-based Python script.
        static const char* py_file_script = R"PY(
import sys, json
from sentence_transformers import SentenceTransformer

model_name = sys.argv[1]
dim = int(sys.argv[2])
input_path = sys.argv[3]
output_path = sys.argv[4]

model = SentenceTransformer(model_name)

with open(input_path) as f:
    lines = [json.loads(line.strip()) for line in f if line.strip()]

if not lines:
    open(output_path, 'w').close()
    sys.exit(0)

embeddings = model.encode(lines, normalize_embeddings=True, show_progress_bar=False)
with open(output_path, 'w') as f:
    for emb in embeddings:
        vec = emb.tolist()
        if len(vec) != dim:
            vec = (vec + [0.0] * dim)[:dim]
        f.write(json.dumps(vec) + '\n')
)PY";

        std::string file_cmd = "python3 -c '" + std::string(py_file_script) + "' "
                             + config_.model_name + " "
                             + std::to_string(config_.embedding_dimension) + " "
                             + tmp_input + " " + tmp_output;

        int rc = std::system(file_cmd.c_str());
        std::remove(tmp_input.c_str());

        if (rc != 0) {
            std::cerr << "[EMBED] SentenceTransformers subprocess failed (exit "
                      << rc << "), falling back to mock" << std::endl;
            std::remove(tmp_output.c_str());
            return embedBatchMock(texts);
        }

        // Read output embeddings.
        std::vector<std::vector<float>> results;
        results.reserve(texts.size());
        {
            std::ifstream ifs(tmp_output);
            if (!ifs) {
                std::cerr << "[EMBED] Failed to read SentenceTransformers output" << std::endl;
                std::remove(tmp_output.c_str());
                return embedBatchMock(texts);
            }
            std::string line;
            while (std::getline(ifs, line)) {
                if (line.empty()) continue;
                try {
                    auto arr = nlohmann::json::parse(line);
                    std::vector<float> vec;
                    vec.reserve(arr.size());
                    for (auto& v : arr) {
                        vec.push_back(v.get<float>());
                    }
                    results.push_back(std::move(vec));
                } catch (const std::exception& e) {
                    std::cerr << "[EMBED] Failed to parse ST output line: " << e.what() << std::endl;
                    results.push_back(std::vector<float>(config_.embedding_dimension, 0.0f));
                }
            }
        }
        std::remove(tmp_output.c_str());

        // If we got fewer results than inputs, pad with zeros.
        while (results.size() < texts.size()) {
            results.push_back(std::vector<float>(config_.embedding_dimension, 0.0f));
        }

        metrics::Registry::instance().incCounter("embed_st_batches");
        metrics::Registry::instance().incCounter("embed_st_texts", static_cast<double>(texts.size()));

        return results;
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

    bool saveToDisk(const std::string& path) const {
        std::ofstream out(path, std::ios::binary);
        if (!out) return false;
        // Header: magic + version
        const uint32_t magic = 0x45434348; // "ECCH"
        const uint32_t version = 1;
        out.write(reinterpret_cast<const char*>(&magic), sizeof(magic));
        out.write(reinterpret_cast<const char*>(&version), sizeof(version));
        // Entry count
        uint64_t count = cache_.size();
        out.write(reinterpret_cast<const char*>(&count), sizeof(count));
        for (const auto& [key, vec] : cache_) {
            // Key: length + data
            uint32_t key_len = static_cast<uint32_t>(key.size());
            out.write(reinterpret_cast<const char*>(&key_len), sizeof(key_len));
            out.write(key.data(), key_len);
            // Embedding: dimension + floats
            uint32_t dim = static_cast<uint32_t>(vec.size());
            out.write(reinterpret_cast<const char*>(&dim), sizeof(dim));
            out.write(reinterpret_cast<const char*>(vec.data()), dim * sizeof(float));
        }
        return out.good();
    }

    bool loadFromDisk(const std::string& path) {
        std::ifstream in(path, std::ios::binary);
        if (!in) return false;
        // Header
        uint32_t magic = 0, version = 0;
        in.read(reinterpret_cast<char*>(&magic), sizeof(magic));
        in.read(reinterpret_cast<char*>(&version), sizeof(version));
        if (magic != 0x45434348 || version != 1) return false;
        // Entry count
        uint64_t count = 0;
        in.read(reinterpret_cast<char*>(&count), sizeof(count));
        cache_.clear();
        for (uint64_t i = 0; i < count && in.good(); ++i) {
            uint32_t key_len = 0;
            in.read(reinterpret_cast<char*>(&key_len), sizeof(key_len));
            if (key_len > 1'000'000) return false; // sanity check
            std::string key(key_len, '\0');
            in.read(key.data(), key_len);
            uint32_t dim = 0;
            in.read(reinterpret_cast<char*>(&dim), sizeof(dim));
            if (dim > 100'000) return false; // sanity check
            std::vector<float> vec(dim);
            in.read(reinterpret_cast<char*>(vec.data()), dim * sizeof(float));
            if (cache_.size() < max_size_) {
                cache_[std::move(key)] = std::move(vec);
            }
        }
        hits_ = 0;
        misses_ = 0;
        return in.good() || in.eof();
    }

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
    // Determine number of parallel workers for ONNX backend
    num_workers_ = 1;
    if (config.backend == EmbeddingBackend::ONNX_RUNTIME) {
        int hw_cores = static_cast<int>(std::thread::hardware_concurrency());
        if (hw_cores <= 0) hw_cores = 4;

        if (config.num_parallel_workers > 0) {
            num_workers_ = config.num_parallel_workers;
        } else {
            // Auto: cores / 4, clamped to [1, 8]
            num_workers_ = std::max(1, std::min(hw_cores / 4, 8));
        }

        // Compute intra-op threads per worker so total ≈ hw_cores
        int intra_threads = config.onnx_intra_op_threads;
        if (intra_threads <= 0) {
            intra_threads = std::max(2, hw_cores / num_workers_);
        }

        // Create a mutable config copy with tuned thread count
        EmbeddingConfig worker_config = config;
        worker_config.onnx_intra_op_threads = intra_threads;

        std::cerr << "[EMBED] Parallel ONNX workers: " << num_workers_
                  << " × " << intra_threads << " intra-op threads"
                  << " (hw_cores=" << hw_cores << ")" << std::endl;

        // Primary backend (also used for single-text embed)
        backend_ = std::make_unique<BackendImpl>(worker_config);

        // Additional worker backends
        for (int i = 1; i < num_workers_; ++i) {
            auto worker = std::make_unique<BackendImpl>(worker_config);
            worker_pool_.push_back(std::move(worker));
        }
    } else {
        backend_ = std::make_unique<BackendImpl>(config);
    }

    cache_ = std::make_unique<EmbeddingCache>(config.cache_size);
    rate_limiter_ = std::make_unique<RateLimiter>(
        config.max_requests_per_minute, config.max_tokens_per_minute);
    
    bool ok = backend_->initialize();

    // Initialize worker pool backends
    for (auto& worker : worker_pool_) {
        bool wok = worker->initialize();
        if (!wok) {
            std::cerr << "[EMBED] WARNING: Worker backend failed to initialize" << std::endl;
        }
    }

    // Only report MiniLM as ready when the ONNX backend actually loaded
    // successfully (model file found, tokenizer parsed, session created).
    if (config.backend == EmbeddingBackend::ONNX_RUNTIME && ok) {
        aipr::metrics::Registry::instance().setGauge(aipr::metrics::MINILM_READY, 1.0);
    } else if (config.backend == EmbeddingBackend::ONNX_RUNTIME && !ok) {
        aipr::metrics::Registry::instance().setGauge(aipr::metrics::MINILM_READY, 0.0);
        std::cerr << "[EMBED] WARNING: ONNX backend failed to initialize, "
                     "embeddings will fall back to mock vectors" << std::endl;
    }
}

EmbeddingEngine::~EmbeddingEngine() = default;

// =============================================================================
// Main Embedding Interface
// =============================================================================

EmbeddingResult EmbeddingEngine::embed(const std::string& text) {
    auto start = std::chrono::steady_clock::now();
    EmbeddingResult result;

    aipr::metrics::Registry::instance().incCounter(aipr::metrics::EMBED_TOTAL_CALLS);

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

            double secs = std::chrono::duration<double>(end_t - start).count();
            aipr::metrics::Registry::instance().observeHistogram(aipr::metrics::EMBED_LATENCY_S, secs);

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

    double secs = std::chrono::duration<double>(end_t - start).count();
    aipr::metrics::Registry::instance().observeHistogram(aipr::metrics::EMBED_LATENCY_S, secs);

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

    // Collect all backends (primary + workers) for parallel dispatch
    std::vector<BackendImpl*> backends;
    backends.push_back(backend_.get());
    for (auto& w : worker_pool_) {
        if (w && w->isInitialized()) {
            backends.push_back(w.get());
        }
    }
    const int active_workers = static_cast<int>(backends.size());

    if (active_workers <= 1 || uncached_texts.size() < 64) {
        // ── Sequential path (single worker or small batch) ──────────────
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
    } else {
        // ── Parallel path: split uncached texts across workers ──────────
        //
        // Each worker gets a contiguous slice of uncached_texts.
        // Workers run concurrently via std::async, each calling
        // its own ONNX session (thread-safe since sessions are separate).
        //
        size_t total_uncached = uncached_texts.size();
        size_t per_worker = (total_uncached + active_workers - 1) / active_workers;

        struct WorkerResult {
            std::vector<std::vector<float>> embeddings;
            std::vector<std::string> errors;  // per-item error (empty = ok)
        };

        std::vector<std::future<WorkerResult>> futures;
        std::atomic<size_t> global_completed{0};

        for (int w = 0; w < active_workers; ++w) {
            size_t w_start = w * per_worker;
            size_t w_end = std::min(w_start + per_worker, total_uncached);
            if (w_start >= total_uncached) break;

            BackendImpl* be = backends[w];
            int batch_sz = config_.batch_size;

            futures.push_back(std::async(std::launch::async,
                [be, &uncached_texts, w_start, w_end, batch_sz,
                 &global_completed, &progress, total_uncached]() -> WorkerResult {
                    WorkerResult wr;
                    size_t slice_size = w_end - w_start;
                    wr.embeddings.reserve(slice_size);
                    wr.errors.resize(slice_size);

                    for (size_t offset = 0; offset < slice_size;
                         offset += static_cast<size_t>(batch_sz)) {
                        size_t end_idx = std::min(offset + static_cast<size_t>(batch_sz),
                                                  slice_size);
                        std::vector<std::string> batch(
                            uncached_texts.begin() + static_cast<long>(w_start + offset),
                            uncached_texts.begin() + static_cast<long>(w_start + end_idx));
                        try {
                            auto batch_embs = be->embedBatch(batch);
                            for (auto& emb : batch_embs) {
                                wr.embeddings.push_back(std::move(emb));
                            }
                        } catch (const std::exception& e) {
                            // Fill failed items
                            for (size_t j = 0; j < batch.size(); ++j) {
                                wr.embeddings.push_back({});
                                wr.errors[offset + j] = e.what();
                            }
                        }

                        size_t done = global_completed.fetch_add(batch.size()) + batch.size();
                        if (progress) {
                            progress(static_cast<int>(done),
                                     static_cast<int>(total_uncached),
                                     "Embedding...");
                        }
                    }
                    return wr;
                }));
        }

        // Gather results from all workers
        size_t global_offset = 0;
        for (auto& fut : futures) {
            auto wr = fut.get();
            for (size_t j = 0; j < wr.embeddings.size(); ++j) {
                size_t orig_idx = uncached_indices[global_offset + j];
                if (wr.errors[j].empty() && !wr.embeddings[j].empty()) {
                    result.embeddings[orig_idx] = std::move(wr.embeddings[j]);
                    result.successful_count++;
                    if (config_.enable_cache) {
                        cache_->put(computeHash(uncached_texts[global_offset + j]),
                                    result.embeddings[orig_idx]);
                    }
                } else {
                    result.errors[orig_idx] = wr.errors[j].empty()
                        ? "empty embedding" : wr.errors[j];
                    result.failed_count++;
                }
            }
            global_offset += wr.embeddings.size();
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
void EmbeddingEngine::saveCache() {
    if (config_.cache_path.empty()) return;
    cache_->saveToDisk(config_.cache_path);
}
void EmbeddingEngine::loadCache() {
    if (config_.cache_path.empty()) return;
    cache_->loadFromDisk(config_.cache_path);
}

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

    // Update cache hit rate gauge
    {
        size_t h = cache_->hits();
        size_t total = h + cache_->misses();
        if (total > 0) {
            double hit_rate = static_cast<double>(h) / static_cast<double>(total);
            aipr::metrics::Registry::instance().setGauge(
                aipr::metrics::EMBED_CACHE_HIT_RATE, hit_rate);
        }
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
    auto start = std::chrono::steady_clock::now();
    IngestResult result;

    // Step 1: Parse repository into code chunks
    RepoParserConfig parser_config;
    RepoParser parser(parser_config);

    std::vector<CodeChunk> chunks;
    try {
        chunks = parser.parseRepository(repo_path);
    } catch (const std::exception& e) {
        result.errors.push_back(std::string("Parse error: ") + e.what());
        auto end_t = std::chrono::steady_clock::now();
        result.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_t - start);
        return result;
    }

    // Stamp every chunk with repo tag
    for (auto& chunk : chunks) {
        chunk.tags.push_back("repo:" + repo_id);
    }

    if (progress) {
        progress(0, static_cast<int>(chunks.size()), "Parsed " + std::to_string(chunks.size()) + " chunks");
    }

    // Step 2 & 3: Embed and store via ingestChunks
    result = ingestChunks(repo_id, chunks, progress);

    auto end_t = std::chrono::steady_clock::now();
    result.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_t - start);
    return result;
}

EmbeddingIngestor::IngestResult EmbeddingIngestor::ingestChunks(
    const std::string& repo_id,
    const std::vector<CodeChunk>& chunks,
    EmbeddingProgressCallback progress)
{
    auto start = std::chrono::steady_clock::now();
    IngestResult result;

    size_t batch_size = static_cast<size_t>(config_.embedding_batch_size);
    int error_count = 0;

    for (size_t offset = 0; offset < chunks.size(); offset += batch_size) {
        size_t end_idx = std::min(offset + batch_size, chunks.size());
        std::vector<CodeChunk> batch(chunks.begin() + static_cast<ptrdiff_t>(offset),
                                     chunks.begin() + static_cast<ptrdiff_t>(end_idx));

        // Stamp repo tag on each chunk
        for (auto& chunk : batch) {
            chunk.tags.push_back("repo:" + repo_id);
        }

        // Compute embeddings for this batch
        BatchEmbeddingResult emb_result = embedding_engine_.embedChunks(batch, nullptr);

        // Collect successfully embedded chunks and their embeddings
        std::vector<CodeChunk> good_chunks;
        std::vector<std::vector<float>> good_embeddings;
        good_chunks.reserve(batch.size());
        good_embeddings.reserve(batch.size());

        for (size_t j = 0; j < batch.size(); ++j) {
            if (j < emb_result.embeddings.size() && !emb_result.embeddings[j].empty()
                && (j >= emb_result.errors.size() || emb_result.errors[j].empty())) {
                good_chunks.push_back(batch[j]);
                good_embeddings.push_back(emb_result.embeddings[j]);
            } else {
                result.chunks_failed++;
                error_count++;
                if (j < emb_result.errors.size() && !emb_result.errors[j].empty()) {
                    result.errors.push_back(emb_result.errors[j]);
                }
            }
        }

        // Store in LTM
        if (!good_chunks.empty()) {
            try {
                ltm_.addBatch(good_chunks, good_embeddings);
                result.chunks_stored += good_chunks.size();
            } catch (const std::exception& e) {
                result.errors.push_back(std::string("LTM addBatch error: ") + e.what());
                result.chunks_failed += good_chunks.size();
                error_count += static_cast<int>(good_chunks.size());
            }
        }

        result.chunks_processed += batch.size();

        if (progress) {
            progress(static_cast<int>(result.chunks_processed),
                     static_cast<int>(chunks.size()),
                     "Embedding & storing...");
        }

        // Abort if too many errors
        if (!config_.continue_on_error && error_count > 0) break;
        if (error_count >= config_.max_errors) {
            result.errors.push_back("Aborted: max error count reached (" +
                                    std::to_string(config_.max_errors) + ")");
            break;
        }
    }

    // Step 4: Detect patterns via MTM
    if (config_.detect_patterns && config_.update_mtm && mtm_ != nullptr) {
        detectAndStorePatterns(chunks);
    }

    auto end_t = std::chrono::steady_clock::now();
    result.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_t - start);
    return result;
}

EmbeddingIngestor::IngestResult EmbeddingIngestor::ingestChunksWithEmbeddings(
    const std::string& repo_id,
    const std::vector<CodeChunk>& chunks,
    const std::vector<std::vector<float>>& embeddings)
{
    auto start = std::chrono::steady_clock::now();
    IngestResult result;

    if (chunks.size() != embeddings.size()) {
        result.errors.push_back("Chunk/embedding count mismatch: " +
                                std::to_string(chunks.size()) + " chunks vs " +
                                std::to_string(embeddings.size()) + " embeddings");
        auto end_t = std::chrono::steady_clock::now();
        result.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_t - start);
        return result;
    }

    // Stamp repo tag and store in LTM
    std::vector<CodeChunk> stamped_chunks = chunks;
    for (auto& chunk : stamped_chunks) {
        chunk.tags.push_back("repo:" + repo_id);
    }

    size_t batch_size = static_cast<size_t>(config_.chunk_batch_size);
    for (size_t offset = 0; offset < stamped_chunks.size(); offset += batch_size) {
        size_t end_idx = std::min(offset + batch_size, stamped_chunks.size());
        std::vector<CodeChunk> chunk_batch(stamped_chunks.begin() + static_cast<ptrdiff_t>(offset),
                                           stamped_chunks.begin() + static_cast<ptrdiff_t>(end_idx));
        std::vector<std::vector<float>> emb_batch(embeddings.begin() + static_cast<ptrdiff_t>(offset),
                                                   embeddings.begin() + static_cast<ptrdiff_t>(end_idx));

        try {
            ltm_.addBatch(chunk_batch, emb_batch);
            result.chunks_stored += chunk_batch.size();
        } catch (const std::exception& e) {
            result.errors.push_back(std::string("LTM addBatch error: ") + e.what());
            result.chunks_failed += chunk_batch.size();
            if (!config_.continue_on_error) break;
        }
        result.chunks_processed += chunk_batch.size();
    }

    // Detect patterns via MTM
    if (config_.detect_patterns && config_.update_mtm && mtm_ != nullptr) {
        detectAndStorePatterns(stamped_chunks);
    }

    auto end_t = std::chrono::steady_clock::now();
    result.total_time = std::chrono::duration_cast<std::chrono::milliseconds>(end_t - start);
    return result;
}

// =============================================================================
// detectAndStorePatterns
// =============================================================================

void EmbeddingIngestor::detectAndStorePatterns(const std::vector<CodeChunk>& chunks) {
    if (!mtm_) return;

    // Group chunks by language to detect language-specific patterns
    std::unordered_map<std::string, std::vector<const CodeChunk*>> by_language;
    for (const auto& chunk : chunks) {
        if (!chunk.language.empty()) {
            by_language[chunk.language].push_back(&chunk);
        }
    }

    for (const auto& [language, lang_chunks] : by_language) {
        // Create a summary pattern entry for each language in this ingestion
        PatternEntry pattern;
        pattern.id = "auto_" + language + "_" + std::to_string(
            std::chrono::steady_clock::now().time_since_epoch().count());
        pattern.name = language + " code pattern";
        pattern.description = "Auto-detected pattern from " +
                              std::to_string(lang_chunks.size()) + " " + language + " chunks";
        pattern.pattern_type = "architecture";
        pattern.applicable_languages.push_back(language);
        pattern.occurrence_count = static_cast<int>(lang_chunks.size());
        pattern.confidence = 0.5;

        // Collect example snippets (up to 5)
        for (size_t i = 0; i < std::min<size_t>(5, lang_chunks.size()); ++i) {
            pattern.example_chunk_ids.push_back(lang_chunks[i]->id);
            std::string snippet = lang_chunks[i]->content.substr(
                0, std::min<size_t>(200, lang_chunks[i]->content.size()));
            pattern.example_snippets.push_back(snippet);
        }

        // Compute average embedding for the pattern
        if (!lang_chunks.empty()) {
            // Embed a representative sample
            std::vector<std::string> sample_texts;
            for (size_t i = 0; i < std::min<size_t>(10, lang_chunks.size()); ++i) {
                sample_texts.push_back(lang_chunks[i]->content);
            }
            auto batch_result = embedding_engine_.embedBatch(sample_texts);
            if (batch_result.successful_count > 0) {
                size_t dim = batch_result.embeddings[0].size();
                pattern.embedding.resize(dim, 0.0f);
                int count = 0;
                for (const auto& emb : batch_result.embeddings) {
                    if (!emb.empty()) {
                        for (size_t d = 0; d < dim; ++d) {
                            pattern.embedding[d] += emb[d];
                        }
                        count++;
                    }
                }
                if (count > 0) {
                    for (float& v : pattern.embedding) v /= static_cast<float>(count);
                }
            }
        }

        mtm_->storePattern(pattern);
    }
}

} // namespace aipr::tms
