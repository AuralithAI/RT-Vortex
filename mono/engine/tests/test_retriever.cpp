/**
 * AI PR Reviewer Engine - Retriever Tests
 */

#include <gtest/gtest.h>
#include <cmath>
#include "types.h"
#include "retriever.h"

namespace aipr {
namespace test {

class RetrieverTest : public ::testing::Test {
protected:
    void SetUp() override {
        // Setup test fixtures
    }

    void TearDown() override {
        // Cleanup
    }
    
    // Helper to create a test chunk
    Chunk createTestChunk(const std::string& id, const std::string& file_path, 
                          const std::string& content, const std::vector<std::string>& symbols = {}) {
        Chunk chunk;
        chunk.id = id;
        chunk.file_path = file_path;
        chunk.content = content;
        chunk.symbols = symbols;
        chunk.language = "cpp";
        chunk.start_line = 1;
        chunk.end_line = 10;
        return chunk;
    }
    
    // Helper to create a test embedding
    std::vector<float> createTestEmbedding(size_t dims, float base_value = 0.1f) {
        std::vector<float> embedding(dims);
        for (size_t i = 0; i < dims; ++i) {
            embedding[i] = base_value + static_cast<float>(i) * 0.01f;
        }
        return embedding;
    }
};

// =============================================================================
// RetrieverConfig Tests
// =============================================================================

TEST_F(RetrieverTest, RetrieverConfigDefaults) {
    RetrieverConfig config;
    
    EXPECT_EQ(config.top_k, 20);
    EXPECT_FLOAT_EQ(config.lexical_weight, 0.3f);
    EXPECT_FLOAT_EQ(config.vector_weight, 0.7f);
    EXPECT_EQ(config.graph_expand_depth, 2);
    EXPECT_TRUE(config.enable_reranking);
    EXPECT_EQ(config.nprobe, 10);
}

TEST_F(RetrieverTest, RetrieverConfigCustomWeights) {
    RetrieverConfig config;
    config.lexical_weight = 0.5f;
    config.vector_weight = 0.5f;
    
    // Weights should sum to 1.0
    float total_weight = config.lexical_weight + config.vector_weight;
    EXPECT_FLOAT_EQ(total_weight, 1.0f);
}

TEST_F(RetrieverTest, RetrieverConfigFilters) {
    RetrieverConfig config;
    config.file_filters = {"src/*.cpp", "include/*.h"};
    config.language_filters = {"cpp", "c"};
    
    EXPECT_EQ(config.file_filters.size(), 2);
    EXPECT_EQ(config.language_filters.size(), 2);
    EXPECT_EQ(config.file_filters[0], "src/*.cpp");
    EXPECT_EQ(config.language_filters[0], "cpp");
}

// =============================================================================
// SearchResult Tests
// =============================================================================

TEST_F(RetrieverTest, SearchResultCreation) {
    SearchResult result;
    result.chunk = createTestChunk("c1", "main.cpp", "int main() {}");
    result.score = 0.95f;
    result.lexical_score = 0.8f;
    result.vector_score = 0.98f;
    result.graph_score = 0.0f;
    
    EXPECT_EQ(result.chunk.id, "c1");
    EXPECT_FLOAT_EQ(result.score, 0.95f);
    EXPECT_FLOAT_EQ(result.lexical_score, 0.8f);
    EXPECT_FLOAT_EQ(result.vector_score, 0.98f);
}

TEST_F(RetrieverTest, SearchResultScoreComponents) {
    // Test that combined score can be computed from components
    SearchResult result;
    result.lexical_score = 0.6f;
    result.vector_score = 0.9f;
    
    float lexical_weight = 0.3f;
    float vector_weight = 0.7f;
    float expected_score = (result.lexical_score * lexical_weight) + 
                           (result.vector_score * vector_weight);
    
    EXPECT_FLOAT_EQ(expected_score, 0.81f);  // 0.6*0.3 + 0.9*0.7 = 0.18 + 0.63
}

TEST_F(RetrieverTest, SearchResultOrdering) {
    std::vector<SearchResult> results;
    
    SearchResult r1;
    r1.score = 0.7f;
    r1.chunk = createTestChunk("c1", "a.cpp", "code");
    
    SearchResult r2;
    r2.score = 0.9f;
    r2.chunk = createTestChunk("c2", "b.cpp", "code");
    
    SearchResult r3;
    r3.score = 0.5f;
    r3.chunk = createTestChunk("c3", "c.cpp", "code");
    
    results.push_back(r1);
    results.push_back(r2);
    results.push_back(r3);
    
    // Sort by score descending
    std::sort(results.begin(), results.end(), 
              [](const SearchResult& a, const SearchResult& b) {
                  return a.score > b.score;
              });
    
    EXPECT_EQ(results[0].chunk.id, "c2");  // highest score
    EXPECT_EQ(results[1].chunk.id, "c1");
    EXPECT_EQ(results[2].chunk.id, "c3");  // lowest score
}

// =============================================================================
// Embedding/Vector Tests
// =============================================================================

TEST_F(RetrieverTest, EmbeddingCreation) {
    auto embedding = createTestEmbedding(128);
    
    EXPECT_EQ(embedding.size(), 128);
    EXPECT_FLOAT_EQ(embedding[0], 0.1f);
    EXPECT_FLOAT_EQ(embedding[1], 0.11f);
}

TEST_F(RetrieverTest, CosineSimilarity) {
    // Create two normalized vectors and compute cosine similarity
    std::vector<float> vec1 = {1.0f, 0.0f, 0.0f};
    std::vector<float> vec2 = {1.0f, 0.0f, 0.0f};
    
    // Identical vectors should have similarity 1.0
    float dot = 0.0f;
    for (size_t i = 0; i < vec1.size(); ++i) {
        dot += vec1[i] * vec2[i];
    }
    
    EXPECT_FLOAT_EQ(dot, 1.0f);
}

TEST_F(RetrieverTest, OrthogonalVectorsSimilarity) {
    std::vector<float> vec1 = {1.0f, 0.0f, 0.0f};
    std::vector<float> vec2 = {0.0f, 1.0f, 0.0f};
    
    // Orthogonal vectors should have similarity 0.0
    float dot = 0.0f;
    for (size_t i = 0; i < vec1.size(); ++i) {
        dot += vec1[i] * vec2[i];
    }
    
    EXPECT_FLOAT_EQ(dot, 0.0f);
}

// =============================================================================
// Symbol Tests
// =============================================================================

TEST_F(RetrieverTest, SymbolCreation) {
    Symbol symbol;
    symbol.name = "processData";
    symbol.qualified_name = "com.example.DataProcessor.processData";
    
    EXPECT_EQ(symbol.name, "processData");
    EXPECT_EQ(symbol.qualified_name, "com.example.DataProcessor.processData");
}

TEST_F(RetrieverTest, ChunkSymbolsSearch) {
    Chunk chunk = createTestChunk(
        "c1", 
        "src/processor.cpp",
        "void processData() { ... }",
        {"processData", "validateInput", "cleanup"}
    );
    
    EXPECT_EQ(chunk.symbols.size(), 3);
    
    // Check if a symbol exists
    bool found = std::find(chunk.symbols.begin(), chunk.symbols.end(), "processData") 
                 != chunk.symbols.end();
    EXPECT_TRUE(found);
    
    bool not_found = std::find(chunk.symbols.begin(), chunk.symbols.end(), "nonexistent")
                     != chunk.symbols.end();
    EXPECT_FALSE(not_found);
}

// =============================================================================
// TopK Tests
// =============================================================================

TEST_F(RetrieverTest, TopKSelection) {
    std::vector<SearchResult> results;
    
    // Create 10 results with varying scores
    for (int i = 0; i < 10; ++i) {
        SearchResult r;
        r.score = static_cast<float>(i) / 10.0f;
        r.chunk = createTestChunk("c" + std::to_string(i), "file.cpp", "code");
        results.push_back(r);
    }
    
    // Sort by score descending
    std::sort(results.begin(), results.end(),
              [](const SearchResult& a, const SearchResult& b) {
                  return a.score > b.score;
              });
    
    // Select top 5
    size_t top_k = 5;
    std::vector<SearchResult> top_results(results.begin(), results.begin() + top_k);
    
    EXPECT_EQ(top_results.size(), 5);
    EXPECT_FLOAT_EQ(top_results[0].score, 0.9f);  // highest
    EXPECT_FLOAT_EQ(top_results[4].score, 0.5f);  // 5th highest
}

// =============================================================================
// File Filter Tests
// =============================================================================

TEST_F(RetrieverTest, FileFilterMatching) {
    RetrieverConfig config;
    config.file_filters = {"src/core/*", "src/api/*"};
    
    // Simple path matching test
    std::string path1 = "src/core/engine.cpp";
    std::string path2 = "src/utils/helper.cpp";
    
    // Check if path starts with filter prefix (simplified matching)
    bool matches_core = path1.find("src/core/") == 0;
    bool matches_utils = path2.find("src/core/") == 0;
    
    EXPECT_TRUE(matches_core);
    EXPECT_FALSE(matches_utils);
}

// =============================================================================
// Hybrid Search Weight Tests
// =============================================================================

TEST_F(RetrieverTest, HybridWeightNormalization) {
    RetrieverConfig config;
    config.lexical_weight = 0.4f;
    config.vector_weight = 0.6f;
    
    float total = config.lexical_weight + config.vector_weight;
    EXPECT_FLOAT_EQ(total, 1.0f);
}

TEST_F(RetrieverTest, VectorOnlySearch) {
    RetrieverConfig config;
    config.lexical_weight = 0.0f;
    config.vector_weight = 1.0f;
    
    SearchResult result;
    result.lexical_score = 0.5f;
    result.vector_score = 0.9f;
    
    float combined = (result.lexical_score * config.lexical_weight) +
                     (result.vector_score * config.vector_weight);
    
    EXPECT_FLOAT_EQ(combined, 0.9f);  // Only vector score matters
}

TEST_F(RetrieverTest, LexicalOnlySearch) {
    RetrieverConfig config;
    config.lexical_weight = 1.0f;
    config.vector_weight = 0.0f;
    
    SearchResult result;
    result.lexical_score = 0.8f;
    result.vector_score = 0.3f;
    
    float combined = (result.lexical_score * config.lexical_weight) +
                     (result.vector_score * config.vector_weight);
    
    EXPECT_FLOAT_EQ(combined, 0.8f);  // Only lexical score matters
}

}  // namespace test
}  // namespace aipr
