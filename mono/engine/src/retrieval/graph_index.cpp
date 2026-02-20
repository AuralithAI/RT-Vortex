/**
 * AI PR Reviewer - Graph Index
 * 
 * Symbol graph for understanding code relationships.
 */

#include "retriever.h"
#include "types.h"
#include <string>
#include <vector>
#include <unordered_map>
#include <unordered_set>
#include <queue>

namespace aipr {

/**
 * Edge type in the symbol graph
 */
enum class EdgeType {
    Calls,          // A calls B
    CalledBy,       // A is called by B
    Imports,        // A imports B
    ImportedBy,     // A is imported by B
    Inherits,       // A inherits from B
    InheritedBy,    // A is inherited by B
    Contains,       // A contains B (e.g., class contains method)
    ContainedBy,    // A is contained by B
    References,     // A references B
    ReferencedBy    // A is referenced by B
};

/**
 * Edge in the symbol graph
 */
struct SymbolEdge {
    std::string target;
    EdgeType type;
    std::string file_path;
    size_t line = 0;
};

/**
 * Symbol graph for code navigation
 */
class GraphIndex {
public:
    /**
     * Add a symbol to the graph
     */
    void addSymbol(const std::string& repo_id, const Symbol& symbol) {
        auto& graph = graphs_[repo_id];
        graph.symbols[symbol.qualified_name] = symbol;
    }
    
    /**
     * Add an edge between symbols
     */
    void addEdge(
        const std::string& repo_id,
        const std::string& from_symbol,
        const std::string& to_symbol,
        EdgeType type,
        const std::string& file_path = "",
        size_t line = 0
    ) {
        auto& graph = graphs_[repo_id];
        
        SymbolEdge edge;
        edge.target = to_symbol;
        edge.type = type;
        edge.file_path = file_path;
        edge.line = line;
        
        graph.edges[from_symbol].push_back(edge);
        
        // Add reverse edge
        SymbolEdge reverse_edge;
        reverse_edge.target = from_symbol;
        reverse_edge.type = reverseEdgeType(type);
        reverse_edge.file_path = file_path;
        reverse_edge.line = line;
        
        graph.edges[to_symbol].push_back(reverse_edge);
    }
    
    /**
     * Get symbol by name
     */
    const Symbol* getSymbol(
        const std::string& repo_id,
        const std::string& name
    ) const {
        auto graph_it = graphs_.find(repo_id);
        if (graph_it == graphs_.end()) return nullptr;
        
        auto sym_it = graph_it->second.symbols.find(name);
        if (sym_it == graph_it->second.symbols.end()) return nullptr;
        
        return &sym_it->second;
    }
    
    /**
     * Get neighbors of a symbol within N hops
     */
    std::vector<Symbol> getNeighbors(
        const std::string& repo_id,
        const std::string& symbol_name,
        size_t max_depth = 1,
        const std::vector<EdgeType>& edge_types = {}
    ) const {
        std::vector<Symbol> result;
        
        auto graph_it = graphs_.find(repo_id);
        if (graph_it == graphs_.end()) return result;
        
        const auto& graph = graph_it->second;
        
        // BFS to find neighbors
        std::unordered_set<std::string> visited;
        std::queue<std::pair<std::string, size_t>> queue;
        
        queue.push({symbol_name, 0});
        visited.insert(symbol_name);
        
        while (!queue.empty()) {
            auto [current, depth] = queue.front();
            queue.pop();
            
            if (depth >= max_depth) continue;
            
            auto edges_it = graph.edges.find(current);
            if (edges_it == graph.edges.end()) continue;
            
            for (const auto& edge : edges_it->second) {
                // Filter by edge type if specified
                if (!edge_types.empty() && 
                    std::find(edge_types.begin(), edge_types.end(), edge.type) == edge_types.end()) {
                    continue;
                }
                
                if (visited.find(edge.target) == visited.end()) {
                    visited.insert(edge.target);
                    queue.push({edge.target, depth + 1});
                    
                    auto sym_it = graph.symbols.find(edge.target);
                    if (sym_it != graph.symbols.end()) {
                        result.push_back(sym_it->second);
                    }
                }
            }
        }
        
        return result;
    }
    
    /**
     * Get callers of a symbol
     */
    std::vector<Symbol> getCallers(
        const std::string& repo_id,
        const std::string& symbol_name
    ) const {
        return getNeighbors(repo_id, symbol_name, 1, {EdgeType::CalledBy});
    }
    
    /**
     * Get callees of a symbol
     */
    std::vector<Symbol> getCallees(
        const std::string& repo_id,
        const std::string& symbol_name
    ) const {
        return getNeighbors(repo_id, symbol_name, 1, {EdgeType::Calls});
    }
    
    /**
     * Find symbols in a file
     */
    std::vector<Symbol> getSymbolsInFile(
        const std::string& repo_id,
        const std::string& file_path
    ) const {
        std::vector<Symbol> result;
        
        auto graph_it = graphs_.find(repo_id);
        if (graph_it == graphs_.end()) return result;
        
        for (const auto& [name, symbol] : graph_it->second.symbols) {
            if (symbol.file_path == file_path) {
                result.push_back(symbol);
            }
        }
        
        // Sort by line number
        std::sort(result.begin(), result.end(),
            [](const Symbol& a, const Symbol& b) { return a.line < b.line; });
        
        return result;
    }
    
    /**
     * Clear graph for a repository
     */
    void clear(const std::string& repo_id) {
        graphs_.erase(repo_id);
    }
    
    /**
     * Remove symbols from a file
     */
    void removeFile(const std::string& repo_id, const std::string& file_path) {
        auto graph_it = graphs_.find(repo_id);
        if (graph_it == graphs_.end()) return;
        
        auto& graph = graph_it->second;
        
        // Find and remove symbols from this file
        std::vector<std::string> to_remove;
        for (const auto& [name, symbol] : graph.symbols) {
            if (symbol.file_path == file_path) {
                to_remove.push_back(name);
            }
        }
        
        for (const auto& name : to_remove) {
            graph.symbols.erase(name);
            graph.edges.erase(name);
            
            // Remove references to this symbol from other edges
            for (auto& [_, edges] : graph.edges) {
                edges.erase(
                    std::remove_if(
                        edges.begin(), edges.end(),
                        [&](const SymbolEdge& e) { return e.target == name; }
                    ),
                    edges.end()
                );
            }
        }
    }
    
private:
    struct SymbolGraph {
        std::unordered_map<std::string, Symbol> symbols;
        std::unordered_map<std::string, std::vector<SymbolEdge>> edges;
    };
    
    std::unordered_map<std::string, SymbolGraph> graphs_;
    
    EdgeType reverseEdgeType(EdgeType type) const {
        switch (type) {
            case EdgeType::Calls: return EdgeType::CalledBy;
            case EdgeType::CalledBy: return EdgeType::Calls;
            case EdgeType::Imports: return EdgeType::ImportedBy;
            case EdgeType::ImportedBy: return EdgeType::Imports;
            case EdgeType::Inherits: return EdgeType::InheritedBy;
            case EdgeType::InheritedBy: return EdgeType::Inherits;
            case EdgeType::Contains: return EdgeType::ContainedBy;
            case EdgeType::ContainedBy: return EdgeType::Contains;
            case EdgeType::References: return EdgeType::ReferencedBy;
            case EdgeType::ReferencedBy: return EdgeType::References;
            default: return type;
        }
    }
};

} // namespace aipr
