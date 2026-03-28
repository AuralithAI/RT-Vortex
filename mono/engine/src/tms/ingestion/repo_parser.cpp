/**
 * TMS Repository Parser Implementation
 * 
 * Tree-sitter based repository parsing with semantic chunking.
 */

#include "tms/repo_parser.h"
#include <algorithm>
#include <fstream>
#include <iostream>
#include <sstream>
#include <filesystem>
#include <regex>
#include <queue>
#include <mutex>
#include <thread>
#include <functional>
#include <condition_variable>
#include <atomic>

#ifdef AIPR_HAS_TREE_SITTER
#include <tree_sitter/api.h>

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
// TreeSitterImpl (pimpl definition)
// =============================================================================

class RepoParser::TreeSitterImpl {
public:
    TreeSitterImpl() = default;

    ~TreeSitterImpl() {
#ifdef AIPR_HAS_TREE_SITTER
        for (auto& [lang, parser] : parsers_) {
            ts_parser_delete(parser);
        }
        parsers_.clear();
#endif
    }

    void initialize() {
#ifdef AIPR_HAS_TREE_SITTER
        addParser("c", tree_sitter_c());
        addParser("cpp", tree_sitter_cpp());
        addParser("java", tree_sitter_java());
        addParser("python", tree_sitter_python());
        addParser("javascript", tree_sitter_javascript());
        addParser("typescript", tree_sitter_typescript());
        addParser("go", tree_sitter_go());
        addParser("rust", tree_sitter_rust());
#endif
    }

    bool hasLanguage(const std::string& lang) const {
#ifdef AIPR_HAS_TREE_SITTER
        return parsers_.find(lang) != parsers_.end();
#else
        (void)lang;
        return false;
#endif
    }

    std::vector<std::string> supportedLanguages() const {
        std::vector<std::string> result;
#ifdef AIPR_HAS_TREE_SITTER
        for (const auto& [lang, _] : parsers_) {
            result.push_back(lang);
        }
#endif
        return result;
    }

    std::vector<CodeChunk> parseFile(const std::string& content,
                                     const std::string& file_path,
                                     const std::string& language) {
        std::vector<CodeChunk> chunks;
#ifdef AIPR_HAS_TREE_SITTER
        auto it = parsers_.find(language);
        if (it == parsers_.end()) return chunks;

        std::lock_guard<std::mutex> lock(ts_mutex_);

        TSParser* parser = it->second;
        TSTree* tree = ts_parser_parse_string(parser, nullptr, content.c_str(), content.size());
        if (!tree) return chunks;

        TSNode root = ts_tree_root_node(tree);
        extractChunksFromNode(root, content, file_path, language, chunks);
        ts_tree_delete(tree);
#else
        (void)content; (void)file_path; (void)language;
#endif
        return chunks;
    }

private:
#ifdef AIPR_HAS_TREE_SITTER
    std::unordered_map<std::string, TSParser*> parsers_;
    std::mutex ts_mutex_;

    void addParser(const std::string& lang, TSLanguage* language) {
        TSParser* parser = ts_parser_new();
        if (ts_parser_set_language(parser, language)) {
            parsers_[lang] = parser;
        } else {
            ts_parser_delete(parser);
        }
    }

