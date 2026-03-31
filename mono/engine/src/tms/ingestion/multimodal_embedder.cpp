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
#include <cstring>

#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif

#ifdef AIPR_HAS_ONNX
#include <onnxruntime_cxx_api.h>
#endif

#ifdef AIPR_HAS_CURL
#include <curl/curl.h>
#endif

// stb_image for JPEG/PNG decoding (header-only)
#define STB_IMAGE_IMPLEMENTATION
#define STBI_NO_GIF
#define STBI_NO_PSD
#define STBI_NO_PIC
#define STBI_NO_PNM
#define STBI_NO_HDR
#define STBI_NO_TGA
#include "stb_image.h"

// stb_image_resize2 for bilinear resize
#define STB_IMAGE_RESIZE_IMPLEMENTATION
#include "stb_image_resize2.h"

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
            "https://huggingface.co/Xenova/siglip-base-patch16-224/resolve/main",
            "onnx/vision_model.onnx",
            {"preprocessor_config.json", "tokenizer.json"},
            "SigLIP base — vision encoder ONNX (768d, ~372 MB)"
        },
        {
            "clap-general",
            EmbeddingModality::AUDIO,
            512,
            "https://huggingface.co/Xenova/larger_clap_general/resolve/main",
            "onnx/audio_model.onnx",
            {"config.json", "preprocessor_config.json"},
            "CLAP general — audio encoder ONNX (512d, ~282 MB)"
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
// Image Preprocessing — decode + resize + normalize for SigLIP
// =============================================================================

// Preprocesses raw image bytes into SigLIP's expected input:
//   pixel_values: [1, 3, 224, 224] float32
//   rescale ×(1/255), normalize (mean=0.5, std=0.5) → range [-1, 1]
static bool preprocessImage(const std::vector<uint8_t>& raw,
                             std::vector<float>& out_tensor) {
    constexpr int TARGET_W = 224;
    constexpr int TARGET_H = 224;
    constexpr int CHANNELS = 3;
    constexpr float RESCALE = 1.0f / 255.0f;
    constexpr float MEAN = 0.5f;
    constexpr float STD  = 0.5f;

    // Decode image using stb_image
    int w = 0, h = 0, c = 0;
    unsigned char* pixels = stbi_load_from_memory(
        raw.data(), static_cast<int>(raw.size()), &w, &h, &c, CHANNELS);
    if (!pixels) {
        LOG_WARN("stb_image: failed to decode image (" +
                 std::string(stbi_failure_reason()) + ")");
        return false;
    }

    // Resize to 224×224 using bilinear interpolation (Lanczos3 / resample=3)
    std::vector<unsigned char> resized(TARGET_W * TARGET_H * CHANNELS);
    stbir_resize_uint8_linear(
        pixels, w, h, w * CHANNELS,
        resized.data(), TARGET_W, TARGET_H, TARGET_W * CHANNELS,
        static_cast<stbir_pixel_layout>(CHANNELS));
    stbi_image_free(pixels);

    // Convert to CHW float32 with SigLIP normalization:
    //   value = (pixel / 255.0 - 0.5) / 0.5
    out_tensor.resize(1 * CHANNELS * TARGET_H * TARGET_W);
    for (int ch = 0; ch < CHANNELS; ++ch) {
        for (int y = 0; y < TARGET_H; ++y) {
            for (int x = 0; x < TARGET_W; ++x) {
                int src_idx = (y * TARGET_W + x) * CHANNELS + ch;
                int dst_idx = ch * TARGET_H * TARGET_W + y * TARGET_W + x;
                float val = static_cast<float>(resized[src_idx]) * RESCALE;
                out_tensor[dst_idx] = (val - MEAN) / STD;
            }
        }
    }
    return true;
}

// =============================================================================
// Audio Preprocessing — decode WAV + mel spectrogram for CLAP
// =============================================================================

// Minimal WAV parser: extracts raw PCM float samples from WAV/RIFF containers.
// Supports 16-bit PCM and 32-bit float formats (mono or stereo → force mono).
static bool decodeWAV(const std::vector<uint8_t>& raw,
                       std::vector<float>& samples, int& sample_rate) {
    if (raw.size() < 44) return false;

    // Verify RIFF header
    if (std::memcmp(raw.data(), "RIFF", 4) != 0 ||
        std::memcmp(raw.data() + 8, "WAVE", 4) != 0) {
        LOG_WARN("Audio: not a valid WAV file (missing RIFF/WAVE header)");
        return false;
    }

    // Parse chunks
    size_t pos = 12;
    int num_channels = 0, bits_per_sample = 0;
    int audio_format = 0;
    const uint8_t* data_start = nullptr;
    size_t data_size = 0;

    while (pos + 8 <= raw.size()) {
        char chunk_id[5] = {};
        std::memcpy(chunk_id, raw.data() + pos, 4);
        uint32_t chunk_size = 0;
        std::memcpy(&chunk_size, raw.data() + pos + 4, 4);

        if (std::strcmp(chunk_id, "fmt ") == 0 && pos + 8 + chunk_size <= raw.size()) {
            const uint8_t* fmt = raw.data() + pos + 8;
            std::memcpy(&audio_format, fmt, 2);
            std::memcpy(&num_channels, fmt + 2, 2);
            std::memcpy(&sample_rate, fmt + 4, 4);
            std::memcpy(&bits_per_sample, fmt + 14, 2);
        } else if (std::strcmp(chunk_id, "data") == 0) {
            data_start = raw.data() + pos + 8;
            data_size = std::min(static_cast<size_t>(chunk_size), raw.size() - pos - 8);
        }

        pos += 8 + ((chunk_size + 1) & ~1u); // chunks are 2-byte aligned
    }

    if (!data_start || data_size == 0 || num_channels == 0) {
        LOG_WARN("Audio: WAV file has no data chunk or invalid format");
        return false;
    }

    // Decode samples
    size_t num_samples = 0;
    if (audio_format == 1 && bits_per_sample == 16) {
        // PCM 16-bit
        num_samples = data_size / (2 * num_channels);
        samples.resize(num_samples);
        for (size_t i = 0; i < num_samples; ++i) {
            float sum = 0.0f;
            for (int ch = 0; ch < num_channels; ++ch) {
                int16_t s = 0;
                std::memcpy(&s, data_start + (i * num_channels + ch) * 2, 2);
                sum += static_cast<float>(s) / 32768.0f;
            }
            samples[i] = sum / num_channels;
        }
    } else if (audio_format == 3 && bits_per_sample == 32) {
        // IEEE float 32-bit
        num_samples = data_size / (4 * num_channels);
        samples.resize(num_samples);
        for (size_t i = 0; i < num_samples; ++i) {
            float sum = 0.0f;
            for (int ch = 0; ch < num_channels; ++ch) {
                float s = 0.0f;
                std::memcpy(&s, data_start + (i * num_channels + ch) * 4, 4);
                sum += s;
            }
            samples[i] = sum / num_channels;
        }
    } else if (audio_format == 1 && bits_per_sample == 24) {
        // PCM 24-bit
        num_samples = data_size / (3 * num_channels);
        samples.resize(num_samples);
        for (size_t i = 0; i < num_samples; ++i) {
            float sum = 0.0f;
            for (int ch = 0; ch < num_channels; ++ch) {
                const uint8_t* p = data_start + (i * num_channels + ch) * 3;
                int32_t s = (static_cast<int32_t>(p[2]) << 24) |
                            (static_cast<int32_t>(p[1]) << 16) |
                            (static_cast<int32_t>(p[0]) << 8);
                s >>= 8; // sign-extend
                sum += static_cast<float>(s) / 8388608.0f;
            }
            samples[i] = sum / num_channels;
        }
    } else {
        LOG_WARN("Audio: unsupported WAV format (fmt=" + std::to_string(audio_format) +
                 ", bits=" + std::to_string(bits_per_sample) + ")");
        return false;
    }

    return !samples.empty();
}

// Simple linear resampler: converts between sample rates.
static std::vector<float> resampleLinear(const std::vector<float>& input,
                                          int from_rate, int to_rate) {
    if (from_rate == to_rate) return input;
    double ratio = static_cast<double>(to_rate) / from_rate;
    size_t out_len = static_cast<size_t>(input.size() * ratio);
    std::vector<float> output(out_len);
    for (size_t i = 0; i < out_len; ++i) {
        double src_idx = i / ratio;
        size_t idx0 = static_cast<size_t>(src_idx);
        double frac = src_idx - idx0;
        size_t idx1 = std::min(idx0 + 1, input.size() - 1);
        output[i] = static_cast<float>(input[idx0] * (1.0 - frac) + input[idx1] * frac);
    }
    return output;
}

// Compute a 64-bin mel spectrogram matching ClapFeatureExtractor config:
//   sampling_rate: 48000, n_fft: 1024, hop_length: 480,
//   feature_size: 64 mel bins, max_length_s: 10 → 1001 frames
// Returns [1, 1, 1001, 64] tensor as flat vector.
static bool computeMelSpectrogram(const std::vector<float>& audio_48k,
                                   std::vector<float>& out_tensor) {
    constexpr int SAMPLE_RATE = 48000;
    constexpr int N_FFT = 1024;
    constexpr int HOP_LENGTH = 480;
    constexpr int N_MELS = 64;
    constexpr int TARGET_FRAMES = 1001;
    constexpr int MAX_SAMPLES = SAMPLE_RATE * 10;  // 10 seconds
    constexpr float FREQ_MIN = 50.0f;
    constexpr float FREQ_MAX = 14000.0f;

    // Pad or truncate to 10 seconds (repeat-pad if shorter)
    std::vector<float> audio(MAX_SAMPLES, 0.0f);
    if (audio_48k.empty()) return false;

    // Repeat-pad strategy: tile the audio to fill 10s
    for (size_t i = 0; i < static_cast<size_t>(MAX_SAMPLES); ++i) {
        audio[i] = audio_48k[i % audio_48k.size()];
    }

    // Precompute Hann window
    std::vector<float> window(N_FFT);
    for (int i = 0; i < N_FFT; ++i) {
        window[i] = 0.5f * (1.0f - std::cos(2.0f * M_PI * i / N_FFT));
    }

    // Compute STFT magnitude squared
    int n_freq = N_FFT / 2 + 1;  // 513 bins
    int n_frames = (static_cast<int>(audio.size()) - N_FFT) / HOP_LENGTH + 1;
    if (n_frames < TARGET_FRAMES) n_frames = TARGET_FRAMES;

    std::vector<float> power_spec(n_frames * n_freq, 0.0f);

    for (int frame = 0; frame < std::min(n_frames, static_cast<int>(audio.size() / HOP_LENGTH)); ++frame) {
        int start = frame * HOP_LENGTH;

        // Apply window and compute DFT via direct computation
        // (FFT would be faster but adds dependency; 1001 frames × 513 bins is tractable)
        for (int k = 0; k < n_freq; ++k) {
            float re = 0.0f, im = 0.0f;
            for (int n = 0; n < N_FFT; ++n) {
                int sample_idx = start + n;
                float val = (sample_idx < static_cast<int>(audio.size()))
                            ? audio[sample_idx] * window[n] : 0.0f;
                float angle = 2.0f * M_PI * k * n / N_FFT;
                re += val * std::cos(angle);
                im -= val * std::sin(angle);
            }
            power_spec[frame * n_freq + k] = re * re + im * im;
        }
    }

    // Build mel filter bank (triangular filters, HTK-style)
    auto hzToMel = [](float hz) { return 2595.0f * std::log10(1.0f + hz / 700.0f); };
    auto melToHz = [](float mel) { return 700.0f * (std::pow(10.0f, mel / 2595.0f) - 1.0f); };

    float mel_min = hzToMel(FREQ_MIN);
    float mel_max = hzToMel(FREQ_MAX);
    std::vector<float> mel_points(N_MELS + 2);
    for (int i = 0; i < N_MELS + 2; ++i) {
        float mel = mel_min + (mel_max - mel_min) * i / (N_MELS + 1);
        mel_points[i] = melToHz(mel);
    }

    // Convert Hz to FFT bin indices
    std::vector<float> fft_bins(N_MELS + 2);
    for (int i = 0; i < N_MELS + 2; ++i) {
        fft_bins[i] = mel_points[i] * N_FFT / SAMPLE_RATE;
    }

    // Apply mel filter bank: [N_MELS × n_freq] × [n_freq] → [N_MELS]
    // Output: [n_frames, N_MELS]
    out_tensor.resize(1 * 1 * TARGET_FRAMES * N_MELS, 0.0f);

    for (int frame = 0; frame < TARGET_FRAMES && frame < n_frames; ++frame) {
        for (int m = 0; m < N_MELS; ++m) {
            float sum = 0.0f;
            int f_start = static_cast<int>(std::floor(fft_bins[m]));
            int f_center = static_cast<int>(std::floor(fft_bins[m + 1]));
            int f_end = static_cast<int>(std::floor(fft_bins[m + 2]));

            // Rising slope
            for (int k = f_start; k <= f_center && k < n_freq; ++k) {
                float weight = (fft_bins[m + 1] - fft_bins[m]) > 0
                    ? (k - fft_bins[m]) / (fft_bins[m + 1] - fft_bins[m])
                    : 0.0f;
                weight = std::max(0.0f, weight);
                sum += power_spec[frame * n_freq + k] * weight;
            }
            // Falling slope
            for (int k = f_center + 1; k <= f_end && k < n_freq; ++k) {
                float weight = (fft_bins[m + 2] - fft_bins[m + 1]) > 0
                    ? (fft_bins[m + 2] - k) / (fft_bins[m + 2] - fft_bins[m + 1])
                    : 0.0f;
                weight = std::max(0.0f, weight);
                sum += power_spec[frame * n_freq + k] * weight;
            }

            // Convert to log mel (dB scale with floor)
            float log_mel = std::log10(std::max(sum, 1e-10f));
            out_tensor[frame * N_MELS + m] = log_mel;
        }
    }

    return true;
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
                              const std::string& mime_type) {
        if (!ready_) {
            LOG_WARN(modalityName() + ": session not ready");
            return std::vector<float>(unified_dim_, 0.0f);
        }

#ifdef AIPR_HAS_ONNX
        try {
            Ort::AllocatorWithDefaultOptions allocator;

            if (modality_ == EmbeddingModality::VISION) {
                // ── SigLIP inference ─────────────────────────────────
                // Input:  pixel_values [1, 3, 224, 224] float32
                // Output: pooler_output [1, 768] float32
                std::vector<float> pixel_tensor;
                if (!preprocessImage(data, pixel_tensor)) {
                    LOG_WARN("Image preprocessing failed");
                    return std::vector<float>(unified_dim_, 0.0f);
                }

                std::array<int64_t, 4> input_shape = {1, 3, 224, 224};
                auto memory_info = Ort::MemoryInfo::CreateCpu(
                    OrtAllocatorType::OrtArenaAllocator, OrtMemTypeDefault);
                Ort::Value input_tensor = Ort::Value::CreateTensor<float>(
                    memory_info, pixel_tensor.data(), pixel_tensor.size(),
                    input_shape.data(), input_shape.size());

                const char* input_names[] = {"pixel_values"};
                const char* output_names[] = {"pooler_output"};

                auto outputs = ort_session_->Run(
                    Ort::RunOptions{nullptr},
                    input_names, &input_tensor, 1,
                    output_names, 1);

                // Extract pooler_output [1, 768]
                float* output_data = outputs[0].GetTensorMutableData<float>();
                std::vector<float> native(output_data, output_data + native_dim_);

                LOG_INFO("SigLIP inference OK — L2 norm: " +
                         std::to_string(std::sqrt(std::inner_product(
                             native.begin(), native.end(), native.begin(), 0.0f))));
                return project(native);

            } else if (modality_ == EmbeddingModality::AUDIO) {
                // ── CLAP inference ───────────────────────────────────
                // Input:  input_features [1, 1, 1001, 64] float32
                // Output: audio_embeds [1, 512] float32
                std::vector<float> samples;
                int sample_rate = 0;

                if (!decodeWAV(data, samples, sample_rate)) {
                    LOG_WARN("Audio: WAV decode failed, mime=" + mime_type);
                    return std::vector<float>(unified_dim_, 0.0f);
                }

                // Resample to 48kHz if needed
                if (sample_rate != 48000 && sample_rate > 0) {
                    LOG_INFO("Audio: resampling from " + std::to_string(sample_rate) +
                             " Hz to 48000 Hz");
                    samples = resampleLinear(samples, sample_rate, 48000);
                }

                // Compute 64-bin mel spectrogram → [1, 1, 1001, 64]
                std::vector<float> mel_tensor;
                if (!computeMelSpectrogram(samples, mel_tensor)) {
                    LOG_WARN("Audio: mel spectrogram computation failed");
                    return std::vector<float>(unified_dim_, 0.0f);
                }

                std::array<int64_t, 4> input_shape = {1, 1, 1001, 64};
                auto memory_info = Ort::MemoryInfo::CreateCpu(
                    OrtAllocatorType::OrtArenaAllocator, OrtMemTypeDefault);
                Ort::Value input_tensor = Ort::Value::CreateTensor<float>(
                    memory_info, mel_tensor.data(), mel_tensor.size(),
                    input_shape.data(), input_shape.size());

                const char* input_names[] = {"input_features"};
                const char* output_names[] = {"audio_embeds"};

                auto outputs = ort_session_->Run(
                    Ort::RunOptions{nullptr},
                    input_names, &input_tensor, 1,
                    output_names, 1);

                // Extract audio_embeds [1, 512]
                float* output_data = outputs[0].GetTensorMutableData<float>();
                std::vector<float> native(output_data, output_data + native_dim_);

                LOG_INFO("CLAP inference OK — L2 norm: " +
                         std::to_string(std::sqrt(std::inner_product(
                             native.begin(), native.end(), native.begin(), 0.0f))));
                return project(native);
            }

            // Unknown modality
            return std::vector<float>(unified_dim_, 0.0f);

        } catch (const std::exception& e) {
            LOG_WARN(modalityName() + " ONNX inference failed: " + std::string(e.what()));
            return std::vector<float>(unified_dim_, 0.0f);
        }
#else
        (void)data;
        (void)mime_type;
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
