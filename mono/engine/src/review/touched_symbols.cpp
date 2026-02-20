/**
 * AI PR Reviewer - Touched Symbol Detection
 * 
 * Identifies symbols affected by a diff.
 */

#include "types.h"
#include <string>
#include <vector>
#include <regex>
#include <unordered_set>

namespace aipr {

/**
 * Extract symbols touched by diff changes
 */
class TouchedSymbolDetector {
public:
    /**
     * Find symbols affected by diff
     */
    std::vector<TouchedSymbol> detect(
        const ParsedDiff& diff,
        const std::vector<Symbol>& known_symbols
    ) {
        std::vector<TouchedSymbol> touched;
        
        // Build index of symbols by file
        std::unordered_map<std::string, std::vector<const Symbol*>> symbols_by_file;
        for (const auto& sym : known_symbols) {
            symbols_by_file[sym.file_path].push_back(&sym);
        }
        
        // For each changed file, find affected symbols
        for (const auto& hunk : diff.hunks) {
            auto it = symbols_by_file.find(hunk.file_path);
            if (it == symbols_by_file.end()) continue;
            
            // Find symbols overlapping with hunk
            for (const auto* sym : it->second) {
                // Simple line-based overlap check
                // In reality, we'd need the symbol's end line too
                size_t hunk_start = hunk.new_start;
                size_t hunk_end = hunk.new_start + hunk.new_lines;
                
                // Assume symbol spans ~20 lines if we don't know
                size_t sym_end = sym->line + 20;
                
                bool overlaps = !(sym_end < hunk_start || sym->line > hunk_end);
                
                if (overlaps) {
                    TouchedSymbol ts;
                    ts.symbol = *sym;
                    ts.change_type = determineChangeType(hunk);
                    ts.additions = hunk.added_lines.size();
                    ts.deletions = hunk.removed_lines.size();
                    touched.push_back(ts);
                }
            }
        }
        
        // Also detect new symbols from added lines
        auto new_symbols = extractNewSymbols(diff);
        for (auto& sym : new_symbols) {
            TouchedSymbol ts;
            ts.symbol = sym;
            ts.change_type = ChangeType::Added;
            touched.push_back(ts);
        }
        
        return touched;
    }
    
private:
    ChangeType determineChangeType(const DiffHunk& hunk) {
        if (hunk.removed_lines.empty() && !hunk.added_lines.empty()) {
            return ChangeType::Added;
        } else if (!hunk.removed_lines.empty() && hunk.added_lines.empty()) {
            return ChangeType::Deleted;
        }
        return ChangeType::Modified;
    }
    
    /**
     * Extract potential new symbol definitions from added lines
     */
    std::vector<Symbol> extractNewSymbols(const ParsedDiff& diff) {
        std::vector<Symbol> symbols;
        
        // Language-agnostic patterns for common symbol definitions
        std::vector<std::pair<std::regex, std::string>> patterns = {
            {std::regex(R"(^\s*(?:public|private|protected|static|\s)*(?:class|interface|struct|enum)\s+(\w+))"), "class"},
            {std::regex(R"(^\s*(?:public|private|protected|static|async|\s)*(?:def|fn|func|function)\s+(\w+))"), "function"},
            {std::regex(R"(^\s*(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?\()"), "function"},
            {std::regex(R"(^\s*(\w+)\s*=\s*function)"), "function"},
            {std::regex(R"(^\s*type\s+(\w+)\s+(?:struct|interface))"), "type"},
        };
        
        for (const auto& hunk : diff.hunks) {
            size_t line_num = hunk.new_start;
            
            for (const auto& line : hunk.added_lines) {
                for (const auto& [pattern, kind] : patterns) {
                    std::smatch match;
                    if (std::regex_search(line, match, pattern)) {
                        Symbol sym;
                        sym.name = match[1].str();
                        sym.qualified_name = sym.name;
                        sym.kind = kind;
                        sym.file_path = hunk.file_path;
                        sym.line = line_num;
                        symbols.push_back(sym);
                        break;
                    }
                }
                line_num++;
            }
        }
        
        return symbols;
    }
};

} // namespace aipr
