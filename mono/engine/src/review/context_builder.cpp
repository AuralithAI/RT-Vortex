/**
 * AI PR Reviewer - Context Builder
 * 
 * Builds grounded context for LLM review.
 */

#include "engine_api.h"
#include "retriever.h"
#include "types.h"
#include <string>
#include <vector>
#include <unordered_set>
#include <sstream>
#include <algorithm>

namespace aipr {

/**
 * Context builder configuration
 */
struct ContextBuilderConfig {
    size_t max_context_chunks = 30;
    size_t max_context_tokens = 8000;
    bool include_touched_symbols = true;
    bool include_heuristic_warnings = true;
    bool include_file_headers = true;
    size_t file_header_lines = 20;  // Include first N lines of each file for context
};

/**
 * Builds context pack for LLM review
 */
class ContextBuilder {
public:
    ContextBuilder(const ContextBuilderConfig& config = {}) : config_(config) {}
    
    /**
     * Build context pack from diff and retrieved chunks
     */
    ContextPack build(
        const std::string& repo_id,
        const ParsedDiff& diff,
        const std::vector<SearchResult>& retrieved_chunks,
        const std::vector<TouchedSymbol>& touched_symbols,
        const std::vector<HeuristicFinding>& heuristic_findings,
        const std::string& pr_title = "",
        const std::string& pr_description = ""
    ) {
        ContextPack pack;
        pack.repo_id = repo_id;
        pack.pr_title = pr_title;
        pack.pr_description = pr_description;
        
        // Build diff string
        pack.diff = buildDiffString(diff);
        
        // Add touched symbols
        if (config_.include_touched_symbols) {
            pack.touched_symbols = touched_symbols;
        }
        
        // Add heuristic warnings
        if (config_.include_heuristic_warnings) {
            for (const auto& finding : heuristic_findings) {
                pack.heuristic_warnings.push_back(formatHeuristicWarning(finding));
            }
        }
        
        // Select and format context chunks
        pack.context_chunks = selectContextChunks(retrieved_chunks, diff);
        
        return pack;
    }
    
    /**
     * Format context pack as text for LLM
     */
    std::string formatForLLM(const ContextPack& pack) {
        std::ostringstream output;
        
        // Header
        output << "# Pull Request Review Context\n\n";
        
        if (!pack.pr_title.empty()) {
            output << "## PR Title\n" << pack.pr_title << "\n\n";
        }
        
        if (!pack.pr_description.empty()) {
            output << "## PR Description\n" << pack.pr_description << "\n\n";
        }
        
        // Touched symbols
        if (!pack.touched_symbols.empty()) {
            output << "## Modified Symbols\n";
            for (const auto& ts : pack.touched_symbols) {
                output << "- `" << ts.symbol.qualified_name << "` ("
                       << ts.symbol.kind << " in " << ts.symbol.file_path
                       << ":" << ts.symbol.line << ")\n";
            }
            output << "\n";
        }
        
        // Heuristic warnings
        if (!pack.heuristic_warnings.empty()) {
            output << "## Automated Warnings\n";
            for (const auto& warning : pack.heuristic_warnings) {
                output << "- " << warning << "\n";
            }
            output << "\n";
        }
        
        // Context chunks
        if (!pack.context_chunks.empty()) {
            output << "## Relevant Code Context\n\n";
            for (const auto& chunk : pack.context_chunks) {
                output << "### " << chunk.file_path 
                       << " (lines " << chunk.start_line << "-" << chunk.end_line << ")\n";
                output << "```" << chunk.language << "\n";
                output << chunk.content << "\n";
                output << "```\n\n";
            }
        }
        
        // Diff
        output << "## Changes to Review\n";
        output << "```diff\n" << pack.diff << "\n```\n";
        
        return output.str();
    }
    
private:
    ContextBuilderConfig config_;
    
    std::string buildDiffString(const ParsedDiff& diff) {
        std::ostringstream output;
        
        for (const auto& hunk : diff.hunks) {
            output << hunk.content;
        }
        
        return output.str();
    }
    
    std::vector<ContextChunk> selectContextChunks(
        const std::vector<SearchResult>& retrieved,
        const ParsedDiff& diff
    ) {
        std::vector<ContextChunk> selected;
        
        // Get files in diff for prioritization
        std::unordered_set<std::string> diff_files;
        for (const auto& file : diff.changed_files) {
            diff_files.insert(file.path);
        }
        
        // Prioritize chunks from changed files, then by score
        std::vector<SearchResult> prioritized = retrieved;
        std::sort(prioritized.begin(), prioritized.end(),
            [&](const SearchResult& a, const SearchResult& b) {
                bool a_in_diff = diff_files.count(a.chunk.file_path) > 0;
                bool b_in_diff = diff_files.count(b.chunk.file_path) > 0;
                
                if (a_in_diff != b_in_diff) {
                    return a_in_diff > b_in_diff;  // Prefer chunks from changed files
                }
                return a.score > b.score;
            });
        
        // Select up to max chunks, avoiding too much overlap
        std::unordered_set<std::string> seen_files;
        size_t tokens_used = 0;
        
        for (const auto& result : prioritized) {
            if (selected.size() >= config_.max_context_chunks) break;
            
            // Estimate tokens
            size_t chunk_tokens = result.chunk.content.size() / 4;
            if (tokens_used + chunk_tokens > config_.max_context_tokens) continue;
            
            // Limit chunks per file
            if (seen_files.count(result.chunk.file_path) >= 3) continue;
            seen_files.insert(result.chunk.file_path);
            
            ContextChunk ctx;
            ctx.id = result.chunk.id;
            ctx.file_path = result.chunk.file_path;
            ctx.start_line = result.chunk.start_line;
            ctx.end_line = result.chunk.end_line;
            ctx.content = result.chunk.content;
            ctx.language = result.chunk.language;
            ctx.symbols = result.chunk.symbols;
            ctx.relevance_score = result.score;
            
            selected.push_back(ctx);
            tokens_used += chunk_tokens;
        }
        
        return selected;
    }
    
    std::string formatHeuristicWarning(const HeuristicFinding& finding) {
        std::ostringstream output;
        output << "[" << severityToString(finding.severity) << "] ";
        if (!finding.file_path.empty()) {
            output << finding.file_path;
            if (finding.line > 0) {
                output << ":" << finding.line;
            }
            output << " - ";
        }
        output << finding.message;
        return output.str();
    }
};

} // namespace aipr
