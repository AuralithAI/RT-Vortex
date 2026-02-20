/**
 * AI PR Reviewer - Core Types
 * 
 * Common type definitions used throughout the engine.
 */

#ifndef AIPR_TYPES_H
#define AIPR_TYPES_H

#include <string>
#include <vector>
#include <cstdint>
#include <optional>

namespace aipr {

/**
 * Severity levels for findings
 */
enum class Severity {
    Info,
    Warning,
    Error,
    Critical
};

/**
 * Check categories
 */
enum class CheckCategory {
    Security,
    Performance,
    Reliability,
    Style,
    Architecture,
    Testing,
    Documentation,
    Other
};

/**
 * File change type
 */
enum class ChangeType {
    Added,
    Modified,
    Deleted,
    Renamed,
    Copied
};

/**
 * Language identifier
 */
struct Language {
    std::string id;         // e.g., "cpp", "java", "python"
    std::string name;       // e.g., "C++", "Java", "Python"
    std::vector<std::string> extensions;  // e.g., {".cpp", ".cc", ".cxx"}
};

/**
 * File metadata
 */
struct FileInfo {
    std::string path;
    std::string blob_sha;
    std::string language;
    size_t size_bytes = 0;
    size_t line_count = 0;
    bool is_binary = false;
    bool is_generated = false;
    ChangeType change_type = ChangeType::Modified;  // For diff contexts
};

/**
 * Code chunk (unit of indexing)
 */
struct Chunk {
    std::string id;
    std::string file_path;
    size_t start_line = 0;
    size_t end_line = 0;
    std::string content;
    std::string content_hash;
    std::string language;
    
    // Optional: AST information
    std::optional<std::string> parent_symbol;
    std::vector<std::string> symbols;
    std::vector<std::string> imports;
    
    // Embedding (populated after embedding)
    std::vector<float> embedding;
};

/**
 * Code symbol (function, class, etc.)
 */
struct Symbol {
    std::string name;
    std::string qualified_name;  // e.g., "com.example.MyClass.myMethod"
    std::string kind;            // e.g., "function", "class", "method"
    std::string file_path;
    size_t line = 0;
    size_t column = 0;
    std::string signature;       // For functions/methods
    std::string doc_comment;     // Associated documentation
};

/**
 * Symbol touched by a diff
 */
struct TouchedSymbol {
    Symbol symbol;
    ChangeType change_type;
    size_t additions = 0;
    size_t deletions = 0;
    std::vector<std::string> callers;    // Functions that call this
    std::vector<std::string> callees;    // Functions called by this
};

/**
 * Diff hunk
 */
struct DiffHunk {
    std::string file_path;
    size_t old_start = 0;
    size_t old_lines = 0;
    size_t new_start = 0;
    size_t new_lines = 0;
    std::string content;
    std::vector<std::string> added_lines;
    std::vector<std::string> removed_lines;
};

/**
 * Parsed diff
 */
struct ParsedDiff {
    std::vector<DiffHunk> hunks;
    std::vector<FileInfo> changed_files;
    size_t total_additions = 0;
    size_t total_deletions = 0;
};

/**
 * Heuristic finding (non-LLM checks)
 */
struct HeuristicFinding {
    std::string id;
    CheckCategory category;
    Severity severity;
    float confidence = 1.0f;
    std::string file_path;
    size_t line = 0;
    std::string message;
    std::string evidence;
    std::string suggestion;
};

/**
 * Diagnostic result
 */
struct DiagnosticResult {
    bool healthy = false;
    std::vector<std::string> checks_passed;
    std::vector<std::string> checks_failed;
    std::vector<std::string> warnings;
    
    // System info
    std::string engine_version;
    std::string platform;
    size_t available_memory_mb = 0;
    size_t available_disk_mb = 0;
};

/**
 * Embedding request
 */
struct EmbedRequest {
    std::vector<std::string> texts;
    std::string model;
};

/**
 * Embedding response
 */
struct EmbedResponse {
    std::vector<std::vector<float>> embeddings;
    size_t total_tokens = 0;
};

// Utility functions for enums
inline const char* severityToString(Severity s) {
    switch (s) {
        case Severity::Info: return "info";
        case Severity::Warning: return "warning";
        case Severity::Error: return "error";
        case Severity::Critical: return "critical";
        default: return "unknown";
    }
}

inline const char* categoryToString(CheckCategory c) {
    switch (c) {
        case CheckCategory::Security: return "security";
        case CheckCategory::Performance: return "performance";
        case CheckCategory::Reliability: return "reliability";
        case CheckCategory::Style: return "style";
        case CheckCategory::Architecture: return "architecture";
        case CheckCategory::Testing: return "testing";
        case CheckCategory::Documentation: return "documentation";
        case CheckCategory::Other: return "other";
        default: return "unknown";
    }
}

inline const char* changeTypeToString(ChangeType ct) {
    switch (ct) {
        case ChangeType::Added: return "added";
        case ChangeType::Modified: return "modified";
        case ChangeType::Deleted: return "deleted";
        case ChangeType::Renamed: return "renamed";
        case ChangeType::Copied: return "copied";
        default: return "unknown";
    }
}

} // namespace aipr

#endif // AIPR_TYPES_H
