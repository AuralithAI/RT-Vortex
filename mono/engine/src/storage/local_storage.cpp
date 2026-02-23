/**
 * AI PR Reviewer - Local Storage Backend Implementation
 * 
 * Filesystem-based storage backend for development and on-premise deployments.
 */

#include "storage_backend.h"
#include <fstream>
#include <sstream>
#include <filesystem>
#include <chrono>
#include <algorithm>
#include <cstring>

namespace fs = std::filesystem;

namespace aipr {

LocalStorageBackend::LocalStorageBackend(const std::string& base_path)
    : base_path_(base_path) {
    // Ensure base path exists
    if (!base_path_.empty()) {
        fs::create_directories(base_path_);
    }
}

bool LocalStorageBackend::isAvailable() {
    try {
        return fs::exists(base_path_) && fs::is_directory(base_path_);
    } catch (...) {
        return false;
    }
}

std::string LocalStorageBackend::resolvePath(const std::string& key) const {
    fs::path path(base_path_);
    path /= key;
    return path.string();
}

void LocalStorageBackend::ensureDirectory(const std::string& path) {
    fs::path p(path);
    if (p.has_parent_path()) {
        fs::create_directories(p.parent_path());
    }
}

std::optional<std::string> LocalStorageBackend::read(const std::string& key) {
    std::string path = resolvePath(key);
    
    if (!fs::exists(path)) {
        return std::nullopt;
    }
    
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        return std::nullopt;
    }
    
    std::ostringstream ss;
    ss << file.rdbuf();
    return ss.str();
}

ssize_t LocalStorageBackend::readInto(const std::string& key, char* buffer, size_t buffer_size) {
    std::string path = resolvePath(key);
    
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        return -1;
    }
    
    file.read(buffer, buffer_size);
    return file.gcount();
}

StorageResult LocalStorageBackend::write(
    const std::string& key,
    const std::string& content,
    const std::string& /*content_type*/,
    const std::map<std::string, std::string>& /*metadata*/
) {
    std::string path = resolvePath(key);
    ensureDirectory(path);
    
    std::ofstream file(path, std::ios::binary);
    if (!file) {
        return StorageResult{false, "Failed to open file for writing: " + path};
    }
    
    file.write(content.data(), content.size());
    
    if (!file) {
        return StorageResult{false, "Failed to write content to file: " + path};
    }
    
    return StorageResult{true, "", "", std::nullopt};
}

StorageResult LocalStorageBackend::writeBuffer(
    const std::string& key,
    const char* data,
    size_t size,
    const std::string& content_type,
    const std::map<std::string, std::string>& metadata
) {
    return write(key, std::string(data, size), content_type, metadata);
}

bool LocalStorageBackend::exists(const std::string& key) {
    return fs::exists(resolvePath(key));
}

std::optional<StorageObjectInfo> LocalStorageBackend::head(const std::string& key) {
    std::string path = resolvePath(key);
    
    if (!fs::exists(path)) {
        return std::nullopt;
    }
    
    StorageObjectInfo info;
    info.key = key;
    info.size = fs::file_size(path);
    info.content_type = "application/octet-stream";
    
    auto ftime = fs::last_write_time(path);
    auto sctp = std::chrono::time_point_cast<std::chrono::system_clock::duration>(
        ftime - fs::file_time_type::clock::now() + std::chrono::system_clock::now()
    );
    info.last_modified = sctp;
    
    return info;
}

StorageResult LocalStorageBackend::remove(const std::string& key) {
    std::string path = resolvePath(key);
    
    if (!fs::exists(path)) {
        return StorageResult{true, ""};  // Already doesn't exist
    }
    
    try {
        fs::remove(path);
        return StorageResult{true, ""};
    } catch (const std::exception& e) {
        return StorageResult{false, e.what()};
    }
}

StorageResult LocalStorageBackend::removeMultiple(const std::vector<std::string>& keys) {
    std::vector<std::string> failed;
    
    for (const auto& key : keys) {
        auto result = remove(key);
        if (!result.success) {
            failed.push_back(key);
        }
    }
    
    if (failed.empty()) {
        return StorageResult{true, ""};
    }
    
    return StorageResult{false, "Failed to delete " + std::to_string(failed.size()) + " objects"};
}