    void extractChunksFromNode(TSNode node, const std::string& content,
                                const std::string& file_path,
                                const std::string& language,
                                std::vector<CodeChunk>& chunks,
                                const std::string& parent_chunk_id = "") {
        const char* type = ts_node_type(node);
        std::string node_type(type ? type : "");

        bool is_function = (node_type == "function_definition" ||
                            node_type == "function_declaration" ||
                            node_type == "method_definition" ||
                            node_type == "method_declaration" ||
                            node_type == "function_item");

        bool is_class = (node_type == "class_definition" ||
                         node_type == "class_declaration" ||
                         node_type == "class_specifier" ||
                         node_type == "struct_specifier" ||
                         node_type == "impl_item");

        // Track current chunk ID so children can reference it as parent
        std::string current_chunk_id = parent_chunk_id;

        if (is_function || is_class) {
            uint32_t start_byte = ts_node_start_byte(node);
            uint32_t end_byte = ts_node_end_byte(node);
            TSPoint start_pt = ts_node_start_point(node);
            TSPoint end_pt = ts_node_end_point(node);

            CodeChunk chunk;
            chunk.id = file_path + ":" + std::to_string(start_pt.row + 1);
            chunk.file_path = file_path;
            chunk.language = language;
            chunk.type = is_function ? "function" : "class";
            chunk.content = content.substr(start_byte, end_byte - start_byte);
            chunk.start_line = static_cast<int>(start_pt.row + 1);
            chunk.end_line = static_cast<int>(end_pt.row + 1);
            chunk.start_byte = static_cast<int>(start_byte);
            chunk.end_byte = static_cast<int>(end_byte);

            // Set parent_chunk_id if this chunk is nested inside another
            if (!parent_chunk_id.empty()) {
                chunk.parent_chunk_id = parent_chunk_id;
            }

            // Extract name from first identifier child
            uint32_t child_count = ts_node_child_count(node);
            for (uint32_t i = 0; i < child_count; ++i) {
                TSNode child = ts_node_child(node, i);
                const char* child_type = ts_node_type(child);
                if (child_type && (std::string(child_type) == "identifier" ||
                                   std::string(child_type) == "name")) {
                    uint32_t cs = ts_node_start_byte(child);
                    uint32_t ce = ts_node_end_byte(child);
                    chunk.name = content.substr(cs, ce - cs);
                    break;
                }
            }

            // Populate symbols: the chunk defines its own name as a symbol
            if (!chunk.name.empty()) {
                chunk.symbols.push_back(chunk.name);
            }

            // For classes, also extract member names as symbols
            if (is_class) {
                extractMemberSymbols(node, content, chunk.symbols);
            }

            current_chunk_id = chunk.id;
            chunks.push_back(std::move(chunk));
        }

        // Recurse into children, passing current scope as parent
        uint32_t child_count = ts_node_child_count(node);
        for (uint32_t i = 0; i < child_count; ++i) {
            extractChunksFromNode(ts_node_child(node, i), content, file_path,
                                  language, chunks, current_chunk_id);
        }
    }

    // Extract member function/field names from a class/struct node
    void extractMemberSymbols(TSNode class_node, const std::string& content,
                               std::vector<std::string>& symbols) {
        uint32_t child_count = ts_node_child_count(class_node);
        for (uint32_t i = 0; i < child_count; ++i) {
            TSNode child = ts_node_child(class_node, i);
            const char* ct = ts_node_type(child);
            if (!ct) continue;
            std::string ctype(ct);

            // Look for body/block nodes that contain members
            bool is_body = (ctype == "class_body" || ctype == "declaration_list" ||
                            ctype == "field_declaration_list" || ctype == "block" ||
                            ctype == "impl_body");
            if (!is_body) continue;

            uint32_t body_count = ts_node_child_count(child);
            for (uint32_t j = 0; j < body_count; ++j) {
                TSNode member = ts_node_child(child, j);
                const char* mt = ts_node_type(member);
                if (!mt) continue;
                std::string mtype(mt);

                // Only grab names from declarations (not full recursion)
                bool is_member = (mtype == "function_definition" ||
                                  mtype == "method_definition" ||
                                  mtype == "method_declaration" ||
                                  mtype == "field_declaration" ||
                                  mtype == "property_definition");
                if (!is_member) continue;

                // Find the identifier child
                uint32_t mc = ts_node_child_count(member);
                for (uint32_t k = 0; k < mc; ++k) {
                    TSNode id_node = ts_node_child(member, k);
                    const char* idt = ts_node_type(id_node);
                    if (idt && (std::string(idt) == "identifier" ||
                                std::string(idt) == "field_identifier" ||
                                std::string(idt) == "property_identifier" ||
                                std::string(idt) == "name")) {
                        uint32_t s = ts_node_start_byte(id_node);
                        uint32_t e = ts_node_end_byte(id_node);
                        std::string sym = content.substr(s, e - s);
                        if (sym.size() >= 2) {
                            symbols.push_back(std::move(sym));
                        }
                        break;
                    }
                }
            }
        }
    }
#endif
};

// =============================================================================
// ThreadPool (pimpl definition)
// =============================================================================

class RepoParser::ThreadPool {
public:
    explicit ThreadPool(int num_threads) : stop_(false) {
        if (num_threads <= 0) {
            num_threads = static_cast<int>(std::thread::hardware_concurrency());
            if (num_threads <= 0) num_threads = 4;
        }
        for (int i = 0; i < num_threads; ++i) {
            workers_.emplace_back([this] { workerLoop(); });
        }
    }

    ~ThreadPool() {
        {
            std::lock_guard<std::mutex> lock(mutex_);
            stop_ = true;
        }
        cv_.notify_all();
        for (auto& w : workers_) {
            if (w.joinable()) w.join();
        }
    }

