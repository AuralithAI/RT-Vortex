/**
 * AI PR Reviewer - AST-based Chunker
 * 
 * Uses tree-sitter grammars for intelligent code chunking.
 * Falls back to regex-based chunking if tree-sitter is not available.
 */

#include "indexer.h"
#include "types.h"
#include <string>
#include <vector>
#include <sstream>
#include <regex>
#include <functional>

#ifdef AIPR_HAS_TREE_SITTER
#include <tree_sitter/api.h>

// Tree-sitter language declarations (linked from grammars)
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

namespace aipr {

/**
 * AST Chunker configuration
 */
struct AstChunkerConfig {
    size_t target_chunk_size = 512;     // Target size in tokens (approximate)
    size_t max_chunk_size = 1024;       // Maximum chunk size
    size_t min_chunk_size = 50;         // Minimum chunk size
    size_t overlap_lines = 3;           // Lines of overlap between chunks
    bool preserve_functions = true;     // Try to keep functions whole
    bool include_context = true;        // Include parent context
    bool use_tree_sitter = true;        // Use tree-sitter when available
};

/**
 * Approximate token count (simple heuristic)
 */
size_t estimateTokenCount(const std::string& text) {
    // Rough approximation: ~4 characters per token for code
    return text.size() / 4;
}

/**
 * Split content into lines
 */
std::vector<std::string> splitLines(const std::string& content) {
    std::vector<std::string> lines;
    std::istringstream stream(content);
    std::string line;
    while (std::getline(stream, line)) {
        lines.push_back(line);
    }
    return lines;
}

/**
 * Join lines into content
 */
std::string joinLines(const std::vector<std::string>& lines, size_t start, size_t end) {
    std::ostringstream result;
    for (size_t i = start; i < end && i < lines.size(); ++i) {
        if (i > start) result << '\n';
        result << lines[i];
    }
    return result.str();
}

/**
 * Code block detected from AST or heuristics
 */
struct CodeBlock {
    size_t start_line;
    size_t end_line;
    size_t start_byte = 0;
    size_t end_byte = 0;
    std::string type;  // "function", "class", "method", "block"
    std::string name;
    int indent_level;
    std::string docstring;
    std::vector<std::string> symbols;  // Symbols defined in this block
};

#ifdef AIPR_HAS_TREE_SITTER
/**
 * Tree-sitter based block extraction
 */
class TreeSitterExtractor {
public:
    TreeSitterExtractor() {
        parser_ = ts_parser_new();
    }
    
    ~TreeSitterExtractor() {
        if (parser_) ts_parser_delete(parser_);
        if (tree_) ts_tree_delete(tree_);
    }
    
    bool setLanguage(const std::string& language) {
        TSLanguage* lang = getLanguage(language);
        if (!lang) return false;
        
        current_language_ = language;
        return ts_parser_set_language(parser_, lang);
    }
    
    bool parse(const std::string& content) {
        if (tree_) {
            ts_tree_delete(tree_);
            tree_ = nullptr;
        }
        
        content_ = content;
        tree_ = ts_parser_parse_string(parser_, nullptr, content.c_str(), content.size());
        return tree_ != nullptr;
    }
    
    std::vector<CodeBlock> extractBlocks() {
        std::vector<CodeBlock> blocks;
        if (!tree_) return blocks;
        
        TSNode root = ts_tree_root_node(tree_);
        extractBlocksRecursive(root, blocks, 0);
        
        return blocks;
    }
    
    /**
     * Extract symbol at a given position
     */
    std::string getSymbolAtLine(size_t line) {
        if (!tree_) return "";
        
        TSNode root = ts_tree_root_node(tree_);
        return findSymbolAtLine(root, line);
    }
    
private:
    TSLanguage* getLanguage(const std::string& language) {
        if (language == "c") return tree_sitter_c();
        if (language == "cpp" || language == "c++") return tree_sitter_cpp();
        if (language == "java") return tree_sitter_java();
        if (language == "python") return tree_sitter_python();
        if (language == "javascript" || language == "js") return tree_sitter_javascript();
        if (language == "typescript" || language == "ts") return tree_sitter_typescript();
        if (language == "go") return tree_sitter_go();
        if (language == "rust") return tree_sitter_rust();
        return nullptr;
    }
    
