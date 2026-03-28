#include "storage_migration.h"

#include <filesystem>
#include <fstream>
#include <iostream>
#include <nlohmann/json.hpp>

namespace fs = std::filesystem;
using json = nlohmann::json;

namespace aipr {

static fs::path repoDir(const std::string& storage_path, const std::string& repo_id) {
    std::string safe = repo_id;
    for (char& c : safe) {
        if (c == '/' || c == '\\' || c == ':') c = '_';
    }
    return fs::path(storage_path) / safe;
}

int StorageMigration::detectVersion(const std::string& storage_path,
                                     const std::string& repo_id) {
    auto dir = repoDir(storage_path, repo_id);
    auto manifest = dir / "manifest.idx";
    if (!fs::exists(manifest)) return 0;

    std::ifstream f(manifest);
    if (!f.good()) return 0;

    try {
        auto j = json::parse(f);
        return j.value("storage_version", 0);
    } catch (...) {
        return 0;
    }
}

bool StorageMigration::migrate(const std::string& storage_path,
                                const std::string& repo_id) {
    int version = detectVersion(storage_path, repo_id);
    if (version >= 1) return true;

    auto dir = repoDir(storage_path, repo_id);
    auto manifest = dir / "manifest.idx";
    if (!fs::exists(manifest)) return true;

    // v0 → v1: write to staging dir, atomic rename
    auto staging = dir / "v1_migrating";
    fs::create_directories(staging);

    try {
        // Copy existing data to staging
        for (auto& entry : fs::directory_iterator(dir)) {
            if (entry.path().filename() == "v1_migrating") continue;
            fs::copy(entry.path(),
                     staging / entry.path().filename(),
                     fs::copy_options::overwrite_existing | fs::copy_options::recursive);
        }

        // Update manifest version in staging
        auto staged_manifest = staging / "manifest.idx";
        std::ifstream in(staged_manifest);
        json j;
        try {
            j = json::parse(in);
        } catch (...) {
            j = json::object();
        }
        in.close();

        j["storage_version"] = 1;

        std::ofstream out(staged_manifest);
        out << j.dump(2);
        out.close();

        // Atomic swap: rename old dir, rename staging, remove old
        auto backup = fs::path(dir.string() + "_v0_backup");
        if (fs::exists(backup)) fs::remove_all(backup);

        // Move current files (except staging) to backup
        fs::create_directories(backup);
        for (auto& entry : fs::directory_iterator(dir)) {
            auto name = entry.path().filename().string();
            if (name == "v1_migrating" || name.find("_v0_backup") != std::string::npos)
                continue;
            fs::rename(entry.path(), backup / entry.path().filename());
        }

        // Move staging contents to repo dir
        for (auto& entry : fs::directory_iterator(staging)) {
            fs::rename(entry.path(), dir / entry.path().filename());
        }
        fs::remove(staging);

        std::cout << "[MIGRATION] " << repo_id << " v0 → v1 complete\n";
        return true;

    } catch (const std::exception& e) {
        std::cerr << "[MIGRATION] Failed for " << repo_id << ": " << e.what() << "\n";
        // Clean up staging on failure
        if (fs::exists(staging)) fs::remove_all(staging);
        return false;
    }
}

} // namespace aipr
