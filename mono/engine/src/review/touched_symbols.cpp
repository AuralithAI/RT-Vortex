/**
 * AI PR Reviewer - Touched Symbol Detection
 * 
 * Identifies symbols affected by a diff and extracts caller/callee relationships.
 */

#include "types.h"
#include <string>
#include <sstream>
#include <vector>
#include <regex>
#include <unordered_map>
#include <unordered_set>

namespace aipr {

/**
 * Represents a call site detected in the code
 */
struct CallSite {
    std::string caller;          // Symbol making the call
    std::string callee;          // Symbol being called
    std::string file_path;
    size_t line;
};

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
        
        // Build symbol name lookup for call detection
        std::unordered_set<std::string> symbol_names;
        for (const auto& sym : known_symbols) {
            symbol_names.insert(sym.name);
            // Also add qualified name parts
            size_t pos = sym.qualified_name.rfind('.');
            if (pos != std::string::npos) {
                symbol_names.insert(sym.qualified_name.substr(pos + 1));
            }
            pos = sym.qualified_name.rfind("::");
            if (pos != std::string::npos) {
                symbol_names.insert(sym.qualified_name.substr(pos + 2));
            }
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
                    
                    // Extract callers and callees from added/modified lines
                    extractCallsFromHunk(hunk, *sym, symbol_names, ts.callers, ts.callees);
                    
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
    
    /**
     * Extract all call sites from a file or chunk of code.
     * Used during indexing to populate the graph.
     */
    std::vector<CallSite> extractCallSites(
        const std::string& code,
        const std::string& file_path,
        const std::string& enclosing_symbol,
        const std::unordered_set<std::string>& known_symbols
    ) {
        std::vector<CallSite> calls;
        
        // Pattern matches: function_name( or method.call( or ns::func(
        std::regex call_pattern(R"((\b[a-zA-Z_]\w*(?:\.[a-zA-Z_]\w*)*(?:::[a-zA-Z_]\w*)*)\s*\()");
        
        size_t line = 1;
        std::istringstream stream(code);
        std::string line_text;
        
        while (std::getline(stream, line_text)) {
            std::sregex_iterator it(line_text.begin(), line_text.end(), call_pattern);
            std::sregex_iterator end;
            
            while (it != end) {
                std::string callee = (*it)[1].str();
                
                // Check if this is a known symbol
                std::string simple_name = callee;
                size_t pos = callee.rfind('.');
                if (pos != std::string::npos) simple_name = callee.substr(pos + 1);
                pos = callee.rfind("::");
                if (pos != std::string::npos) simple_name = callee.substr(pos + 2);
                
                // Skip common keywords and standard library
                if (!isKeyword(simple_name) && known_symbols.count(simple_name)) {
                    CallSite site;
                    site.caller = enclosing_symbol;
                    site.callee = simple_name;
                    site.file_path = file_path;
                    site.line = line;
                    calls.push_back(site);
                }
                
                ++it;
            }
            line++;
        }
        
        return calls;
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
     * Extract calls from a diff hunk within a symbol's scope
     */
    void extractCallsFromHunk(
        const DiffHunk& hunk,
        const Symbol& symbol,
        const std::unordered_set<std::string>& known_symbols,
        std::vector<std::string>& callers,
        std::vector<std::string>& callees
    ) {
        // Pattern matches function/method calls
        std::regex call_pattern(R"((\b[a-zA-Z_]\w*)\s*\()");
        
        // Scan added lines for callees (what this symbol calls)
        for (const auto& line : hunk.added_lines) {
            std::sregex_iterator it(line.begin(), line.end(), call_pattern);
            std::sregex_iterator end;
            
            while (it != end) {
                std::string callee = (*it)[1].str();
                
                // Skip keywords and check if it's a known symbol
                if (!isKeyword(callee) && known_symbols.count(callee)) {
                    if (std::find(callees.begin(), callees.end(), callee) == callees.end()) {
                        callees.push_back(callee);
                    }
                }
                ++it;
            }
        }
        
        // Note: Finding callers of a symbol requires scanning other files
        // This is typically done during the full indexing pass
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
    
    /**
     * Check if a name is a language keyword (not a symbol)
     */
    bool isKeyword(const std::string& name) {
        static const std::unordered_set<std::string> keywords = {
            // Common across languages
            "if", "else", "for", "while", "do", "switch", "case", "break",
            "continue", "return", "try", "catch", "throw", "finally",
            "new", "delete", "this", "self", "super", "true", "false", "null",
            "nullptr", "void", "int", "long", "short", "float", "double",
            "char", "bool", "boolean", "string", "byte", "var", "let", "const",
            "static", "final", "abstract", "virtual", "override", "public",
            "private", "protected", "internal", "class", "struct", "enum",
            "interface", "trait", "impl", "fn", "func", "function", "def",
            "async", "await", "yield", "import", "export", "from", "as",
            "package", "namespace", "using", "include", "require", "module",
            "typeof", "instanceof", "sizeof", "alignof", "decltype",
            // Common standard library
            "print", "println", "printf", "sprintf", "console", "log",
            "assert", "sizeof", "len", "length", "size", "append", "push",
            "pop", "map", "filter", "reduce", "forEach", "some", "every",
            "find", "indexOf", "includes", "slice", "splice", "concat",
            "join", "split", "trim", "toLowerCase", "toUpperCase", "replace",
            "match", "test", "exec", "toString", "valueOf", "parseInt",
            "parseFloat", "isNaN", "isFinite", "Math", "Date", "Array",
            "Object", "String", "Number", "Boolean", "RegExp", "Error",
            "Promise", "Set", "Map", "WeakSet", "WeakMap", "Symbol",
        };
        return keywords.count(name) > 0;
    }
};

} // namespace aipr
