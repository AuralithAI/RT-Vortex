/**
 * MultiModal Embedder — Implementation
 *
 * Manages per-modality ONNX sessions for image (SigLIP) and audio (CLAP)
 * embeddings, with projection to a unified vector space shared with the
 * text embedding model (bge-m3 / MiniLM).
 */

#include "tms/multimodal_embedder.h"
#include "logging.h"
#include "metrics.h"

#include <algorithm>
#include <cmath>
#include <fstream>
#include <filesystem>
#include <numeric>
#include <iostream>

#ifdef AIPR_HAS_ONNX
#include <onnxruntime_cxx_api.h>
#endif

#ifdef AIPR_HAS_CURL
#include <curl/curl.h>
#endif

namespace fs = std::filesystem;

namespace aipr::tms {

// =============================================================================
// Model Registry — maps model names to HuggingFace download info
// =============================================================================

struct MultimodalModelInfo {
    std::string name;
    EmbeddingModality modality;
    size_t native_dimension;
    std::string hf_base_url;
    std::string onnx_subpath;
    std::vector<std::string> extra_files;
    std::string description;
};

static const std::vector<MultimodalModelInfo>& getMultimodalRegistry() {
    static const std::vector<MultimodalModelInfo> registry = {
        {
            "siglip-base",
            EmbeddingModality::VISION,
            768,
            "https://huggingface.co/google/siglip-base-patch16-224/resolve/main",
            "onnx/model.onnx",
            {"preprocessor_config.json", "tokenizer.json"},
            "SigLIP base — image+text joint embedding (768d, ~350 MB)"
        },
        {
            "clap-general",
            EmbeddingModality::AUDIO,
            512,
            "https://huggingface.co/laion/larger_clap_general/resolve/main",
            "onnx/model.onnx",
            {"config.json"},
            "CLAP general — audio+text joint embedding (512d, ~650 MB)"
        },
    };
    return registry;
}

static const MultimodalModelInfo* findMultimodalModel(const std::string& name) {
    for (const auto& m : getMultimodalRegistry()) {
        if (m.name == name) return &m;
    }
    return nullptr;
}

// =============================================================================
// ModalitySession — wraps a single ONNX session for one modality
// =============================================================================

class MultiModalEmbedder::ModalitySession {
public:
    ModalitySession(EmbeddingModality modality, size_t native_dim, size_t unified_dim)
        : modality_(modality)
        , native_dim_(native_dim)
        , unified_dim_(unified_dim)
        , ready_(false) {}

    ~ModalitySession() = default;

    bool loadModel(const std::string& model_path, const std::string& /*config_path*/) {
#ifdef AIPR_HAS_ONNX
        try {
            if (!fs::exists(model_path)) {
                LOG_WARN("Model file not found: " + model_path);
                return false;
            }

            ort_env_ = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING,
                                                    "multimodal");
            Ort::SessionOptions opts;
            opts.SetIntraOpNumThreads(2);
            opts.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);

