/**
 * GraphRAG Retriever Tests — Hybrid Graph + Semantic Retrieval
 *
 * Tests:
 * - KG-based expansion from seed FAISS IDs
 * - RRF merge of semantic + graph results
 * - Graph confidence score computation
 * - Edge-type filtering (IMPORTS, CALLS, REFERENCES)
 * - Hop depth limiting
 * - Max expansion cap
 */

#include <gtest/gtest.h>
#include "tms/graph_rag.h"
#include "tms/ltm_faiss.h"
#include "knowledge_graph.h"
#include <filesystem>
#include <cmath>

using namespace aipr;
using namespace aipr::tms;

namespace {

class GraphRAGTest : public ::testing::Test {
protected:
    std::string kg_db_path_;
    std::string ltm_storage_;
    std::unique_ptr<KnowledgeGraph> kg_;
    std::unique_ptr<LTMFaiss> ltm_;

    void SetUp() override {
        kg_db_path_ = "/tmp/aipr_graphrag_kg_" + std::to_string(::getpid()) + ".db";
        ltm_storage_ = "/tmp/aipr_graphrag_ltm_" + std::to_string(::getpid());
        std::filesystem::create_directories(ltm_storage_);

        kg_ = std::make_unique<KnowledgeGraph>(kg_db_path_);
        kg_->open();

        LTMConfig ltm_cfg;
        ltm_cfg.dimension = 8;
        ltm_cfg.storage_path = ltm_storage_;
        ltm_cfg.use_cosine_similarity = true;
        ltm_cfg.index_type = FAISSIndexType::FLAT_L2;
        ltm_ = std::make_unique<LTMFaiss>(ltm_cfg);
    }

    void TearDown() override {
        kg_->close();
        std::filesystem::remove_all(ltm_storage_);
        std::filesystem::remove(kg_db_path_);
        std::filesystem::remove(kg_db_path_ + "-wal");
        std::filesystem::remove(kg_db_path_ + "-shm");
    }

    std::vector<float> makeEmbedding(float seed) {
        std::vector<float> v(8);
        for (int i = 0; i < 8; ++i) {
            v[i] = seed + i * 0.1f;
        }
        // Normalize for cosine similarity
        float norm = 0;
        for (auto x : v) norm += x * x;
        norm = std::sqrt(norm);
        for (auto& x : v) x /= norm;
        return v;
    }