    void enqueue(std::function<void()> task) {
        {
            std::lock_guard<std::mutex> lock(mutex_);
            tasks_.push(std::move(task));
        }
        cv_.notify_one();
    }

    void wait() {
        std::unique_lock<std::mutex> lock(mutex_);
        cv_done_.wait(lock, [this] { return tasks_.empty() && active_ == 0; });
    }

private:
    void workerLoop() {
        while (true) {
            std::function<void()> task;
            {
                std::unique_lock<std::mutex> lock(mutex_);
                cv_.wait(lock, [this] { return stop_ || !tasks_.empty(); });
                if (stop_ && tasks_.empty()) return;
                task = std::move(tasks_.front());
                tasks_.pop();
                active_++;
            }
            task();
            {
                std::lock_guard<std::mutex> lock(mutex_);
                active_--;
            }
            cv_done_.notify_all();
        }
    }

    std::vector<std::thread> workers_;
    std::queue<std::function<void()>> tasks_;
    std::mutex mutex_;
    std::condition_variable cv_;
    std::condition_variable cv_done_;
    bool stop_;
    int active_ = 0;
};

// =============================================================================
// Constructor / Destructor
// =============================================================================

RepoParser::RepoParser(const RepoParserConfig& config)
    : config_(config) {
    tree_sitter_ = std::make_unique<TreeSitterImpl>();
    tree_sitter_->initialize();
}

RepoParser::~RepoParser() = default;

// =============================================================================
// Main Parsing Interface
// =============================================================================

std::vector<CodeChunk> RepoParser::parseRepository(
    const std::string& repo_path,
    ParseProgressCallback progress_callback
) {
    auto start = std::chrono::steady_clock::now();
    last_stats_ = RepoStats{};

    auto files = walkDirectory(repo_path);
    last_stats_.total_files = files.size();
    std::cerr << "[PARSER] parseRepository: path=" << repo_path
              << " files_found=" << files.size() << std::endl;

    std::vector<CodeChunk> all_chunks;
    std::mutex chunks_mutex;

    initThreadPool();

    for (size_t i = 0; i < files.size(); ++i) {
        const auto& file = files[i];
        thread_pool_->enqueue([&, i, file] {
            try {
                auto parsed = parseFile(file, repo_path);
                auto file_chunks = chunkFile(parsed);

                // Propagate file-level imports into each chunk's dependencies
                // so the KG edge inference can create IMPORTS edges.
                if (!parsed.imports.empty()) {
                    for (auto& c : file_chunks) {
                        if (c.dependencies.empty()) {
                            c.dependencies = parsed.imports;
                        }
                    }
                }

                std::lock_guard<std::mutex> lock(chunks_mutex);
                for (auto& c : file_chunks) {
                    all_chunks.push_back(std::move(c));
                }
                last_stats_.parsed_files++;
                last_stats_.total_lines += parsed.line_count;
                last_stats_.total_bytes += parsed.size_bytes;
                last_stats_.total_chunks += file_chunks.size();
                last_stats_.files_by_language[parsed.language.id]++;
            } catch (...) {
                std::lock_guard<std::mutex> lock(chunks_mutex);
                last_stats_.failed_files++;
            }

            if (progress_callback) {
                float progress = static_cast<float>(i + 1) / files.size();
                progress_callback(progress, file, "Parsing...");
            }
        });
    }

    thread_pool_->wait();

    std::cerr << "[PARSER] parseRepository done: total_chunks=" << all_chunks.size()
              << " parsed_files=" << last_stats_.parsed_files
              << " failed_files=" << last_stats_.failed_files << std::endl;

    auto end = std::chrono::steady_clock::now();
    last_stats_.parse_time = std::chrono::duration_cast<std::chrono::milliseconds>(end - start);

    return all_chunks;
}

std::vector<std::string> RepoParser::listFiles(const std::string& repo_path) {
    return walkDirectory(repo_path);
}

std::vector<CodeChunk> RepoParser::parseFiles(
    const std::string& repo_root,
    const std::vector<std::string>& file_paths
) {
    std::vector<CodeChunk> all_chunks;
    for (const auto& fp : file_paths) {
        auto parsed = parseFile(fp, repo_root);
        auto file_chunks = chunkFile(parsed);
        // Propagate imports → chunk dependencies
        if (!parsed.imports.empty()) {
            for (auto& c : file_chunks) {
                if (c.dependencies.empty()) {
                    c.dependencies = parsed.imports;
                }
            }
        }
        all_chunks.insert(all_chunks.end(),
                          std::make_move_iterator(file_chunks.begin()),
                          std::make_move_iterator(file_chunks.end()));
    }
    return all_chunks;
}

