/**
 * TMS Repository Parser Implementation
 * 
 * Tree-sitter based repository parsing with semantic chunking.
 */

#include "tms/repo_parser.h"
#include <algorithm>
#include <fstream>
#include <sstream>
#include <filesystem>
#include <regex>
#include <queue>

#ifdef AIPR_HAS_TREE_SITTER
#include <tree_sitter/api.h>

// External language parsers
extern "C" {
TSLanguage* tree_sitter_c();
TSLanguage* tree_sitter_cpp();
TSLanguage* tree_sitter_java();
TSLanguage* tree_sitter_python();
TSLanguage* tree_sitter_javascript();
TSLanguage* tree_sitter_typescript();
TSLanguage* tree_sitter_go();
TSLanguage* tree_sitter_rust();
}
#endif

namespace aipr::tms {

namespace fs = std::filesystem;

// =============================================================================
// Constructor / Destructor
// =============================================================================

RepoParser::RepoParser(const ParserConfig& config)
    : config_(config)
    , initialized_(false) {
}

RepoParser::~RepoParser() {
    shutdown();
}

// =============================================================================
// Initialization
// =============================================================================

bool RepoParser::initialize() {
    std::lock_guard<std::mutex> lock(mutex_);
    
    if (initialized_) {
        return true;
    }
    
    try {
#ifdef AIPR_HAS_TREE_SITTER
        // Initialize tree-sitter parsers for enabled languages
        initializeLanguageParsers();
#endif
        
        // Build extension to language map
        buildExtensionMap();
        
        // Compile ignore patterns
        compileIgnorePatterns();
        
        initialized_ = true;
        return true;
        
    } catch (const std::exception& e) {
        last_error_ = std::string("Parser initialization failed: ") + e.what();
        return false;
    }
}

void RepoParser::shutdown() {
    std::lock_guard<std::mutex> lock(mutex_);
    
#ifdef AIPR_HAS_TREE_SITTER
    // Free tree-sitter parsers
    for (auto& [lang, parser] : ts_parsers_) {
        ts_parser_delete(parser);
    }
    ts_parsers_.clear();
#endif
    
    initialized_ = false;
}

// =============================================================================
// Repository Parsing
// =============================================================================

ParseResult RepoParser::parseRepository(const std::string& repo_path) {
    auto start = std::chrono::steady_clock::now();
    
    ParseResult result;
    result.root_path = repo_path;
    
    if (!fs::exists(repo_path)) {
        result.errors.push_back("Repository path does not exist: " + repo_path);
        return result;
    }
    
    // Collect all source files
    std::vector<std::string> files;
    try {
        collectSourceFiles(repo_path, files);
    } catch (const std::exception& e) {
        result.errors.push_back(std::string("File collection error: ") + e.what());
        return result;
    }
    
    result.total_files = files.size();
    
    // Parse files (potentially in parallel)
    if (config_.enable_parallel && files.size() > config_.parallel_threshold) {
        parseFilesParallel(files, result);
    } else {
        for (const auto& file : files) {
            parseFileSingle(file, result);
        }
    }
    
    // Extract cross-file dependencies
    if (config_.extract_dependencies) {
        extractCrossFileDependencies(result);
    }
    
    // Build file dependency graph
    buildDependencyGraph(result);
    
    // Update stats
    auto end = std::chrono::steady_clock::now();
    result.parse_time = std::chrono::duration_cast<std::chrono::milliseconds>(end - start);
    
    updateStats(result);
    
    return result;
}

std::vector<CodeChunk> RepoParser::parseFile(const std::string& file_path) {
    std::vector<CodeChunk> chunks;
    
    if (!fs::exists(file_path)) {
        return chunks;
    }
    
    std::string language = detectLanguage(file_path);
    if (language.empty()) {
        // Unsupported file type, treat as text
        language = "text";
    }
    
    std::string content = readFile(file_path);
    if (content.empty()) {
        return chunks;
    }
    
#ifdef AIPR_HAS_TREE_SITTER
    auto it = ts_parsers_.find(language);
    if (it != ts_parsers_.end()) {
        chunks = parseWithTreeSitter(content, language, file_path, it->second);
    } else {
        chunks = parseAsFallback(content, language, file_path);
    }
#else
    chunks = parseAsFallback(content, language, file_path);
#endif
    
    // Post-process chunks
    for (auto& chunk : chunks) {
        // Extract dependencies
        chunk.dependencies = extractDependencies(chunk.content, language);
        
        // Compute hash for deduplication
        chunk.content_hash = computeContentHash(chunk.content);
    }
    
    return chunks;
}

std::string RepoParser::detectLanguage(const std::string& file_path) {
    // Get extension
    std::string ext = fs::path(file_path).extension().string();
    if (!ext.empty() && ext[0] == '.') {
        ext = ext.substr(1);
    }
    
    // Convert to lowercase
    std::transform(ext.begin(), ext.end(), ext.begin(), ::tolower);
    
    auto it = extension_map_.find(ext);
    if (it != extension_map_.end()) {
        return it->second;
    }
    
    // Try to detect from shebang
    std::ifstream file(file_path);
    if (file.good()) {
        std::string first_line;
        std::getline(file, first_line);
        if (first_line.find("#!/") != std::string::npos) {
            if (first_line.find("python") != std::string::npos) return "python";
            if (first_line.find("node") != std::string::npos) return "javascript";
            if (first_line.find("bash") != std::string::npos || 
                first_line.find("sh") != std::string::npos) return "shell";
        }
    }
    
    return "";
}

// =============================================================================
// File Collection
// =============================================================================

void RepoParser::collectSourceFiles(const std::string& root_path, std::vector<std::string>& files) {
    std::queue<std::string> dirs;
    dirs.push(root_path);
    
    while (!dirs.empty()) {
        std::string current = dirs.front();
        dirs.pop();
        
        try {
            for (const auto& entry : fs::directory_iterator(current)) {
                std::string path = entry.path().string();
                std::string name = entry.path().filename().string();
                
                // Check ignore patterns
                if (shouldIgnore(path, name)) {
                    continue;
                }
                
                if (entry.is_directory()) {
                    dirs.push(path);
                } else if (entry.is_regular_file()) {
                    if (isSupportedFile(path)) {
                        files.push_back(path);
                    }
                }
            }
        } catch (const fs::filesystem_error& e) {
            // Skip inaccessible directories
        }
    }
}

bool RepoParser::shouldIgnore(const std::string& path, const std::string& name) {
    // Check against compiled patterns
    for (const auto& pattern : compiled_ignore_patterns_) {
        if (std::regex_search(path, pattern)) {
            return true;
        }
    }
    
    // Built-in ignores
    static const std::vector<std::string> default_ignores = {
        ".git", ".svn", ".hg",
        "node_modules", "vendor", "third_party",
        "build", "dist", "out", "target",
        "__pycache__", ".pytest_cache",
        ".idea", ".vscode", ".vs",
        "CMakeFiles"
    };
    
    for (const auto& ignore : default_ignores) {
        if (name == ignore) {
            return true;
        }
    }
    
    return false;
}

bool RepoParser::isSupportedFile(const std::string& path) {
    std::string lang = detectLanguage(path);
    if (!lang.empty()) {
        // Check if language is enabled
        if (config_.enabled_languages.empty()) {
            return true;  // All languages enabled
        }
        return std::find(config_.enabled_languages.begin(),
                        config_.enabled_languages.end(),
                        lang) != config_.enabled_languages.end();
    }
    return false;
}

// =============================================================================
// Tree-Sitter Parsing
// =============================================================================

#ifdef AIPR_HAS_TREE_SITTER

void RepoParser::initializeLanguageParsers() {
    // Create parsers for each language
    auto addParser = [this](const std::string& lang, TSLanguage* ts_lang) {
        TSParser* parser = ts_parser_new();
        if (ts_parser_set_language(parser, ts_lang)) {
            ts_parsers_[lang] = parser;
        } else {
            ts_parser_delete(parser);
        }
    };
    
    addParser("c", tree_sitter_c());
    addParser("cpp", tree_sitter_cpp());
    addParser("java", tree_sitter_java());
    addParser("python", tree_sitter_python());
    addParser("javascript", tree_sitter_javascript());
    addParser("typescript", tree_sitter_typescript());
    addParser("go", tree_sitter_go());
    addParser("rust", tree_sitter_rust());
}

std::vector<CodeChunk> RepoParser::parseWithTreeSitter(
    const std::string& content,
    const std::string& language,
    const std::string& file_path,
    TSParser* parser
) {
    std::vector<CodeChunk> chunks;
    
    TSTree* tree = ts_parser_parse_string(
        parser, nullptr,
        content.c_str(), content.length()
    );
    
    if (!tree) {
        return parseAsFallback(content, language, file_path);
    }
    
    TSNode root = ts_tree_root_node(tree);
    
    // Walk tree and extract semantic chunks
    extractChunksFromTree(root, content, file_path, language, chunks);
    
    ts_tree_delete(tree);
    
    return chunks;
}

void RepoParser::extractChunksFromTree(
    TSNode node,
    const std::string& content,
    const std::string& file_path,
    const std::string& language,
    std::vector<CodeChunk>& chunks
) {
    const char* type = ts_node_type(node);
    std::string node_type(type);
    
    // Determine chunk types based on node type
    ChunkType chunk_type = ChunkType::UNKNOWN;
    bool extract = false;
    
    // Language-specific node type mapping
    if (language == "cpp" || language == "c") {
        if (node_type == "function_definition" || node_type == "function_declarator") {
            chunk_type = ChunkType::FUNCTION;
            extract = true;
        } else if (node_type == "class_specifier" || node_type == "struct_specifier") {
            chunk_type = ChunkType::CLASS;
            extract = true;
        } else if (node_type == "namespace_definition") {
            chunk_type = ChunkType::NAMESPACE;
            extract = true;
        }
    } else if (language == "java") {
        if (node_type == "method_declaration" || node_type == "constructor_declaration") {
            chunk_type = ChunkType::METHOD;
            extract = true;
        } else if (node_type == "class_declaration" || node_type == "interface_declaration") {
            chunk_type = ChunkType::CLASS;
            extract = true;
        }
    } else if (language == "python") {
        if (node_type == "function_definition") {
            chunk_type = ChunkType::FUNCTION;
            extract = true;
        } else if (node_type == "class_definition") {
            chunk_type = ChunkType::CLASS;
            extract = true;
        }
    } else if (language == "javascript" || language == "typescript") {
        if (node_type == "function_declaration" || node_type == "arrow_function" ||
            node_type == "method_definition") {
            chunk_type = ChunkType::FUNCTION;
            extract = true;
        } else if (node_type == "class_declaration") {
            chunk_type = ChunkType::CLASS;
            extract = true;
        }
    } else if (language == "go") {
        if (node_type == "function_declaration" || node_type == "method_declaration") {
            chunk_type = ChunkType::FUNCTION;
            extract = true;
        } else if (node_type == "type_declaration") {
            chunk_type = ChunkType::TYPE;
            extract = true;
        }
    } else if (language == "rust") {
        if (node_type == "function_item") {
            chunk_type = ChunkType::FUNCTION;
            extract = true;
        } else if (node_type == "struct_item" || node_type == "impl_item" ||
                   node_type == "trait_item") {
            chunk_type = ChunkType::TYPE;
            extract = true;
        }
    }
    
    if (extract) {
        CodeChunk chunk;
        chunk.id = generateChunkId(file_path, ts_node_start_point(node).row);
        chunk.file_path = file_path;
        chunk.language = language;
        chunk.type = chunk_type;
        
        uint32_t start_byte = ts_node_start_byte(node);
        uint32_t end_byte = ts_node_end_byte(node);
        
        chunk.content = content.substr(start_byte, end_byte - start_byte);
        chunk.start_line = ts_node_start_point(node).row + 1;
        chunk.end_line = ts_node_end_point(node).row + 1;
        
        // Extract name from first identifier child
        uint32_t child_count = ts_node_child_count(node);
        for (uint32_t i = 0; i < child_count; ++i) {
            TSNode child = ts_node_child(node, i);
            const char* child_type = ts_node_type(child);
            if (std::string(child_type) == "identifier" || 
                std::string(child_type) == "name") {
                uint32_t name_start = ts_node_start_byte(child);
                uint32_t name_end = ts_node_end_byte(child);
                chunk.name = content.substr(name_start, name_end - name_start);
                break;
            }
        }
        
        // Check chunk size limits
        if (chunk.content.length() <= config_.max_chunk_size) {
            chunks.push_back(std::move(chunk));
        } else {
            // Split large chunk
            auto sub_chunks = splitLargeChunk(chunk);
            chunks.insert(chunks.end(), sub_chunks.begin(), sub_chunks.end());
        }
    }
    
    // Recurse into children
    uint32_t child_count = ts_node_child_count(node);
    for (uint32_t i = 0; i < child_count; ++i) {
        TSNode child = ts_node_child(node, i);
        extractChunksFromTree(child, content, file_path, language, chunks);
    }
}

#else

void RepoParser::initializeLanguageParsers() {
    // No tree-sitter available
}

#endif

// =============================================================================
// Fallback Parsing
// =============================================================================

std::vector<CodeChunk> RepoParser::parseAsFallback(
    const std::string& content,
    const std::string& language,
    const std::string& file_path
) {
    std::vector<CodeChunk> chunks;
    
    // Simple line-based chunking
    std::istringstream stream(content);
    std::string line;
    std::vector<std::string> lines;
    
    while (std::getline(stream, line)) {
        lines.push_back(line);
    }
    
    // Create chunks of target size
    size_t chunk_start = 0;
    size_t current_size = 0;
    std::ostringstream chunk_content;
    
    for (size_t i = 0; i < lines.size(); ++i) {
        chunk_content << lines[i] << "\n";
        current_size += lines[i].length() + 1;
        
        if (current_size >= config_.target_chunk_size || i == lines.size() - 1) {
            CodeChunk chunk;
            chunk.id = generateChunkId(file_path, chunk_start);
            chunk.file_path = file_path;
            chunk.language = language;
            chunk.type = ChunkType::BLOCK;
            chunk.content = chunk_content.str();
            chunk.start_line = chunk_start + 1;
            chunk.end_line = i + 1;
            
            chunks.push_back(std::move(chunk));
            
            chunk_start = i + 1;
            current_size = 0;
            chunk_content.str("");
            chunk_content.clear();
        }
    }
    
    return chunks;
}

std::vector<CodeChunk> RepoParser::splitLargeChunk(const CodeChunk& chunk) {
    std::vector<CodeChunk> sub_chunks;
    
    std::istringstream stream(chunk.content);
    std::string line;
    std::vector<std::string> lines;
    
    while (std::getline(stream, line)) {
        lines.push_back(line);
    }
    
    size_t lines_per_chunk = config_.target_chunk_size / 80;  // Assume 80 chars per line
    if (lines_per_chunk == 0) lines_per_chunk = 1;
    
    for (size_t start = 0; start < lines.size(); start += lines_per_chunk) {
        size_t end = std::min(start + lines_per_chunk, lines.size());
        
        std::ostringstream content;
        for (size_t i = start; i < end; ++i) {
            content << lines[i] << "\n";
        }
        
        CodeChunk sub;
        sub.id = generateChunkId(chunk.file_path, chunk.start_line + start);
        sub.file_path = chunk.file_path;
        sub.language = chunk.language;
        sub.type = chunk.type;
        sub.name = chunk.name;
        sub.content = content.str();
        sub.start_line = chunk.start_line + start;
        sub.end_line = chunk.start_line + end - 1;
        
        sub_chunks.push_back(std::move(sub));
    }
    
    return sub_chunks;
}

// =============================================================================
// Dependency Extraction
// =============================================================================

std::vector<std::string> RepoParser::extractDependencies(
    const std::string& content,
    const std::string& language
) {
    std::vector<std::string> deps;
    
    if (language == "cpp" || language == "c") {
        // Extract #include
        std::regex include_re(R"(#include\s*[<"]([^>"]+)[>"])");
        std::sregex_iterator it(content.begin(), content.end(), include_re);
        std::sregex_iterator end;
        while (it != end) {
            deps.push_back((*it)[1].str());
            ++it;
        }
    } else if (language == "java") {
        // Extract import
        std::regex import_re(R"(import\s+([a-zA-Z0-9_.]+);)");
        std::sregex_iterator it(content.begin(), content.end(), import_re);
        std::sregex_iterator end;
        while (it != end) {
            deps.push_back((*it)[1].str());
            ++it;
        }
    } else if (language == "python") {
        // Extract import / from ... import
        std::regex import_re(R"((?:from\s+([a-zA-Z0-9_.]+)\s+)?import\s+([a-zA-Z0-9_.]+))");
        std::sregex_iterator it(content.begin(), content.end(), import_re);
        std::sregex_iterator end;
        while (it != end) {
            if ((*it)[1].matched) {
                deps.push_back((*it)[1].str() + "." + (*it)[2].str());
            } else {
                deps.push_back((*it)[2].str());
            }
            ++it;
        }
    } else if (language == "javascript" || language == "typescript") {
        // Extract require / import
        std::regex require_re(R"(require\s*\(['"](.*?)['"]\))");
        std::regex import_re(R"(import\s+.*?\s+from\s+['"](.*?)['"])");
        
        std::sregex_iterator it1(content.begin(), content.end(), require_re);
        std::sregex_iterator end;
        while (it1 != end) {
            deps.push_back((*it1)[1].str());
            ++it1;
        }
        
        std::sregex_iterator it2(content.begin(), content.end(), import_re);
        while (it2 != end) {
            deps.push_back((*it2)[1].str());
            ++it2;
        }
    } else if (language == "go") {
        // Extract import
        std::regex import_re(R"(import\s+(?:\(|\s*)["]([^"]+)["])");
        std::sregex_iterator it(content.begin(), content.end(), import_re);
        std::sregex_iterator end;
        while (it != end) {
            deps.push_back((*it)[1].str());
            ++it;
        }
    }
    
    return deps;
}

void RepoParser::extractCrossFileDependencies(ParseResult& result) {
    // Build a map of defined symbols to files
    std::unordered_map<std::string, std::string> symbol_to_file;
    
    for (const auto& chunk : result.chunks) {
        if (!chunk.name.empty()) {
            symbol_to_file[chunk.name] = chunk.file_path;
        }
    }
    
    // For each chunk, check if its dependencies reference known files
    for (auto& chunk : result.chunks) {
        for (const auto& dep : chunk.dependencies) {
            // Check if dependency matches a symbol we know about
            auto it = symbol_to_file.find(dep);
            if (it != symbol_to_file.end() && it->second != chunk.file_path) {
                result.file_dependencies[chunk.file_path].push_back(it->second);
            }
        }
    }
}

void RepoParser::buildDependencyGraph(ParseResult& result) {
    // Already built in extractCrossFileDependencies
    // Could add more sophisticated analysis here
}

// =============================================================================
// Utility Functions
// =============================================================================

std::string RepoParser::readFile(const std::string& path) {
    std::ifstream file(path, std::ios::binary);
    if (!file.good()) {
        return "";
    }
    
    // Check file size
    file.seekg(0, std::ios::end);
    size_t size = file.tellg();
    file.seekg(0, std::ios::beg);
    
    if (size > config_.max_file_size) {
        // File too large
        return "";
    }
    
    std::string content(size, '\0');
    file.read(&content[0], size);
    
    return content;
}

std::string RepoParser::generateChunkId(const std::string& file_path, size_t line) {
    std::hash<std::string> hasher;
    size_t hash = hasher(file_path + ":" + std::to_string(line));
    return "chunk_" + std::to_string(hash);
}

std::string RepoParser::computeContentHash(const std::string& content) {
    std::hash<std::string> hasher;
    return std::to_string(hasher(content));
}

void RepoParser::buildExtensionMap() {
    extension_map_ = {
        {"c", "c"},
        {"h", "c"},
        {"cpp", "cpp"},
        {"cc", "cpp"},
        {"cxx", "cpp"},
        {"hpp", "cpp"},
        {"hxx", "cpp"},
        {"java", "java"},
        {"py", "python"},
        {"pyw", "python"},
        {"js", "javascript"},
        {"jsx", "javascript"},
        {"mjs", "javascript"},
        {"ts", "typescript"},
        {"tsx", "typescript"},
        {"go", "go"},
        {"rs", "rust"},
        {"rb", "ruby"},
        {"php", "php"},
        {"cs", "csharp"},
        {"swift", "swift"},
        {"kt", "kotlin"},
        {"scala", "scala"},
        {"sh", "shell"},
        {"bash", "shell"},
        {"sql", "sql"},
        {"json", "json"},
        {"yaml", "yaml"},
        {"yml", "yaml"},
        {"xml", "xml"},
        {"html", "html"},
        {"css", "css"},
        {"md", "markdown"}
    };
}

void RepoParser::compileIgnorePatterns() {
    for (const auto& pattern : config_.ignore_patterns) {
        try {
            compiled_ignore_patterns_.push_back(std::regex(pattern));
        } catch (const std::regex_error&) {
            // Invalid pattern, skip
        }
    }
}

void RepoParser::parseFilesParallel(const std::vector<std::string>& files, ParseResult& result) {
    // For simplicity, use sequential parsing with potential for OpenMP
    // In production, use thread pool
    for (const auto& file : files) {
        parseFileSingle(file, result);
    }
}

void RepoParser::parseFileSingle(const std::string& file, ParseResult& result) {
    try {
        auto chunks = parseFile(file);
        
        std::lock_guard<std::mutex> lock(mutex_);
        for (auto& chunk : chunks) {
            result.chunks.push_back(std::move(chunk));
        }
        result.parsed_files++;
        result.total_lines += result.chunks.back().end_line;
        
    } catch (const std::exception& e) {
        std::lock_guard<std::mutex> lock(mutex_);
        result.errors.push_back("Error parsing " + file + ": " + e.what());
    }
}

// =============================================================================
// Statistics
// =============================================================================

void RepoParser::updateStats(const ParseResult& result) {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    
    stats_.total_repos_parsed++;
    stats_.total_files_parsed += result.parsed_files;
    stats_.total_chunks_created += result.chunks.size();
    
    double n = static_cast<double>(stats_.total_repos_parsed);
    stats_.avg_parse_time_ms = (stats_.avg_parse_time_ms * (n - 1) + result.parse_time.count()) / n;
    stats_.avg_chunks_per_file = 
        static_cast<double>(stats_.total_chunks_created) / stats_.total_files_parsed;
}

RepoParser::Stats RepoParser::getStats() const {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    return stats_;
}

void RepoParser::resetStats() {
    std::lock_guard<std::mutex> lock(stats_mutex_);
    stats_ = Stats{};
}

bool RepoParser::isReady() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return initialized_;
}

std::string RepoParser::getLastError() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return last_error_;
}

} // namespace aipr::tms