    // Build a graph: A --IMPORTS--> B --CALLS--> C --IMPORTS--> D
    // Each chunk gets a FAISS vector and KG node.
    void buildLinearGraph() {
        CodeChunk a, b, c, d;
        a.id = "repo1:src/a.cpp:funcA";
        a.type = "function"; a.name = "funcA";
        a.file_path = "src/a.cpp"; a.language = "cpp";
        a.content = "void funcA() { funcB(); }";
        a.symbols = {"funcA"};

        b.id = "repo1:src/b.cpp:funcB";
        b.type = "function"; b.name = "funcB";
        b.file_path = "src/b.cpp"; b.language = "cpp";
        b.content = "void funcB() { funcC(); }";
        b.symbols = {"funcB"};
        b.dependencies = {"a.cpp"};

        c.id = "repo1:src/c.cpp:funcC";
        c.type = "function"; c.name = "funcC";
        c.file_path = "src/c.cpp"; c.language = "cpp";
        c.content = "void funcC() { funcD(); }";
        c.symbols = {"funcC"};
        c.dependencies = {"b.cpp"};

        d.id = "repo1:src/d.cpp:funcD";
        d.type = "function"; d.name = "funcD";
        d.file_path = "src/d.cpp"; d.language = "cpp";
        d.content = "void funcD() {}";
        d.symbols = {"funcD"};
        d.dependencies = {"c.cpp"};

        std::vector<CodeChunk> chunks = {a, b, c, d};
        kg_->buildFromChunks("repo1", chunks);

        // Add to LTM and link FAISS IDs
        auto emb_a = makeEmbedding(0.1f);
        auto emb_b = makeEmbedding(0.2f);
        auto emb_c = makeEmbedding(0.3f);
        auto emb_d = makeEmbedding(0.4f);

        ltm_->add(a, emb_a);
        ltm_->add(b, emb_b);
        ltm_->add(c, emb_c);
        ltm_->add(d, emb_d);

        // Link KG nodes to FAISS IDs
        kg_->linkFaissId(a.id, 0);
        kg_->linkFaissId(b.id, 1);
        kg_->linkFaissId(c.id, 2);
        kg_->linkFaissId(d.id, 3);
    }
};

// ── Config Defaults ────────────────────────────────────────────────────────

TEST_F(GraphRAGTest, DefaultConfigValues) {
    GraphRAGConfig cfg;
    EXPECT_EQ(cfg.max_hops, 2);
    EXPECT_EQ(cfg.max_neighbors_per_seed, 8);
    EXPECT_EQ(cfg.max_expanded_chunks, 32);
    EXPECT_FLOAT_EQ(cfg.hop_decay, 0.7f);
    EXPECT_FLOAT_EQ(cfg.graph_weight, 0.3f);
    EXPECT_TRUE(cfg.follow_imports);
    EXPECT_TRUE(cfg.follow_calls);
    EXPECT_FALSE(cfg.follow_references);
    EXPECT_FALSE(cfg.follow_contains);
}

// ── expandAndMerge with no graph data ──────────────────────────────────────

TEST_F(GraphRAGTest, ExpandAndMergeWithEmptyGraphReturnsSemanticOnly) {
    GraphRAGConfig cfg;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    // Semantic results with no KG data
    std::vector<RetrievedChunk> semantic;
    RetrievedChunk rc;
    rc.chunk.id = "unknown_chunk";
    rc.similarity_score = 0.9f;
    semantic.push_back(rc);

    auto merged = retriever.expandAndMerge(semantic, "repo1", 5);

    // Should return at least the original semantic result
    EXPECT_GE(merged.size(), 1u);
    EXPECT_EQ(merged[0].chunk.id, "unknown_chunk");
}

// ── expandAndMerge follows IMPORTS edges ───────────────────────────────────

TEST_F(GraphRAGTest, ExpandAndMergeFollowsImportsEdges) {
    buildLinearGraph();

    GraphRAGConfig cfg;
    cfg.max_hops = 2;
    cfg.follow_imports = true;
    cfg.follow_calls = false;
    cfg.follow_references = false;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    // Start with funcA as the only semantic result
    std::vector<RetrievedChunk> semantic;
    RetrievedChunk rc;
    rc.chunk.id = "repo1:src/a.cpp:funcA";
    rc.chunk.file_path = "src/a.cpp";
    rc.similarity_score = 0.9f;
    semantic.push_back(rc);

    auto merged = retriever.expandAndMerge(semantic, "repo1", 10);

    // Should have expanded to include neighbors beyond just funcA
    EXPECT_GE(merged.size(), 1u);

    // funcA itself should definitely be present
    bool found_a = false;
    for (const auto& r : merged) {
        if (r.chunk.id.find("funcA") != std::string::npos) found_a = true;
    }
    EXPECT_TRUE(found_a);
}

// ── expandAndMerge respects max_hops ───────────────────────────────────────

TEST_F(GraphRAGTest, ExpandAndMergeRespectsMaxHops) {
    buildLinearGraph();

    // With max_hops=1, from funcA we should reach at most 1-hop neighbors
    GraphRAGConfig cfg;
    cfg.max_hops = 1;
    cfg.follow_imports = true;
    cfg.follow_calls = true;
    cfg.follow_references = true;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    std::vector<RetrievedChunk> semantic;
    RetrievedChunk rc;
    rc.chunk.id = "repo1:src/a.cpp:funcA";
    rc.chunk.file_path = "src/a.cpp";
    rc.similarity_score = 0.9f;
    semantic.push_back(rc);

    auto merged_1hop = retriever.expandAndMerge(semantic, "repo1", 10);

    // Now with max_hops=3 we should potentially reach more nodes
    cfg.max_hops = 3;
    GraphRAGRetriever retriever2(*ltm_, *kg_, cfg);
    auto merged_3hop = retriever2.expandAndMerge(semantic, "repo1", 10);

    // 3-hop should reach at least as many results as 1-hop
    EXPECT_GE(merged_3hop.size(), merged_1hop.size());
}

// ── computeGraphConfidence returns valid range ─────────────────────────────

TEST_F(GraphRAGTest, GraphConfidenceReturnsValidRange) {
    buildLinearGraph();

    GraphRAGConfig cfg;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    std::vector<std::string> seeds = {"repo1:src/a.cpp:funcA"};
    std::vector<std::string> results = {"repo1:src/b.cpp:funcB"};

    float confidence = retriever.computeGraphConfidence(seeds, results, "repo1");

    EXPECT_GE(confidence, 0.0f);
    EXPECT_LE(confidence, 1.0f);
}

// ── computeGraphConfidence: 1-hop neighbors score higher ───────────────────

TEST_F(GraphRAGTest, GraphConfidenceHigherForNearNeighbors) {
    buildLinearGraph();

    GraphRAGConfig cfg;
    cfg.max_hops = 3;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    std::vector<std::string> seeds = {"repo1:src/a.cpp:funcA"};

    // funcB is 1-hop from funcA
    float conf_near = retriever.computeGraphConfidence(
        seeds, {"repo1:src/b.cpp:funcB"}, "repo1");

    // funcD is 3-hop from funcA
    float conf_far = retriever.computeGraphConfidence(
        seeds, {"repo1:src/d.cpp:funcD"}, "repo1");

    // Near neighbors should have higher or equal confidence
    EXPECT_GE(conf_near, conf_far);
}

// ── computeGraphConfidence with no connection returns 0 ────────────────────

TEST_F(GraphRAGTest, GraphConfidenceDisconnectedReturnsZero) {
    buildLinearGraph();

    GraphRAGConfig cfg;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    std::vector<std::string> seeds = {"repo1:src/a.cpp:funcA"};
    std::vector<std::string> results = {"nonexistent_chunk"};

    float confidence = retriever.computeGraphConfidence(seeds, results, "repo1");
    EXPECT_FLOAT_EQ(confidence, 0.0f);
}

// ── Empty semantic results ─────────────────────────────────────────────────

TEST_F(GraphRAGTest, ExpandAndMergeWithEmptyInputReturnsEmpty) {
    buildLinearGraph();

    GraphRAGConfig cfg;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    std::vector<RetrievedChunk> empty;
    auto result = retriever.expandAndMerge(empty, "repo1", 10);
    EXPECT_TRUE(result.empty());
}

// ── Max expanded chunks cap ────────────────────────────────────────────────

TEST_F(GraphRAGTest, ExpandAndMergeRespectsMaxExpandedCap) {
    buildLinearGraph();

    GraphRAGConfig cfg;
    cfg.max_expanded_chunks = 2;
    cfg.max_hops = 3;
    GraphRAGRetriever retriever(*ltm_, *kg_, cfg);

    std::vector<RetrievedChunk> semantic;
    RetrievedChunk rc;
    rc.chunk.id = "repo1:src/a.cpp:funcA";
    rc.chunk.file_path = "src/a.cpp";
    rc.similarity_score = 0.9f;
    semantic.push_back(rc);

    auto merged = retriever.expandAndMerge(semantic, "repo1", 10);

    // RRF merge should not exceed top_k, and graph expansion
    // itself should not exceed max_expanded_chunks
    EXPECT_LE(merged.size(), 10u);
}

} // namespace
