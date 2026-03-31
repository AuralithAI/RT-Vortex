/**
 * MultiModal Embedder — Manages per-modality ONNX sessions
 *
 * Architecture:
 * ┌───────────────────────────────────────────────────────────────────┐
 * │                     MultiModalEmbedder                            │
 * │                                                                   │
 * │  ┌───────────────┐  ┌───────────────┐  ┌──────────────┐           │
 * │  │  TEXT slot    │  │  IMAGE slot   │  │  AUDIO slot  │           │
 * │  │  EmbeddingEng │  │  SigLIP ONNX  │  │  CLAP ONNX   │           │
 * │  │  1024d        │  │  768d         │  │  512d        │           │
 * │  └──────┬────────┘  └──────┬────────┘  └──────┬───────┘           │
 * │         │ identity         │ linear           │ linear            │
 * │         ▼                  ▼                  ▼                   │
 * │  ┌────────────────────────────────────────────────────────────┐   │
 * │  │              Unified 1024d Embedding Space                 │   │
 * │  │              (stored in single FAISS index)                │   │
 * │  └────────────────────────────────────────────────────────────┘   │
 * └───────────────────────────────────────────────────────────────────┘
 *
 * The text slot is the existing EmbeddingEngine (bge-m3 or MiniLM).
 * Image and audio slots are optional — they auto-download on first boot
 * when enabled, and gracefully degrade to "not available" if missing.
 *
 * Projection layers are simple learned linear maps:
 *   image: W_img (768 × 1024) + b_img (1024)
 *   audio: W_aud (512 × 1024) + b_aud (1024)
 * Stored as small ONNX files (~4 MB each).
 */

#pragma once

#include "tms_types.h"
#include "embedding_engine.h"
#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include <map>
#include <functional>

namespace aipr::tms {

/**
 * Model status for a single modality slot.
 */
struct ModalityStatus {
    EmbeddingModality modality;
    std::string model_name;
    bool enabled = false;
    bool ready = false;             // ONNX session loaded and functional
    bool downloading = false;
    int download_progress = 0;      // 0-100
    size_t native_dimension = 0;
    size_t projected_dimension = 0;
    std::string error;
};

/**
 * Multimodal embedding result.
 */
struct MultiModalEmbeddingResult {
    std::vector<float> embedding;       // Projected to unified dimension
    EmbeddingModality modality;
    AssetType asset_type;
    int tokens_used = 0;
    bool success = true;
    std::string error;
};

/**
 * Progress callback for model downloads.
 */
using ModelDownloadCallback = std::function<void(
    const std::string& model_name,
    int progress,               // 0-100
    const std::string& phase,   // "downloading", "loading", "ready"
    bool done,
    bool success,
    const std::string& error
)>;

/**
 * MultiModalEmbedder — unified interface for all modality embeddings.
 */
class MultiModalEmbedder {
public:
    /**
     * Construct with the existing text EmbeddingEngine and TMS config.
     * The text engine is NOT owned — caller retains ownership.
     */
    MultiModalEmbedder(EmbeddingEngine& text_engine, const TMSConfig& config);
    ~MultiModalEmbedder();

    MultiModalEmbedder(const MultiModalEmbedder&) = delete;
    MultiModalEmbedder& operator=(const MultiModalEmbedder&) = delete;

    // ─── Initialization ──────────────────────────────────────────────

    /**
     * Initialize all enabled modality slots.
     * Auto-downloads missing models if enabled.
     *
     * @param models_dir  Base directory for model files (e.g. RTVORTEX_HOME/models)
     * @param progress    Optional download progress callback
     * @return true if at least the text modality is ready
     */
    bool initialize(const std::string& models_dir,
                    ModelDownloadCallback progress = nullptr);

    // ─── Embedding ───────────────────────────────────────────────────

    /**
     * Embed text content (code, PDF text, webpage text, documents).
     * Delegates to the existing text EmbeddingEngine.
     */
    MultiModalEmbeddingResult embedText(const std::string& text);

    /**
     * Embed an image from raw pixel data.
     *
     * @param image_data  Raw image bytes (PNG/JPG encoded)
     * @param mime_type   MIME type for format detection
     * @return Projected embedding in unified space
     */
    MultiModalEmbeddingResult embedImage(const std::vector<uint8_t>& image_data,
                                          const std::string& mime_type = "image/png");

    /**
     * Embed audio from raw PCM or encoded data.
     *
     * @param audio_data  Raw audio bytes (WAV/MP3 encoded)
     * @param mime_type   MIME type for format detection
     * @return Projected embedding in unified space
     */
    MultiModalEmbeddingResult embedAudio(const std::vector<uint8_t>& audio_data,
                                          const std::string& mime_type = "audio/wav");

    /**
     * Route-and-embed: automatically dispatch based on asset type.
     *
     * @param asset_type   Determines which model to use
     * @param text_content Text content (for TEXT modality assets)
     * @param binary_data  Binary data (for IMAGE/AUDIO assets)
     * @param mime_type    MIME type hint
     */
    MultiModalEmbeddingResult embed(AssetType asset_type,
                                     const std::string& text_content,
                                     const std::vector<uint8_t>& binary_data,
                                     const std::string& mime_type = "");

    // ─── Status & Configuration ──────────────────────────────────────

    /**
     * Get status of all modality slots.
     */
    std::map<EmbeddingModality, ModalityStatus> getStatus() const;

    /**
     * Check if a specific modality is available.
     */
    bool isModalityReady(EmbeddingModality modality) const;

    /**
     * Enable or disable a modality at runtime.
     * If enabling and the model isn't downloaded, triggers download.
     */
    bool setModalityEnabled(EmbeddingModality modality, bool enabled,
                            ModelDownloadCallback progress = nullptr);

    /**
     * Get the unified embedding dimension (all modalities project to this).
     */
    size_t unifiedDimension() const { return config_.unified_dimension; }

private:
    EmbeddingEngine& text_engine_;
    TMSConfig config_;
    std::string models_dir_;

    // Per-modality ONNX sessions (image + audio only; text reuses text_engine_)
    class ModalitySession;
    std::unique_ptr<ModalitySession> image_session_;
    std::unique_ptr<ModalitySession> audio_session_;

    mutable std::mutex mu_;

    // Projection: native_dim → unified_dim via learned linear layer
    std::vector<float> projectToUnified(const std::vector<float>& native_embedding,
                                         EmbeddingModality modality) const;

    // Model download helpers
    bool downloadAndLoadModel(EmbeddingModality modality,
                              ModelDownloadCallback progress);
};

} // namespace aipr::tms
