/**
 * TMS Repository Parser
 * 
 * Walks a repository tree, parses files with tree-sitter, and extracts
 * semantic code chunks with rich metadata.
 * 
 * This is the entry point for the ingestion pipeline:
 * RepoParser → ChunkStrategy → EmbeddingIngestor → LTM
 * 
 * Features:
 * - Multi-language support via tree-sitter
 * - Parallel file processing
 * - Dependency extraction (imports/includes)
 * - Call graph construction
 * - Smart chunking (preserves semantic boundaries)
 * - Incremental parsing (only changed files)
 */

#pragma once

#include "tms_types.h"
#include <string>
#include <vector>
#include <functional>
#include <memory>
#include <unordered_set>
#include <filesystem>

namespace aipr::tms {

/**
 * File filter configuration
 */
struct FileFilter {
    // Include patterns (globs)
    std::vector<std::string> include_patterns = {
        "*.cpp", "*.hpp", "*.c", "*.h",
        "*.java",
        "*.py",
        "*.js", "*.ts", "*.jsx", "*.tsx",
        "*.go",
        "*.rs",
        "*.rb",
        "*.cs",
        "*.swift",
        "*.kt", "*.kts",
        "*.scala",
        "*.php",
        "*.lua",
        "*.sh", "*.bash",
        "*.sql",
        "*.proto",
        "*.yaml", "*.yml",
        "*.json",
        "*.toml",
        "*.xml",
        "Makefile", "CMakeLists.txt", "BUILD", "BUILD.bazel",
        "Dockerfile", "docker-compose.yml",
        "*.md", "*.rst",
        "*.properties", "*.conf", "*.env", "*.ini", "*.cfg", "*.resources"
    };
    
    // Exclude patterns
    std::vector<std::string> exclude_patterns = {
        "node_modules/*",
        "vendor/*",
        ".git/*",
        "build/*",
        "dist/*",
        "target/*",
        "*.min.js",
        "*.min.css",
        "*.map",
        "*.lock",
        "package-lock.json",
        "yarn.lock",
        "*_test.go",
        "*_generated.*",
        "*.pb.go",
        "*.pb.h",
        "*.pb.cc"
    };
    
    // Size limits
    size_t max_file_size_bytes = 1024 * 1024;  // 1MB
    size_t min_file_size_bytes = 10;            // Skip tiny files
    
    // Binary detection
    bool skip_binary = true;
    bool skip_generated = true;
};

/**
 * Language detection result
 */
struct DetectedLanguage {
    std::string id;                 // "cpp", "java", "python", etc.
    std::string name;               // "C++", "Java", "Python"
    double confidence;              // Detection confidence
    bool tree_sitter_supported;     // Whether tree-sitter grammar exists
};

/**
 * Parsed file result
 */
struct ParsedFile {
    std::string path;               // Relative path
    std::string absolute_path;
    DetectedLanguage language;
    std::string content;
    size_t size_bytes;
    size_t line_count;
    
    // Extracted chunks
    std::vector<CodeChunk> chunks;
    
    // File-level metadata
    std::string file_summary;
    std::vector<std::string> imports;       // All imports/includes
    std::vector<std::string> exports;       // Public symbols
    std::vector<std::string> dependencies;  // External dependencies
    
    // Errors during parsing (if any)
    std::vector<std::string> parse_errors;
    bool fully_parsed = true;
};

/**
 * Repository statistics
 */
struct RepoStats {
    size_t total_files = 0;
    size_t parsed_files = 0;
    size_t skipped_files = 0;
    size_t failed_files = 0;
    size_t total_chunks = 0;
    size_t total_lines = 0;
    size_t total_bytes = 0;
    std::map<std::string, size_t> files_by_language;
    std::map<std::string, size_t> chunks_by_language;
    std::chrono::milliseconds parse_time{0};
};

/**
 * Parser configuration
 */
struct RepoParserConfig {
    FileFilter file_filter;
    ChunkingConfig chunking;
    
    // Parallelism
    int num_threads = 0;            // 0 = auto-detect
    
    // Tree-sitter settings
    bool use_tree_sitter = true;
    bool fallback_to_regex = true;  // If tree-sitter fails
    
    // Dependency analysis
    bool extract_imports = true;
    bool build_call_graph = true;
    int call_graph_depth = 3;       // How deep to follow calls
    
    // File summaries
    bool generate_file_summaries = true;
    int summary_max_lines = 50;     // Max lines for summary
    
    // Progress
    bool verbose = false;
};

/**
 * Progress callback
 */
using ParseProgressCallback = std::function<void(
    float progress,                 // 0.0 to 1.0
    const std::string& current_file,
    const std::string& status
)>;

/**
 * RepoParser
 */
class RepoParser {
public:
    explicit RepoParser(const RepoParserConfig& config);
    ~RepoParser();
    
    // Non-copyable
    RepoParser(const RepoParser&) = delete;
    RepoParser& operator=(const RepoParser&) = delete;
    
    // =========================================================================
    // Main Parsing Interface
    // =========================================================================
    