StorageListResult LocalStorageBackend::list(
    const std::string& prefix,
    const std::string& delimiter,
    int max_keys,
    const std::string& /*continuation_token*/
) {
    StorageListResult result;
    result.is_truncated = false;
    
    std::string search_path = resolvePath(prefix);
    fs::path base(base_path_);
    
    // If prefix is a directory, list its contents
    // If prefix is partial, list parent and filter
    fs::path search_dir = fs::path(search_path).parent_path();
    std::string prefix_filter = fs::path(search_path).filename().string();
    
    if (fs::is_directory(search_path)) {
        search_dir = search_path;
        prefix_filter = "";
    }
    
    if (!fs::exists(search_dir)) {
        return result;
    }
    
    std::set<std::string> common_prefixes_set;
    int count = 0;
    
    for (const auto& entry : fs::recursive_directory_iterator(search_dir)) {
        if (count >= max_keys) {
            result.is_truncated = true;
            break;
        }
        
        fs::path rel_path = fs::relative(entry.path(), base);
        std::string key = rel_path.string();
        
        // Convert backslashes to forward slashes for consistency
        std::replace(key.begin(), key.end(), '\\', '/');
        
        // Apply prefix filter
        if (!prefix.empty() && key.find(prefix) != 0) {
            continue;
        }
        
        // Handle delimiter for hierarchical listing
        if (!delimiter.empty()) {
            size_t prefix_len = prefix.length();
            size_t delim_pos = key.find(delimiter, prefix_len);
            
            if (delim_pos != std::string::npos) {
                // This is a "folder" - add to common prefixes
                std::string common_prefix = key.substr(0, delim_pos + 1);
                common_prefixes_set.insert(common_prefix);
                continue;
            }
        }
        
        if (entry.is_regular_file()) {
            StorageObjectInfo info;
            info.key = key;
            info.size = entry.file_size();
            info.content_type = "application/octet-stream";
            
            auto ftime = entry.last_write_time();
            auto sctp = std::chrono::time_point_cast<std::chrono::system_clock::duration>(
                ftime - fs::file_time_type::clock::now() + std::chrono::system_clock::now()
            );
            info.last_modified = sctp;
            
            result.objects.push_back(info);
            count++;
        }
    }
    
    result.common_prefixes.assign(common_prefixes_set.begin(), common_prefixes_set.end());
    
    return result;
}

std::vector<StorageObjectInfo> LocalStorageBackend::listAll(const std::string& prefix) {
    std::vector<StorageObjectInfo> all_objects;
    std::string token;
    
    do {
        auto result = list(prefix, "", 10000, token);
        all_objects.insert(all_objects.end(), result.objects.begin(), result.objects.end());
        token = result.continuation_token;
    } while (!token.empty());
    
    return all_objects;
}

StorageResult LocalStorageBackend::copy(const std::string& source_key, const std::string& dest_key) {
    std::string source_path = resolvePath(source_key);
    std::string dest_path = resolvePath(dest_key);
    
    if (!fs::exists(source_path)) {
        return StorageResult{false, "Source does not exist: " + source_key};
    }
    
    try {
        ensureDirectory(dest_path);
        fs::copy_file(source_path, dest_path, fs::copy_options::overwrite_existing);
        return StorageResult{true, ""};
    } catch (const std::exception& e) {
        return StorageResult{false, e.what()};
    }
}

StorageResult LocalStorageBackend::move(const std::string& source_key, const std::string& dest_key) {
    std::string source_path = resolvePath(source_key);
    std::string dest_path = resolvePath(dest_key);
    
    if (!fs::exists(source_path)) {
        return StorageResult{false, "Source does not exist: " + source_key};
    }
    
    try {
        ensureDirectory(dest_path);
        fs::rename(source_path, dest_path);
        return StorageResult{true, ""};
    } catch (const std::exception& e) {
        return StorageResult{false, e.what()};
    }
}

std::optional<std::string> LocalStorageBackend::getPresignedUrl(
    const std::string& /*key*/,
    int /*expiry_seconds*/,
    bool /*for_upload*/
) {
    // Presigned URLs not supported for local storage
    return std::nullopt;
}

StorageResult LocalStorageBackend::writeWithProgress(
    const std::string& key,
    const std::string& content,
    ProgressCallback progress,
    const std::string& content_type
) {
    std::string path = resolvePath(key);
    ensureDirectory(path);
    
    std::ofstream file(path, std::ios::binary);
    if (!file) {
        return StorageResult{false, "Failed to open file for writing: " + path};
    }
    
    size_t total_size = content.size();
    size_t chunk_size = 64 * 1024;  // 64KB chunks
    size_t written = 0;
    
    while (written < total_size) {
        size_t to_write = std::min(chunk_size, total_size - written);
        file.write(content.data() + written, to_write);
        
        if (!file) {
            return StorageResult{false, "Write failed at offset " + std::to_string(written)};
        }
        
        written += to_write;
        
        if (progress) {
            progress(written, total_size);
        }
    }
    
    return StorageResult{true, ""};
}

std::optional<std::string> LocalStorageBackend::readWithProgress(
    const std::string& key,
    ProgressCallback progress
) {
    std::string path = resolvePath(key);
    
    if (!fs::exists(path)) {
        return std::nullopt;
    }
    
    size_t total_size = fs::file_size(path);
    std::ifstream file(path, std::ios::binary);
    
    if (!file) {
        return std::nullopt;
    }
    
    std::string content;
    content.reserve(total_size);
    
    size_t chunk_size = 64 * 1024;  // 64KB chunks
    std::vector<char> buffer(chunk_size);
    size_t read_total = 0;
    
    while (file) {
        file.read(buffer.data(), chunk_size);
        size_t read_count = file.gcount();
        
        if (read_count > 0) {
            content.append(buffer.data(), read_count);
            read_total += read_count;
            
            if (progress) {
                progress(read_total, total_size);
            }
        }
    }
    
    return content;
}

} // namespace aipr
