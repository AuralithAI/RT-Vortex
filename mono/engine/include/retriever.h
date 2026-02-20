/**
 * AI PR Reviewer - Retriever Interface
 */

#ifndef AIPR_RETRIEVER_H
#define AIPR_RETRIEVER_H

#include "types.h"
#include <string>
#include <vector>
#include <memory>

namespace aipr {

/**
 * Retrieval configuration
 */
struct RetrieverConfig {
    size_t top_k = 20;
    float lexical_weight = 0.3f;
    float vector_weight = 0.7f;
    size_t graph_expand_depth = 2;
    bool enable_reranking = true;
    
    // Vector search settings
    size_t nprobe = 10;  // FAISS nprobe parameter
    
    // Filters
    std::vector<std::string> file_filters;  // Only search in these files
    std::vector<std::string> language_filters;  // Only these languages
};

/**
 * Search result
 */
struct SearchResult {
    Chunk chunk;
    float score = 0.0f;
    
    // Score components for debugging
    float lexical_score = 0.0f;
    float vector_score = 0.0f;
    float graph_score = 0.0f;
};

/**
 * Hybrid retriever interface
 */
class Retriever {
public:
    virtual ~Retriever() = default;
    
    /**
     * Search for relevant chunks using hybrid retrieval
     */
    virtual std::vector<SearchResult> search(
        const std::string& repo_id,
        const std::string& query,
        const RetrieverConfig& config
    ) = 0;
    
    /**
     * Search for chunks related to specific symbols
     */
    virtual std::vector<SearchResult> searchBySymbols(
        const std::string& repo_id,
        const std::vector<std::string>& symbols,
        const RetrieverConfig& config
    ) = 0;
    
    /**
     * Get chunks by file path
     */
    virtual std::vector<Chunk> getChunksByFile(
        const std::string& repo_id,
        const std::string& file_path
    ) = 0;
    
    /**
     * Get symbol graph neighbors
     */
    virtual std::vector<Symbol> getSymbolNeighbors(
        const std::string& repo_id,
        const std::string& symbol_name,
        size_t depth = 1
    ) = 0;
    
    /**
     * Add chunks to index (called by Indexer)
     */
    virtual void addChunks(
        const std::string& repo_id,
        const std::vector<Chunk>& chunks
    ) = 0;
    
    /**
     * Remove chunks from index
     */
    virtual void removeChunks(
        const std::string& repo_id,
        const std::vector<std::string>& chunk_ids
    ) = 0;
    
    /**
     * Factory method
     */
    static std::unique_ptr<Retriever> create(const std::string& storage_path);
};

}

#endif