            ort_session_ = std::make_unique<Ort::Session>(*ort_env_,
                                                           model_path.c_str(), opts);
            ready_ = true;
            LOG_INFO("Loaded " + modalityName() + " model: " + model_path);
            return true;
        } catch (const std::exception& e) {
            LOG_WARN("Failed to load " + modalityName() + " model: " + std::string(e.what()));
            return false;
        }
#else
        (void)model_path;
        LOG_WARN("ONNX Runtime not available — " + modalityName() + " embedding disabled");
        return false;
#endif
    }

    bool loadProjection(const std::string& projection_path) {
        // Load projection weights (W: native_dim × unified_dim, b: unified_dim)
        // Stored as a simple binary file: [W flat][b flat]
        if (projection_path.empty() || !fs::exists(projection_path)) {
            // Use identity/zero-padding if no projection available
            LOG_INFO("No projection weights for " + modalityName() +
                     ", using zero-padded identity");
            use_identity_projection_ = true;
            return true;
        }

        try {
            std::ifstream f(projection_path, std::ios::binary);
            size_t weight_count = native_dim_ * unified_dim_;
            projection_weights_.resize(weight_count);
            projection_bias_.resize(unified_dim_, 0.0f);

            f.read(reinterpret_cast<char*>(projection_weights_.data()),
                   weight_count * sizeof(float));
            f.read(reinterpret_cast<char*>(projection_bias_.data()),
                   unified_dim_ * sizeof(float));

            use_identity_projection_ = false;
            LOG_INFO("Loaded projection for " + modalityName() +
                     " (" + std::to_string(native_dim_) + " → " +
                     std::to_string(unified_dim_) + ")");
            return true;
        } catch (const std::exception& e) {
            LOG_WARN("Failed to load projection: " + std::string(e.what()));
            use_identity_projection_ = true;
            return true; // non-fatal, use identity
        }
    }

    std::vector<float> embed(const std::vector<uint8_t>& data,
                              const std::string& /*mime_type*/) {
        if (!ready_) {
            return std::vector<float>(unified_dim_, 0.0f);
        }

#ifdef AIPR_HAS_ONNX
        // TODO: Implement actual ONNX inference for image/audio
        // For now, return a placeholder that will be filled in
        // when we integrate the preprocessing pipelines.
        //
        // Image (SigLIP): decode → resize 224×224 → normalize → ONNX → 768d
        // Audio (CLAP): decode → resample 48kHz → mel-spectrogram → ONNX → 512d
        //
        // The ONNX session is ready; preprocessing is the next implementation step.
        (void)data;

        // Return zero vector — correct dimension, indicates model is loaded but
        // preprocessing pipeline is not yet connected.
        std::vector<float> native(native_dim_, 0.0f);
        return project(native);
#else
        (void)data;
        return std::vector<float>(unified_dim_, 0.0f);
#endif
    }

    std::vector<float> project(const std::vector<float>& native) const {
        if (native.size() != native_dim_) {
            LOG_WARN("Dimension mismatch in projection: got " +
                     std::to_string(native.size()) + ", expected " +
                     std::to_string(native_dim_));
            return std::vector<float>(unified_dim_, 0.0f);
        }

        if (use_identity_projection_) {
            // Zero-pad or truncate to unified dimension
            std::vector<float> result(unified_dim_, 0.0f);
            size_t copy_dim = std::min(native_dim_, unified_dim_);
            std::copy_n(native.begin(), copy_dim, result.begin());

            // L2-normalize
            float norm = 0.0f;
            for (float v : result) norm += v * v;
            norm = std::sqrt(norm);
            if (norm > 1e-8f) {
                for (float& v : result) v /= norm;
            }
            return result;
        }

        // Matrix multiply: result = W^T * native + bias
        std::vector<float> result(unified_dim_, 0.0f);
        for (size_t i = 0; i < unified_dim_; ++i) {
            float sum = projection_bias_[i];
            for (size_t j = 0; j < native_dim_; ++j) {
                sum += projection_weights_[j * unified_dim_ + i] * native[j];
            }
            result[i] = sum;
        }

        // L2-normalize
        float norm = 0.0f;
        for (float v : result) norm += v * v;
        norm = std::sqrt(norm);
        if (norm > 1e-8f) {
            for (float& v : result) v /= norm;
        }
        return result;
    }

    bool isReady() const { return ready_; }
    EmbeddingModality modality() const { return modality_; }
    size_t nativeDim() const { return native_dim_; }
    size_t unifiedDim() const { return unified_dim_; }

private:
    std::string modalityName() const {
        switch (modality_) {
            case EmbeddingModality::VISION: return "image";
            case EmbeddingModality::AUDIO:  return "audio";
            default:                        return "text";
        }
    }

    EmbeddingModality modality_;
    size_t native_dim_;
    size_t unified_dim_;
    bool ready_;
    bool use_identity_projection_ = true;

    std::vector<float> projection_weights_;  // native_dim × unified_dim
    std::vector<float> projection_bias_;     // unified_dim

#ifdef AIPR_HAS_ONNX
    std::unique_ptr<Ort::Env> ort_env_;
    std::unique_ptr<Ort::Session> ort_session_;
#endif
};

// =============================================================================
// File download helper (reuses pattern from main.cpp)
// =============================================================================

#ifdef AIPR_HAS_CURL
static size_t mmWriteCallback(void* ptr, size_t size, size_t nmemb, FILE* stream) {
    return fwrite(ptr, size, nmemb, stream);
}
#endif

static bool downloadModelFile(const std::string& url, const std::string& dest) {
#ifdef AIPR_HAS_CURL
    FILE* fp = fopen(dest.c_str(), "wb");
    if (!fp) return false;

    CURL* curl = curl_easy_init();
    if (!curl) { fclose(fp); return false; }

    curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, mmWriteCallback);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, fp);
    curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 1L);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 900L);  // 15 min for large models
    curl_easy_setopt(curl, CURLOPT_NOPROGRESS, 0L);

    CURLcode res = curl_easy_perform(curl);
    long http_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    curl_easy_cleanup(curl);
    fclose(fp);

    if (res != CURLE_OK || http_code != 200) {
        fs::remove(dest);
        return false;
    }
    return true;