ParsedFile RepoParser::parseFile(
    const std::string& file_path,
    const std::string& repo_root
) {
    ParsedFile result;
    result.absolute_path = file_path;
    result.path = repo_root.empty() ? file_path :
        fs::relative(file_path, repo_root).string();

    result.content = readFile(file_path);
    result.size_bytes = result.content.size();
    result.line_count = std::count(result.content.begin(), result.content.end(), '\n') + 1;
    result.language = detectLanguage(file_path);

    // Tree-sitter parse if available
    if (config_.use_tree_sitter && tree_sitter_->hasLanguage(result.language.id)) {
        result.chunks = tree_sitter_->parseFile(result.content, result.path, result.language.id);
        result.fully_parsed = true;
    } else if (config_.fallback_to_regex) {
        // Fallback: create a single chunk for the whole file
        result.chunks = chunkWithFallback(result);
        result.fully_parsed = true;
    }

    if (config_.extract_imports) {
        result.imports = extractImports(result);
    }

    return result;
}

ParsedFile RepoParser::parseContent(
    const std::string& content,
    const std::string& file_path,
    const std::string& language
) {
    ParsedFile result;
    result.path = file_path;
    result.absolute_path = file_path;
    result.content = content;
    result.size_bytes = content.size();
    result.line_count = std::count(content.begin(), content.end(), '\n') + 1;

    if (!language.empty()) {
        result.language.id = language;
        result.language.name = language;
        result.language.confidence = 1.0;
        result.language.tree_sitter_supported = tree_sitter_->hasLanguage(language);
    } else {
        result.language = detectLanguage(file_path);
    }

    if (tree_sitter_->hasLanguage(result.language.id)) {
        result.chunks = tree_sitter_->parseFile(content, file_path, result.language.id);
    } else {
        result.chunks = chunkWithFallback(result);
    }

    result.fully_parsed = true;
    return result;
}

// =============================================================================
// Incremental Parsing
// =============================================================================

std::vector<CodeChunk> RepoParser::parseIncremental(
    const std::string& repo_path,
    const std::vector<std::string>& changed_files,
    const std::vector<std::string>& /*deleted_files*/
) {
    return parseFiles(repo_path, changed_files);
}

// =============================================================================
// Language Detection
// =============================================================================

DetectedLanguage RepoParser::detectLanguage(const std::string& file_path) {
    DetectedLanguage result;
    result.confidence = 0.0;
    result.tree_sitter_supported = false;

    std::string ext;
    auto dot_pos = file_path.rfind('.');
    if (dot_pos != std::string::npos) {
        ext = file_path.substr(dot_pos + 1);
    }

    static const std::unordered_map<std::string, std::pair<std::string, std::string>> ext_map = {
        {"c", {"c", "C"}}, {"h", {"c", "C"}},
        {"cpp", {"cpp", "C++"}}, {"cc", {"cpp", "C++"}}, {"cxx", {"cpp", "C++"}},
        {"hpp", {"cpp", "C++"}}, {"hxx", {"cpp", "C++"}},
        {"java", {"java", "Java"}},
        {"py", {"python", "Python"}},
        {"js", {"javascript", "JavaScript"}}, {"jsx", {"javascript", "JavaScript"}},
        {"ts", {"typescript", "TypeScript"}}, {"tsx", {"typescript", "TypeScript"}},
        {"go", {"go", "Go"}},
        {"rs", {"rust", "Rust"}},
        {"rb", {"ruby", "Ruby"}},
        {"cs", {"csharp", "C#"}},
        {"swift", {"swift", "Swift"}},
        {"kt", {"kotlin", "Kotlin"}}, {"kts", {"kotlin", "Kotlin"}},
        {"scala", {"scala", "Scala"}},
        {"php", {"php", "PHP"}},
        {"lua", {"lua", "Lua"}},
        {"sh", {"bash", "Bash"}}, {"bash", {"bash", "Bash"}},
        {"sql", {"sql", "SQL"}},
        {"proto", {"protobuf", "Protocol Buffers"}},
        {"yaml", {"yaml", "YAML"}}, {"yml", {"yaml", "YAML"}},
        {"json", {"json", "JSON"}},
        {"toml", {"toml", "TOML"}},
        {"xml", {"xml", "XML"}},
        {"md", {"markdown", "Markdown"}}, {"rst", {"rst", "reStructuredText"}},
    };

    auto it = ext_map.find(ext);
    if (it != ext_map.end()) {
        result.id = it->second.first;
        result.name = it->second.second;
        result.confidence = 0.95;
        result.tree_sitter_supported = tree_sitter_->hasLanguage(result.id);
    } else {
        result.id = "unknown";
        result.name = "Unknown";
    }

    return result;
}

