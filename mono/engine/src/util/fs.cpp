/**
 * AI PR Reviewer - Filesystem Utilities
 * 
 * Cross-platform filesystem helpers.
 */

#include <string>
#include <vector>
#include <fstream>
#include <sstream>
#include <stdexcept>
#include <cstdint>
#include <cstring>
#include <algorithm>
#include <functional>

#ifdef _WIN32
#include <windows.h>
#include <direct.h>
#define PATH_SEPARATOR '\\'
#else
#include <unistd.h>
#include <sys/stat.h>
#include <dirent.h>
#define PATH_SEPARATOR '/'
#endif

namespace aipr {
namespace fs {

/**
 * Read entire file contents
 */
inline std::string readFile(const std::string& path) {
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        throw std::runtime_error("Cannot open file: " + path);
    }
    
    std::ostringstream ss;
    ss << file.rdbuf();
    return ss.str();
}

/**
 * Write content to file
 */
inline void writeFile(const std::string& path, const std::string& content) {
    std::ofstream file(path, std::ios::binary);
    if (!file) {
        throw std::runtime_error("Cannot write to file: " + path);
    }
    file << content;
}

/**
 * Check if path exists
 */
inline bool exists(const std::string& path) {
#ifdef _WIN32
    return GetFileAttributesA(path.c_str()) != INVALID_FILE_ATTRIBUTES;
#else
    struct stat st;
    return stat(path.c_str(), &st) == 0;
#endif
}

/**
 * Check if path is a directory
 */
inline bool isDirectory(const std::string& path) {
#ifdef _WIN32
    DWORD attrs = GetFileAttributesA(path.c_str());
    return attrs != INVALID_FILE_ATTRIBUTES && (attrs & FILE_ATTRIBUTE_DIRECTORY);
#else
    struct stat st;
    if (stat(path.c_str(), &st) != 0) return false;
    return S_ISDIR(st.st_mode);
#endif
}

/**
 * Check if path is a regular file
 */
inline bool isFile(const std::string& path) {
#ifdef _WIN32
    DWORD attrs = GetFileAttributesA(path.c_str());
    return attrs != INVALID_FILE_ATTRIBUTES && !(attrs & FILE_ATTRIBUTE_DIRECTORY);
#else
    struct stat st;
    if (stat(path.c_str(), &st) != 0) return false;
    return S_ISREG(st.st_mode);
#endif
}

/**
 * Get file size
 */
inline size_t fileSize(const std::string& path) {
#ifdef _WIN32
    WIN32_FILE_ATTRIBUTE_DATA fad;
    if (!GetFileAttributesExA(path.c_str(), GetFileExInfoStandard, &fad)) {
        return 0;
    }
    return static_cast<size_t>(fad.nFileSizeLow) | (static_cast<size_t>(fad.nFileSizeHigh) << 32);
#else
    struct stat st;
    if (stat(path.c_str(), &st) != 0) return 0;
    return static_cast<size_t>(st.st_size);
#endif
}

/**
 * Get last modification time
 */
inline uint64_t lastModified(const std::string& path) {
#ifdef _WIN32
    WIN32_FILE_ATTRIBUTE_DATA fad;
    if (!GetFileAttributesExA(path.c_str(), GetFileExInfoStandard, &fad)) {
        return 0;
    }
    return static_cast<uint64_t>(fad.ftLastWriteTime.dwLowDateTime) |
           (static_cast<uint64_t>(fad.ftLastWriteTime.dwHighDateTime) << 32);
#else
    struct stat st;
    if (stat(path.c_str(), &st) != 0) return 0;
    return static_cast<uint64_t>(st.st_mtime);
#endif
}

/**
 * List directory contents
 */
inline std::vector<std::string> listDir(const std::string& path) {
    std::vector<std::string> entries;
    
#ifdef _WIN32
    WIN32_FIND_DATAA fd;
    std::string pattern = path + "\\*";
    HANDLE h = FindFirstFileA(pattern.c_str(), &fd);
    
    if (h != INVALID_HANDLE_VALUE) {
        do {
            if (strcmp(fd.cFileName, ".") != 0 && strcmp(fd.cFileName, "..") != 0) {
                entries.push_back(fd.cFileName);
            }
        } while (FindNextFileA(h, &fd));
        FindClose(h);
    }
#else
    DIR* dir = opendir(path.c_str());
    if (dir) {
        struct dirent* entry;
        while ((entry = readdir(dir)) != nullptr) {
            if (strcmp(entry->d_name, ".") != 0 && strcmp(entry->d_name, "..") != 0) {
                entries.push_back(entry->d_name);
            }
        }
        closedir(dir);
    }
#endif
    
    return entries;
}

/**
 * Create directory (with parents)
 */
inline bool createDir(const std::string& path) {
#ifdef _WIN32
    return CreateDirectoryA(path.c_str(), nullptr) || GetLastError() == ERROR_ALREADY_EXISTS;
#else
    return mkdir(path.c_str(), 0755) == 0 || errno == EEXIST;
#endif
}

/**
 * Create directories recursively
 */
inline bool createDirs(const std::string& path) {
    std::string current;
    
    for (size_t i = 0; i < path.size(); ++i) {
        current += path[i];
        
        if (path[i] == '/' || path[i] == '\\' || i == path.size() - 1) {
            if (!current.empty() && !exists(current)) {
                if (!createDir(current)) {
                    return false;
                }
            }
        }
    }
    
    return true;
}

/**
 * Join path components
 */
inline std::string joinPath(const std::string& base, const std::string& path) {
    if (base.empty()) return path;
    if (path.empty()) return base;
    
    char last = base.back();
    if (last == '/' || last == '\\') {
        return base + path;
    }
    return base + PATH_SEPARATOR + path;
}

/**
 * Get parent directory
 */
inline std::string parentPath(const std::string& path) {
    size_t pos = path.find_last_of("/\\");
    if (pos == std::string::npos) return ".";
    if (pos == 0) return "/";
    return path.substr(0, pos);
}

/**
 * Get filename from path
 */
inline std::string fileName(const std::string& path) {
    size_t pos = path.find_last_of("/\\");
    if (pos == std::string::npos) return path;
    return path.substr(pos + 1);
}

/**
 * Get file extension
 */
inline std::string extension(const std::string& path) {
    std::string name = fileName(path);
    size_t pos = name.rfind('.');
    if (pos == std::string::npos || pos == 0) return "";
    return name.substr(pos);
}

/**
 * Normalize path separators
 */
inline std::string normalizePath(const std::string& path) {
    std::string result = path;
    
#ifdef _WIN32
    std::replace(result.begin(), result.end(), '/', '\\');
#else
    std::replace(result.begin(), result.end(), '\\', '/');
#endif
    
    return result;
}

/**
 * Make path relative to base
 */
inline std::string relativePath(const std::string& path, const std::string& base) {
    std::string norm_path = normalizePath(path);
    std::string norm_base = normalizePath(base);
    
    if (norm_path.size() > norm_base.size() &&
        norm_path.compare(0, norm_base.size(), norm_base) == 0 &&
        (norm_path[norm_base.size()] == PATH_SEPARATOR)) {
        return norm_path.substr(norm_base.size() + 1);
    }
    
    return path;
}

/**
 * Get current working directory
 */
inline std::string currentDir() {
#ifdef _WIN32
    char buffer[MAX_PATH];
    if (_getcwd(buffer, MAX_PATH)) {
        return buffer;
    }
#else
    char buffer[4096];
    if (getcwd(buffer, sizeof(buffer))) {
        return buffer;
    }
#endif
    return ".";
}

/**
 * Recursive file walker
 */
class FileWalker {
public:
    using Callback = std::function<void(const std::string& path, bool is_dir)>;
    
    void walk(const std::string& root, const Callback& callback) {
        walkRecursive(root, callback);
    }
    
private:
    void walkRecursive(const std::string& path, const Callback& callback) {
        auto entries = listDir(path);
        
        for (const auto& entry : entries) {
            std::string full_path = joinPath(path, entry);
            bool is_dir = isDirectory(full_path);
            
            callback(full_path, is_dir);
            
            if (is_dir) {
                walkRecursive(full_path, callback);
            }
        }
    }
};

} // namespace fs
} // namespace aipr