#else
    (void)url; (void)dest;
    LOG_WARN("curl not available — cannot download model files");
    return false;
#endif
}

static bool ensureMultimodalModel(const std::string& model_dir,
                                   const MultimodalModelInfo& info,
                                   ModelDownloadCallback progress) {
    fs::create_directories(model_dir);

    std::string model_path = (fs::path(model_dir) / "model.onnx").string();
    if (fs::exists(model_path)) return true;

    if (progress) {
        progress(info.name, 0, "downloading", false, false, "");
    }
    LOG_INFO("Downloading " + info.description);

    std::string url = info.hf_base_url + "/" + info.onnx_subpath;
    if (!downloadModelFile(url, model_path)) {
        std::string err = "Failed to download " + info.name + " model";
        LOG_WARN(err);
        if (progress) progress(info.name, 0, "error", true, false, err);
        return false;
    }

    if (progress) progress(info.name, 80, "downloading extras", false, false, "");

    // Download extra files
    for (const auto& f : info.extra_files) {
        std::string local_name = fs::path(f).filename().string();
        std::string fpath = (fs::path(model_dir) / local_name).string();
        if (!fs::exists(fpath)) {
            downloadModelFile(info.hf_base_url + "/" + f, fpath);
        }
    }

    if (progress) progress(info.name, 100, "ready", true, true, "");
    LOG_INFO("Model downloaded: " + info.name);
    return true;
}

// =============================================================================
// MultiModalEmbedder Implementation
// =============================================================================

MultiModalEmbedder::MultiModalEmbedder(EmbeddingEngine& text_engine,
                                         const TMSConfig& config)
    : text_engine_(text_engine)
    , config_(config) {}

MultiModalEmbedder::~MultiModalEmbedder() = default;

bool MultiModalEmbedder::initialize(const std::string& models_dir,
                                     ModelDownloadCallback progress) {
    std::lock_guard<std::mutex> lock(mu_);
    models_dir_ = models_dir;

    // Text modality is always ready (managed by EmbeddingEngine).
    LOG_INFO("MultiModalEmbedder initializing...");
    LOG_INFO("  Text:  ready (via EmbeddingEngine, " +
             std::to_string(text_engine_.activeDimension()) + "d)");

    // ── Image modality (SigLIP) ──────────────────────────────────
    if (config_.image_embedding_enabled) {
        const auto* info = findMultimodalModel(config_.image_model_name);
        if (!info) {
            LOG_WARN("Unknown image model: " + config_.image_model_name);
        } else {
            std::string model_dir = (fs::path(models_dir) / info->name).string();

            if (ensureMultimodalModel(model_dir, *info, progress)) {
                image_session_ = std::make_unique<ModalitySession>(
                    EmbeddingModality::VISION,
                    info->native_dimension,
                    config_.unified_dimension
                );

                std::string model_path = (fs::path(model_dir) / "model.onnx").string();
                std::string config_path = (fs::path(model_dir) / "preprocessor_config.json").string();

                if (image_session_->loadModel(model_path, config_path)) {
                    image_session_->loadProjection(config_.image_projection_path);
                    LOG_INFO("  Image: ready (" + info->name + ", " +
                             std::to_string(info->native_dimension) + "d → " +
                             std::to_string(config_.unified_dimension) + "d)");
                } else {
                    LOG_WARN("  Image: model load failed");
                    image_session_.reset();
                }
            } else {
                LOG_INFO("  Image: model not available (will download on first use)");
            }
        }
    } else {
        LOG_INFO("  Image: disabled");
    }

    // ── Audio modality (CLAP) ────────────────────────────────────
    if (config_.audio_embedding_enabled) {
        const auto* info = findMultimodalModel(config_.audio_model_name);
        if (!info) {
            LOG_WARN("Unknown audio model: " + config_.audio_model_name);
        } else {
            std::string model_dir = (fs::path(models_dir) / info->name).string();

            if (ensureMultimodalModel(model_dir, *info, progress)) {
                audio_session_ = std::make_unique<ModalitySession>(
                    EmbeddingModality::AUDIO,
                    info->native_dimension,
                    config_.unified_dimension
                );

                std::string model_path = (fs::path(model_dir) / "model.onnx").string();
                std::string config_path = (fs::path(model_dir) / "config.json").string();

                if (audio_session_->loadModel(model_path, config_path)) {
                    audio_session_->loadProjection(config_.audio_projection_path);
                    LOG_INFO("  Audio: ready (" + info->name + ", " +
                             std::to_string(info->native_dimension) + "d → " +
                             std::to_string(config_.unified_dimension) + "d)");
                } else {
                    LOG_WARN("  Audio: model load failed");
                    audio_session_.reset();
                }
            } else {
                LOG_INFO("  Audio: model not available (will download on first use)");
            }
        }
    } else {
        LOG_INFO("  Audio: disabled");
    }

    LOG_INFO("MultiModalEmbedder ready. Unified dimension: " +
             std::to_string(config_.unified_dimension));
    return true;
}