DetectedLanguage RepoParser::detectLanguageFromContent(const std::string& content) {
    DetectedLanguage result;
    result.id = "unknown";
    result.name = "Unknown";
    result.confidence = 0.0;
    result.tree_sitter_supported = false;

    // Simple heuristics
    if (content.find("#include") != std::string::npos) {
        result.id = "cpp"; result.name = "C++"; result.confidence = 0.6;
    } else if (content.find("def ") != std::string::npos && content.find("import ") != std::string::npos) {
        result.id = "python"; result.name = "Python"; result.confidence = 0.6;
    } else if (content.find("public class ") != std::string::npos) {
        result.id = "java"; result.name = "Java"; result.confidence = 0.7;
    } else if (content.find("func ") != std::string::npos && content.find("package ") != std::string::npos) {
        result.id = "go"; result.name = "Go"; result.confidence = 0.7;
    }

    result.tree_sitter_supported = tree_sitter_->hasLanguage(result.id);
    return result;
}

bool RepoParser::hasTreeSitterSupport(const std::string& language) {
    return tree_sitter_->hasLanguage(language);
}

std::vector<std::string> RepoParser::getSupportedLanguages() {
    return tree_sitter_->supportedLanguages();
}

// =============================================================================
// Dependency Analysis
// =============================================================================

std::vector<std::string> RepoParser::extractImports(const ParsedFile& file) {
    std::vector<std::string> imports;

    // Regex-based import extraction per language
    std::vector<std::regex> patterns;

    if (file.language.id == "python") {
        patterns.push_back(std::regex(R"(^\s*(?:from\s+\S+\s+)?import\s+(.+))", std::regex::multiline));
    } else if (file.language.id == "java") {
        patterns.push_back(std::regex(R"(^\s*import\s+(.+?)\s*;)", std::regex::multiline));
    } else if (file.language.id == "cpp" || file.language.id == "c") {
        patterns.push_back(std::regex(R"(^\s*#include\s+[<"](.+?)[>"])", std::regex::multiline));
    } else if (file.language.id == "javascript" || file.language.id == "typescript") {
        patterns.push_back(std::regex(R"((?:import|require)\s*\(?['\"](.+?)['\"])", std::regex::multiline));
    } else if (file.language.id == "go") {
        patterns.push_back(std::regex(R"(^\s*\"(.+?)\")", std::regex::multiline));
    } else if (file.language.id == "rust") {
        patterns.push_back(std::regex(R"(^\s*use\s+(.+?)\s*;)", std::regex::multiline));
    }

    for (const auto& pattern : patterns) {
        auto begin = std::sregex_iterator(file.content.begin(), file.content.end(), pattern);
        auto end = std::sregex_iterator();
        for (auto it = begin; it != end; ++it) {
            if (it->size() > 1) {
                imports.push_back((*it)[1].str());
            }
        }
    }

    return imports;
}

std::vector<RepoParser::CallGraphNode> RepoParser::buildCallGraph(
    const std::vector<ParsedFile>& files
) {
    // Build call graph by analyzing function definitions and call sites
    // across parsed files using symbol references from chunks

    // Step 1: Collect all defined symbols and their file locations
    std::unordered_map<std::string, CallGraphNode> nodes;
    
    for (const auto& file : files) {
        for (const auto& chunk : file.chunks) {
            if (chunk.type == "function" || chunk.type == "method" || chunk.type == "class") {
                if (!chunk.name.empty()) {
                    auto& node = nodes[chunk.name];
                    node.symbol_name = chunk.name;
                    node.file_path = file.path;
                }
            }
        }
    }

    // Step 2: Scan chunk contents for references to known symbols
    for (const auto& file : files) {
        for (const auto& chunk : file.chunks) {
            if (chunk.name.empty()) continue;

            // For each known symbol, check if this chunk's content references it
            for (auto& [sym_name, sym_node] : nodes) {
                if (sym_name == chunk.name) continue; // skip self-reference

                // Simple heuristic: check if the symbol name appears in the content
                // followed by '(' (function call) or '.' (method access)
                std::string call_pattern1 = sym_name + "(";
                std::string call_pattern2 = "." + sym_name + "(";
                std::string call_pattern3 = "::" + sym_name + "(";

                if (chunk.content.find(call_pattern1) != std::string::npos ||
                    chunk.content.find(call_pattern2) != std::string::npos ||
                    chunk.content.find(call_pattern3) != std::string::npos) {
                    // chunk.name calls sym_name
                    auto caller_it = nodes.find(chunk.name);
                    if (caller_it != nodes.end()) {
                        auto& caller_callees = caller_it->second.callees;
                        if (std::find(caller_callees.begin(), caller_callees.end(), sym_name)
                            == caller_callees.end()) {
                            caller_callees.push_back(sym_name);
                        }

                        auto& callee_callers = sym_node.callers;
                        if (std::find(callee_callers.begin(), callee_callers.end(), chunk.name)
                            == callee_callers.end()) {
                            callee_callers.push_back(chunk.name);
                        }
                    }
                }
            }
        }
    }

    // Step 3: Convert to result vector
    std::vector<CallGraphNode> result;
    result.reserve(nodes.size());
    for (auto& [_, node] : nodes) {
        result.push_back(std::move(node));
    }
    return result;
}

