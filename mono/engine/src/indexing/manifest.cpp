/**
 * AI PR Reviewer - Index Manifest
 * 
 * Tracks indexed content with file hashes for incremental updates.
 */

#include "indexer.h"
#include "types.h"
#include <fstream>
#include <sstream>
#include <filesystem>
#include <chrono>
#include <iomanip>
#include <unordered_map>
#include <unordered_set>
#include <nlohmann/json.hpp>

namespace fs = std::filesystem;
using json = nlohmann::json;

namespace aipr {

/**
 * Get current ISO 8601 timestamp
 */
std::string getCurrentTimestamp() {
    auto now = std::chrono::system_clock::now();
    auto time = std::chrono::system_clock::to_time_t(now);
    std::stringstream ss;
    ss << std::put_time(std::gmtime(&time), "%Y-%m-%dT%H:%M:%SZ");
    return ss.str();
}

/**
 * Manifest manager
 */
class ManifestManager {
public:
    ManifestManager(const std::string& storage_path) 
        : storage_path_(storage_path) {}
    
    /**
     * Load manifest for a repository
     */
    IndexManifest load(const std::string& repo_id) {
        IndexManifest manifest;
        manifest.repo_id = repo_id;
        
        auto path = getManifestPath(repo_id);
        if (!fs::exists(path)) {
            return manifest;
        }
        
        std::ifstream file(path);
        if (!file) {
            return manifest;
        }
        
        try {
            json j = json::parse(file);
            
            manifest.version = j.value("version", "");
            manifest.commit_sha = j.value("commit_sha", "");
            manifest.created_at = j.value("created_at", "");
            manifest.updated_at = j.value("updated_at", "");
            
            if (j.contains("entries") && j["entries"].is_array()) {
                for (const auto& je : j["entries"]) {
                    ManifestEntry entry;
                    entry.file_path = je.value("file_path", "");
                    entry.blob_sha = je.value("blob_sha", "");
                    entry.language = je.value("language", "");
                    entry.size_bytes = je.value("size_bytes", static_cast<size_t>(0));
                    entry.last_indexed = je.value("last_indexed", "");
                    
                    if (je.contains("chunk_ids") && je["chunk_ids"].is_array()) {
                        for (const auto& cid : je["chunk_ids"]) {
                            entry.chunk_ids.push_back(cid.get<std::string>());
                        }
                    }
                    
                    manifest.entries.push_back(std::move(entry));
                }
            }
        } catch (const json::exception&) {
            // If JSON parsing fails, try legacy line-based format for backwards compatibility
            file.clear();
            file.seekg(0);
            
            std::string line;
            if (std::getline(file, line)) manifest.version = line;
            if (std::getline(file, line)) manifest.commit_sha = line;
            if (std::getline(file, line)) manifest.created_at = line;
            if (std::getline(file, line)) manifest.updated_at = line;
            
            while (std::getline(file, line)) {
                ManifestEntry entry;
                std::istringstream ss(line);
                std::string chunk_ids_str;
                
                std::getline(ss, entry.file_path, '\t');
                std::getline(ss, entry.blob_sha, '\t');
                std::getline(ss, entry.language, '\t');
                
                std::string size_str;
                std::getline(ss, size_str, '\t');
                try { entry.size_bytes = std::stoull(size_str); }
                catch (...) { entry.size_bytes = 0; }
                
                std::getline(ss, entry.last_indexed, '\t');
                std::getline(ss, chunk_ids_str, '\t');
                
                std::istringstream chunk_ss(chunk_ids_str);
                std::string chunk_id;
                while (std::getline(chunk_ss, chunk_id, ',')) {
                    if (!chunk_id.empty()) {
                        entry.chunk_ids.push_back(chunk_id);
                    }
                }
                
                manifest.entries.push_back(std::move(entry));
            }
        }
        
        return manifest;
    }
    
