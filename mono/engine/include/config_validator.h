#pragma once

#include "engine_api.h"
#include <string>
#include <vector>

namespace aipr {

struct ValidationError {
    std::string field;
    std::string message;
};

class ConfigValidator {
public:
    static std::vector<ValidationError> validate(const EngineConfig& config);
    static bool isValid(const EngineConfig& config);
};

} // namespace aipr