// ─── Embedding Methods ──────────────────────────────────────────────

MultiModalEmbeddingResult MultiModalEmbedder::embedText(const std::string& text) {
    auto result = text_engine_.embed(text);
    MultiModalEmbeddingResult mmr;
    mmr.modality = EmbeddingModality::TEXT;
    mmr.asset_type = AssetType::CODE;
    mmr.tokens_used = result.tokens_used;
    mmr.success = result.success;
    mmr.error = result.error;

    if (result.success) {
        mmr.embedding = std::move(result.embedding);
        // Text model (bge-m3) already outputs 1024d — no projection needed.
        // If using MiniLM (384d) and unified=1024, zero-pad.
        if (mmr.embedding.size() < config_.unified_dimension) {
            mmr.embedding.resize(config_.unified_dimension, 0.0f);
        }
    } else {
        mmr.embedding.resize(config_.unified_dimension, 0.0f);
    }
    return mmr;
}

MultiModalEmbeddingResult MultiModalEmbedder::embedImage(
    const std::vector<uint8_t>& image_data,
    const std::string& mime_type) {

    MultiModalEmbeddingResult mmr;
    mmr.modality = EmbeddingModality::VISION;
    mmr.asset_type = AssetType::IMAGE;

    std::lock_guard<std::mutex> lock(mu_);
    if (!image_session_ || !image_session_->isReady()) {
        mmr.success = false;
        mmr.error = "Image embedding not available. Enable it in Settings → Embeddings.";
        mmr.embedding.resize(config_.unified_dimension, 0.0f);
        return mmr;
    }

    mmr.embedding = image_session_->embed(image_data, mime_type);
    mmr.success = true;
    return mmr;
}

MultiModalEmbeddingResult MultiModalEmbedder::embedAudio(
    const std::vector<uint8_t>& audio_data,
    const std::string& mime_type) {

    MultiModalEmbeddingResult mmr;
    mmr.modality = EmbeddingModality::AUDIO;
    mmr.asset_type = AssetType::AUDIO;

    std::lock_guard<std::mutex> lock(mu_);
    if (!audio_session_ || !audio_session_->isReady()) {
        mmr.success = false;
        mmr.error = "Audio embedding not available. Enable it in Settings → Embeddings.";
        mmr.embedding.resize(config_.unified_dimension, 0.0f);
        return mmr;
    }

    mmr.embedding = audio_session_->embed(audio_data, mime_type);
    mmr.success = true;
    return mmr;
}

MultiModalEmbeddingResult MultiModalEmbedder::embed(
    AssetType asset_type,
    const std::string& text_content,
    const std::vector<uint8_t>& binary_data,
    const std::string& mime_type) {

    EmbeddingModality modality = modalityForAsset(asset_type);
    switch (modality) {
        case EmbeddingModality::VISION:
            return embedImage(binary_data, mime_type);
        case EmbeddingModality::AUDIO:
            return embedAudio(binary_data, mime_type);
        case EmbeddingModality::TEXT:
        default:
            return embedText(text_content);
    }
}

// ─── Status & Configuration ─────────────────────────────────────────