    void extractBlocksRecursive(TSNode node, std::vector<CodeBlock>& blocks, int depth) {
        const char* type = ts_node_type(node);
        
        // Check if this is a block-level node
        bool is_block = isBlockNode(type);
        
        if (is_block) {
            CodeBlock block;
            block.start_line = ts_node_start_point(node).row;
            block.end_line = ts_node_end_point(node).row;
            block.start_byte = ts_node_start_byte(node);
            block.end_byte = ts_node_end_byte(node);
            block.type = categorizeNode(type);
            block.name = extractNodeName(node);
            block.indent_level = depth;
            
            // Extract docstring if available
            block.docstring = extractDocstring(node);
            
            blocks.push_back(block);
        }
        
        // Recurse into children
        uint32_t child_count = ts_node_child_count(node);
        for (uint32_t i = 0; i < child_count; ++i) {
            TSNode child = ts_node_child(node, i);
            extractBlocksRecursive(child, blocks, depth + (is_block ? 1 : 0));
        }
    }
    
    bool isBlockNode(const char* type) {
        static const std::vector<std::string> block_types = {
            // Functions
            "function_definition", "function_declaration", "method_definition",
            "method_declaration", "arrow_function", "function_expression",
            "function_item", "function",
            // Classes/Types
            "class_definition", "class_declaration", "class_specifier",
            "struct_definition", "struct_specifier", "struct_item",
            "enum_definition", "enum_specifier", "enum_item",
            "interface_declaration", "trait_item", "impl_item",
            // Other blocks
            "module", "namespace_definition", "package_clause",
        };
        
        std::string t(type);
        for (const auto& bt : block_types) {
            if (t == bt) return true;
        }
        return false;
    }
    
    std::string categorizeNode(const char* type) {
        std::string t(type);
        
        if (t.find("function") != std::string::npos || 
            t.find("method") != std::string::npos) {
            return "function";
        }
        if (t.find("class") != std::string::npos) {
            return "class";
        }
        if (t.find("struct") != std::string::npos) {
            return "struct";
        }
        if (t.find("enum") != std::string::npos) {
            return "enum";
        }
        if (t.find("interface") != std::string::npos ||
            t.find("trait") != std::string::npos) {
            return "interface";
        }
        if (t.find("impl") != std::string::npos) {
            return "impl";
        }
        if (t.find("module") != std::string::npos ||
            t.find("namespace") != std::string::npos) {
            return "module";
        }
        return "block";
    }
    
    std::string extractNodeName(TSNode node) {
        // Look for identifier child node
        uint32_t child_count = ts_node_child_count(node);
        for (uint32_t i = 0; i < child_count; ++i) {
            TSNode child = ts_node_child(node, i);
            const char* child_type = ts_node_type(child);
            
            if (strcmp(child_type, "identifier") == 0 ||
                strcmp(child_type, "name") == 0 ||
                strcmp(child_type, "type_identifier") == 0) {
                
                uint32_t start = ts_node_start_byte(child);
                uint32_t end = ts_node_end_byte(child);
                return content_.substr(start, end - start);
            }
        }
        
        // For named nodes, try field access
        TSNode name_node = ts_node_child_by_field_name(node, "name", 4);
        if (!ts_node_is_null(name_node)) {
            uint32_t start = ts_node_start_byte(name_node);
            uint32_t end = ts_node_end_byte(name_node);
            return content_.substr(start, end - start);
        }
        
        return "";
    }
    
    std::string extractDocstring(TSNode node) {
        // Look for comment immediately before this node
        TSNode prev = ts_node_prev_sibling(node);
        if (ts_node_is_null(prev)) return "";
        
        const char* prev_type = ts_node_type(prev);
        if (strcmp(prev_type, "comment") == 0 ||
            strcmp(prev_type, "string") == 0 ||   // Python docstrings
            strcmp(prev_type, "expression_statement") == 0) {
            
            uint32_t start = ts_node_start_byte(prev);
            uint32_t end = ts_node_end_byte(prev);
            return content_.substr(start, end - start);
        }
        
        return "";
    }
    
    std::string findSymbolAtLine(TSNode node, size_t line) {
        uint32_t start_line = ts_node_start_point(node).row;
        uint32_t end_line = ts_node_end_point(node).row;
        
        if (line < start_line || line > end_line) {
            return "";
        }
        
        // Check if this node is a definition
        const char* type = ts_node_type(node);
        if (isBlockNode(type)) {
            std::string name = extractNodeName(node);
            if (!name.empty()) {
                return name;
            }
        }
        
        // Check children
        uint32_t child_count = ts_node_child_count(node);
        for (uint32_t i = 0; i < child_count; ++i) {
            TSNode child = ts_node_child(node, i);
            std::string result = findSymbolAtLine(child, line);
            if (!result.empty()) return result;
        }
        
        return "";
    }
    
