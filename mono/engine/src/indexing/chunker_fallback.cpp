/**
 * AI PR Reviewer - Fallback Text Chunker
 * 
 * Simple text-based chunking when AST parsing is not available.
 */

#include "indexer.h"
#include "types.h"
#include <string>
#include <vector>
#include <sstream>

namespace aipr {

/**
 * Fallback chunker configuration
 */
struct FallbackChunkerConfig {
    size_t chunk_size_chars = 2000;     // Target chunk size in characters
    size_t overlap_chars = 200;          // Overlap between chunks
    size_t min_chunk_size = 100;         // Minimum chunk size
    bool split_on_blank_lines = true;    // Prefer splitting on blank lines
};

/**
 * Fallback text chunker
 */
class FallbackChunker {
public:
    FallbackChunker(const FallbackChunkerConfig& config = {}) : config_(config) {}
    
    std::vector<Chunk> chunk(
        const std::string& file_path,
        const std::string& content,
        const std::string& language
    ) {
        std::vector<Chunk> chunks;
        
        if (content.empty()) {
            return chunks;
        }
        
        // Split by paragraphs/blank lines first if enabled
        if (config_.split_on_blank_lines) {
            return chunkByParagraphs(file_path, content, language);
        }
        
        return chunkBySize(file_path, content, language);
    }
    
private:
    FallbackChunkerConfig config_;
    
    std::vector<Chunk> chunkByParagraphs(
        const std::string& file_path,
        const std::string& content,
        const std::string& language
    ) {
        std::vector<Chunk> chunks;
        
        // Find paragraph boundaries (blank lines)
        std::vector<std::pair<size_t, size_t>> paragraphs;
        size_t start = 0;
        size_t line_num = 1;
        bool in_blank = false;
        
        for (size_t i = 0; i < content.size(); ++i) {
            if (content[i] == '\n') {
                line_num++;
                
                // Check if next line is blank
                bool next_blank = (i + 1 < content.size()) && 
                                 (content[i + 1] == '\n' || content[i + 1] == '\r');
                
                if (next_blank && !in_blank) {
                    // End of paragraph
                    if (i > start) {
                        paragraphs.push_back({start, i});
                    }
                    in_blank = true;
                } else if (!next_blank && in_blank) {
                    // Start of new paragraph
                    start = i + 1;
                    in_blank = false;
                }
            }
        }
        
        // Add final paragraph
        if (start < content.size()) {
            paragraphs.push_back({start, content.size()});
        }
        
        // Merge small paragraphs, split large ones
        std::string current_content;
        size_t current_start_line = 1;
        size_t current_end_line = 1;
        int chunk_index = 0;
        size_t line_counter = 1;
        
        for (const auto& [para_start, para_end] : paragraphs) {
            std::string para = content.substr(para_start, para_end - para_start);
            
            // Count lines in this paragraph
            size_t para_lines = 1;
            for (char c : para) {
                if (c == '\n') para_lines++;
            }
            
            if (current_content.size() + para.size() > config_.chunk_size_chars) {
                // Save current chunk if non-empty
                if (current_content.size() >= config_.min_chunk_size) {
                    Chunk chunk;
                    chunk.id = file_path + ":" + std::to_string(chunk_index++);
                    chunk.file_path = file_path;
                    chunk.start_line = current_start_line;
                    chunk.end_line = current_end_line;
                    chunk.content = current_content;
                    chunk.language = language;
                    chunks.push_back(chunk);
                }
                
                current_content.clear();
                current_start_line = line_counter;
            }
            
            if (!current_content.empty()) {
                current_content += "\n\n";
                current_end_line += 2;
            }
            
            if (current_content.empty()) {
                current_start_line = line_counter;
            }
            
            current_content += para;
            current_end_line = line_counter + para_lines - 1;
            line_counter += para_lines;
        }
        
        // Final chunk
        if (current_content.size() >= config_.min_chunk_size) {
            Chunk chunk;
            chunk.id = file_path + ":" + std::to_string(chunk_index);
            chunk.file_path = file_path;
            chunk.start_line = current_start_line;
            chunk.end_line = current_end_line;
            chunk.content = current_content;
            chunk.language = language;
            chunks.push_back(chunk);
        }
        
        return chunks;
    }
    
    std::vector<Chunk> chunkBySize(
        const std::string& file_path,
        const std::string& content,
        const std::string& language
    ) {
        std::vector<Chunk> chunks;
        
        size_t start = 0;
        int chunk_index = 0;
        
        while (start < content.size()) {
            size_t end = std::min(start + config_.chunk_size_chars, content.size());
            
            // Try to end at a line boundary
            if (end < content.size()) {
                size_t line_end = content.rfind('\n', end);
                if (line_end != std::string::npos && line_end > start) {
                    end = line_end + 1;
                }
            }
            
            std::string chunk_content = content.substr(start, end - start);
            
            // Count lines
            size_t start_line = 1;
            for (size_t i = 0; i < start; ++i) {
                if (content[i] == '\n') start_line++;
            }
            
            size_t end_line = start_line;
            for (char c : chunk_content) {
                if (c == '\n') end_line++;
            }
            
            if (chunk_content.size() >= config_.min_chunk_size) {
                Chunk chunk;
                chunk.id = file_path + ":" + std::to_string(chunk_index++);
                chunk.file_path = file_path;
                chunk.start_line = start_line;
                chunk.end_line = end_line;
                chunk.content = chunk_content;
                chunk.language = language;
                chunks.push_back(chunk);
            }
            
            // Move start with overlap
            start = end - config_.overlap_chars;
            if (start >= end) {
                start = end;
            }
        }
        
        return chunks;
    }
};

} // namespace aipr
