#pragma once

#include <string>

namespace aipr {

class StorageMigration {
public:
    static int  detectVersion(const std::string& storage_path, const std::string& repo_id);
    static bool migrate(const std::string& storage_path, const std::string& repo_id);
};

} // namespace aipr
