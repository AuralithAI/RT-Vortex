#include "config_validator.h"
#include <filesystem>

namespace fs = std::filesystem;

namespace aipr {

std::vector<ValidationError> ConfigValidator::validate(const EngineConfig& config) {
    std::vector<ValidationError> errors;

    if (config.storage_path.empty()) {
        errors.push_back({"storage_path", "must not be empty"});
    }

    if (config.embed_dimensions == 0) {
        errors.push_back({"embed_dimensions", "must be > 0"});
    }

    if (config.embed_provider == EmbedProvider::LOCAL_ONNX) {
        const auto& name = config.onnx_model_name;
        if (name == "minilm" && config.embed_dimensions != 384) {
            errors.push_back({"embed_dimensions",
                "minilm model requires 384 dimensions"});
        } else if (name == "bge-m3" && config.embed_dimensions != 1024) {
            errors.push_back({"embed_dimensions",
                "bge-m3 model requires 1024 dimensions"});
        }
        if (!config.onnx_model_path.empty() &&
            !fs::exists(config.onnx_model_path)) {
            errors.push_back({"onnx_model_path",
                "file not found: " + config.onnx_model_path});
        }
    }

    if (config.embed_provider == EmbedProvider::HTTP) {
        if (config.embed_endpoint.empty()) {
            errors.push_back({"embed_endpoint",
                "HTTP provider requires a non-empty endpoint"});
        }
    }

    if (config.chunk_size == 0) {
        errors.push_back({"chunk_size", "must be > 0"});
    }

    if (config.chunk_overlap >= config.chunk_size) {
        errors.push_back({"chunk_overlap", "must be less than chunk_size"});
    }

    if (config.top_k == 0) {
        errors.push_back({"top_k", "must be > 0"});
    }

    if (config.lexical_weight + config.vector_weight < 0.01f) {
        errors.push_back({"lexical_weight/vector_weight",
            "combined weights must be > 0"});
    }

    return errors;
}

bool ConfigValidator::isValid(const EngineConfig& config) {
    return validate(config).empty();
}

} // namespace aipr