    TSParser* parser_ = nullptr;
    TSTree* tree_ = nullptr;
    std::string content_;
    std::string current_language_;
};
#endif // AIPR_HAS_TREE_SITTER

std::vector<CodeBlock> findCodeBlocks(
    const std::vector<std::string>& lines,
    const std::string& language
) {
    std::vector<CodeBlock> blocks;
    
    // Language-specific patterns
    std::regex func_pattern;
    std::regex class_pattern;
    
    if (language == "python") {
        func_pattern = std::regex(R"(^\s*(async\s+)?def\s+(\w+))");
        class_pattern = std::regex(R"(^\s*class\s+(\w+))");
    } else if (language == "java" || language == "kotlin" || language == "csharp") {
        func_pattern = std::regex(R"(^\s*(public|private|protected|static|\s)*\s*(void|int|String|boolean|[A-Z]\w*)\s+(\w+)\s*\()");
        class_pattern = std::regex(R"(^\s*(public|private|protected|abstract|final|\s)*\s*class\s+(\w+))");
    } else if (language == "javascript" || language == "typescript") {
        func_pattern = std::regex(R"(^\s*(async\s+)?function\s+(\w+)|^\s*(const|let|var)\s+(\w+)\s*=\s*(async\s+)?\(|^\s*(\w+)\s*\(.*\)\s*\{)");
        class_pattern = std::regex(R"(^\s*class\s+(\w+))");
    } else if (language == "go") {
        func_pattern = std::regex(R"(^func\s+(\([^)]*\)\s*)?(\w+))");
        class_pattern = std::regex(R"(^type\s+(\w+)\s+struct)");
    } else if (language == "rust") {
        func_pattern = std::regex(R"(^\s*(pub\s+)?(async\s+)?fn\s+(\w+))");
        class_pattern = std::regex(R"(^\s*(pub\s+)?(struct|enum|impl)\s+(\w+))");
    } else if (language == "cpp" || language == "c") {
        func_pattern = std::regex(R"(^\s*(\w+\s+)*(\w+)\s*\([^)]*\)\s*(const)?\s*\{?)");
        class_pattern = std::regex(R"(^\s*(class|struct)\s+(\w+))");
    }
    
    // Simple brace/indent tracking
    int brace_depth = 0;
    int current_indent = 0;
    size_t block_start = 0;
    std::string block_name;
    std::string block_type;
    bool in_block = false;
    
    for (size_t i = 0; i < lines.size(); ++i) {
        const auto& line = lines[i];
        
        // Calculate indent
        int indent = 0;
        for (char c : line) {
            if (c == ' ') indent++;
            else if (c == '\t') indent += 4;
            else break;
        }
        
        // Check for function/class start
        std::smatch match;
        if (std::regex_search(line, match, func_pattern)) {
            if (in_block && brace_depth == 0) {
                // End previous block
                blocks.push_back({block_start, i - 1, block_type, block_name, current_indent});
            }
            block_start = i;
            block_type = "function";
            block_name = match[match.size() - 1].str();
            in_block = true;
            current_indent = indent;
        } else if (std::regex_search(line, match, class_pattern)) {
            if (in_block && brace_depth == 0) {
                blocks.push_back({block_start, i - 1, block_type, block_name, current_indent});
            }
            block_start = i;
            block_type = "class";
            block_name = match[match.size() - 1].str();
            in_block = true;
            current_indent = indent;
        }
        
        // Track braces
        for (char c : line) {
            if (c == '{') brace_depth++;
            else if (c == '}') brace_depth--;
        }
        
        // End of block detection (for brace-based languages)
        if (in_block && brace_depth == 0 && line.find('}') != std::string::npos) {
            blocks.push_back({block_start, i, block_type, block_name, current_indent});
            in_block = false;
        }
    }
    
    // Close any remaining block
    if (in_block) {
        blocks.push_back({block_start, lines.size() - 1, block_type, block_name, current_indent});
    }
    
    return blocks;
}

/**
 * AST-based chunker
 */
class AstChunker {
public:
    AstChunker(const AstChunkerConfig& config = {}) : config_(config) {}
    