// =============================================================================
// Statistics
// =============================================================================

void RepoParser::resetStats() {
    last_stats_ = RepoStats{};
}

// =============================================================================
// Private Helpers
// =============================================================================

std::vector<std::string> RepoParser::walkDirectory(const std::string& root) {
    std::vector<std::string> files;
    std::cerr << "[PARSER] walkDirectory: root=" << root
              << " exists=" << fs::exists(root)
              << " is_dir=" << fs::is_directory(root) << std::endl;
    try {
        size_t total_seen = 0;
        for (const auto& entry : fs::recursive_directory_iterator(root,
                fs::directory_options::skip_permission_denied)) {
            if (!entry.is_regular_file()) continue;
            total_seen++;
            std::string path = entry.path().string();
            if (shouldIncludeFile(path)) {
                files.push_back(path);
            }
        }
        std::cerr << "[PARSER] walkDirectory: total_seen=" << total_seen
                  << " included=" << files.size() << std::endl;
    } catch (const std::exception& e) {
        std::cerr << "[PARSER] walkDirectory EXCEPTION: " << e.what() << std::endl;
    } catch (...) {
        std::cerr << "[PARSER] walkDirectory UNKNOWN EXCEPTION" << std::endl;
    }
    return files;
}

bool RepoParser::shouldIncludeFile(const std::string& path) {
    // Check excludes
    for (const auto& pattern : config_.file_filter.exclude_patterns) {
        // Simple glob matching: just check if the pattern appears in the path
        std::string clean_pattern = pattern;
        // Remove leading/trailing *
        while (!clean_pattern.empty() && clean_pattern.front() == '*') clean_pattern.erase(0, 1);
        while (!clean_pattern.empty() && clean_pattern.back() == '*') clean_pattern.pop_back();
        if (!clean_pattern.empty() && path.find(clean_pattern) != std::string::npos) {
            return false;
        }
    }

    // Check size
    try {
        auto size = fs::file_size(path);
        if (size > config_.file_filter.max_file_size_bytes) return false;
        if (size < config_.file_filter.min_file_size_bytes) return false;
    } catch (...) { return false; }

    // Check includes (at least one pattern must match)
    std::string ext;
    auto dot_pos = path.rfind('.');
    if (dot_pos != std::string::npos) {
        ext = "*" + path.substr(dot_pos);
    } else {
        ext = fs::path(path).filename().string();
    }

    for (const auto& pattern : config_.file_filter.include_patterns) {
        if (pattern == ext) return true;
        // Also check filename match
        if (fs::path(path).filename().string() == pattern) return true;
    }

    return false;
}

bool RepoParser::isBinaryFile(const std::string& path) {
    std::ifstream f(path, std::ios::binary);
    char buf[512];
    f.read(buf, sizeof(buf));
    auto count = f.gcount();
    for (int i = 0; i < count; ++i) {
        if (buf[i] == 0) return true;
    }
    return false;
}

bool RepoParser::isGeneratedFile(const std::string& path) {
    // Check common generated file indicators
    std::string filename = fs::path(path).filename().string();
    return filename.find("_generated") != std::string::npos ||
           filename.find(".pb.") != std::string::npos ||
           filename.find(".min.") != std::string::npos;
}