    /**
     * Save manifest (JSON format)
     */
    void save(const IndexManifest& manifest) {
        auto path = getManifestPath(manifest.repo_id);
        
        // Ensure directory exists
        fs::create_directories(path.parent_path());
        
        json j;
        j["version"] = manifest.version;
        j["repo_id"] = manifest.repo_id;
        j["commit_sha"] = manifest.commit_sha;
        j["created_at"] = manifest.created_at;
        j["updated_at"] = manifest.updated_at;
        
        json entries_json = json::array();
        for (const auto& entry : manifest.entries) {
            json je;
            je["file_path"] = entry.file_path;
            je["blob_sha"] = entry.blob_sha;
            je["language"] = entry.language;
            je["size_bytes"] = entry.size_bytes;
            je["last_indexed"] = entry.last_indexed;
            je["chunk_ids"] = entry.chunk_ids;
            entries_json.push_back(je);
        }
        j["entries"] = entries_json;
        
        std::ofstream file(path);
        if (!file) {
            throw std::runtime_error("Failed to write manifest: " + path.string());
        }
        file << j.dump(2);
    }
    
    /**
     * Create a new manifest
     */
    IndexManifest create(
        const std::string& repo_id,
        const std::string& commit_sha
    ) {
        IndexManifest manifest;
        manifest.repo_id = repo_id;
        manifest.version = "1";
        manifest.commit_sha = commit_sha;
        manifest.created_at = getCurrentTimestamp();
        manifest.updated_at = manifest.created_at;
        return manifest;
    }
    
    /**
     * Update manifest with new entries
     */
    void updateEntry(
        IndexManifest& manifest,
        const ManifestEntry& entry
    ) {
        // Find and update existing entry, or add new
        for (auto& existing : manifest.entries) {
            if (existing.file_path == entry.file_path) {
                existing = entry;
                manifest.updated_at = getCurrentTimestamp();
                return;
            }
        }
        
        manifest.entries.push_back(entry);
        manifest.updated_at = getCurrentTimestamp();
    }
    
    /**
     * Remove entry from manifest
     */
    void removeEntry(IndexManifest& manifest, const std::string& file_path) {
        manifest.entries.erase(
            std::remove_if(
                manifest.entries.begin(),
                manifest.entries.end(),
                [&](const ManifestEntry& e) { return e.file_path == file_path; }
            ),
            manifest.entries.end()
        );
        manifest.updated_at = getCurrentTimestamp();
    }
    
    /**
     * Get files that need re-indexing
     */
    std::vector<std::string> getStaleFiles(
        const IndexManifest& manifest,
        const std::vector<FileInfo>& current_files
    ) {
        std::vector<std::string> stale;
        
        // Build map of existing entries
        std::unordered_map<std::string, const ManifestEntry*> existing;
        for (const auto& entry : manifest.entries) {
            existing[entry.file_path] = &entry;
        }
        
        // Check each current file
        for (const auto& file : current_files) {
            auto it = existing.find(file.path);
            if (it == existing.end()) {
                // New file
                stale.push_back(file.path);
            } else if (it->second->blob_sha != file.blob_sha) {
                // Changed file
                stale.push_back(file.path);
            }
        }
        
        return stale;
    }
    
    /**
     * Get files that were deleted
     */
    std::vector<std::string> getDeletedFiles(
        const IndexManifest& manifest,
        const std::vector<FileInfo>& current_files
    ) {
        std::vector<std::string> deleted;
        
        std::unordered_set<std::string> current_paths;
        for (const auto& file : current_files) {
            current_paths.insert(file.path);
        }
        
        for (const auto& entry : manifest.entries) {
            if (current_paths.find(entry.file_path) == current_paths.end()) {
                deleted.push_back(entry.file_path);
            }
        }
        
        return deleted;
    }
    
    /**
     * Delete manifest
     */
    void remove(const std::string& repo_id) {
        auto path = getManifestPath(repo_id);
        if (fs::exists(path)) {
            fs::remove(path);
        }
    }
    
private:
    std::string storage_path_;
    
    fs::path getManifestPath(const std::string& repo_id) {
        // Sanitize repo_id for filesystem
        std::string safe_id = repo_id;
        for (char& c : safe_id) {
            if (c == '/' || c == '\\' || c == ':') {
                c = '_';
            }
        }
        return fs::path(storage_path_) / safe_id / "manifest.idx";
    }
};

} // namespace aipr