    /**
     * Parse an entire repository
     * 
     * @param repo_path Path to repository root
     * @param progress_callback Optional progress callback
     * @return Vector of CodeChunks ready for embedding
     */
    std::vector<CodeChunk> parseRepository(
        const std::string& repo_path,
        ParseProgressCallback progress_callback = nullptr
    );
    
    /**
     * Parse specific files only
     */
    std::vector<CodeChunk> parseFiles(
        const std::string& repo_root,
        const std::vector<std::string>& file_paths
    );
    
    /**
     * Parse a single file
     */
    ParsedFile parseFile(
        const std::string& file_path,
        const std::string& repo_root = ""
    );
    
    /**
     * Parse file content directly (for testing or external sources)
     */
    ParsedFile parseContent(
        const std::string& content,
        const std::string& file_path,
        const std::string& language = ""
    );
    
    // =========================================================================
    // Incremental Parsing
    // =========================================================================
    
    /**
     * Parse only changed files
     * 
     * @param repo_path Repository root
     * @param changed_files Files that changed
     * @param deleted_files Files that were deleted
     * @return Chunks from changed files (caller should remove old chunks for deleted files)
     */
    std::vector<CodeChunk> parseIncremental(
        const std::string& repo_path,
        const std::vector<std::string>& changed_files,
        const std::vector<std::string>& deleted_files
    );
    
    // =========================================================================
    // Language Detection
    // =========================================================================
    
    /**
     * Detect language from file extension
     */
    DetectedLanguage detectLanguage(const std::string& file_path);
    
    /**
     * Detect language from content
     */
    DetectedLanguage detectLanguageFromContent(const std::string& content);
    
    /**
     * Check if language has tree-sitter support
     */
    bool hasTreeSitterSupport(const std::string& language);
    
    /**
     * Get all supported languages
     */
    std::vector<std::string> getSupportedLanguages();
    
    // =========================================================================
    // Dependency Analysis
    // =========================================================================
    
    /**
     * Extract imports from a file
     */
    std::vector<std::string> extractImports(const ParsedFile& file);
    
    /**
     * Build call graph for a file
     */
    struct CallGraphNode {
        std::string symbol_name;
        std::string file_path;
        std::vector<std::string> callers;
        std::vector<std::string> callees;
    };
    
    std::vector<CallGraphNode> buildCallGraph(
        const std::vector<ParsedFile>& files
    );
    
    // =========================================================================
    // Statistics
    // =========================================================================
    
    /**
     * Get statistics from last parse
     */
    const RepoStats& getLastStats() const { return last_stats_; }
    
    /**
     * Reset statistics
     */
    void resetStats();

private:
    RepoParserConfig config_;
    RepoStats last_stats_;
    
    // Tree-sitter implementation (pimpl)
    class TreeSitterImpl;
    std::unique_ptr<TreeSitterImpl> tree_sitter_;
    
    // Helpers
    std::vector<std::string> walkDirectory(const std::string& root);
    bool shouldIncludeFile(const std::string& path);
    bool isBinaryFile(const std::string& path);
    bool isGeneratedFile(const std::string& path);
    std::string readFile(const std::string& path);
    std::string computeContentHash(const std::string& content);
    std::string generateFileSummary(const ParsedFile& file);
    
    // Chunking
    std::vector<CodeChunk> chunkFile(const ParsedFile& file);
    std::vector<CodeChunk> chunkWithTreeSitter(const ParsedFile& file);
    std::vector<CodeChunk> chunkWithFallback(const ParsedFile& file);
    
    // Thread pool
    class ThreadPool;
    std::unique_ptr<ThreadPool> thread_pool_;
    void initThreadPool();
};

// =============================================================================
// Chunk Strategy
// =============================================================================

/**
 * Chunk Strategy - Defines chunking policies for different use cases
 */
class ChunkStrategy {
public:
    explicit ChunkStrategy(const ChunkingConfig& config);
    ~ChunkStrategy();
    
    /**
     * Apply chunking strategy to a parsed file
     */
    std::vector<CodeChunk> apply(const ParsedFile& file);
    
    /**
     * Get file-level summary chunk
     */
    CodeChunk createFileSummaryChunk(const ParsedFile& file);
    
    /**
     * Get class/module level chunks
     */
    std::vector<CodeChunk> createClassChunks(const ParsedFile& file);
    
    /**
     * Get function/method level chunks
     */
    std::vector<CodeChunk> createFunctionChunks(const ParsedFile& file);
    
    /**
     * Get dependency chunks
     */
    std::vector<CodeChunk> createDependencyChunks(const ParsedFile& file);
    
    /**
     * Get cross-file context chunks
     */
    std::vector<CodeChunk> createCrossFileChunks(
        const ParsedFile& file,
        const std::vector<ParsedFile>& related_files
    );

private:
    ChunkingConfig config_;
    
    // Chunk ID generation
    std::string generateChunkId(const std::string& file_path, const std::string& name, int index);
    
    // Token estimation
    size_t estimateTokens(const std::string& content);
    
    // Overlap handling
    std::string getContextBefore(const std::vector<std::string>& lines, int start, int overlap);
    std::string getContextAfter(const std::vector<std::string>& lines, int end, int overlap);
};

} // namespace aipr::tms