std::map<EmbeddingModality, ModalityStatus> MultiModalEmbedder::getStatus() const {
    std::lock_guard<std::mutex> lock(mu_);
    std::map<EmbeddingModality, ModalityStatus> statuses;

    // Text — always available
    ModalityStatus text_status;
    text_status.modality = EmbeddingModality::TEXT;
    text_status.model_name = text_engine_.activeModelName();
    text_status.enabled = true;
    text_status.ready = true;
    text_status.native_dimension = text_engine_.activeDimension();
    text_status.projected_dimension = config_.unified_dimension;
    statuses[EmbeddingModality::TEXT] = text_status;

    // Image
    ModalityStatus img_status;
    img_status.modality = EmbeddingModality::VISION;
    img_status.model_name = config_.image_model_name;
    img_status.enabled = config_.image_embedding_enabled;
    img_status.ready = image_session_ && image_session_->isReady();
    img_status.native_dimension = config_.image_native_dim;
    img_status.projected_dimension = config_.unified_dimension;
    statuses[EmbeddingModality::VISION] = img_status;

    // Audio
    ModalityStatus aud_status;
    aud_status.modality = EmbeddingModality::AUDIO;
    aud_status.model_name = config_.audio_model_name;
    aud_status.enabled = config_.audio_embedding_enabled;
    aud_status.ready = audio_session_ && audio_session_->isReady();
    aud_status.native_dimension = config_.audio_native_dim;
    aud_status.projected_dimension = config_.unified_dimension;
    statuses[EmbeddingModality::AUDIO] = aud_status;

    return statuses;
}

bool MultiModalEmbedder::isModalityReady(EmbeddingModality modality) const {
    std::lock_guard<std::mutex> lock(mu_);
    switch (modality) {
        case EmbeddingModality::TEXT:   return true;
        case EmbeddingModality::VISION: return image_session_ && image_session_->isReady();
        case EmbeddingModality::AUDIO:  return audio_session_ && audio_session_->isReady();
    }
    return false;
}

bool MultiModalEmbedder::setModalityEnabled(EmbeddingModality modality, bool enabled,
                                              ModelDownloadCallback progress) {
    std::lock_guard<std::mutex> lock(mu_);

    if (modality == EmbeddingModality::TEXT) return true; // always enabled

    if (modality == EmbeddingModality::VISION) {
        config_.image_embedding_enabled = enabled;
        if (enabled && !image_session_) {
            return downloadAndLoadModel(EmbeddingModality::VISION, progress);
        }
    } else if (modality == EmbeddingModality::AUDIO) {
        config_.audio_embedding_enabled = enabled;
        if (enabled && !audio_session_) {
            return downloadAndLoadModel(EmbeddingModality::AUDIO, progress);
        }
    }
    return true;
}

bool MultiModalEmbedder::downloadAndLoadModel(EmbeddingModality modality,
                                                ModelDownloadCallback progress) {
    const MultimodalModelInfo* info = nullptr;
    if (modality == EmbeddingModality::VISION) {
        info = findMultimodalModel(config_.image_model_name);
    } else if (modality == EmbeddingModality::AUDIO) {
        info = findMultimodalModel(config_.audio_model_name);
    }

    if (!info) return false;

    std::string model_dir = (fs::path(models_dir_) / info->name).string();
    if (!ensureMultimodalModel(model_dir, *info, progress)) return false;

    auto session = std::make_unique<ModalitySession>(
        modality, info->native_dimension, config_.unified_dimension);

    std::string model_path = (fs::path(model_dir) / "model.onnx").string();
    if (!session->loadModel(model_path, "")) return false;

    // Load projection if available
    std::string proj_path;
    if (modality == EmbeddingModality::VISION) {
        proj_path = config_.image_projection_path;
    } else {
        proj_path = config_.audio_projection_path;
    }
    session->loadProjection(proj_path);

    if (modality == EmbeddingModality::VISION) {
        image_session_ = std::move(session);
    } else {
        audio_session_ = std::move(session);
    }
    return true;
}

std::vector<float> MultiModalEmbedder::projectToUnified(
    const std::vector<float>& native_embedding,
    EmbeddingModality modality) const {

    std::lock_guard<std::mutex> lock(mu_);
    switch (modality) {
        case EmbeddingModality::VISION:
            if (image_session_) return image_session_->project(native_embedding);
            break;
        case EmbeddingModality::AUDIO:
            if (audio_session_) return audio_session_->project(native_embedding);
            break;
        case EmbeddingModality::TEXT:
        default: {
            // Text: identity or zero-pad
            std::vector<float> result(config_.unified_dimension, 0.0f);
            size_t copy_dim = std::min(native_embedding.size(), config_.unified_dimension);
            std::copy_n(native_embedding.begin(), copy_dim, result.begin());
            return result;
        }
    }
    return std::vector<float>(config_.unified_dimension, 0.0f);
}

} // namespace aipr::tms