std::string RepoParser::readFile(const std::string& path) {
    std::ifstream f(path);
    if (!f.is_open()) return "";
    std::stringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

std::string RepoParser::computeContentHash(const std::string& content) {
    std::hash<std::string> hasher;
    return std::to_string(hasher(content));
}

std::string RepoParser::generateFileSummary(const ParsedFile& file) {
    std::ostringstream oss;
    oss << "File: " << file.path << " (" << file.language.name << ")\n";
    oss << "Lines: " << file.line_count << "\n";
    if (!file.imports.empty()) {
        oss << "Imports: ";
        for (size_t i = 0; i < file.imports.size() && i < 5; ++i) {
            if (i > 0) oss << ", ";
            oss << file.imports[i];
        }
        if (file.imports.size() > 5) oss << " (+" << (file.imports.size() - 5) << " more)";
        oss << "\n";
    }
    oss << "Chunks: " << file.chunks.size() << "\n";
    return oss.str();
}

// =============================================================================
// Chunking
// =============================================================================

std::vector<CodeChunk> RepoParser::chunkFile(const ParsedFile& file) {
    if (!file.chunks.empty()) {
        return file.chunks;
    }
    return chunkWithFallback(file);
}

std::vector<CodeChunk> RepoParser::chunkWithTreeSitter(const ParsedFile& file) {
    return tree_sitter_->parseFile(file.content, file.path, file.language.id);
}

std::vector<CodeChunk> RepoParser::chunkWithFallback(const ParsedFile& file) {
    std::vector<CodeChunk> chunks;

    // Simple line-based chunking
    std::istringstream stream(file.content);
    std::string line;
    std::string current_chunk;
    int line_num = 0;
    int chunk_start = 1;
    size_t target_tokens = config_.chunking.target_chunk_tokens;
    size_t max_tokens = config_.chunking.max_chunk_tokens;

    while (std::getline(stream, line)) {
        line_num++;
        current_chunk += line + "\n";

        // Estimate tokens (~4 chars per token)
        size_t estimated_tokens = current_chunk.size() / 4;

        if (estimated_tokens >= target_tokens) {
            CodeChunk chunk;
            chunk.id = file.path + ":" + std::to_string(chunk_start);
            chunk.file_path = file.path;
            chunk.language = file.language.id;
            chunk.type = "block";
            chunk.content = current_chunk;
            chunk.start_line = chunk_start;
            chunk.end_line = line_num;
            chunk.content_hash = computeContentHash(current_chunk);
            chunks.push_back(std::move(chunk));

            current_chunk.clear();
            chunk_start = line_num + 1;
        }

        if (current_chunk.size() / 4 > max_tokens) {
            CodeChunk chunk;
            chunk.id = file.path + ":" + std::to_string(chunk_start);
            chunk.file_path = file.path;
            chunk.language = file.language.id;
            chunk.type = "block";
            chunk.content = current_chunk;
            chunk.start_line = chunk_start;
            chunk.end_line = line_num;
            chunks.push_back(std::move(chunk));

            current_chunk.clear();
            chunk_start = line_num + 1;
        }
    }

    // Remaining content
    if (!current_chunk.empty()) {
        CodeChunk chunk;
        chunk.id = file.path + ":" + std::to_string(chunk_start);
        chunk.file_path = file.path;
        chunk.language = file.language.id;
        chunk.type = "block";
        chunk.content = current_chunk;
        chunk.start_line = chunk_start;
        chunk.end_line = line_num;
        chunks.push_back(std::move(chunk));
    }

    // Also create a file summary chunk
    if (config_.chunking.generate_file_summaries) {
        CodeChunk summary;
        summary.id = file.path + ":summary";
        summary.file_path = file.path;
        summary.language = file.language.id;
        summary.type = "file_summary";
        summary.content = generateFileSummary(file);
        summary.start_line = 1;
        summary.end_line = static_cast<int>(file.line_count);
        chunks.insert(chunks.begin(), std::move(summary));
    }

    return chunks;
}

// =============================================================================
// Thread Pool Management
// =============================================================================

void RepoParser::initThreadPool() {
    if (!thread_pool_) {
        thread_pool_ = std::make_unique<ThreadPool>(config_.num_threads);
    }
}

// =============================================================================
// ChunkStrategy Implementation
// =============================================================================

ChunkStrategy::ChunkStrategy(const ChunkingConfig& config)
    : config_(config) {
}

ChunkStrategy::~ChunkStrategy() = default;

std::vector<CodeChunk> ChunkStrategy::apply(const ParsedFile& file) {
    std::vector<CodeChunk> chunks;
    auto func_chunks = createFunctionChunks(file);
    chunks.insert(chunks.end(), func_chunks.begin(), func_chunks.end());

    if (config_.generate_file_summaries) {
        auto summary = createFileSummaryChunk(file);
        chunks.insert(chunks.begin(), std::move(summary));
    }
    return chunks;
}

CodeChunk ChunkStrategy::createFileSummaryChunk(const ParsedFile& file) {
    CodeChunk chunk;
    chunk.id = generateChunkId(file.path, "summary", 0);
    chunk.file_path = file.path;
    chunk.language = file.language.id;
    chunk.type = "file_summary";
    chunk.content = file.file_summary.empty() ?
        ("File: " + file.path + " (" + file.language.name + ")") :
        file.file_summary;
    chunk.start_line = 1;
    chunk.end_line = static_cast<int>(file.line_count);
    return chunk;
}

std::vector<CodeChunk> ChunkStrategy::createClassChunks(const ParsedFile& file) {
    std::vector<CodeChunk> result;
    for (const auto& c : file.chunks) {
        if (c.type == "class" || c.type == "module") {
            result.push_back(c);
        }
    }
    return result;
}

std::vector<CodeChunk> ChunkStrategy::createFunctionChunks(const ParsedFile& file) {
    std::vector<CodeChunk> result;
    for (const auto& c : file.chunks) {
        if (c.type == "function" || c.type == "method") {
            result.push_back(c);
        }
    }
    return result;
}

std::vector<CodeChunk> ChunkStrategy::createDependencyChunks(const ParsedFile& file) {
    std::vector<CodeChunk> result;
    if (!file.imports.empty()) {
        CodeChunk chunk;
        chunk.id = generateChunkId(file.path, "deps", 0);
        chunk.file_path = file.path;
        chunk.language = file.language.id;
        chunk.type = "dependency";
        std::ostringstream oss;
        for (const auto& imp : file.imports) {
            oss << imp << "\n";
        }
        chunk.content = oss.str();
        result.push_back(std::move(chunk));
    }
    return result;
}

std::vector<CodeChunk> ChunkStrategy::createCrossFileChunks(
    const ParsedFile& file,
    const std::vector<ParsedFile>& related_files
) {
    std::vector<CodeChunk> result;

    // Cross-file chunks capture the relationship between a file's imports
    // and the symbols exported by related files.
    // This gives the embedding engine context about how files connect.

    if (file.imports.empty() || related_files.empty()) return result;

    // Build an export map from related files
    std::unordered_map<std::string, const ParsedFile*> export_map;
    for (const auto& rf : related_files) {
        for (const auto& exp : rf.exports) {
            export_map[exp] = &rf;
        }
    }

    // For each import in the file, check if a related file exports it
    std::ostringstream oss;
    oss << "Cross-file context for " << file.path << ":\n";
    int match_count = 0;

    for (const auto& imp : file.imports) {
        auto it = export_map.find(imp);
        if (it != export_map.end()) {
            oss << "  imports '" << imp << "' from " << it->second->path << "\n";
            // Include a snippet from the related file's matching chunk
            for (const auto& chunk : it->second->chunks) {
                if (chunk.name == imp) {
                    std::string snippet = chunk.content.substr(
                        0, std::min<size_t>(200, chunk.content.size()));
                    oss << "    definition: " << snippet << "\n";
                    break;
                }
            }
            match_count++;
        }
    }

    if (match_count > 0) {
        CodeChunk chunk;
        chunk.id = generateChunkId(file.path, "cross_file", 0);
        chunk.file_path = file.path;
        chunk.language = file.language.id;
        chunk.type = "cross_file_context";
        chunk.name = "cross_file:" + file.path;
        chunk.content = oss.str();
        chunk.start_line = 0;
        chunk.end_line = 0;
        result.push_back(std::move(chunk));
    }

    return result;
}

std::string ChunkStrategy::generateChunkId(const std::string& file_path, const std::string& name, int index) {
    return file_path + ":" + name + ":" + std::to_string(index);
}

size_t ChunkStrategy::estimateTokens(const std::string& content) {
    return content.size() / 4;
}

std::string ChunkStrategy::getContextBefore(const std::vector<std::string>& lines, int start, int overlap) {
    std::ostringstream oss;
    int begin = std::max(0, start - overlap);
    for (int i = begin; i < start && i < static_cast<int>(lines.size()); ++i) {
        oss << lines[i] << "\n";
    }
    return oss.str();
}

std::string ChunkStrategy::getContextAfter(const std::vector<std::string>& lines, int end, int overlap) {
    std::ostringstream oss;
    int limit = std::min(static_cast<int>(lines.size()), end + overlap);
    for (int i = end; i < limit; ++i) {
        oss << lines[i] << "\n";
    }
    return oss.str();
}

} // namespace aipr::tms