    std::vector<Chunk> chunk(
        const std::string& file_path,
        const std::string& content,
        const std::string& language
    ) {
        std::vector<Chunk> chunks;
        auto lines = splitLines(content);
        
        if (lines.empty()) {
            return chunks;
        }
        
        // Try tree-sitter first if enabled and available
        std::vector<CodeBlock> blocks;
        
#ifdef AIPR_HAS_TREE_SITTER
        if (config_.use_tree_sitter) {
            TreeSitterExtractor extractor;
            if (extractor.setLanguage(language) && extractor.parse(content)) {
                blocks = extractor.extractBlocks();
            }
        }
#endif
        
        // Fall back to regex-based detection if tree-sitter unavailable
        if (blocks.empty()) {
            blocks = findCodeBlocks(lines, language);
        }
        
        if (blocks.empty() || !config_.preserve_functions) {
            // Fall back to simple line-based chunking
            return chunkByLines(file_path, lines, language);
        }
        
        // Chunk by code blocks
        size_t current_line = 0;
        int chunk_index = 0;
        
        for (const auto& block : blocks) {
            // Add any content before this block
            if (block.start_line > current_line) {
                auto pre_chunks = chunkByLines(
                    file_path, 
                    std::vector<std::string>(lines.begin() + current_line, lines.begin() + block.start_line),
                    language,
                    current_line,
                    chunk_index
                );
                chunks.insert(chunks.end(), pre_chunks.begin(), pre_chunks.end());
                chunk_index += pre_chunks.size();
            }
            
            // Process the block
            std::string block_content = joinLines(lines, block.start_line, block.end_line + 1);
            size_t block_tokens = estimateTokenCount(block_content);
            
            if (block_tokens <= config_.max_chunk_size) {
                // Block fits in one chunk
                Chunk chunk;
                chunk.id = file_path + ":" + std::to_string(chunk_index++);
                chunk.file_path = file_path;
                chunk.start_line = block.start_line + 1;  // 1-based
                chunk.end_line = block.end_line + 1;
                chunk.content = block_content;
                chunk.language = language;
                
                // Add symbols from this block
                if (!block.name.empty()) {
                    chunk.symbols.push_back(block.name);
                }
                for (const auto& sym : block.symbols) {
                    if (std::find(chunk.symbols.begin(), chunk.symbols.end(), sym) == chunk.symbols.end()) {
                        chunk.symbols.push_back(sym);
                    }
                }
                
                // Include docstring if available
                if (!block.docstring.empty() && config_.include_context) {
                    chunk.content = block.docstring + "\n" + chunk.content;
                }
                
                chunks.push_back(chunk);
            } else {
                // Block too large, split it
                auto block_lines = std::vector<std::string>(
                    lines.begin() + block.start_line,
                    lines.begin() + block.end_line + 1
                );
                auto sub_chunks = chunkByLines(
                    file_path, block_lines, language, block.start_line, chunk_index
                );
                // Add symbol to first chunk
                if (!sub_chunks.empty() && !block.name.empty()) {
                    sub_chunks[0].symbols.push_back(block.name);
                }
                chunks.insert(chunks.end(), sub_chunks.begin(), sub_chunks.end());
                chunk_index += sub_chunks.size();
            }
            
            current_line = block.end_line + 1;
        }
        
        // Add any remaining content
        if (current_line < lines.size()) {
            auto tail_chunks = chunkByLines(
                file_path,
                std::vector<std::string>(lines.begin() + current_line, lines.end()),
                language,
                current_line,
                chunk_index
            );
            chunks.insert(chunks.end(), tail_chunks.begin(), tail_chunks.end());
        }
        
        return chunks;
    }
    
private:
    AstChunkerConfig config_;
    
    std::vector<Chunk> chunkByLines(
        const std::string& file_path,
        const std::vector<std::string>& lines,
        const std::string& language,
        size_t line_offset = 0,
        int chunk_start_index = 0
    ) {
        std::vector<Chunk> chunks;
        
        size_t current_start = 0;
        std::string current_content;
        size_t current_tokens = 0;
        int chunk_index = chunk_start_index;
        
        for (size_t i = 0; i < lines.size(); ++i) {
            const auto& line = lines[i];
            size_t line_tokens = estimateTokenCount(line);
            
            if (current_tokens + line_tokens > config_.target_chunk_size && 
                current_tokens >= config_.min_chunk_size) {
                // Create chunk
                Chunk chunk;
                chunk.id = file_path + ":" + std::to_string(chunk_index++);
                chunk.file_path = file_path;
                chunk.start_line = line_offset + current_start + 1;
                chunk.end_line = line_offset + i;
                chunk.content = current_content;
                chunk.language = language;
                chunks.push_back(chunk);
                
                // Start new chunk with overlap
                size_t overlap_start = (i > config_.overlap_lines) ? i - config_.overlap_lines : 0;
                current_start = overlap_start;
                current_content = joinLines(lines, overlap_start, i);
                current_tokens = estimateTokenCount(current_content);
            }
            
            if (!current_content.empty()) {
                current_content += '\n';
            }
            current_content += line;
            current_tokens += line_tokens;
        }
        
        // Final chunk
        if (!current_content.empty() && current_tokens >= config_.min_chunk_size) {
            Chunk chunk;
            chunk.id = file_path + ":" + std::to_string(chunk_index);
            chunk.file_path = file_path;
            chunk.start_line = line_offset + current_start + 1;
            chunk.end_line = line_offset + lines.size();
            chunk.content = current_content;
            chunk.language = language;
            chunks.push_back(chunk);
        }
        
        return chunks;
    }
};

} // namespace aipr
